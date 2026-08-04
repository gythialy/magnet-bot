package handler

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gythialy/magnet/pkg/utils"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"github.com/gythialy/magnet/pkg/rule"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/panjf2000/ants/v2"
)

const (
	poolSize = 10
)

var (
	htmlTagsRegex = regexp.MustCompile(`<[^>]*>|</[^>]*>|<[^/][^>]*/>|<\s*[a-zA-Z][^>]*>`)
	htmlAttrRegex = regexp.MustCompile(`\s+\w+\s*=\s*("[^"]*"|'[^']*')`)
)

// ProcessData holds the data for processing
type ProcessData struct {
	UserId       int64
	ProjectRules []*rule.ComplexRule
	AlarmKeyword []string
	Projects     []*Project
	Alarms       []*model.Alarm
	IsForced     bool
}

type InfoProcessor struct {
	ctx          *BotContext
	pool         *ants.PoolWithFunc
	crawler      *Crawler
	urlLocks     *KeyedLock // in-process guard for project URLs
	pushPipeline *PushPipeline
}

func NewInfoProcessor(ctx *BotContext) (*InfoProcessor, error) {
	processor := &InfoProcessor{
		ctx:          ctx,
		crawler:      NewCrawler(ctx),
		urlLocks:     NewKeyedLock(),
		pushPipeline: NewPushPipeline(),
	}

	if pool, err := ants.NewPoolWithFunc(poolSize, processor.Handler); err != nil {
		return nil, err
	} else {
		processor.pool = pool
	}
	return processor, nil
}

func (r *InfoProcessor) Process() {
	projects := r.crawler.Projects()
	conf := r.config()
	for _, data := range conf {
		data.Projects = projects
		data.Alarms = r.crawler.Alarms(data.AlarmKeyword, data.UserId)
		data.IsForced = false
		if err := r.pool.Invoke(data); err != nil {
			r.ctx.Logger.Error().Stack().Err(err).Msg("")
		}
	}
}

func (r *InfoProcessor) Get(userId int64) {
	results := r.crawler.Projects()
	if len(results) > 0 {
		data := r.get(userId)
		data.Projects = results
		data.IsForced = true
		if err := r.pool.Invoke(data); err != nil {
			r.ctx.Logger.Error().Stack().Err(err).Msg("")
		}
	}
}

func (r *InfoProcessor) Release() {
	r.pool.Release()
}

func (r *InfoProcessor) config() map[int64]ProcessData {
	ids := dal.Keyword.Ids()
	m := make(map[int64]ProcessData)
	for _, id := range ids {
		if _, ok := m[id]; !ok {
			m[id] = r.get(id)
		}
	}

	return m
}

func (r *InfoProcessor) get(id int64) ProcessData {
	keywords := dal.Keyword.GetByUserIdAndType(id, model.PROJECT)
	var rules []*rule.ComplexRule
	for _, kw := range keywords {
		r := rule.NewComplexRule(kw)
		if r != nil {
			rules = append(rules, r)
		}
	}
	rule.SortComplexRules(rules)
	return ProcessData{
		UserId:       id,
		ProjectRules: rules,
		AlarmKeyword: dal.Keyword.GetKeywords(id, model.ALARM),
	}
}

// shouldSkipProcessing is the render-time pre-filter for project URLs. It is
// deliberately NOT the authority: its checks (in-process lock + IsUrlExist)
// only avoid spending CPU on rendering something that is already handled or
// in flight. The actual at-most-once guarantee comes from the DB claim
// (InsertIfAbsent) performed later by the PushPipeline — a stale pre-filter
// pass can only waste a render, never cause a duplicate push.
//
// On success (skip=false) the in-process lock is held until the caller
// finishes with the URL (see releaseLock), so a concurrent invocation in the
// same process cannot double-render.
func (r *InfoProcessor) shouldSkipProcessing(userId int64, url string, isForced bool) (skip bool, err error) {
	lockKey := fmt.Sprintf("%d:%s", userId, url)

	// Check if URL is already being processed in this process.
	if !r.urlLocks.TryLock(lockKey) {
		return true, nil // URL is being processed, skip it
	}

	// If not forced, check if URL exists in the database.
	if !isForced {
		exists, err := dal.History.IsUrlExist(userId, url)
		if err != nil {
			r.urlLocks.Unlock(lockKey)
			return false, err
		}
		if exists {
			r.urlLocks.Unlock(lockKey)
			return true, nil // URL already exists in DB, skip it
		}
	}

	return false, nil
}

