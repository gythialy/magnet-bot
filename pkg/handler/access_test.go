package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/gythialy/magnet/pkg/config"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/server"
	"github.com/gythialy/magnet/pkg/utils"
)

// Synthetic accounts used by the access-middleware tests. They are far away
// from the ids other handler tests use, so a leaked row is easy to spot.
const (
	accessMWAdminID    int64 = 9101
	accessMWAllowedID  int64 = 9102
	accessMWStrangerID int64 = 9103
)

// accessMWCall is one recorded Bot API request.
type accessMWCall struct {
	method string
	form   url.Values
}

// accessMWAPI is a local Bot API serving the methods the access middleware
// calls. It records every request and can serve a webhook info snapshot.
type accessMWAPI struct {
	server *httptest.Server

	mu          sync.Mutex
	calls       []accessMWCall
	webhookInfo models.WebhookInfo
	failWebhook bool
}

func newAccessMWAPI(t *testing.T) *accessMWAPI {
	t.Helper()

	api := &accessMWAPI{
		webhookInfo: models.WebhookInfo{
			URL:                "https://bot.example.com/telegram/webhook",
			PendingUpdateCount: 3,
			LastErrorMessage:   "synthetic delivery failure",
		},
	}
	api.server = httptest.NewServer(http.HandlerFunc(api.handle))
	t.Cleanup(api.server.Close)
	return api
}

func (a *accessMWAPI) handle(w http.ResponseWriter, r *http.Request) {
	form := url.Values{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		form = r.MultipartForm.Value
	}

	method := path.Base(r.URL.Path)
	a.mu.Lock()
	a.calls = append(a.calls, accessMWCall{method: method, form: form})
	info, fail := a.webhookInfo, a.failWebhook
	a.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch method {
	case "getWebhookInfo":
		if fail {
			_, _ = io.WriteString(w, `{"ok":false,"error_code":500,"description":"synthetic failure"}`)
			return
		}
		body, err := json.Marshal(struct {
			OK     bool               `json:"ok"`
			Result models.WebhookInfo `json:"result"`
		}{OK: true, Result: info})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	case "sendMessage", "editMessageText":
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":77,"date":1700000000,`+
			`"chat":{"id":1,"type":"private"}}}`)
	default:
		// answerCallbackQuery and friends decode a plain boolean.
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}
}

// ofMethod returns the recorded calls of one Bot API method.
func (a *accessMWAPI) ofMethod(method string) []accessMWCall {
	a.mu.Lock()
	defer a.mu.Unlock()

	var matched []accessMWCall
	for _, call := range a.calls {
		if call.method == method {
			matched = append(matched, call)
		}
	}
	return matched
}

func (a *accessMWAPI) setWebhookFailure(fail bool) {
	a.mu.Lock()
	a.failWebhook = fail
	a.mu.Unlock()
}

