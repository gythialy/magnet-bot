package handler

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/utils"
)

func DefaultHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	m := update.Message
	if m == nil {
		m = update.EditedMessage
	}
	userId := m.Chat.ID

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    userId,
		Text:      "<code>" + utils.ToString(update) + "</code>",
		ParseMode: models.ParseModeHTML,
		ReplyParameters: &models.ReplyParameters{
			MessageID: m.ID,
		},
	}); err != nil {
		slog.Error("Failed to send message", "error", err)
	}
}

func DebugHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.Chat.ID
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    userId,
		Text:      "<a href=\"https://t.me/magnet_bot?start=alarm_5\">Alarm 1</a>",
		ParseMode: models.ParseModeHTML,
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