func (r *InfoProcessor) releaseLock(userId int64, url string) {
	lockKey := fmt.Sprintf("%d:%s", userId, url)
	r.urlLocks.Unlock(lockKey)
}

// contentResult holds the outcome of a single project's message rendering.
type contentResult struct {
	chunks []string
	total  int
}

// projectContentSem limits how many per-project content renderings run at
// once, keeping the fan-out bounded instead of spawning one goroutine per
// project.
const projectContentSem = 5

func (r *InfoProcessor) Handler(i interface{}) {
	switch pd := i.(type) {
	case ProcessData:
		// Run the two pipelines concurrently so alarm notifications do not
		// wait behind a large project batch. Both pipelines are safe to run
		// in parallel: the PushPipeline's keyed lock is concurrent-safe, and
		// the two handle sets never share a key (projects carry no Key, the
		// pre-filter lock only guards project URLs).
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.processProjects(pd)
		}()
		go func() {
			defer wg.Done()
			r.processAlarms(pd)
		}()
		wg.Wait()
	}
}

// projectPushState 汇聚一个用户一轮 project 推送的共享状态，由 PushPipeline
// 串行消费，因此各字段无需并发保护。
type projectPushState struct {
	userId       int64
	isForced     bool
	now          time.Time
	failed       []string
	filterFailed map[string]*Project
	processedURL []*model.History
}

// processProjects handles the project-notice pipeline for one user: filter by
// keyword rules, render matched content, then push each URL through the
// PushPipeline skeleton. The DB primary key (user_id, url) acts as a
// distributed lock: only the invocation that actually inserts the history row
// may send, so concurrent runs — even multiple bot instances — can never push
// the same project twice. A fully failed send rolls the claim back so the
// next run retries. The forced path (/retry) bypasses the claim on purpose.
func (r *InfoProcessor) processProjects(pd ProcessData) {
	historyDao := dal.History
	projects := NewProjects(r.ctx, pd.Projects, pd.ProjectRules).Filter()
	logger := r.ctx.Logger
	st := &projectPushState{
		userId:       pd.UserId,
		isForced:     pd.IsForced,
		now:          time.Now(),
		failed:       []string{"failed:"},
		filterFailed: make(map[string]*Project),
	}

	// Filter out URLs that were already processed or are being processed by
	// another worker, so we only render new content. The in-process lock is
	// held from here until the project has been sent (see shouldSkipProcessing
	// and sendProject), so the PushPipeline handles carry no Key.
	var pending []*Project
	for _, project := range projects {
		shouldSkip, err := r.shouldSkipProcessing(st.userId, project.Pageurl, pd.IsForced)
		if err != nil {
			logger.Error().Stack().Err(err).Msg("check url processing failed")
			continue
		}
		if shouldSkip {
			logger.Debug().Msgf("URL %s is already processed or being processed, skipping", project.ShortTitle)
			continue
		}
		pending = append(pending, project)
	}

	// Rendering is CPU heavy, so render the pending projects concurrently
	// with a bounded fan-out instead of blocking on each project in turn.
	results := make([]contentResult, len(pending))
	sem := make(chan struct{}, projectContentSem)
	var wg sync.WaitGroup
	for i, pj := range pending {
		wg.Add(1)
		go func(idx int, pj *Project) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			chunks, total := r.ToMessage(pj)
			results[idx] = contentResult{chunks: chunks, total: total}
		}(i, pj)
	}
	wg.Wait()

	// Build one handle per URL; the pipeline owns claim/send/rollback and the
	// pre-filter lock already covers the whole lifecycle, so Key is empty.
	handles := make([]ClaimHandle, 0, len(pending))
	for j, project := range pending {
		pageURL := project.Pageurl
		chunks := results[j].chunks
		total := results[j].total
		logger.Debug().Msgf("split content to %d parts", total)

		handles = append(handles, ClaimHandle{
			Claim: func() (bool, error) {
				if st.isForced {
					return true, nil // forced path re-pushes without claiming
				}
				return historyDao.InsertIfAbsent(&model.History{
					UserID:        st.userId,
					URL:           pageURL,
					Title:         project.ShortTitle,
					UpdatedAt:     st.now,
					HasTenderCode: btoi(project.HasTenderCode),
				})
			},
			Send: func() error {
				return r.sendProject(st, project, chunks, total)
			},
			Rollback: func() error {
				if st.isForced {
					return nil
				}
				return historyDao.Remove(st.userId, pageURL)
			},
		})
	}
	r.pushPipeline.Run(handles)

	// The pre-filter lock was held from shouldSkipProcessing through the
	// pipeline run; release every claimed URL now so a failed project (whose
	// history row was rolled back) can be retried by a later run instead of
	// being permanently skipped by the in-process lock.
	for _, project := range pending {
		r.releaseLock(st.userId, project.Pageurl)
	}

	if len(st.failed) > 1 {
		if _, err := r.ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
			ChatID:    st.userId,
			Text:      strings.Join(st.failed, "\n"),
			ParseMode: models.ParseModeHTML,
		}); err != nil {
			logger.Error().Stack().Err(err).Msg("")
		} else {
			// The failure summary reached the user, so persist the failed
			// URLs to avoid re-pushing them on every run. Their claims (if
			// any) were already rolled back when the first chunk failed.
			for _, v := range st.filterFailed {
				if _, err := historyDao.InsertIfAbsent(&model.History{
					UserID:        st.userId,
					URL:           v.Pageurl,
					Title:         v.ShortTitle,
					HasTenderCode: btoi(v.HasTenderCode),
					UpdatedAt:     st.now,
				}); err != nil {
					logger.Error().Stack().Err(err).Msg("")
				}
			}
		}
	}

	if len(st.processedURL) > 0 {
		if err := historyDao.Insert(st.processedURL); err != nil {
			logger.Error().Stack().Err(err).Msg("")
		}
	}
}

