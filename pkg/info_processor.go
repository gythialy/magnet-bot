package pkg

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gythialy/magnet/pkg/rule"

	"github.com/gythialy/magnet/pkg/utiles"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/entities"
	"github.com/panjf2000/ants/v2"
)

type InfoProcessor struct {
	context    *BotContext
	pool       *ants.PoolWithFunc
	crawler    *Crawler
	keywordDao *entities.KeywordDao
	historyDao *entities.HistoryDao
	alarmDao   *entities.AlarmDao
}

func NewInfoProcessor(ctx *BotContext) (*InfoProcessor, error) {
	historyDao := entities.NewHistoryDao(ctx.DB)
	alarmDao := entities.NewAlarmDao(ctx.DB)
	pool, err := ants.NewPoolWithFunc(10, func(i interface{}) {
		switch m := i.(type) {
		case ConfigData:
			// process projects
			messages := NewProjects(ctx, m.Projects, m.ProjectRules).ToMarkdown()
			failed := []string{"failed:"}
			userId := m.UserId
			histories := historyDao.Cache(userId)
			var newHistories []*entities.History
			now := time.Now()
			for title, msg := range messages {
				// already processed, skip it
				url := msg.Project.Pageurl
				if _, ok := histories[url]; ok {
					continue
				}
				if _, err := ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
					ChatID:    userId,
					Text:      msg.Content,
					ParseMode: models.ParseModeMarkdown,
				}); err != nil {
					failed = append(failed, fmt.Sprintf("[%s](%s)  ", utiles.Escape(title), url))
					ctx.Logger.Error().Err(err)
				}
				newHistories = append(newHistories, &entities.History{
					UserId:    userId,
					Url:       url,
					Title:     msg.Project.ShortTitle,
					UpdatedAt: now,
				})
				time.Sleep(50 * time.Millisecond)
			}

			if len(failed) > 1 {
				if _, err := ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
					ChatID:    userId,
					Text:      strings.Join(failed, "\n"),
					ParseMode: models.ParseModeMarkdown,
				}); err != nil {
					ctx.Logger.Error().Err(err)
				}
			}

			if len(newHistories) > 0 {
				if err, rows := historyDao.Insert(newHistories); err != nil {
					ctx.Logger.Error().Err(err)
				} else {
					ctx.Logger.Info().Msgf("insert %d projects", rows)
				}
			}

			// process alarms
			alarmCache := alarmDao.Cache(userId)
			var newAlarms []*entities.Alarm
			for _, alarm := range m.Alarms {
				if _, ok := alarmCache[alarm.CreditCode]; ok {
					continue
				}
				msg := alarm.ToMarkdown()
				if _, err := ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
					ChatID:    userId,
					Text:      msg,
					ParseMode: models.ParseModeHTML,
				}); err != nil {
					ctx.Logger.Error().Err(err)
				}

				newAlarms = append(newAlarms, alarm)
			}

			if len(newAlarms) > 0 {
				if err, rows := alarmDao.Insert(newAlarms); err != nil {
					ctx.Logger.Error().Err(err)
				} else {
					ctx.Logger.Info().Msgf("insert %d alarms", rows)
				}
			}
		}
	})
	if err != nil {
		return nil, err
	}

	return &InfoProcessor{
		context:    ctx,
		pool:       pool,
		crawler:    NewCrawler(ctx),
		keywordDao: entities.NewKeywordDao(ctx.DB),
		historyDao: historyDao,
		alarmDao:   alarmDao,
	}, nil
}

func (r *InfoProcessor) Process() {
	// fetch info
	projects := r.crawler.FetchProjects()
	config := r.config()
	for _, data := range config {
		data.Projects = projects
		// fetch alarm data by userId
		data.Alarms = r.crawler.Fetch(data.AlarmKeyword, data.UserId)
		if err := r.pool.Invoke(data); err != nil {
			r.context.Logger.Error().Err(err)
		}
	}
}

func (r *InfoProcessor) Get(id int64) {
	// fetch info
	results := r.crawler.FetchProjects()
	if len(results) > 0 {
		data := r.get(id)
		data.Projects = results
		if err := r.pool.Invoke(data); err != nil {
			r.context.Logger.Error().Err(err)
		}
	}
}

func (r *InfoProcessor) Release() {
	r.pool.Release()
}

func (r *InfoProcessor) config() map[int64]ConfigData {
	ids := r.keywordDao.Ids()
	m := make(map[int64]ConfigData)
	for _, id := range ids {
		if _, ok := m[id]; !ok {
			m[id] = r.get(id)
		}
	}

	return m
}

func (r *InfoProcessor) get(id int64) ConfigData {
	keywords := r.keywordDao.ListKeywords(id, entities.PROJECT)
	var rules []*rule.ComplexRule
	for _, keyword := range keywords {
		rule := rule.NewComplexRule(keyword)
		if rule != nil {
			rules = append(rules, rule)
		}
	}
	return ConfigData{
		UserId:       id,
		ProjectRules: rules,
		AlarmKeyword: r.keywordDao.ListKeywords(id, entities.ALARM),
	}
}

type ConfigData struct {
	UserId       int64
	ProjectRules []*rule.ComplexRule
	AlarmKeyword []string
	Projects     []*Project
	Alarms       []*entities.Alarm
}
