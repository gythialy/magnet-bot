package handler

import (
	"strings"

	"github.com/gythialy/magnet/pkg/constant"

	"github.com/go-telegram/bot/models"
)

// CommandSpec describes a bot command. It is the single source of truth for
// both SetMyCommands registration and the /start help text, so adding a
// command requires touching exactly one place.
type CommandSpec struct {
	Command     string
	Description string
	// Usage is the argument placeholder shown in help, e.g. "<k1,k2>".
	// Leave empty for commands without arguments.
	Usage     string
	AdminOnly bool
}

var commandSpecs = []CommandSpec{
	{Command: constant.Me, Description: "Show user information"},
	{Command: constant.AddKeyword, Description: "Add project monitoring keywords", Usage: "<k1,k2>"},
	{Command: constant.DeleteKeyword, Description: "Delete keywords by IDs, separated by commas", Usage: "<id1,id2>"},
	{Command: constant.EditKeyword, Description: "Edit keywords, eg: 1=keyword1; 2=keyword2", Usage: "id1=kw1;id2=kw2"},
	{Command: constant.AddAlarmKeyword, Description: "Add alarm monitoring keywords", Usage: "<k1,k2>"},
	{Command: constant.SearchAlarmRecords, Description: "Search alarm records by keyword", Usage: "<term>"},
	{Command: constant.SearchHistory, Description: "Search history records by title", Usage: "<term>"},
	{Command: constant.ListToday, Description: "List today's records"},
	{Command: constant.Statistics, Description: "Show statistics"},
	{Command: constant.ConvertPDF, Description: "Convert URL to PDF", Usage: "<url>"},
	{Command: constant.ConvertIMG, Description: "Convert URL to image", Usage: "<url>"},
	{Command: constant.Alarm, Description: "Get alarm details", Usage: "<id>"},
	{Command: constant.Retry, Description: "Retry failed tasks", AdminOnly: true},
	{Command: constant.Clean, Description: "Clean cache files", AdminOnly: true},
}

// TelegramCommands returns the command list for SetMyCommands. Admin-only
// commands are still exposed in the Telegram command menu (the handlers
// enforce the permission), matching the previous behaviour.
func TelegramCommands() []models.BotCommand {
	cmds := make([]models.BotCommand, 0, len(commandSpecs))
	for _, spec := range commandSpecs {
		desc := spec.Description
		if spec.AdminOnly {
			desc += " (admin only)"
		}
		cmds = append(cmds, models.BotCommand{
			Command:     spec.Command,
			Description: desc,
		})
	}
	return cmds
}

// HelpText generates the /start help message from the command registry.
func HelpText() string {
	var b strings.Builder
	b.WriteString("Here are the commands you can use:\n")
	for _, spec := range commandSpecs {
		line := spec.Command
		if spec.Usage != "" {
			line += " " + spec.Usage
		}
		line += " - " + spec.Description
		if spec.AdminOnly {
			line += " (admin only)"
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
