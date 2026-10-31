package handler

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/dal"
)

type ManagerHandler struct {
	ctx *BotContext
}

func NewManagerHandler(ctx *BotContext) *ManagerHandler {
	return &ManagerHandler{ctx: ctx}
}

func (h *ManagerHandler) Retry(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.Chat.ID
	// Admin rights come from the access-control tables, not from the legacy
	// single MANAGER_ID setting, so a promoted account works without a restart.
	if !h.ctx.IsAdmin(userId) {
		return
	}

	sentMsg, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: userId,
		Text:   "Processing, please wait...",
	})
	if err != nil {
		h.ctx.Logger.Error().Stack().Err(err).Msg("")
		return
	}

	// The bot context outlives this handler, so the push keeps running after
	// the update has been answered.
	go func() {
		h.ctx.processor.Get(userId)

		if _, err := b.EditMessageText(h.ctx.context(), &bot.EditMessageTextParams{
			ChatID:    userId,
			MessageID: sentMsg.ID,
			Text:      "Processing completed.",
		}); err != nil {
			h.ctx.Logger.Error().Stack().Err(err).Msg("")
		}
	}()
}

func (h *ManagerHandler) Clean(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	if !h.ctx.IsAdmin(id) {
		return
	}

	msg := "done."
	// 0 keeps the default retention window (see dal.defaultDays).
	if err := dal.History.Clean(0); err != nil {
		msg = err.Error()
	} else if err := dal.Alarm.Clean(); err != nil {
		msg = err.Error()
	}

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: id,
		Text:   msg,
	}); err != nil {
		h.ctx.Logger.Error().Stack().Err(err).Msg("")
	}
}
