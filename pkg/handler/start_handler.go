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

	// /start alarm_<id> deep-links straight to the alarm detail.
	if rest, ok := strings.CutPrefix(command, strings.TrimPrefix(constant.Alarm, "/")); ok {
		id, found := strings.CutPrefix(rest, "_")
		id = strings.TrimSpace(id)
		if !found || id == "" {
			s.cmdHandler.sendErrorMessage(ctx, b, update, fmt.Sprintf("invalid alarm %s", command))
			return
		}
		update.Message.Text = constant.Alarm + " " + id
		s.cmdHandler.AlarmRecordHandler(ctx, b, update)
		return
	}

	s.sendHelpMessage(ctx, b, update)
}

func (s *startHandler) sendHelpMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   HelpText(s.cmdHandler.ctx.miniAppEnabled()),
	}); err != nil {
		s.cmdHandler.ctx.Logger.Error().Err(err).Msg("Failed to send help message")
	}
}