// newAccessMWBot points a bot at the local API without contacting Telegram.
func newAccessMWBot(t *testing.T, api *accessMWAPI) *bot.Bot {
	t.Helper()

	telegramBot, err := bot.New("123:token", bot.WithServerURL(api.server.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatalf("bot.New: %v", err)
	}
	return telegramBot
}

func newAccessMWContext() *BotContext {
	nopLogger := zerolog.Nop()
	return &BotContext{
		Logger: &utils.Logger{Logger: &nopLogger},
		Config: &config.ServiceConfig{},
	}
}

// setupAccessMWDB opens a throwaway database with the access tables and points
// the DAL at it.
func setupAccessMWDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/access_mw.db"), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Setting{}); err != nil {
		t.Fatalf("migrate access tables: %v", err)
	}
	dal.SetDefault(db)
	dal.User.Invalidate()

	t.Cleanup(func() {
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func seedAccessMWUser(t *testing.T, db *gorm.DB, userID int64, role int32, enabled bool) {
	t.Helper()

	now := time.Now().Truncate(time.Second)
	row := &model.User{
		UserID:    userID,
		Role:      role,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create user %d: %v", userID, err)
	}
	dal.User.Invalidate()
}

func setAccessMWMode(t *testing.T, mode string) {
	t.Helper()

	if err := dal.SetSetting(model.SettingAccessMode, mode); err != nil {
		t.Fatalf("set access mode %q: %v", mode, err)
	}
}

// setupRestrictedAccessMW returns a restricted deployment with one enabled
// administrator and one enabled regular user, plus the fake Bot API they talk
// to.
func setupRestrictedAccessMW(t *testing.T) (*gorm.DB, *BotContext, *bot.Bot, *accessMWAPI) {
	t.Helper()

	db := setupAccessMWDB(t)
	seedAccessMWUser(t, db, accessMWAdminID, model.RoleAdmin, true)
	seedAccessMWUser(t, db, accessMWAllowedID, model.RoleUser, true)
	setAccessMWMode(t, model.AccessModeRestricted)

	api := newAccessMWAPI(t)
	return db, newAccessMWContext(), newAccessMWBot(t, api), api
}

func accessMWPrivateMessage(userID int64, text string) *models.Update {
	return &models.Update{Message: &models.Message{
		ID:   1,
		Text: text,
		From: &models.User{ID: userID, Username: fmt.Sprintf("user%d", userID), FirstName: "Test", LastName: "Account"},
		Chat: models.Chat{ID: userID, Type: models.ChatTypePrivate},
	}}
}

func accessMWGroupMessage(userID int64, text string) *models.Update {
	update := accessMWPrivateMessage(userID, text)
	update.Message.Chat = models.Chat{ID: -100123456, Type: models.ChatTypeSupergroup}
	return update
}

func accessMWCallback(userID int64, chatType models.ChatType) *models.Update {
	return &models.Update{CallbackQuery: &models.CallbackQuery{
		ID:   "callback-1",
		From: models.User{ID: userID, Username: fmt.Sprintf("user%d", userID), FirstName: "Test"},
		Message: models.MaybeInaccessibleMessage{
			Type:    models.MaybeInaccessibleMessageTypeMessage,
			Message: &models.Message{ID: 5, Chat: models.Chat{ID: userID, Type: chatType}},
		},
	}}
}

// runAccessMW executes the middleware once and reports how often the wrapped
// handler ran.
func runAccessMW(t *testing.T, ctx *BotContext, telegramBot *bot.Bot, update *models.Update) int {
	t.Helper()

	nextCalls := 0
	next := func(context.Context, *bot.Bot, *models.Update) {
		nextCalls++
	}
	ctx.accessMiddleware(next)(context.Background(), telegramBot, update)
	return nextCalls
}

func TestAccessMiddlewareAllowsAuthorizedAccounts(t *testing.T) {
	tests := []struct {
		name   string
		userID int64
	}{
		{name: "administrator", userID: accessMWAdminID},
		{name: "allowed user", userID: accessMWAllowedID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ctx, telegramBot, api := setupRestrictedAccessMW(t)

			if calls := runAccessMW(t, ctx, telegramBot, accessMWPrivateMessage(tt.userID, "/start")); calls != 1 {
				t.Fatalf("next calls = %d, want 1", calls)
			}
			if sent := api.ofMethod("sendMessage"); len(sent) != 0 {
				t.Fatalf("authorised account received a denial message: %v", sent[0].form)
			}

			// The first update from an account refreshes last_seen.
			row, ok := dal.User.Get(tt.userID)
			if !ok {
				t.Fatalf("user %d disappeared", tt.userID)
			}
			if row.LastSeen == nil {
				t.Fatalf("last_seen was not recorded for %d", tt.userID)
			}
		})
	}
}

func TestAccessMiddlewareDeniesStrangerMessageOnce(t *testing.T) {
	_, ctx, telegramBot, api := setupRestrictedAccessMW(t)

	if calls := runAccessMW(t, ctx, telegramBot, accessMWPrivateMessage(accessMWStrangerID, "hello")); calls != 0 {
		t.Fatalf("next calls = %d, want 0 for a stranger", calls)
	}

	sent := api.ofMethod("sendMessage")
	if len(sent) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(sent))
	}
	if got := sent[0].form.Get("text"); got != accessDeniedMessageText {
		t.Fatalf("denial text = %q, want %q", got, accessDeniedMessageText)
	}
	if got := sent[0].form.Get("chat_id"); got != strconv.FormatInt(accessMWStrangerID, 10) {
		t.Fatalf("denial chat_id = %q, want %d", got, accessMWStrangerID)
	}

	// A second attempt inside the ten-minute window is silent.
	if calls := runAccessMW(t, ctx, telegramBot, accessMWPrivateMessage(accessMWStrangerID, "hello again")); calls != 0 {
		t.Fatalf("next calls = %d, want 0", calls)
	}
	if got := len(api.ofMethod("sendMessage")); got != 1 {
		t.Fatalf("sendMessage calls = %d, want 1 (the notice is throttled)", got)
	}

	denied := ctx.RecentDenied()
	if len(denied) != 2 {
		t.Fatalf("RecentDenied() = %d entries, want 2 attempts", len(denied))
	}
	last := denied[len(denied)-1]
	if last.UserID != accessMWStrangerID || last.Username != "user9103" || last.Name != "Test Account" {
		t.Fatalf("denied entry = %+v, want the stranger's identity", last)
	}
	if last.At.IsZero() {
		t.Fatal("denied entry has no timestamp")
	}
}