// sendProject sends a project's chunked message, collecting failures for the
// summary. It returns an error only when the very first chunk failed (nothing
// was delivered), which makes the PushPipeline roll back the claim so the
// next run can retry.
func (r *InfoProcessor) sendProject(st *projectPushState, project *Project, chunks []string, total int) error {
	logger := r.ctx.Logger
	pageURL := project.Pageurl
	title := project.Title
	shortTitle := project.ShortTitle

	isSuccessful := false
	for idx, chunk := range chunks {
		if _, errSend := r.ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
			ChatID:    st.userId,
			Text:      chunk,
			ParseMode: models.ParseModeHTML,
		}); errSend != nil {
			if !isSuccessful {
				if _, ok := st.filterFailed[pageURL]; !ok {
					st.filterFailed[pageURL] = project
					st.failed = append(st.failed, fmt.Sprintf("%d. <b>[%s]</b> <a href=\"%s\">%s</a>",
						len(st.failed), project.Keyword, pageURL, title))
				}
			}
			logger.Error().Stack().Err(errSend).Msgf("content: %s", chunk)
			if idx == 0 {
				return errSend // nothing delivered → pipeline rolls back
			}
		} else {
			isSuccessful = true
			logger.Info().Msgf("notify: %s[%s]-%d", shortTitle, project.OpenTenderCode, idx)
		}
		time.Sleep(500 * time.Millisecond)
	}

	if isSuccessful && total > 0 && st.isForced {
		// Forced path bypasses the claim, so persist history here.
		// Normal path already persisted the row at claim time.
		st.processedURL = append(st.processedURL, &model.History{
			UserID:        st.userId,
			URL:           pageURL,
			Title:         shortTitle,
			UpdatedAt:     st.now,
			HasTenderCode: btoi(project.HasTenderCode),
		})
	}
	return nil
}

