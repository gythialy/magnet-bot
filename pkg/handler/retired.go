package handler

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gythialy/magnet/pkg/constant"
)

// Prompts shown for commands whose functionality moved into the Mini App. Each
// group answers the same way so the chat does not need to explain every command
// individually.
const (
	retiredStatisticsPrompt = "Statistics are available in the Mini App."
	retiredKeywordsPrompt   = "Keyword management is available in the Mini App."
	retiredAlarmsPrompt     = "Alarm keywords are available in the Mini App."
	retiredListsPrompt      = "Lists and search are available in the Mini App."
	retiredConversionPrompt = "PDF and PNG conversion is available in the Mini App."

	// appButtonText labels the inline button that opens the Mini App from a
	// retired-command reply.
	appButtonText = "📱 Open app"
	// appMenuButtonText labels the persistent chat menu button.
	appMenuButtonText = "Open app"
)

// retiredCommands is the single source of truth for command retirement: while
// the Mini App is available, a command listed here is answered with only its
// prompt, and it is hidden from the command menu and the help text.
var retiredCommands = map[string]string{
	constant.Statistics:         retiredStatisticsPrompt,
	constant.AddKeyword:         retiredKeywordsPrompt,
	constant.EditKeyword:        retiredKeywordsPrompt,
	constant.DeleteKeyword:      retiredKeywordsPrompt,
	constant.AddAlarmKeyword:    retiredAlarmsPrompt,
	constant.SearchAlarmRecords: retiredListsPrompt,
	constant.SearchHistory:      retiredListsPrompt,
	constant.ListToday:          retiredListsPrompt,
	constant.ConvertPDF:         retiredConversionPrompt,
	constant.ConvertIMG:         retiredConversionPrompt,
}

// retiredPrefixCommands are the retired commands the default handler can also
// see as prefixes: the old dispatch reads the argument (a URL or a search term)
// from the rest of the message.
var retiredPrefixCommands = []string{
	constant.ConvertPDF,
	constant.ConvertIMG,
	constant.SearchAlarmRecords,
	constant.SearchHistory,
}

// miniAppEnabled reports whether the Mini App is configured and reachable from
// Telegram. It is the only place that decides retirement, the command menu and
// the help text, so the three surfaces cannot disagree.
func (ctx *BotContext) miniAppEnabled() bool {
	if ctx == nil || ctx.Config == nil {
		return false
	}
	return ctx.Config.MiniAppEnabled && ctx.Config.PublicEnabled()
}

// retiredPrompt returns the replacement prompt for a retired command.
func retiredPrompt(command string) (string, bool) {
	prompt, retired := retiredCommands[command]
	return prompt, retired
}

// appButton returns the one-row keyboard that opens the Mini App.
func (ctx *BotContext) appButton() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: appButtonText, WebApp: &models.WebAppInfo{URL: ctx.Config.AppURL()}}},
		},
	}
}

// commandHandler chooses the handler registered for one command: while the Mini
// App is available a retired command gets the prompt-only handler, and every
// other command keeps its live implementation. Keeping the decision here means
// registration, the fallback dispatch and the tests all read one function.
func (ctx *BotContext) commandHandler(command string, live bot.HandlerFunc) bot.HandlerFunc {
	if ctx.miniAppEnabled() {
		if prompt, retired := retiredPrompt(command); retired {
			return ctx.retiredCommandHandler(prompt)
		}
	}
	return live
}

// retiredCommandHandler answers a retired command without running its old
// implementation.
func (ctx *BotContext) retiredCommandHandler(prompt string) bot.HandlerFunc {
	return func(hctx context.Context, b *bot.Bot, update *models.Update) {
		ctx.sendRetiredPrompt(hctx, b, update, prompt)
	}
}

// sendRetiredPrompt sends the prompt and the Mini App button. It is shared by
// the registered handler and the default-handler prefix guard so both answer a
// retired command identically.
func (ctx *BotContext) sendRetiredPrompt(hctx context.Context, b *bot.Bot, update *models.Update, prompt string) {
	message := update.Message
	if message == nil && update.EditedMessage != nil {
		message = update.EditedMessage
	}
	if message == nil {
		return
	}

	if _, err := b.SendMessage(hctx, &bot.SendMessageParams{
		ChatID:      message.Chat.ID,
		Text:        prompt,
		ReplyMarkup: ctx.appButton(),
	}); err != nil {
		ctx.Logger.Error().Stack().Err(err).Msg("send Mini App prompt")
	}
}

// chatMenuButton returns the Mini App menu button the chat header should offer,
// or nil when the default command menu must be kept. It is a pure decision so
// the wiring can be tested without contacting Telegram.
func (ctx *BotContext) chatMenuButton() *models.MenuButtonWebApp {
	if !ctx.miniAppEnabled() {
		return nil
	}
	return &models.MenuButtonWebApp{
		Type:   models.MenuButtonTypeWebApp,
		Text:   appMenuButtonText,
		WebApp: models.WebAppInfo{URL: ctx.Config.AppURL()},
	}
}
