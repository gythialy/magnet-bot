package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/rs/zerolog"

	"github.com/gythialy/magnet/pkg/config"
	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/utils"
)

const (
	retiredTestAppBaseURL = "https://bot.example.com"
	retiredTestAppURL     = "https://bot.example.com/app/"
	retiredTestNoticeHost = "notices.example.com"
	retiredTestChatID     = int64(4242)
	retiredTestMessageID  = 77
)

// retiredCall is one recorded Bot API request.
type retiredCall struct {
	method string
	form   url.Values
}

// retiredAPI is a local Bot API used to observe what the handlers send. It
// answers sendMessage/editMessageText with a message and every other method
// with a plain boolean, which is what setMyCommands/setChatMenuButton decode.
type retiredAPI struct {
	server *httptest.Server

	mu      sync.Mutex
	calls   []retiredCall
	failFor map[string]bool
}

func newRetiredAPI(t *testing.T) *retiredAPI {
	t.Helper()

	api := &retiredAPI{failFor: make(map[string]bool)}
	api.server = httptest.NewServer(http.HandlerFunc(api.handle))
	t.Cleanup(api.server.Close)
	return api
}

// failMethod makes the next requests of one Bot API method return an error.
func (a *retiredAPI) failMethod(method string) {
	a.mu.Lock()
	a.failFor[method] = true
	a.mu.Unlock()
}

func (a *retiredAPI) handle(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	method := path.Base(r.URL.Path)
	a.mu.Lock()
	a.calls = append(a.calls, retiredCall{method: method, form: r.MultipartForm.Value})
	fail := a.failFor[method]
	a.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if fail {
		_, _ = io.WriteString(w, `{"ok":false,"error_code":500,"description":"synthetic failure"}`)
		return
	}
	switch method {
	case "sendMessage", "editMessageText":
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":77,"date":1700000000,`+
			`"chat":{"id":4242,"type":"private"}}}`)
	default:
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}
}

// ofMethod returns the recorded calls of one Bot API method.
func (a *retiredAPI) ofMethod(method string) []retiredCall {
	a.mu.Lock()
	defer a.mu.Unlock()

	var matched []retiredCall
	for _, call := range a.calls {
		if call.method == method {
			matched = append(matched, call)
		}
	}
	return matched
}

