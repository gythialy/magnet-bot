package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/constant"
)

type startHandler struct {
	cmdHandler *CommandsHandler
}

func (s *startHandler) Handler(ctx context.Context, b *bot.Bot, update *models.Update) {
	m := update.Message
	command := strings.TrimSpace(strings.TrimPrefix(m.Text, constant.Start))
	switch {
	case strings.HasPrefix(command, constant.Alarm[1:]):
		split := strings.Split(command, "_")
		if len(split) == 2 {
			update.Message.Text = fmt.Sprintf("%s %s", constant.Alarm, strings.TrimSpace(split[1]))
			s.cmdHandler.AlarmRecordHandler(ctx, b, update)
		} else {
			s.cmdHandler.sendErrorMessage(ctx, b, update, fmt.Sprintf("invalid alarm %s", command))
		}
	default:
		s.sendHelpMessage(ctx, b, update)
	}
}

func (s *startHandler) sendHelpMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	helpText := "Here are the commands you can use:\n" +
		"/start - Start interacting with the bot\n" +
		"/alarm - Get alarm details\n" +
		"... (other commands)"

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    update.Message.Chat.ID,
		Text:      helpText,
		ParseMode: models.ParseModeHTML,
	}); err != nil {
		s.cmdHandler.ctx.Logger.Error().Err(err).Msg("Failed to send help message")
	}
}
