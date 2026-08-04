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
	ctx        *BotContext
	pool       *ants.PoolWithFunc
	crawler    *Crawler
	urlLocks   *KeyedLock // in-process guard for project URLs
	alarmLocks *KeyedLock // in-process guard for alarms (userId:CreditCode)
}

func NewInfoProcessor(ctx *BotContext) (*InfoProcessor, error) {
	processor := &InfoProcessor{
		ctx:        ctx,
		crawler:    NewCrawler(ctx),
		urlLocks:   NewKeyedLock(),
		alarmLocks: NewKeyedLock(),
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

// shouldSkipProcessing checks if a URL is already processed in DB or is being processed,
// returning true if processing should be skipped
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
		r.processProjects(pd)
		r.processAlarms(pd)
	}
}

// processProjects handles the project-notice pipeline for one user: filter by
// keyword rules, render matched content, send messages and record history.
// The check-send-insert critical section is made atomic by claiming each URL
// with the DB primary key (user_id, url): only the invocation that actually
// inserts the history row may send, so concurrent runs — even multiple bot
// instances sharing the same DB — can never push the same project twice. A
// fully failed send rolls the claim back so the next run retries. The forced
// path (/retry) intentionally bypasses the claim so the operator can re-push.
func (r *InfoProcessor) processProjects(pd ProcessData) {
	historyDao := dal.History
	projects := NewProjects(r.ctx, pd.Projects, pd.ProjectRules).Filter()
	failed := []string{"failed:"}
	filterFailed := make(map[string]*Project)
	userId := pd.UserId
	var processedURL []*model.History
	now := time.Now()

	logger := r.ctx.Logger
	// Filter out URLs that were already processed or are being processed by
	// another worker, so we only render new content.
	var pending []*Project
	for _, project := range projects {
		shouldSkip, err := r.shouldSkipProcessing(userId, project.Pageurl, pd.IsForced)
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

	for j, project := range pending {
		pageURL := project.Pageurl
		title := project.Title
		shortTitle := project.ShortTitle
		chunks := results[j].chunks
		total := results[j].total
		logger.Debug().Msgf("split content to %d parts", total)

		// Wrap each URL in a closure so the in-process lock is released by
		// defer on every exit path — including future ones — instead of
		// relying on each branch remembering to call releaseLock.
		func() {
			defer r.releaseLock(userId, pageURL)

			// Claim the URL with the DB primary key acting as a distributed
			// lock. Only the caller that actually created the history row
			// proceeds to send; any concurrent caller (even from another bot
			// instance) gets RowsAffected == 0 and skips. The forced path is
			// exempt so /retry can deliberately re-push already-seen projects.
			if !pd.IsForced {
				claimed, err := historyDao.InsertIfAbsent(&model.History{
					UserID:        userId,
					URL:           pageURL,
					Title:         shortTitle,
					UpdatedAt:     now,
					HasTenderCode: btoi(project.HasTenderCode),
				})
				if err != nil {
					logger.Error().Stack().Err(err).Msg("claim project")
					return
				}
				if !claimed {
					logger.Debug().Msgf("URL %s claimed by another invocation, skip", shortTitle)
					return
				}
			}

			// only save all parts failed to the failed list
			isSuccessful := false
			for idx, chunk := range chunks {
				if _, errSend := r.ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
					ChatID:    userId,
					Text:      chunk,
					ParseMode: models.ParseModeHTML,
				}); errSend != nil {
					if !isSuccessful {
						if _, ok := filterFailed[pageURL]; !ok {
							filterFailed[pageURL] = project
							failed = append(failed, fmt.Sprintf("%d. <b>[%s]</b> <a href=\"%s\">%s</a>",
								len(failed), project.Keyword, pageURL, title))
						}
					}
					logger.Error().Stack().Err(errSend).Msgf("content: %s", chunk)
					if idx == 0 {
						// Nothing was delivered: roll back the claim so the
						// next run can retry this project.
						if !pd.IsForced {
							if rErr := historyDao.Remove(userId, pageURL); rErr != nil {
								logger.Error().Stack().Err(rErr).Msg("rollback project claim")
							}
						}
						return
					}
				} else {
					isSuccessful = true
					logger.Info().Msgf("notify: %s[%s]-%d", shortTitle, project.OpenTenderCode, idx)
				}
				time.Sleep(500 * time.Millisecond)
			}

			if isSuccessful && total > 0 && pd.IsForced {
				// Forced path bypasses the claim, so persist history here.
				// Normal path already persisted the row at claim time.
				processedURL = append(processedURL, &model.History{
					UserID:        userId,
					URL:           pageURL,
					Title:         shortTitle,
					UpdatedAt:     now,
					HasTenderCode: btoi(project.HasTenderCode),
				})
			}
		}()
	}

	if len(failed) > 1 {
		if _, err := r.ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
			ChatID:    userId,
			Text:      strings.Join(failed, "\n"),
			ParseMode: models.ParseModeHTML,
		}); err != nil {
			logger.Error().Stack().Err(err).Msg("")
		} else {
			// The failure summary reached the user, so persist the failed
			// URLs to avoid re-pushing them on every run. Their claims (if
			// any) were already rolled back when the first chunk failed.
			for _, v := range filterFailed {
				if _, err := historyDao.InsertIfAbsent(&model.History{
					UserID:        userId,
					URL:           v.Pageurl,
					Title:         v.ShortTitle,
					HasTenderCode: btoi(v.HasTenderCode),
					UpdatedAt:     now,
				}); err != nil {
					logger.Error().Stack().Err(err).Msg("")
				}
			}
		}
	}

	if len(processedURL) > 0 {
		if err := historyDao.Insert(processedURL); err != nil {
			logger.Error().Stack().Err(err).Msg("")
		}
	}

}