// newRetiredTestBot points a bot at the local API. WithSkipGetMe keeps bot.New
// from contacting anything beyond this test.
func newRetiredTestBot(t *testing.T, api *retiredAPI) *bot.Bot {
	t.Helper()

	telegramBot, err := bot.New("123:token", bot.WithServerURL(api.server.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatalf("bot.New: %v", err)
	}
	return telegramBot
}

// newRetiredTestContext builds the parts retirement reads: the Mini App flags
// and a logger. MessageServerUrl is set so the non-retired conversion path can
// reject a foreign host without any further fixture.
func newRetiredTestContext(miniApp bool, baseURL string) *BotContext {
	nopLogger := zerolog.Nop()
	return &BotContext{
		Logger: &utils.Logger{Logger: &nopLogger},
		Config: &config.ServiceConfig{
			MiniAppEnabled:   miniApp,
			PublicBaseURL:    baseURL,
			MessageServerUrl: retiredTestNoticeHost,
		},
	}
}

func retiredTestUpdate(text string) *models.Update {
	return &models.Update{Message: &models.Message{
		ID:   7,
		Text: text,
		Chat: models.Chat{ID: retiredTestChatID},
	}}
}

// recordingHandler is the "old implementation" stand-in: it only records that
// it ran, so a test can prove a retired command never reaches it.
func recordingHandler(called *bool) bot.HandlerFunc {
	return func(context.Context, *bot.Bot, *models.Update) {
		*called = true
	}
}

// replyMarkup decodes the reply_markup form field of one sendMessage call.
func replyMarkup(t *testing.T, call retiredCall) models.InlineKeyboardMarkup {
	t.Helper()

	var keyboard models.InlineKeyboardMarkup
	if err := json.Unmarshal([]byte(call.form.Get("reply_markup")), &keyboard); err != nil {
		t.Fatalf("decode reply_markup %q: %v", call.form.Get("reply_markup"), err)
	}
	return keyboard
}

func TestMiniAppEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		baseURL string
		want    bool
	}{
		{name: "enabled with public url", enabled: true, baseURL: retiredTestAppBaseURL, want: true},
		{name: "enabled without public url", enabled: true, baseURL: "", want: false},
		{name: "disabled with public url", enabled: false, baseURL: retiredTestAppBaseURL, want: false},
		{name: "disabled without public url", enabled: false, baseURL: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newRetiredTestContext(tt.enabled, tt.baseURL)
			if got := ctx.miniAppEnabled(); got != tt.want {
				t.Fatalf("miniAppEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRetiredCommandsOnlyAnswerWithPrompt walks the ten retired commands: each
// must answer once with its group prompt and the Mini App button, and the old
// handler must never run.
func TestRetiredCommandsOnlyAnswerWithPrompt(t *testing.T) {
	api := newRetiredAPI(t)
	telegramBot := newRetiredTestBot(t, api)
	ctx := newRetiredTestContext(true, retiredTestAppBaseURL)

	for command, prompt := range retiredCommands {
		t.Run(command[1:], func(t *testing.T) {
			before := len(api.ofMethod("sendMessage"))

			liveCalled := false
			handler := ctx.commandHandler(command, recordingHandler(&liveCalled))
			// The argument is part of the message for the prefix-dispatched
			// commands; a retired handler must ignore it.
			handler(context.Background(), telegramBot, retiredTestUpdate(command+" https://x.test/notice/1"))

			if liveCalled {
				t.Fatal("the old implementation ran for a retired command")
			}

			sent := api.ofMethod("sendMessage")
			if len(sent) != before+1 {
				t.Fatalf("sendMessage calls = %d, want %d", len(sent)-before, 1)
			}
			call := sent[len(sent)-1]
			if got := call.form.Get("text"); got != prompt {
				t.Fatalf("prompt = %q, want %q", got, prompt)
			}
			if got := call.form.Get("chat_id"); got != "4242" {
				t.Fatalf("chat_id = %q, want 4242", got)
			}

			keyboard := replyMarkup(t, call)
			if len(keyboard.InlineKeyboard) != 1 || len(keyboard.InlineKeyboard[0]) != 1 {
				t.Fatalf("keyboard = %+v, want one row with one button", keyboard.InlineKeyboard)
			}
			button := keyboard.InlineKeyboard[0][0]
			if button.Text != appButtonText {
				t.Errorf("button text = %q, want %q", button.Text, appButtonText)
			}
			if button.WebApp == nil {
				t.Fatal("button has no web_app payload")
			}
			if button.WebApp.URL != retiredTestAppURL {
				t.Errorf("button url = %q, want %q", button.WebApp.URL, retiredTestAppURL)
			}
		})
	}
}

// TestRetiredCommandsKeepLiveHandlerWithoutApp proves retirement is a Mini App
// decision: with the app off every retired command still reaches its handler.
func TestRetiredCommandsKeepLiveHandlerWithoutApp(t *testing.T) {
	api := newRetiredAPI(t)
	telegramBot := newRetiredTestBot(t, api)
	ctx := newRetiredTestContext(false, "")

	for command := range retiredCommands {
		t.Run(command[1:], func(t *testing.T) {
			before := len(api.ofMethod("sendMessage"))

			liveCalled := false
			handler := ctx.commandHandler(command, recordingHandler(&liveCalled))
			handler(context.Background(), telegramBot, retiredTestUpdate(command))

			if !liveCalled {
				t.Fatal("the live handler did not run while the Mini App is disabled")
			}
			if got := len(api.ofMethod("sendMessage")) - before; got != 0 {
				t.Fatalf("prompt messages = %d, want 0", got)
			}
		})
	}
}

// TestRetainedCommandsKeepLiveHandlerWithApp guards the other half of the
// matrix: retirement must not touch the commands the chat keeps.
func TestRetainedCommandsKeepLiveHandlerWithApp(t *testing.T) {
	api := newRetiredAPI(t)
	telegramBot := newRetiredTestBot(t, api)
	ctx := newRetiredTestContext(true, retiredTestAppBaseURL)

	for _, command := range []string{
		constant.Me, constant.Start, constant.Alarm, constant.Debug, constant.Retry, constant.Clean,
	} {
		t.Run(command[1:], func(t *testing.T) {
			before := len(api.ofMethod("sendMessage"))

			liveCalled := false
			handler := ctx.commandHandler(command, recordingHandler(&liveCalled))
			handler(context.Background(), telegramBot, retiredTestUpdate(command))

			if !liveCalled {
				t.Fatal("a retained command was retired")
			}
			if got := len(api.ofMethod("sendMessage")) - before; got != 0 {
				t.Fatalf("prompt messages = %d, want 0", got)
			}
		})
	}
}

// TestDefaultHandlerAppliesRetirementToPrefixDispatch covers the fallback
// dispatch in default_handler: it must answer with the prompt instead of
// running the old conversion code.
func TestDefaultHandlerAppliesRetirementToPrefixDispatch(t *testing.T) {
	api := newRetiredAPI(t)
	telegramBot := newRetiredTestBot(t, api)
	ctx := newRetiredTestContext(true, retiredTestAppBaseURL)
	ctx.cmdHandler = NewCommandsHandler(ctx)

	(&defaultHandler{cmd: ctx.cmdHandler}).Handler(context.Background(), telegramBot,
		retiredTestUpdate("/convertpdf https://evil.example.com/notice/1"))

	sent := api.ofMethod("sendMessage")
	if len(sent) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(sent))
	}
	if got := sent[0].form.Get("text"); got != retiredConversionPrompt {
		t.Fatalf("text = %q, want %q", got, retiredConversionPrompt)
	}
	if got := replyMarkup(t, sent[0]).InlineKeyboard[0][0].WebApp.URL; got != retiredTestAppURL {
		t.Fatalf("prompt button url = %q, want %q", got, retiredTestAppURL)
	}
}

// TestDefaultHandlerRunsOldDispatchWithoutApp is the control for the previous
// test: with the app off the same message takes the old conversion path, which
// rejects the foreign host.
func TestDefaultHandlerRunsOldDispatchWithoutApp(t *testing.T) {
	api := newRetiredAPI(t)
	telegramBot := newRetiredTestBot(t, api)
	ctx := newRetiredTestContext(false, "")
	ctx.cmdHandler = NewCommandsHandler(ctx)

	(&defaultHandler{cmd: ctx.cmdHandler}).Handler(context.Background(), telegramBot,
		retiredTestUpdate("/convertpdf https://evil.example.com/notice/1"))

	sent := api.ofMethod("sendMessage")
	if len(sent) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(sent))
	}
	if got := sent[0].form.Get("text"); got != "Error: URL domain is not allowed" {
		t.Fatalf("text = %q, want the old domain rejection", got)
	}
	if markup := sent[0].form.Get("reply_markup"); markup != "" {
		t.Fatalf("old dispatch unexpectedly sent a keyboard: %q", markup)
	}
}

