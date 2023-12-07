package handler

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg"
)

const (
	RETRY = "/retry"
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
	id := update.Message.Chat.ID
	if id == h.ctx.ManagerId {
		h.ctx.Processor.Get(id)
	}
}