func TestAccessMiddlewareAnswersDeniedCallback(t *testing.T) {
	_, ctx, telegramBot, api := setupRestrictedAccessMW(t)

	if calls := runAccessMW(t, ctx, telegramBot, accessMWCallback(accessMWStrangerID, models.ChatTypePrivate)); calls != 0 {
		t.Fatalf("next calls = %d, want 0 for a denied callback", calls)
	}

	answers := api.ofMethod("answerCallbackQuery")
	if len(answers) != 1 {
		t.Fatalf("answerCallbackQuery calls = %d, want 1", len(answers))
	}
	if got := answers[0].form.Get("callback_query_id"); got != "callback-1" {
		t.Fatalf("answered callback = %q, want callback-1", got)
	}
	if got := answers[0].form.Get("text"); got != accessDeniedCallbackText {
		t.Fatalf("callback text = %q, want %q", got, accessDeniedCallbackText)
	}
	if sent := api.ofMethod("sendMessage"); len(sent) != 0 {
		t.Fatalf("denied callback also sent a message: %v", sent[0].form)
	}
	if denied := ctx.RecentDenied(); len(denied) != 1 || denied[0].UserID != accessMWStrangerID {
		t.Fatalf("RecentDenied() = %+v, want the denied callback", denied)
	}
}

func TestAccessMiddlewareIgnoresGroupChats(t *testing.T) {
	_, ctx, telegramBot, api := setupRestrictedAccessMW(t)

	// Even the administrator gets no answer in a group: the bot is private-only.
	if calls := runAccessMW(t, ctx, telegramBot, accessMWGroupMessage(accessMWAdminID, "/start")); calls != 0 {
		t.Fatalf("next calls = %d, want 0 in a group chat", calls)
	}
	if sent := api.ofMethod("sendMessage"); len(sent) != 0 {
		t.Fatalf("group chat received a reply: %v", sent[0].form)
	}
	if denied := ctx.RecentDenied(); len(denied) != 0 {
		t.Fatalf("a group message was recorded as a denied account: %+v", denied)
	}
}

func TestAccessMiddlewareAllowsEveryoneWhenAccessIsOff(t *testing.T) {
	setupAccessMWDB(t)
	setAccessMWMode(t, model.AccessModeOff)
	api := newAccessMWAPI(t)
	ctx, telegramBot := newAccessMWContext(), newAccessMWBot(t, api)

	if calls := runAccessMW(t, ctx, telegramBot, accessMWPrivateMessage(accessMWStrangerID, "hello")); calls != 1 {
		t.Fatalf("next calls = %d, want 1 while access control is off", calls)
	}
	if sent := api.ofMethod("sendMessage"); len(sent) != 0 {
		t.Fatalf("denial message sent while access control is off: %v", sent[0].form)
	}
	// A stranger has no row, so the throttled last_seen write reports "not
	// found" and must be ignored rather than failing the update.
	if denied := ctx.RecentDenied(); len(denied) != 0 {
		t.Fatalf("RecentDenied() = %+v, want none", denied)
	}
}

