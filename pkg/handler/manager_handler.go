package handler

import (
	"context"

	"github.com/gythialy/magnet/pkg/dal"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg"
)

type ManagerHandler struct {
	ctx *pkg.BotContext
}

func NewManagerHandler(ctx *pkg.BotContext) *ManagerHandler {
	return &ManagerHandler{
		ctx: ctx,
	}
}

func (h *ManagerHandler) Retry(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.Chat.ID
	if userId == h.ctx.ManagerId {
		// Send initial processing message
		sentMsg, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: userId,
			Text:   "Processing, please wait...",
		})
		if err != nil {
			h.ctx.Logger.Err(err)
			return
		}

		go func() {
			h.ctx.Processor.Get(userId)

			// Edit the message when processing is done
			if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID:    userId,
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
		if err := dal.History.Clean(0); err != nil {
			msg = err.Error()
		} else {
			if err := dal.Alarm.Clean(); err != nil {
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
