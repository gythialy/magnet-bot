package handler

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// SettingsCommand opens the administrator settings screen.
//
// The name lives here rather than in pkg/constant because the command was added
// together with its handler; it can move to the constants package once that file
// is next touched, and command_registry.go already references this constant.
const SettingsCommand = "/settings"

// Replies of the /settings command. The three cases are deliberately different:
// a non-administrator learns nothing about the deployment, an administrator is
// sent to the Mini App when it is available, and a chat-only deployment gets the
// environment variables that manage access there.
const (
	settingsAdminsOnlyText = "Admins only."
	settingsMiniAppText    = "Manage people and access in the Mini App."
	settingsDisabledText   = "Mini App is disabled. Manage access with MANAGER_ID / ALLOWED_USER_IDS."
	settingsAppButtonText  = "📱 Open settings"

	// settingsAppHashSuffix routes the Mini App to its own settings screen.
	// AppURL already ends with "/app/", so the result keeps the fragment.
	settingsAppHashSuffix = "#/settings"
)

// adminSettingsHandler answers /settings.
//
// The access middleware only rejects accounts outside the allow-list; it does
// not check roles. This handler therefore enforces the administrator role
// itself, and answers a regular (but authorised) account with "Admins only."
func (ctx *BotContext) adminSettingsHandler(hctx context.Context, b *bot.Bot, update *models.Update) {
	message := update.Message
	if message == nil && update.EditedMessage != nil {
		message = update.EditedMessage
	}
	if message == nil {
		return
	}

	// In a private chat the chat id and the account id are the same, but the
	// role check must be about the sender, not about where the message landed.
	userID := message.Chat.ID
	if message.From != nil {
		userID = message.From.ID
	}

	switch {
	case !ctx.IsAdmin(userID):
		ctx.sendAdminSettings(hctx, b, message.Chat.ID, settingsAdminsOnlyText, nil)
	case ctx.miniAppEnabled():
		ctx.sendAdminSettings(hctx, b, message.Chat.ID, settingsMiniAppText, ctx.settingsAppButton())
	default:
		ctx.sendAdminSettings(hctx, b, message.Chat.ID, settingsDisabledText, nil)
	}
}

// settingsAppButton builds the single-row keyboard that opens the settings
// screen of the Mini App.
func (ctx *BotContext) settingsAppButton() *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: settingsAppButtonText, WebApp: &models.WebAppInfo{URL: ctx.Config.AppURL() + settingsAppHashSuffix}}},
		},
	}
}

// sendAdminSettings sends one /settings reply. A nil keyboard means the reply
// has no action attached (either the caller may not use the screen or there is
// no screen to open).
func (ctx *BotContext) sendAdminSettings(hctx context.Context, b *bot.Bot, chatID int64, text string,
	keyboard *models.InlineKeyboardMarkup,
) {
	params := &bot.SendMessageParams{
		ChatID: chatID,
		Text:   text,
	}
	if keyboard != nil {
		params.ReplyMarkup = keyboard
	}

	if _, err := b.SendMessage(hctx, params); err != nil {
		ctx.Logger.Error().Stack().Err(err).Msg("send admin settings")
	}
}
