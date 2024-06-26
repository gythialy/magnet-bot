package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/gythialy/magnet/pkg/constant"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg"
	"github.com/gythialy/magnet/pkg/entities"
)

type CommandsHandler struct {
	ctx        *pkg.BotContext
	keywordDao *entities.KeywordDao
	alarmDao   *entities.AlarmDao
}

func NewCommandsHandler(ctx *pkg.BotContext) *CommandsHandler {
	db := ctx.DB
	return &CommandsHandler{
		ctx:        ctx,
		keywordDao: entities.NewKeywordDao(db),
		alarmDao:   entities.NewAlarmDao(db),
	}
}

func (c *CommandsHandler) addKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update, prefix string, t entities.KeywordType) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, prefix))
	keywords := strings.Split(tmp, ",")
	id := update.Message.Chat.ID
	result := c.keywordDao.Add(keywords, id, t)
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   fmt.Sprintf("%s: %s", prefix, result),
	}); err != nil {
		c.ctx.Logger.Error().Err(err)
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
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   fmt.Sprintf("All %s keywords: %s", t.String(), strings.Join(result, ", ")),
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
	var result strings.Builder
	if len(alarms) == 0 {
		result.WriteString("can not find any records...")
	} else {
		idx := 1
		for _, alarm := range alarms {
			result.WriteString(fmt.Sprintf("%d. #%s, %s to %s \n", idx, alarm.CreditName,
				alarm.StartDate.Format("2006-01-02"), alarm.EndDate.Format("2006-01-02")))
			idx++
		}
	}

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    update.Message.Chat.ID,
		Text:      result.String(),
		ParseMode: models.ParseModeHTML,
	}); err != nil {
		c.ctx.Logger.Error().Err(err)
	}
}
