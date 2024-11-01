package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"github.com/gythialy/magnet/pkg/rule"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/panjf2000/ants/v2"
)

const (
	poolSize         = 10
	maxMessageLength = 3900
)

type InfoProcessor struct {
	context *BotContext
	pool    *ants.PoolWithFunc
	crawler *Crawler
}

func NewInfoProcessor(ctx *BotContext) (*InfoProcessor, error) {
	historyDao := dal.History
	alarmDao := dal.Alarm
	if pool, err := ants.NewPoolWithFunc(poolSize, func(i interface{}) {
		switch pd := i.(type) {
		case ProcessData:
			// process projects
			messages := NewProjects(ctx, pd.Projects, pd.ProjectRules).ToMessage()
			failed := []string{"failed:"}
			filterFailed := make(map[string]struct{})
			userId := pd.UserId
			var newHistories []*model.History
			now := time.Now()
			limiter := time.NewTicker(500 * time.Millisecond)
			defer limiter.Stop()

			for title, msg := range messages {
				// already processed, skip it
				pageURL := msg.Project.Pageurl
				shortTitle := msg.Project.ShortTitle
				if historyDao.IsUrlExist(userId, pageURL) && !pd.IsForced {
					ctx.Logger.Debug().Msgf("%s already processed", shortTitle)
					continue
				}

				if chunks, total := splitMessage(msg.Content); total > 0 {
					for idx, chunk := range chunks {
						<-limiter.C

						if _, err := ctx.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
							ChatID:    userId,
							Text:      chunk,
							ParseMode: models.ParseModeHTML,
						}); err != nil {
							if _, ok := filterFailed[pageURL]; !ok {
								filterFailed[pageURL] = struct{}{}
								failed = append(failed, fmt.Sprintf("<a href=\"%s\">%s</a>", pageURL, title))
							}
							ctx.Logger.Error().Msg(err.Error())
						} else {
							ctx.Logger.Info().Msgf("notify: %s[%s]-%d", msg.Project.ShortTitle,
								msg.Project.OpenTenderCode, idx)
						}
					}
					newHistories = append(newHistories, &model.History{
						UserID:    userId,
						URL:       pageURL,
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
				if err := historyDao.Insert(newHistories); err != nil {
					ctx.Logger.Error().Msg(err.Error())
				}
			}

			// process alarms
			alarmCache := alarmDao.Cache(userId)
			var newAlarms []*model.Alarm
			for _, alarm := range pd.Alarms {
				if _, ok := alarmCache[alarm.CreditCode]; ok {
					continue
				}
				msg, _ := alarm.ToTelegramMessage()
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
				if err := alarmDao.Insert(newAlarms); err != nil {
					ctx.Logger.Error().Msg(err.Error())
				}
			}
		}
	}); err != nil {
		return nil, err
	} else {
		return &InfoProcessor{
			context: ctx,
			pool:    pool,
			crawler: NewCrawler(ctx),
		}, nil
	}
}

func (r *InfoProcessor) Process() {
	projects := r.crawler.Projects()
	config := r.config()
	for _, data := range config {
		data.Projects = projects
		data.Alarms = r.crawler.Alarms(data.AlarmKeyword, data.UserId)
		data.IsForced = false
		if err := r.pool.Invoke(data); err != nil {
			r.context.Logger.Error().Msg(err.Error())
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
			r.context.Logger.Error().Msg(err.Error())
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
	return ProcessData{
		UserId:       id,
		ProjectRules: rules,
		AlarmKeyword: dal.Keyword.GetKeywords(id, model.ALARM),
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

type ProcessData struct {
	UserId       int64
	ProjectRules []*rule.ComplexRule
	AlarmKeyword []string
	Projects     []*Project
	Alarms       []*model.Alarm
	IsForced     bool
}