func TestChatMenuButton(t *testing.T) {
	enabled := newRetiredTestContext(true, retiredTestAppBaseURL).chatMenuButton()
	if enabled == nil {
		t.Fatal("chatMenuButton() = nil while the Mini App is available")
	}
	if enabled.Type != models.MenuButtonTypeWebApp {
		t.Errorf("menu button type = %q, want %q", enabled.Type, models.MenuButtonTypeWebApp)
	}
	if enabled.Text != appMenuButtonText {
		t.Errorf("menu button text = %q, want %q", enabled.Text, appMenuButtonText)
	}
	if enabled.WebApp.URL != retiredTestAppURL {
		t.Errorf("menu button url = %q, want %q", enabled.WebApp.URL, retiredTestAppURL)
	}

	if got := newRetiredTestContext(false, "").chatMenuButton(); got != nil {
		t.Fatalf("chatMenuButton() = %+v without the Mini App, want nil", got)
	}
}

// TestInitBotPublishesFilteredCommandsAndMenu checks the startup wiring against
// a local Bot API: the command menu hides the retired commands and the chat
// menu button opens the Mini App.
func TestInitBotPublishesFilteredCommandsAndMenu(t *testing.T) {
	api := newRetiredAPI(t)
	ctx := newRetiredTestContext(true, retiredTestAppBaseURL)
	ctx.Bot = newRetiredTestBot(t, api)
	ctx.cmdHandler = NewCommandsHandler(ctx)

	if err := ctx.initBot(); err != nil {
		t.Fatalf("initBot() error = %v", err)
	}

	setCommands := api.ofMethod("setMyCommands")
	if len(setCommands) != 1 {
		t.Fatalf("setMyCommands calls = %d, want 1", len(setCommands))
	}
	var commands []models.BotCommand
	if err := json.Unmarshal([]byte(setCommands[0].form.Get("commands")), &commands); err != nil {
		t.Fatalf("decode commands: %v", err)
	}
	if want := len(commandSpecs) - len(retiredCommands); len(commands) != want {
		t.Fatalf("published commands = %d, want %d", len(commands), want)
	}
	for _, command := range commands {
		if isRetiredCommand(command.Command) {
			t.Errorf("setMyCommands still publishes retired command %s", command.Command)
		}
	}

	setMenu := api.ofMethod("setChatMenuButton")
	if len(setMenu) != 1 {
		t.Fatalf("setChatMenuButton calls = %d, want 1", len(setMenu))
	}
	var menuButton models.MenuButtonWebApp
	if err := json.Unmarshal([]byte(setMenu[0].form.Get("menu_button")), &menuButton); err != nil {
		t.Fatalf("decode menu_button: %v", err)
	}
	if menuButton.Type != models.MenuButtonTypeWebApp || menuButton.WebApp.URL != retiredTestAppURL {
		t.Fatalf("menu button = %+v, want a web_app button for %s", menuButton, retiredTestAppURL)
	}
}

