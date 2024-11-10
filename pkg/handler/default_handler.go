package handler

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gythialy/magnet/pkg/constant"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/utils"
)

type defaultHandler struct {
	cmd *CommandsHandler
}

func (d *defaultHandler) Handler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Use the edited message if available, otherwise use the original message
	message := update.EditedMessage
	if message == nil {
		message = update.Message
	}

	userId := message.Chat.ID
	command := message.Text

	// Handle specific commands
	switch {
	case strings.HasPrefix(command, constant.ConvertPDF):
		d.cmd.ConvertURLToPDFHandler(ctx, b, update)
		return
	case strings.HasPrefix(command, constant.ConvertIMG):
		d.cmd.ConvertURLToIMGHandler(ctx, b, update)
		return
	}

	// Send a default message if no command is matched
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    userId,
		Text:      "<code>" + utils.ToString(update) + "</code>",
		ParseMode: models.ParseModeHTML,
		ReplyParameters: &models.ReplyParameters{
			MessageID: message.ID,
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
