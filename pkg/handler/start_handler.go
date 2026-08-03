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
		"/me - Show user information\n" +
		"/add_keywords <k1,k2> - Add project monitoring keywords\n" +
		"/delete_keywords <id1,id2> - Delete keywords by IDs\n" +
		"/edit_keywords id1=kw1;id2=kw2 - Edit keywords\n" +
		"/add_alarm_keywords <k1,k2> - Add alarm monitoring keywords\n" +
		"/search_alarm_records <term> - Search alarm records\n" +
		"/search_history_title <term> - Search history records\n" +
		"/list_today - List today's records\n" +
		"/statistics - Show statistics\n" +
		"/convertpdf <url> - Convert URL to PDF\n" +
		"/convertimg <url> - Convert URL to image\n" +
		"/alarm <id> - Get alarm details\n" +
		"/retry - Retry failed tasks (admin only)\n" +
		"/clean - Clean cache files (admin only)"

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    update.Message.Chat.ID,
		Text:      helpText,
		ParseMode: models.ParseModeHTML,
	}); err != nil {
		s.cmdHandler.ctx.Logger.Error().Err(err).Msg("Failed to send help message")
	}
}
