package handler

import (
	"context"

	"github.com/gythialy/magnet/pkg/entities"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg"
)

type ManagerHandler struct {
	ctx        *pkg.BotContext
	historyDao *entities.HistoryDao
	alarmDao   *entities.AlarmDao
}

func NewManagerHandler(ctx *pkg.BotContext) *ManagerHandler {
	return &ManagerHandler{
		ctx:        ctx,
		historyDao: entities.NewHistoryDao(ctx.DB),
		alarmDao:   entities.NewAlarmDao(ctx.DB),
	}
}

func (h *ManagerHandler) Retry(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	if id == h.ctx.ManagerId {
		// Send initial processing message
		sentMsg, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "Processing, please wait...",
		})
		if err != nil {
			h.ctx.Logger.Err(err)
			return
		}

		go func() {
			h.ctx.Processor.Get(id)

			// Edit the message when processing is done
			if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    update.Message.Chat.ID,
				MessageID: sentMsg.ID,
				Text:      "Processing completed.",
			}); err != nil {
				h.ctx.Logger.Err(err)
			}
		}()
	}
}

func (h *ManagerHandler) Clean(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	if id == h.ctx.ManagerId {
		msg := "done."
		if err := h.historyDao.Clean(); err != nil {
			msg = err.Error()
		} else {
			if err := h.alarmDao.Clean(); err != nil {
				msg = err.Error()
			}
		}
		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   msg,
		}); err != nil {
			h.ctx.Logger.Err(err)
		}
	}
}
