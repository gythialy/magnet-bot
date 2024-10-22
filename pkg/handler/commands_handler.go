package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/gythialy/magnet/pkg/rule"

	"github.com/gythialy/magnet/pkg/constant"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg"
	"github.com/gythialy/magnet/pkg/entities"
)

const (
	maxHistorySize      = 20
	maxAlarmsPerMessage = 20
)

type CommandsHandler struct {
	ctx        *pkg.BotContext
	keywordDao *entities.KeywordDao
	alarmDao   *entities.AlarmDao
	historyDao *entities.HistoryDao
}

func NewCommandsHandler(ctx *pkg.BotContext) *CommandsHandler {
	db := ctx.DB
	return &CommandsHandler{
		ctx:        ctx,
		keywordDao: entities.NewKeywordDao(db),
		alarmDao:   entities.NewAlarmDao(db),
		historyDao: entities.NewHistoryDao(db),
	}
}

func (c *CommandsHandler) addKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update, prefix string, t entities.KeywordType) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, prefix))
	keywords := strings.Split(tmp, ",")
	id := update.Message.Chat.ID
	result := c.keywordDao.Add(keywords, id, t)
	r := rule.NewComplexRule(result)
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   fmt.Sprintf("%s: %s to %s", prefix, result, r.ToString()),
	}); err != nil {
		c.ctx.Logger.Error().Msg(err.Error())
	}
}

func (c *CommandsHandler) deleteKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update, prefix string, t entities.KeywordType) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, prefix))
	keywords := strings.Split(tmp, ",")
	id := update.Message.Chat.ID
	result := c.keywordDao.Delete(keywords, id, t)

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   fmt.Sprintf("%s: %s", prefix, result),
	}); err != nil {
		c.ctx.Logger.Error().Err(err)
	}
}

func (c *CommandsHandler) listKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update, t entities.KeywordType) {
	id := update.Message.Chat.ID
	result := c.keywordDao.ListKeywords(id, t)
	if t == entities.PROJECT {
		for i, v := range result {
			if cr := rule.NewComplexRule(v); cr != nil {
				result[i] = fmt.Sprintf("%s[%s]", v, cr.ToString())
			}
		}
	}
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   fmt.Sprintf("All %s keywords:\n%s", t.String(), strings.Join(result, "\n")),
	}); err != nil {
		c.ctx.Logger.Error().Err(err)
	}
}

func (c *CommandsHandler) AddKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	c.addKeywordHandler(ctx, b, update, constant.AddKeyword, entities.PROJECT)
}

func (c *CommandsHandler) DeleteKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	c.deleteKeywordHandler(ctx, b, update, constant.DeleteKeyword, entities.PROJECT)
}

func (c *CommandsHandler) ListKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	c.listKeywordHandler(ctx, b, update, entities.PROJECT)
}

func (c *CommandsHandler) AddAlarmKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	c.addKeywordHandler(ctx, b, update, constant.AddAlarmKeyword, entities.ALARM)
}

func (c *CommandsHandler) DeleteAlarmKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	c.deleteKeywordHandler(ctx, b, update, constant.DeleteAlarmKeyword, entities.ALARM)
}

func (c *CommandsHandler) ListAlarmKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	c.listKeywordHandler(ctx, b, update, entities.ALARM)
}

func (c *CommandsHandler) ListAlarmRecordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	alarms := c.alarmDao.Cache(id)

	if len(alarms) == 0 {
		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: id,
			Text:   "Can not find any records...",
		}); err != nil {
			c.ctx.Logger.Error().Msg(err.Error())
		}
		return
	}

	totalMessages := (len(alarms) + maxAlarmsPerMessage - 1) / maxAlarmsPerMessage

	// Convert map to a slice of alarms for easier iteration
	alarmSlice := make([]entities.Alarm, 0, len(alarms))
	for _, alarm := range alarms {
		alarmSlice = append(alarmSlice, alarm)
	}

	for i := 0; i < totalMessages; i++ {
		start := i * maxAlarmsPerMessage
		end := (i + 1) * maxAlarmsPerMessage
		if end > len(alarmSlice) {
			end = len(alarmSlice)
		}

		var result strings.Builder
		for idx := start; idx < end; idx++ {
			alarm := alarmSlice[idx]
			result.WriteString(fmt.Sprintf("%d. %s, %s to %s\n", idx+1, alarm.CreditName,
				alarm.StartDate.Format("2006-01-02"), alarm.EndDate.Format("2006-01-02")))
		}

		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    id,
			Text:      result.String(),
			ParseMode: models.ParseModeHTML,
		}); err != nil {
			c.ctx.Logger.Error().Msg(err.Error())
		}
	}

	if totalMessages > 1 {
		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: id,
			Text:   fmt.Sprintf("Total %d records displayed in %d messages.", len(alarms), totalMessages),
		}); err != nil {
			c.ctx.Logger.Error().Msg(err.Error())
		}
	}
}

func (c *CommandsHandler) SearchHistoryHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	query := strings.TrimSpace(strings.TrimPrefix(text, constant.SearchHistory))
	id := update.Message.Chat.ID

	if query == "" {
		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: id,
			Text:   "Please provide a search term",
		}); err != nil {
			c.ctx.Logger.Error().Msg(err.Error())
		}
		return
	}

	results := c.historyDao.SearchByTitle(id, query)

	if len(results) == 0 {
		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: id,
			Text:   "No matching history found.",
		}); err != nil {
			c.ctx.Logger.Error().Msg(err.Error())
		}
		return
	}

	// Send initial message
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: id,
		Text:   fmt.Sprintf("Search results for '%s':", query),
	}); err != nil {
		c.ctx.Logger.Error().Msg(err.Error())
		return
	}

	// Send results in batches
	batchSize := maxHistorySize
	for i := 0; i < len(results); i += batchSize {
		end := i + batchSize
		if end > len(results) {
			end = len(results)
		}

		var response strings.Builder
		for j, history := range results[i:end] {
			response.WriteString(fmt.Sprintf("%d. <a href=\"%s\">%s</a>\n", i+j+1, history.Url, history.Title))
		}

		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:    id,
			Text:      response.String(),
			ParseMode: models.ParseModeHTML,
		}); err != nil {
			c.ctx.Logger.Error().Msg(err.Error())
		}
	}

	// Send summary message if there are more results than maxHistorySize
	if len(results) > maxHistorySize {
		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: id,
			Text:   fmt.Sprintf("Displayed all %d results.", len(results)),
		}); err != nil {
			c.ctx.Logger.Error().Msg(err.Error())
		}
	}
}
