package pkg

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gythialy/magnet/pkg/rule"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/entities"
	"github.com/panjf2000/ants/v2"
)

const (
	poolSize         = 10
	maxMessageLength = 3900
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
	if pool, err := ants.NewPoolWithFunc(poolSize, func(i interface{}) {
		switch m := i.(type) {
		case ConfigData:
			// process projects
			messages := NewProjects(ctx, m.Projects, m.ProjectRules).ToMessage()
			failed := []string{"failed:"}
			userId := m.UserId
			var newHistories []*entities.History
			now := time.Now()
			for title, msg := range messages {
				// already processed, skip it
				url := msg.Project.Pageurl
				shortTitle := msg.Project.ShortTitle
				if historyDao.IsUrlExist(userId, url) && !m.IsForced {
					ctx.Logger.Debug().Msgf("%s already processed", shortTitle)
					continue
				}

				if chunks, total := splitMessage(msg.Content); total > 0 {
					for i, chunk := range chunks {
						header := fmt.Sprintf("Long Message (%d/%d)\n\n", i+1, total)
						fullMessage := header + chunk
						if _, err := ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
							ChatID:    userId,
							Text:      fullMessage,
							ParseMode: models.ParseModeHTML,
						}); err != nil {
							failed = append(failed, fmt.Sprintf("<a href=\"%s\">%s</a>", url, title))
							ctx.Logger.Error().Msg(err.Error())
						} else {
							ctx.Logger.Info().Msgf("notify: %s[%s]", msg.Project.ShortTitle, msg.Project.OpenTenderCode)
						}
						time.Sleep(50 * time.Millisecond)
					}
					newHistories = append(newHistories, &entities.History{
						UserId:    userId,
						Url:       url,
						Title:     shortTitle,
						UpdatedAt: now,
					})
				} else {
					ctx.Logger.Warn().Msgf("Empty message for %s", shortTitle)
				}
			}

			if len(failed) > 1 {
				if _, err := ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
					ChatID:    userId,
					Text:      strings.Join(failed, "\n"),
					ParseMode: models.ParseModeHTML,
				}); err != nil {
					ctx.Logger.Error().Msg(err.Error())
				}
			}

			if len(newHistories) > 0 {
				if err, rows := historyDao.Insert(newHistories); err != nil {
					ctx.Logger.Error().Msg(err.Error())
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
					ctx.Logger.Error().Msg(err.Error())
				} else {
					newAlarms = append(newAlarms, alarm)
				}
			}

			if len(newAlarms) > 0 {
				if err, rows := alarmDao.Insert(newAlarms); err != nil {
					ctx.Logger.Error().Msg(err.Error())
				} else {
					ctx.Logger.Info().Msgf("insert %d alarms", rows)
				}
			}
		}
	}); err != nil {
		return nil, err
	} else {
		return &InfoProcessor{
			context:    ctx,
			pool:       pool,
			crawler:    NewCrawler(ctx),
			keywordDao: entities.NewKeywordDao(ctx.DB),
			historyDao: historyDao,
			alarmDao:   alarmDao,
		}, nil
	}
}

func (r *InfoProcessor) Process() {
	// fetch info
	projects := r.crawler.FetchProjects()
	config := r.config()
	for _, data := range config {
		data.Projects = projects
		// fetch alarm data by userId
		data.Alarms = r.crawler.Fetch(data.AlarmKeyword, data.UserId)
		data.IsForced = false
		if err := r.pool.Invoke(data); err != nil {
			r.context.Logger.Error().Msg(err.Error())
		}
	}
}

func (r *InfoProcessor) Get(id int64) {
	// fetch info
	results := r.crawler.FetchProjects()
	if len(results) > 0 {
		data := r.get(id)
		data.Projects = results
		data.IsForced = true
		if err := r.pool.Invoke(data); err != nil {
			r.context.Logger.Error().Msg(err.Error())
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

func splitMessage(message string) ([]string, int) {
	var chunks []string
	for len(message) > 0 {
		if len(message) <= maxMessageLength {
			chunks = append(chunks, message)
			break
		}

		chunk := message[:maxMessageLength]
		lastNewline := strings.LastIndex(chunk, "\n")
		if lastNewline > 0 {
			chunk = chunk[:lastNewline]
		}

		chunks = append(chunks, chunk)
		message = message[len(chunk):]
	}
	return chunks, len(chunks)
}

type ConfigData struct {
	UserId       int64
	ProjectRules []*rule.ComplexRule
	AlarmKeyword []string
	Projects     []*Project
	Alarms       []*entities.Alarm
	IsForced     bool
}