func TestAccessMiddlewareThrottlesLastSeen(t *testing.T) {
	db, ctx, telegramBot, _ := setupRestrictedAccessMW(t)

	if calls := runAccessMW(t, ctx, telegramBot, accessMWPrivateMessage(accessMWAllowedID, "first")); calls != 1 {
		t.Fatalf("next calls = %d, want 1", calls)
	}
	row, ok := dal.User.Get(accessMWAllowedID)
	if !ok || row.LastSeen == nil {
		t.Fatal("first message did not record last_seen")
	}

	// Replace the stored value with a sentinel: a second write would overwrite
	// it, a throttled one must leave it untouched.
	sentinel := time.Unix(1_000_000, 0)
	if err := db.Model(&model.User{}).Where("user_id = ?", accessMWAllowedID).
		UpdateColumn("last_seen", sentinel).Error; err != nil {
		t.Fatalf("write sentinel last_seen: %v", err)
	}

	if calls := runAccessMW(t, ctx, telegramBot, accessMWPrivateMessage(accessMWAllowedID, "second")); calls != 1 {
		t.Fatalf("next calls = %d, want 1", calls)
	}
	row, ok = dal.User.Get(accessMWAllowedID)
	if !ok || row.LastSeen == nil {
		t.Fatal("last_seen disappeared")
	}
	if got := row.LastSeen.Unix(); got != sentinel.Unix() {
		t.Fatalf("last_seen = %v, want the sentinel %v (write must be throttled)", row.LastSeen, sentinel)
	}
}

func TestAccessMiddlewareDeniedRingBufferKeepsNewest(t *testing.T) {
	_, ctx, telegramBot, _ := setupRestrictedAccessMW(t)

	// Sixty different strangers: the history must keep only the newest fifty.
	for i := range 60 {
		userID := int64(i + 1)
		update := accessMWPrivateMessage(userID, "spam")
		runAccessMW(t, ctx, telegramBot, update)
	}

	denied := ctx.RecentDenied()
	if len(denied) != accessDeniedLimit {
		t.Fatalf("RecentDenied() = %d entries, want %d", len(denied), accessDeniedLimit)
	}
	if denied[0].UserID != 11 || denied[len(denied)-1].UserID != 60 {
		t.Fatalf("kept user ids %d..%d, want the newest 11..60", denied[0].UserID, denied[len(denied)-1].UserID)
	}

	// Concurrent denials must not corrupt the history.
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Go(func() {
			userID := int64(1000 + i)
			ctx.accessMiddleware(func(context.Context, *bot.Bot, *models.Update) {
				t.Error("denied update reached the next handler")
			})(context.Background(), telegramBot, accessMWPrivateMessage(userID, "spam"))
		})
	}
	wg.Wait()

	denied = ctx.RecentDeniedEntries()
	if len(denied) != accessDeniedLimit {
		t.Fatalf("RecentDeniedEntries() = %d entries, want %d", len(denied), accessDeniedLimit)
	}
	seen := make(map[int64]bool, len(denied))
	for _, entry := range denied {
		if entry.UserID < 1000 || entry.UserID >= 1100 {
			t.Fatalf("kept an old entry %d, want only the concurrent batch", entry.UserID)
		}
		if seen[entry.UserID] {
			t.Fatalf("duplicate denial entry for %d", entry.UserID)
		}
		seen[entry.UserID] = true
	}
}

