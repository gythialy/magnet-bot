package handler

import (
	"context"

	"github.com/gythialy/magnet/pkg/entities"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg"
)

type ManagerHandler struct {
	ctx     *pkg.BotContext
	history *entities.HistoryDao
	alarm   *entities.AlarmDao
}

func NewManagerHandler(ctx *pkg.BotContext) *ManagerHandler {
	return &ManagerHandler{
		ctx:     ctx,
		history: entities.NewHistoryDao(ctx.DB),
		alarm:   entities.NewAlarmDao(ctx.DB),
	}
}

func (h *ManagerHandler) Retry(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	if id == h.ctx.ManagerId {
		h.ctx.Processor.Get(id)
		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "Processing, please waiting...",
		}); err != nil {
			h.ctx.Logger.Err(err)
		}
	}
}

func (h *ManagerHandler) Clean(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	if id == h.ctx.ManagerId {
		msg := "done."
		if err := h.history.Clean(); err != nil {
			msg = err.Error()
		} else {
			if err := h.alarm.Clean(); err != nil {
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