// TestInitBotKeepsDefaultMenuWithoutApp is the control: no menu button is
// published and every command is listed.
func TestInitBotKeepsDefaultMenuWithoutApp(t *testing.T) {
	api := newRetiredAPI(t)
	ctx := newRetiredTestContext(false, "")
	ctx.Bot = newRetiredTestBot(t, api)
	ctx.cmdHandler = NewCommandsHandler(ctx)

	if err := ctx.initBot(); err != nil {
		t.Fatalf("initBot() error = %v", err)
	}

	setCommands := api.ofMethod("setMyCommands")
	if len(setCommands) != 1 {
		t.Fatalf("setMyCommands calls = %d, want 1", len(setCommands))
	}
	var commands []models.BotCommand
	if err := json.Unmarshal([]byte(setCommands[0].form.Get("commands")), &commands); err != nil {
		t.Fatalf("decode commands: %v", err)
	}
	if len(commands) != len(commandSpecs) {
		t.Fatalf("published commands = %d, want %d", len(commands), len(commandSpecs))
	}

	if calls := api.ofMethod("setChatMenuButton"); len(calls) != 0 {
		t.Fatalf("setChatMenuButton calls = %d, want 0 without the Mini App", len(calls))
	}
}

// TestInitBotSurvivesMenuButtonFailure pins the failure policy: the menu button
// is an affordance, so Telegram rejecting it must not abort startup after the
// command list was already published.
func TestInitBotSurvivesMenuButtonFailure(t *testing.T) {
	api := newRetiredAPI(t)
	api.failMethod("setChatMenuButton")
	ctx := newRetiredTestContext(true, retiredTestAppBaseURL)
	ctx.Bot = newRetiredTestBot(t, api)
	ctx.cmdHandler = NewCommandsHandler(ctx)

	if err := ctx.initBot(); err != nil {
		t.Fatalf("initBot() error = %v, want nil when only the menu button fails", err)
	}

	if got := len(api.ofMethod("setMyCommands")); got != 1 {
		t.Fatalf("setMyCommands calls = %d, want 1", got)
	}
	if got := len(api.ofMethod("setChatMenuButton")); got != 1 {
		t.Fatalf("setChatMenuButton calls = %d, want 1", got)
	}
}

// TestStartHandlerHelpFollowsMiniAppState checks the user-visible wiring: the
// /start help omits the retired commands and points at the app, while the
// chat-only deployment keeps the full command list.
func TestStartHandlerHelpFollowsMiniAppState(t *testing.T) {
	t.Run("app enabled", func(t *testing.T) {
		api := newRetiredAPI(t)
		telegramBot := newRetiredTestBot(t, api)
		ctx := newRetiredTestContext(true, retiredTestAppBaseURL)
		ctx.cmdHandler = NewCommandsHandler(ctx)

		(&startHandler{cmdHandler: ctx.cmdHandler}).Handler(context.Background(), telegramBot,
			retiredTestUpdate(constant.Start))

		sent := api.ofMethod("sendMessage")
		if len(sent) != 1 {
			t.Fatalf("sendMessage calls = %d, want 1", len(sent))
		}
		help := sent[0].form.Get("text")
		if !strings.Contains(help, miniAppHelpHint) {
			t.Errorf("help is missing the Mini App hint:\n%s", help)
		}
		if strings.Contains(help, constant.AddKeyword) {
			t.Errorf("help still lists a retired command:\n%s", help)
		}
	})

	t.Run("app disabled", func(t *testing.T) {
		api := newRetiredAPI(t)
		telegramBot := newRetiredTestBot(t, api)
		ctx := newRetiredTestContext(false, "")
		ctx.cmdHandler = NewCommandsHandler(ctx)

		(&startHandler{cmdHandler: ctx.cmdHandler}).Handler(context.Background(), telegramBot,
			retiredTestUpdate(constant.Start))

		sent := api.ofMethod("sendMessage")
		if len(sent) != 1 {
			t.Fatalf("sendMessage calls = %d, want 1", len(sent))
		}
		help := sent[0].form.Get("text")
		if strings.Contains(help, miniAppHelpHint) {
			t.Errorf("help points at the Mini App while it is disabled:\n%s", help)
		}
		if !strings.Contains(help, constant.AddKeyword) {
			t.Errorf("help is missing a live command:\n%s", help)
		}
	})
}