// processAlarms handles the alarm-notice pipeline for one user: dedupe alarms
// per run, then push each new alarm through the PushPipeline skeleton. The DB
// primary key (user_id, credit_code) acts as a distributed lock: only the
// invocation that actually inserts the row may send, so concurrent runs —
// even multiple bot instances sharing the same DB — can never push the same
// alarm twice. A failed send rolls the claim back so the next run retries.
func (r *InfoProcessor) processAlarms(pd ProcessData) {
	userId := pd.UserId
	processedAlarms := make(map[string]struct{})

	handles := make([]ClaimHandle, 0, len(pd.Alarms))
	for _, alarm := range pd.Alarms {
		alarmKey := fmt.Sprintf("%d:%s", userId, alarm.CreditCode)

		// Crawler already dedupes by credit code; keep the per-run map as a
		// cheap second line of defence.
		if _, exists := processedAlarms[alarmKey]; exists {
			continue
		}
		processedAlarms[alarmKey] = struct{}{}
		alarm.UserID = userId

		handles = append(handles, ClaimHandle{
			Key: alarmKey, // in-process guard, owned by the pipeline
			Claim: func() (bool, error) {
				return r.claimAlarm(alarm)
			},
			Send: func() error {
				return r.sendAlarm(alarm)
			},
			Rollback: func() error {
				return dal.Alarm.Remove(userId, alarm.CreditCode)
			},
		})
	}
	r.pushPipeline.Run(handles)
}

// claimAlarm claims an alarm via the (user_id, credit_code) unique constraint:
// fast-path IsExist, then InsertIfAbsent. A conflict with no active record
// means a stale (expired, not yet cleaned) row — remove it and claim once
// more. Returns whether this call actually claimed the alarm.
func (r *InfoProcessor) claimAlarm(alarm *model.Alarm) (bool, error) {
	logger := r.ctx.Logger
	userId := alarm.UserID

	isExist, err := dal.Alarm.IsExist(userId, alarm.CreditCode)
	if err != nil {
		logger.Error().Err(err).Msg("failed to check alarm existence")
		return false, err
	}
	if isExist {
		return false, nil
	}

	inserted, err := dal.Alarm.InsertIfAbsent(alarm)
	if err != nil {
		logger.Error().Stack().Err(err).Msg("claim alarm")
		return false, err
	}
	if inserted {
		return true, nil
	}

	// A row exists but IsExist() reported it inactive — a stale record whose
	// end date has passed but has not been cleaned up yet. Remove and retry.
	stillExist, e := dal.Alarm.IsExist(userId, alarm.CreditCode)
	if e != nil {
		logger.Error().Stack().Err(e).Msg("recheck alarm existence")
		return false, e
	}
	if stillExist {
		return false, nil // claimed by a concurrent invocation in between
	}
	if rErr := dal.Alarm.Remove(userId, alarm.CreditCode); rErr != nil {
		logger.Error().Stack().Err(rErr).Msg("remove stale alarm")
		return false, rErr
	}
	return dal.Alarm.InsertIfAbsent(alarm)
}

// sendAlarm renders and sends a single alarm message.
func (r *InfoProcessor) sendAlarm(alarm *model.Alarm) error {
	logger := r.ctx.Logger
	msg, err := alarm.ToMessage()
	if err != nil {
		logger.Error().Stack().Err(err).Msg("alarm to msg")
		return err
	}
	if _, msgErr := r.ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
		ChatID:    alarm.UserID,
		Text:      msg,
		ParseMode: models.ParseModeHTML,
	}); msgErr != nil {
		logger.Error().Stack().Err(msgErr).Msg("send alarm")
		return msgErr
	}
	return nil
}

func (r *InfoProcessor) ToMessage(project *Project) ([]string, int) {
	project.Content = utils.SimplifyContent(project.Content)
	return project.SplitMessage()
}

func cleanContent(content string) string {
	// Remove HTML attributes and tags
	content = htmlAttrRegex.ReplaceAllString(content, "")
	content = htmlTagsRegex.ReplaceAllString(content, "")

	// Remove markdown style bold and any remaining < or > characters
	content = strings.NewReplacer(
		"**", "",
		"__", "",
	).Replace(content)

	return content
}

func btoi(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