func TestIsAdmin(t *testing.T) {
	db := setupAccessMWDB(t)
	seedAccessMWUser(t, db, accessMWAdminID, model.RoleAdmin, true)
	seedAccessMWUser(t, db, accessMWAllowedID, model.RoleUser, true)
	seedAccessMWUser(t, db, 9104, model.RoleAdmin, false)
	setAccessMWMode(t, model.AccessModeOff)

	ctx := newAccessMWContext()
	for _, tt := range []struct {
		name   string
		userID int64
		want   bool
	}{
		{name: "enabled administrator", userID: accessMWAdminID, want: true},
		{name: "regular user", userID: accessMWAllowedID, want: false},
		{name: "disabled administrator", userID: 9104, want: false},
		{name: "unknown account", userID: 9199, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ctx.IsAdmin(tt.userID); got != tt.want {
				t.Fatalf("IsAdmin(%d) = %v, want %v", tt.userID, got, tt.want)
			}
		})
	}
}

// TestAccessMiddlewareWiredThroughBotOptions proves the registration path used
// by NewBotContext: a bot created with WithMiddlewares(accessMiddleware) rejects
// an unauthorised update before the matched handler runs.
func TestAccessMiddlewareWiredThroughBotOptions(t *testing.T) {
	_, ctx, _, api := setupRestrictedAccessMW(t)

	telegramBot, err := bot.New("123:token",
		bot.WithServerURL(api.server.URL),
		bot.WithSkipGetMe(),
		bot.WithNotAsyncHandlers(),
		bot.WithMiddlewares(ctx.accessMiddleware),
	)
	if err != nil {
		t.Fatalf("bot.New: %v", err)
	}

	nextCalls := 0
	telegramBot.RegisterHandler(bot.HandlerTypeMessageText, "/me", bot.MatchTypePrefix,
		func(context.Context, *bot.Bot, *models.Update) { nextCalls++ })

	telegramBot.ProcessUpdate(context.Background(), accessMWPrivateMessage(accessMWStrangerID, "/me"))
	if nextCalls != 0 {
		t.Fatalf("handler calls = %d, want 0 for an unauthorised update", nextCalls)
	}
	if sent := api.ofMethod("sendMessage"); len(sent) != 1 {
		t.Fatalf("sendMessage calls = %d, want the denial notice", len(sent))
	}

	telegramBot.ProcessUpdate(context.Background(), accessMWPrivateMessage(accessMWAdminID, "/me"))
	if nextCalls != 1 {
		t.Fatalf("handler calls = %d, want 1 for an administrator", nextCalls)
	}
}

