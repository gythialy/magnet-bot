package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gythialy/magnet/pkg/model"
)

const (
	// settingsTestAppBaseURL is a synthetic public origin. AppURL appends
	// "/app/", so the settings deep link must end with "/app/#/settings".
	settingsTestAppBaseURL = "https://bot.example.com"
	settingsTestAppURL     = "https://bot.example.com/app/#/settings"
)

// settingsTestSetup returns a context with one administrator, one regular
// account and a fake Bot API. The access tests provide the fixtures, so this
// suite does not duplicate the fake server.
func settingsTestSetup(t *testing.T) (*BotContext, *bot.Bot, *accessMWAPI) {
	t.Helper()

	db := setupAccessMWDB(t)
	seedAccessMWUser(t, db, accessMWAdminID, model.RoleAdmin, true)
	seedAccessMWUser(t, db, accessMWAllowedID, model.RoleUser, true)
	setAccessMWMode(t, model.AccessModeOff)

	api := newAccessMWAPI(t)
	return newAccessMWContext(), newAccessMWBot(t, api), api
}

// settingsReplyMarkup decodes the keyboard of one sendMessage call, or returns
// nil when the reply carried none.
func settingsReplyMarkup(t *testing.T, form map[string][]string) *models.InlineKeyboardMarkup {
	t.Helper()

	raw := ""
	if values, ok := form["reply_markup"]; ok && len(values) > 0 {
		raw = values[0]
	}
	if raw == "" {
		return nil
	}

	var keyboard models.InlineKeyboardMarkup
	if err := json.Unmarshal([]byte(raw), &keyboard); err != nil {
		t.Fatalf("decode reply_markup %q: %v", raw, err)
	}
	return &keyboard
}

// TestAdminSettingsRejectsNonAdministrator proves the handler enforces the role
// itself: the access middleware lets an authorised regular account through, and
// /settings must still answer with "Admins only." and leak no app address.
func TestAdminSettingsRejectsNonAdministrator(t *testing.T) {
	ctx, telegramBot, api := settingsTestSetup(t)
	// The Mini App is available: without the role check this would produce the
	// deep link, so the test can detect a leaked address.
	ctx.Config.MiniAppEnabled = true
	ctx.Config.PublicBaseURL = settingsTestAppBaseURL

	ctx.adminSettingsHandler(context.Background(), telegramBot,
		accessMWPrivateMessage(accessMWAllowedID, SettingsCommand))

	sent := api.ofMethod("sendMessage")
	if len(sent) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(sent))
	}
	if got := sent[0].form.Get("text"); got != settingsAdminsOnlyText {
		t.Fatalf("text = %q, want %q", got, settingsAdminsOnlyText)
	}
	if keyboard := settingsReplyMarkup(t, sent[0].form); keyboard != nil {
		t.Fatalf("non-administrator received a keyboard: %+v", keyboard.InlineKeyboard)
	}
	for _, values := range sent[0].form {
		for _, value := range values {
			if strings.Contains(value, settingsTestAppBaseURL) {
				t.Fatalf("reply leaked the Mini App address: %q", value)
			}
		}
	}
}

// TestAdminSettingsOpensMiniApp covers the administrator reply while the Mini
// App is available, including the exact deep link.
func TestAdminSettingsOpensMiniApp(t *testing.T) {
	ctx, telegramBot, api := settingsTestSetup(t)
	ctx.Config.MiniAppEnabled = true
	ctx.Config.PublicBaseURL = settingsTestAppBaseURL

	ctx.adminSettingsHandler(context.Background(), telegramBot,
		accessMWPrivateMessage(accessMWAdminID, SettingsCommand))

	sent := api.ofMethod("sendMessage")
	if len(sent) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(sent))
	}
	if got := sent[0].form.Get("text"); got != settingsMiniAppText {
		t.Fatalf("text = %q, want %q", got, settingsMiniAppText)
	}

	keyboard := settingsReplyMarkup(t, sent[0].form)
	if keyboard == nil {
		t.Fatal("administrator reply has no keyboard")
	}
	if len(keyboard.InlineKeyboard) != 1 || len(keyboard.InlineKeyboard[0]) != 1 {
		t.Fatalf("keyboard = %+v, want one row with one button", keyboard.InlineKeyboard)
	}
	button := keyboard.InlineKeyboard[0][0]
	if button.Text != settingsAppButtonText {
		t.Errorf("button text = %q, want %q", button.Text, settingsAppButtonText)
	}
	if button.WebApp == nil {
		t.Fatal("button has no web_app payload")
	}
	if button.WebApp.URL != settingsTestAppURL {
		t.Errorf("button url = %q, want %q", button.WebApp.URL, settingsTestAppURL)
	}
}

