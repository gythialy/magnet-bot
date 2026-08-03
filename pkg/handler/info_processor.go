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
	ctx            *BotContext
	pool           *ants.PoolWithFunc
	crawler        *Crawler
	processingLock sync.Mutex
	processingURLs map[string]bool
	// processingAlarms guards the check-send-insert critical section for alarms,
	// preventing duplicate alarm messages when Process() runs overlap (e.g. a
	// previous run still in progress when the next scheduled run starts).
	processingAlarms map[string]bool
}

func NewInfoProcessor(ctx *BotContext) (*InfoProcessor, error) {
	processor := &InfoProcessor{
		ctx:              ctx,
		crawler:          NewCrawler(ctx),
		processingURLs:   make(map[string]bool),
		processingAlarms: make(map[string]bool),
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
	r.processingLock.Lock()
	defer r.processingLock.Unlock()

	// Check if URL is already being processed
	if r.processingURLs[lockKey] {
		return true, nil // URL is being processed, skip it
	}

	// If not forced, check if URL exists in the database
	if !isForced {
		exists, err := dal.History.IsUrlExist(userId, url)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil // URL already exists in DB, skip it
		}
	}

	// Mark as being processed if we're going to process it
	r.processingURLs[lockKey] = true
	return false, nil
}

func (r *InfoProcessor) releaseLock(userId int64, url string) {
	lockKey := fmt.Sprintf("%d:%s", userId, url)
	r.processingLock.Lock()
	defer r.processingLock.Unlock()

	delete(r.processingURLs, lockKey)
}

// tryLockAlarm marks an alarm (userId:CreditCode) as being processed by the
// current invocation. It returns false when another concurrent invocation is
// already handling the same alarm, in which case the caller must skip it to
// avoid pushing a duplicate message.
func (r *InfoProcessor) tryLockAlarm(key string) bool {
	r.processingLock.Lock()
	defer r.processingLock.Unlock()

	if r.processingAlarms[key] {
		return false
	}
	r.processingAlarms[key] = true
	return true
}

func (r *InfoProcessor) unlockAlarm(key string) {
	r.processingLock.Lock()
	defer r.processingLock.Unlock()

	delete(r.processingAlarms, key)
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

projectLoop:
	for j, project := range pending {
		title := project.Title
		pageURL := project.Pageurl
		shortTitle := project.ShortTitle

		chunks := results[j].chunks
		total := results[j].total
		logger.Debug().Msgf("split content to %d parts", total)

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
					r.releaseLock(userId, pageURL)
					continue projectLoop
				}
			} else {
				isSuccessful = true
				logger.Info().Msgf("notify: %s[%s]-%d", shortTitle, project.OpenTenderCode, idx)
			}
			time.Sleep(500 * time.Millisecond)
		}

		if isSuccessful && total > 0 {
			processedURL = append(processedURL, &model.History{
				UserID:        userId,
				URL:           pageURL,
				Title:         shortTitle,
				UpdatedAt:     now,
				HasTenderCode: btoi(project.HasTenderCode),
			})
		}

		// Release the lock for this URL
		r.releaseLock(userId, pageURL)
	}

	if len(failed) > 1 {
		if _, err := r.ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
			ChatID:    userId,
			Text:      strings.Join(failed, "\n"),
			ParseMode: models.ParseModeHTML,
		}); err != nil {
			logger.Error().Stack().Err(err).Msg("")
		} else {
			for _, v := range filterFailed {
				processedURL = append(processedURL, &model.History{
					UserID:        userId,
					URL:           v.Pageurl,
					Title:         v.ShortTitle,
					HasTenderCode: btoi(v.HasTenderCode),
					UpdatedAt:     now,
				})
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
// per run, guard the check-send-insert critical section against concurrent
// invocations, push new alarms and batch-insert only the ones that were sent.
func (r *InfoProcessor) processAlarms(pd ProcessData) {
	alarmDao := dal.Alarm
	userId := pd.UserId
	logger := r.ctx.Logger
	processedAlarms := make(map[string]struct{})
	var successfulAlarms []*model.Alarm
	var lockedAlarms []string
	defer func() {
		for _, key := range lockedAlarms {
			r.unlockAlarm(key)
		}
	}()

	for _, alarm := range pd.Alarms {
		alarmKey := fmt.Sprintf("%d:%s", userId, alarm.CreditCode)

		if _, exists := processedAlarms[alarmKey]; exists {
			continue
		}
		processedAlarms[alarmKey] = struct{}{}

		// Process() hands tasks to the pool asynchronously, so two
		// invocations for the same user can run concurrently (e.g. a
		// previous scheduled run's handlers still in flight when the
		// next run dispatches). Guard the check-send-insert critical
		// section so both invocations cannot pass IsExist() and push
		// the same alarm twice.
		if !r.tryLockAlarm(alarmKey) {
			logger.Debug().Msgf("alarm %s is being processed by another invocation, skip", alarmKey)
			continue
		}
		lockedAlarms = append(lockedAlarms, alarmKey)

		alarm.UserID = userId
		isExist, err := dal.Alarm.IsExist(userId, alarm.CreditCode)
		if err != nil {
			logger.Error().Err(err).Msg("failed to check alarm existence")
			continue
		}

		if isExist {
			continue
		}

		if msg, err := alarm.ToMessage(); err == nil {
			if _, msgErr := r.ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
				ChatID:    userId,
				Text:      msg,
				ParseMode: models.ParseModeHTML,
			}); msgErr != nil {
				logger.Error().Stack().Err(msgErr).Msg("send alarm")
				continue
			}

			successfulAlarms = append(successfulAlarms, alarm)
		} else {
			logger.Error().Stack().Err(err).Msg("alarm to msg")
		}
	}

	// Batch inserts only the alarms that were successfully sent
	if len(successfulAlarms) > 0 {
		if err := alarmDao.Insert(successfulAlarms); err != nil {
			logger.Error().Stack().Err(err).Msg("batch insert alarms")
		}
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