func TestWebhookSnapshot(t *testing.T) {
	api := newAccessMWAPI(t)
	ctx := newAccessMWContext()
	ctx.Bot = newAccessMWBot(t, api)

	url, lastError, pending, ok := ctx.WebhookSnapshot(context.Background())
	if !ok {
		t.Fatal("WebhookSnapshot() ok = false, want the configured snapshot")
	}
	if url != "https://bot.example.com/telegram/webhook" || lastError != "synthetic delivery failure" || pending != 3 {
		t.Fatalf("WebhookSnapshot() = (%q, %q, %d), want the fake snapshot", url, lastError, pending)
	}

	api.setWebhookFailure(true)
	if _, _, _, ok := ctx.WebhookSnapshot(context.Background()); ok {
		t.Fatal("WebhookSnapshot() ok = true after getWebhookInfo failed")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, ok := ctx.WebhookSnapshot(cancelled); ok {
		t.Fatal("WebhookSnapshot() ok = true with a cancelled context")
	}
}

// TestLogAccessBootstrap checks the startup log policy: restricted names the
// administrator count and off is a warning.
func TestLogAccessBootstrap(t *testing.T) {
	db := setupAccessMWDB(t)
	seedAccessMWUser(t, db, accessMWAdminID, model.RoleAdmin, true)

	var buf bytes.Buffer
	zlogger := zerolog.New(&buf)
	logger := &utils.Logger{Logger: &zlogger}

	logAccessBootstrap(logger, model.AccessModeRestricted, []int64{accessMWAdminID})
	restricted := buf.String()
	infoLine, debugLine := "", ""
	for line := range strings.SplitSeq(restricted, "\n") {
		if strings.Contains(line, "access control is restricted") {
			infoLine = line
		}
		if strings.Contains(line, "manager_ids") {
			debugLine = line
		}
	}
	if infoLine == "" || !strings.Contains(infoLine, `"administrators":1`) {
		t.Fatalf("restricted info line does not report the administrator count: %q", restricted)
	}
	if strings.Contains(infoLine, strconv.FormatInt(accessMWAdminID, 10)) {
		t.Fatalf("restricted info line leaks the administrator id: %q", infoLine)
	}
	// The ids are available for debugging, just not on the info line.
	if !strings.Contains(debugLine, strconv.FormatInt(accessMWAdminID, 10)) {
		t.Fatalf("restricted debug line does not list the seeded ids: %q", restricted)
	}

	buf.Reset()
	logAccessBootstrap(logger, model.AccessModeOff, nil)
	if got := buf.String(); !strings.Contains(got, "access control is off: no administrator configured") {
		t.Fatalf("off log = %q", got)
	}
}

// TestManagerCommandsRequireAdministrator guards the IsAdmin switch: a regular
// account gets the old silent treatment from /retry and /clean.
func TestManagerCommandsRequireAdministrator(t *testing.T) {
	db := setupAccessMWDB(t)
	seedAccessMWUser(t, db, accessMWAllowedID, model.RoleUser, true)
	setAccessMWMode(t, model.AccessModeOff)

	api := newAccessMWAPI(t)
	ctx, telegramBot := newAccessMWContext(), newAccessMWBot(t, api)
	manager := NewManagerHandler(ctx)

	update := accessMWPrivateMessage(accessMWAllowedID, "/clean")
	manager.Clean(context.Background(), telegramBot, update)
	manager.Retry(context.Background(), telegramBot, update)

	if sent := api.ofMethod("sendMessage"); len(sent) != 0 {
		t.Fatalf("a non-administrator got a reply: %v", sent[0].form)
	}
}

// TestSystemStatus covers the AdminStatusProvider projection used by the admin
// status screen: denied accounts are copied over and the webhook snapshot is
// only reported in webhook mode.
func TestSystemStatus(t *testing.T) {
	_, ctx, telegramBot, api := setupRestrictedAccessMW(t)
	ctx.Bot = telegramBot

	// One denied attempt is enough to show up in the status.
	runAccessMW(t, ctx, telegramBot, accessMWPrivateMessage(accessMWStrangerID, "hello"))

	// Polling mode: there is no webhook to report.
	status := ctx.SystemStatus(context.Background())
	if status.Webhook != nil {
		t.Fatalf("SystemStatus().Webhook = %+v in polling mode, want nil", status.Webhook)
	}
	if len(status.RecentDenied) != 1 {
		t.Fatalf("SystemStatus().RecentDenied = %+v, want one entry", status.RecentDenied)
	}
	denied := status.RecentDenied[0]
	if denied.UserID != accessMWStrangerID || denied.Username != "user9103" || denied.Name != "Test Account" || denied.At.IsZero() {
		t.Fatalf("SystemStatus().RecentDenied[0] = %+v, want the denied account", denied)
	}
	if got := server.DeniedUser(denied); got.UserID != denied.UserID {
		t.Fatalf("DeniedUser projection lost the user id: %+v", got)
	}

	// Webhook mode: the fake getWebhookInfo snapshot is exposed.
	ctx.Config.PublicBaseURL = "https://bot.example.com"
	ctx.Config.TelegramWebhookEnabled = true
	status = ctx.SystemStatus(context.Background())
	if status.Webhook == nil {
		t.Fatal("SystemStatus().Webhook = nil in webhook mode")
	}
	if status.Webhook.URL != "https://bot.example.com/telegram/webhook" ||
		status.Webhook.LastError != "synthetic delivery failure" || status.Webhook.Pending != 3 {
		t.Fatalf("SystemStatus().Webhook = %+v, want the fake snapshot", status.Webhook)
	}

	// A failed lookup leaves the section empty instead of failing the screen.
	api.setWebhookFailure(true)
	if got := ctx.SystemStatus(context.Background()).Webhook; got != nil {
		t.Fatalf("SystemStatus().Webhook = %+v after a failed lookup, want nil", got)
	}
}
