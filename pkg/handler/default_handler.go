package handler

import (
	"context"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/utiles"
)

const (
	ME = "/me"
)

func DefaultHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "/magnet append tracker servers",
	})
}

func MeHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chat, _ := b.GetChat(ctx, &bot.GetChatParams{
		ChatID: update.Message.Chat.ID,
	})
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   utiles.ToString(chat),
	})
}