// processAlarms handles the alarm-notice pipeline for one user: dedupe alarms
// per run and push new alarms. The check-send-insert critical section is made
// atomic by claiming the alarm with the DB primary key (user_id, credit_code):
// only the invocation that actually inserts the row may send the message, so
// concurrent runs — even multiple bot instances sharing the same DB — can
// never push the same alarm twice. A failed send rolls the claim back so the
// next run retries.
func (r *InfoProcessor) processAlarms(pd ProcessData) {
	userId := pd.UserId
	logger := r.ctx.Logger
	processedAlarms := make(map[string]struct{})
	var lockedAlarms []string
	defer func() {
		for _, key := range lockedAlarms {
			r.alarmLocks.Unlock(key)
		}
	}()

	for _, alarm := range pd.Alarms {
		alarmKey := fmt.Sprintf("%d:%s", userId, alarm.CreditCode)

		if _, exists := processedAlarms[alarmKey]; exists {
			continue
		}
		processedAlarms[alarmKey] = struct{}{}

		// In-process guard as a cheap first line of defence; the DB unique
		// constraint below is the real authority (it also covers multiple
		// bot instances, where in-process locks are invisible).
		if !r.alarmLocks.TryLock(alarmKey) {
			logger.Debug().Msgf("alarm %s is being processed by another invocation, skip", alarmKey)
			continue
		}
		lockedAlarms = append(lockedAlarms, alarmKey)

		alarm.UserID = userId

		// Fast path: an active record for this company already exists.
		isExist, err := dal.Alarm.IsExist(userId, alarm.CreditCode)
		if err != nil {
			logger.Error().Err(err).Msg("failed to check alarm existence")
			continue
		}
		if isExist {
			continue
		}

		// Claim the alarm with the DB primary key acting as a distributed
		// lock. Only the caller that actually created the row proceeds to
		// send; any concurrent caller gets RowsAffected == 0 and skips.
		inserted, err := dal.Alarm.InsertIfAbsent(alarm)
		if err != nil {
			logger.Error().Stack().Err(err).Msg("claim alarm")
			continue
		}
		if !inserted {
			// A row exists but IsExist() reported it inactive — a stale
			// record whose end date has passed but has not been cleaned
			// up yet. Remove it and claim once more.
			stillExist, e := dal.Alarm.IsExist(userId, alarm.CreditCode)
			if e != nil {
				logger.Error().Stack().Err(e).Msg("recheck alarm existence")
				continue
			}
			if stillExist {
				continue // claimed by a concurrent invocation in between
			}
			if rErr := dal.Alarm.Remove(userId, alarm.CreditCode); rErr != nil {
				logger.Error().Stack().Err(rErr).Msg("remove stale alarm")
				continue
			}
			inserted, err = dal.Alarm.InsertIfAbsent(alarm)
			if err != nil {
				logger.Error().Stack().Err(err).Msg("claim alarm after cleanup")
				continue
			}
			if !inserted {
				continue // lost the race again, another invocation handles it
			}
		}

		msg, err := alarm.ToMessage()
		if err != nil {
			logger.Error().Stack().Err(err).Msg("alarm to msg")
			if rErr := dal.Alarm.Remove(userId, alarm.CreditCode); rErr != nil {
				logger.Error().Stack().Err(rErr).Msg("rollback alarm after render error")
			}
			continue
		}
		if _, msgErr := r.ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
			ChatID:    userId,
			Text:      msg,
			ParseMode: models.ParseModeHTML,
		}); msgErr != nil {
			logger.Error().Stack().Err(msgErr).Msg("send alarm")
			// Roll back the claim so the next run can retry this alarm.
			if rErr := dal.Alarm.Remove(userId, alarm.CreditCode); rErr != nil {
				logger.Error().Stack().Err(rErr).Msg("rollback alarm after send failure")
			}
			continue
		}
		// Sent successfully — the record was already persisted by the claim.
	}
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
