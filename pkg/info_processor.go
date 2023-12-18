package pkg

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gythialy/magnet/pkg/utiles"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/entities"
	"github.com/panjf2000/ants/v2"
)

type InfoProcessor struct {
	context       *BotContext
	pool          *ants.PoolWithFunc
	crawler       *Crawler
	keywordDao    *entities.KeywordDao
	tenderCodeDao *entities.TenderCodeDao
	historyDao    *entities.HistoryDao
}

func NewInfoProcessor(ctx *BotContext) (*InfoProcessor, error) {
	historyDao := entities.NewHistoryDao(ctx.DB)
	pool, err := ants.NewPoolWithFunc(10, func(i interface{}) {
		m := i.(ConfigData)
		results := entities.NewResults(m.Results)
		results.Filter(m.Keywords, m.TenderCode)
		messages := results.ToMarkdown()
		failed := []string{"failed:"}
		userId := m.ID
		histories := historyDao.Cache(userId)
		var tobeUpdated []entities.History
		now := time.Now()
		for title, msg := range messages {
			// already processed, skip it
			url := msg.Result.Pageurl
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
			tobeUpdated = append(tobeUpdated, entities.History{
				UserId:    userId,
				Url:       url,
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

		if len(tobeUpdated) > 0 {
			if err, rows := historyDao.Insert(tobeUpdated); err != nil {
				ctx.Logger.Error().Err(err)
			} else {
				ctx.Logger.Info().Msgf("insert %d data", rows)
			}
		}
	})
	if err != nil {
		return nil, err
	}

	return &InfoProcessor{
		context:       ctx,
		pool:          pool,
		crawler:       NewCrawler(ctx),
		keywordDao:    entities.NewKeywordDao(ctx.DB),
		tenderCodeDao: entities.NewTenderCodeDao(ctx.DB),
		historyDao:    historyDao,
	}, nil
}

func (r *InfoProcessor) Process() {
	// fetch info
	results := r.crawler.Get()
	if len(results) > 0 {
		config := r.config()
		for _, data := range config {
			data.Results = results
			if err := r.pool.Invoke(data); err != nil {
				r.context.Logger.Error().Err(err)
			}
		}
	}
}

func (r *InfoProcessor) Get(id int64) {
	// fetch info
	results := r.crawler.Get()
	if len(results) > 0 {
		data := r.get(id)
		data.Results = results
		if err := r.pool.Invoke(data); err != nil {
			r.context.Logger.Error().Err(err)
		}
	}
}

func (r *InfoProcessor) Release() {
	r.pool.Release()
}

func (r *InfoProcessor) config() map[int64]ConfigData {
	ids1 := r.keywordDao.Ids()
	ids2 := r.tenderCodeDao.Ids()
	m := make(map[int64]ConfigData)
	for _, id := range ids1 {
		if _, ok := m[id]; !ok {
			m[id] = r.get(id)
		}
	}

	for _, id := range ids2 {
		if _, ok := m[id]; !ok {
			m[id] = r.get(id)
		}
	}

	return m
}

func (r *InfoProcessor) get(id int64) ConfigData {
	keywords := r.keywordDao.ListKeywords(id)
	codes := r.tenderCodeDao.ListTenderCodes(id)
	return ConfigData{
		ID:         id,
		Keywords:   keywords,
		TenderCode: codes,
	}
}

type ConfigData struct {
	ID         int64
	Keywords   []string
	TenderCode []string
	Results    []*entities.Result
}
