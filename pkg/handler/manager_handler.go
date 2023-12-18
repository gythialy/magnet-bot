package handler

import (
	"context"
	"log/slog"

	"github.com/gythialy/magnet/pkg/entities"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg"
)

const (
	RETRY = "/retry"
	CLEAN = "/clean"
)

type ManagerHandler struct {
	ctx     *pkg.BotContext
	history *entities.HistoryDao
}

func NewManagerHandler(ctx *pkg.BotContext) *ManagerHandler {
	return &ManagerHandler{
		ctx:     ctx,
		history: entities.NewHistoryDao(ctx.DB),
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
			slog.Error("%v", err)
		}
	}
}

func (h *ManagerHandler) Clean(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	if id == h.ctx.ManagerId {
		msg := "done."
		if err := h.history.Clean(); err != nil {
			msg = err.Error()
		}
		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   msg,
		}); err != nil {
			slog.Error("%v", err)
		}
	}
}