// TestAdminSettingsWithoutMiniApp covers both ways the Mini App can be
// unavailable: disabled by flag, or enabled without a public base URL.
func TestAdminSettingsWithoutMiniApp(t *testing.T) {
	tests := []struct {
		name    string
		miniApp bool
		baseURL string
	}{
		{name: "disabled flag", miniApp: false, baseURL: settingsTestAppBaseURL},
		{name: "missing public url", miniApp: true, baseURL: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, telegramBot, api := settingsTestSetup(t)
			ctx.Config.MiniAppEnabled = tt.miniApp
			ctx.Config.PublicBaseURL = tt.baseURL

			ctx.adminSettingsHandler(context.Background(), telegramBot,
				accessMWPrivateMessage(accessMWAdminID, SettingsCommand))

			sent := api.ofMethod("sendMessage")
			if len(sent) != 1 {
				t.Fatalf("sendMessage calls = %d, want 1", len(sent))
			}
			if got := sent[0].form.Get("text"); got != settingsDisabledText {
				t.Fatalf("text = %q, want %q", got, settingsDisabledText)
			}
			if keyboard := settingsReplyMarkup(t, sent[0].form); keyboard != nil {
				t.Fatalf("chat-only reply carries a keyboard: %+v", keyboard.InlineKeyboard)
			}
		})
	}
}

// TestSettingsCommandWiredThroughInitBot proves initBot registers /settings on
// the real dispatch path: both an administrator and a regular account reach the
// handler, and the handler applies the role switch.
func TestSettingsCommandWiredThroughInitBot(t *testing.T) {
	db := setupAccessMWDB(t)
	seedAccessMWUser(t, db, accessMWAdminID, model.RoleAdmin, true)
	seedAccessMWUser(t, db, accessMWAllowedID, model.RoleUser, true)
	setAccessMWMode(t, model.AccessModeOff)

	api := newAccessMWAPI(t)
	telegramBot, err := bot.New("123:token",
		bot.WithServerURL(api.server.URL),
		bot.WithSkipGetMe(),
		bot.WithNotAsyncHandlers(),
	)
	if err != nil {
		t.Fatalf("bot.New: %v", err)
	}

	ctx := newAccessMWContext()
	ctx.Bot = telegramBot
	ctx.cmdHandler = NewCommandsHandler(ctx)
	ctx.Config.MiniAppEnabled = true
	ctx.Config.PublicBaseURL = settingsTestAppBaseURL

	if err := ctx.initBot(); err != nil {
		t.Fatalf("initBot() error = %v", err)
	}

	telegramBot.ProcessUpdate(context.Background(),
		accessMWPrivateMessage(accessMWAdminID, SettingsCommand))
	sent := api.ofMethod("sendMessage")
	if len(sent) != 1 {
		t.Fatalf("sendMessage calls = %d, want the administrator reply", len(sent))
	}
	if got := sent[0].form.Get("text"); got != settingsMiniAppText {
		t.Fatalf("administrator reply = %q, want %q", got, settingsMiniAppText)
	}

	telegramBot.ProcessUpdate(context.Background(),
		accessMWPrivateMessage(accessMWAllowedID, SettingsCommand))
	sent = api.ofMethod("sendMessage")
	if len(sent) != 2 {
		t.Fatalf("sendMessage calls = %d, want the regular-account reply too", len(sent))
	}
	if got := sent[1].form.Get("text"); got != settingsAdminsOnlyText {
		t.Fatalf("regular-account reply = %q, want %q", got, settingsAdminsOnlyText)
	}
}
