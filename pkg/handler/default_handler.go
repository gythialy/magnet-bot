package handler

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/utils"
)

func DefaultHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "/magnet append tracker servers",
	}); err != nil {
		slog.Error("Failed to send message", "error", err)
	}
}

func MeHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chat, _ := b.GetChat(ctx, &bot.GetChatParams{
		ChatID: update.Message.Chat.ID,
	})
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   utils.ToString(chat),
	}); err != nil {
		slog.Error("Failed to send message", "error", err)
	}
}
