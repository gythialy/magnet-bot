package handler

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/utils"
)

// Conversion fixture constants. The notice page returns a heading with a
// tender code so the file name derivation is exercised instead of falling back
// to the URL path, which would make the test pass for the wrong reason.
const (
	testConversionNoticeTitle = `<html><body><h1 class="info-title">Test Notice （2024-AB-C1）</h1></body></html>`
	testConversionNoticeCode  = "2024-AB-C1"
	testConversionChatID      = int64(12345)
	testConversionReplyID     = 55
	testConversionMessageID   = 77
)

// telegramCall is one recorded Bot API request.
type telegramCall struct {
	method string
	form   url.Values
}

// fakeTelegram is a local Bot API. It records every call and always answers
// with a message, so SendMessage/EditMessageText can run without a real token.
type fakeTelegram struct {
	server *httptest.Server

	mu    sync.Mutex
	calls []telegramCall
}

func newFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()

	fake := &fakeTelegram{}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeTelegram) handle(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	f.calls = append(f.calls, telegramCall{
		method: path.Base(r.URL.Path),
		form:   r.MultipartForm.Value,
	})
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w,
		`{"ok":true,"result":{"message_id":%d,"date":1700000000,"chat":{"id":%d,"type":"private"}}}`,
		testConversionMessageID, testConversionChatID)
}

// ofMethod returns the recorded calls for one Bot API method.
func (f *fakeTelegram) ofMethod(method string) []telegramCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	var matched []telegramCall
	for _, call := range f.calls {
		if call.method == method {
			matched = append(matched, call)
		}
	}
	return matched
}

// gotenbergRequest is one conversion job received by the fake Gotenberg.
type fakeGotenberg struct {
	server *httptest.Server

	mu     sync.Mutex
	urls   []string
	status int
	hooks  []string
}

func newFakeGotenberg(t *testing.T) *fakeGotenberg {
	t.Helper()

	fake := &fakeGotenberg{status: http.StatusNoContent}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeGotenberg) handle(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	f.urls = append(f.urls, r.FormValue("url"))
	// The webhook target is a request header, not a form field.
	f.hooks = append(f.hooks, r.Header.Get("Gotenberg-Webhook-Url"))
	status := f.status
	f.mu.Unlock()

	w.WriteHeader(status)
}

func (f *fakeGotenberg) setStatus(status int) {
	f.mu.Lock()
	f.status = status
	f.mu.Unlock()
}

func (f *fakeGotenberg) submittedURLs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.urls...)
}

// webhookURLs returns the callback URL of every submitted job. Its last path
// segment is the request id the service stores, which lets a test recover it
// without exporting Store internals.
func (f *fakeGotenberg) webhookURLs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.hooks...)
}

// newNoticePageServer serves the page extractFileName reads. The URL is local
// so the test never touches a real notice site.
func newNoticePageServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, testConversionNoticeTitle)
	}))
	t.Cleanup(server.Close)
	return server
}

// noticeHost is the host:port part a ServiceConfig.MessageServerUrl carries.
func noticeHost(server *httptest.Server) string {
	return strings.TrimPrefix(server.URL, "http://")
}

// newConversionTestContext builds the parts StartConversion needs: a bot
// pointed at the fake API, a Gotenberg client pointed at the fake converter and
// a store.
func newConversionTestContext(t *testing.T, host, telegramURL, gotenbergURL string) *BotContext {
	t.Helper()

	telegramBot, err := bot.New("123:token", bot.WithServerURL(telegramURL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatalf("bot.New: %v", err)
	}

	gotenberg, err := NewGotenbergClient(gotenbergURL, "http://callback.invalid", "")
	if err != nil {
		t.Fatalf("NewGotenbergClient: %v", err)
	}

	nopLogger := zerolog.Nop()
	return &BotContext{
		Bot:       telegramBot,
		Store:     NewStore[model.RequestInfo](),
		Logger:    &utils.Logger{Logger: &nopLogger},
		Config:    &config.ServiceConfig{MessageServerUrl: host},
		Gotenberg: gotenberg,
	}
}

func TestStartConversionSubmitsJobAndTracksRequest(t *testing.T) {
	tests := []struct {
		name     string
		kind     model.FileType
		label    string
		fileName string
	}{
		{name: "pdf", kind: model.PDF, label: "PDF", fileName: testConversionNoticeCode + ".pdf"},
		{name: "image", kind: model.IMG, label: "IMG", fileName: testConversionNoticeCode + ".png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notice := newNoticePageServer(t)
			telegram := newFakeTelegram(t)
			gotenberg := newFakeGotenberg(t)
			botContext := newConversionTestContext(t, noticeHost(notice), telegram.server.URL, gotenberg.server.URL)
			noticeURL := notice.URL + "/notice/42.html"

			requestID, err := botContext.StartConversion(context.Background(),
				testConversionChatID, noticeURL, tt.kind, testConversionReplyID)
			if err != nil {
				t.Fatalf("StartConversion() error = %v", err)
			}
			if requestID == "" {
				t.Fatal("StartConversion() returned an empty request id")
			}

			// The placeholder wording and the derived file name are the
			// command's user-visible contract, so they are asserted verbatim.
			sent := telegram.ofMethod("sendMessage")
			if len(sent) != 1 {
				t.Fatalf("sendMessage calls = %d, want 1", len(sent))
			}
			wantText := fmt.Sprintf("Converting URL to %s(%s). Please wait...⌛", tt.label, tt.fileName)
			if got := sent[0].form.Get("text"); got != wantText {
				t.Fatalf("placeholder = %q, want %q", got, wantText)
			}
			if got := sent[0].form.Get("chat_id"); got != fmt.Sprint(testConversionChatID) {
				t.Fatalf("placeholder chat_id = %q, want %d", got, testConversionChatID)
			}

			stored, found := botContext.Store.Get(requestID)
			if !found {
				t.Fatal("request id was not stored for the webhook")
			}
			if stored.ChatId != testConversionChatID {
				t.Errorf("stored ChatId = %d, want %d", stored.ChatId, testConversionChatID)
			}
			if stored.MessageId != testConversionMessageID {
				t.Errorf("stored MessageId = %d, want %d", stored.MessageId, testConversionMessageID)
			}
			if stored.ReplyMessageId != testConversionReplyID {
				t.Errorf("stored ReplyMessageId = %d, want %d", stored.ReplyMessageId, testConversionReplyID)
			}
			if stored.Message != noticeURL {
				t.Errorf("stored Message = %q, want %q", stored.Message, noticeURL)
			}
			if stored.FileName != tt.fileName {
				t.Errorf("stored FileName = %q, want %q", stored.FileName, tt.fileName)
			}
			if stored.Type != tt.kind {
				t.Errorf("stored Type = %d, want %d", stored.Type, tt.kind)
			}

			if submitted := gotenberg.submittedURLs(); len(submitted) != 1 || submitted[0] != noticeURL {
				t.Fatalf("Gotenberg URLs = %v, want [%s]", submitted, noticeURL)
			}
			if edits := telegram.ofMethod("editMessageText"); len(edits) != 0 {
				t.Fatalf("editMessageText calls = %d, want 0 on success", len(edits))
			}
		})
	}
}

func TestStartConversionEditsPlaceholderWhenGotenbergFails(t *testing.T) {
	notice := newNoticePageServer(t)
	telegram := newFakeTelegram(t)
	gotenberg := newFakeGotenberg(t)
	gotenberg.setStatus(http.StatusInternalServerError)
	botContext := newConversionTestContext(t, noticeHost(notice), telegram.server.URL, gotenberg.server.URL)

	requestID, err := botContext.StartConversion(context.Background(),
		testConversionChatID, notice.URL+"/notice/42.html", model.PDF, testConversionReplyID)
	if err == nil {
		t.Fatal("StartConversion() error = nil, want the Gotenberg status error")
	}
	if requestID != "" {
		t.Fatalf("StartConversion() request id = %q, want empty on failure", requestID)
	}

	edits := telegram.ofMethod("editMessageText")
	if len(edits) != 1 {
		t.Fatalf("editMessageText calls = %d, want 1", len(edits))
	}
	const wantText = "Converting to PDF failed. Please try again later."
	if got := edits[0].form.Get("text"); got != wantText {
		t.Fatalf("failure text = %q, want %q", got, wantText)
	}
	if got := edits[0].form.Get("message_id"); got != fmt.Sprint(testConversionMessageID) {
		t.Fatalf("failure edit message_id = %q, want %d", got, testConversionMessageID)
	}
}

func TestStartConversionRejectsForeignHost(t *testing.T) {
	telegram := newFakeTelegram(t)
	gotenberg := newFakeGotenberg(t)
	// The allowed host deliberately differs from the requested one.
	botContext := newConversionTestContext(t, "notices.example.com", telegram.server.URL, gotenberg.server.URL)

	requestID, err := botContext.StartConversion(context.Background(),
		testConversionChatID, "https://evil.example.com/notice/42.html", model.PDF, testConversionReplyID)
	if !errors.Is(err, ErrDomainNotAllowed) {
		t.Fatalf("StartConversion() error = %v, want ErrDomainNotAllowed", err)
	}
	if requestID != "" {
		t.Fatalf("StartConversion() request id = %q, want empty", requestID)
	}
	if calls := telegram.ofMethod("sendMessage"); len(calls) != 0 {
		t.Fatalf("sendMessage calls = %d, want 0 for a rejected host", len(calls))
	}
	if urls := gotenberg.submittedURLs(); len(urls) != 0 {
		t.Fatalf("Gotenberg URLs = %v, want none", urls)
	}
}

func TestStartConversionRejectsUnknownKind(t *testing.T) {
	notice := newNoticePageServer(t)
	telegram := newFakeTelegram(t)
	gotenberg := newFakeGotenberg(t)
	botContext := newConversionTestContext(t, noticeHost(notice), telegram.server.URL, gotenberg.server.URL)

	_, err := botContext.StartConversion(context.Background(),
		testConversionChatID, notice.URL+"/notice/42.html", model.FileType(99), testConversionReplyID)
	if err == nil {
		t.Fatal("StartConversion() error = nil, want an unsupported-type error")
	}
	if calls := telegram.ofMethod("sendMessage"); len(calls) != 0 {
		t.Fatalf("sendMessage calls = %d, want 0 for an unknown kind", len(calls))
	}
	if urls := gotenberg.submittedURLs(); len(urls) != 0 {
		t.Fatalf("Gotenberg URLs = %v, want none", urls)
	}
}

// newConversionCommandUpdate builds the minimal update the /convertpdf and
// /convertimg handlers parse: the URL follows the command in the message text.
func newConversionCommandUpdate(text string) *models.Update {
	return &models.Update{Message: &models.Message{
		ID:   9,
		Text: text,
		Chat: models.Chat{ID: testConversionChatID},
	}}
}

// TestCommandConvertRejectsInvalidURL pins the command's existing
// user-visible rejection: a URL that cannot be parsed is reported verbatim.
func TestCommandConvertRejectsInvalidURL(t *testing.T) {
	notice := newNoticePageServer(t)
	telegram := newFakeTelegram(t)
	gotenberg := newFakeGotenberg(t)
	botContext := newConversionTestContext(t, noticeHost(notice), telegram.server.URL, gotenberg.server.URL)

	NewCommandsHandler(botContext).ConvertURLToPDFHandler(context.Background(), botContext.Bot,
		newConversionCommandUpdate("/convertpdf ://not-a-url"))

	sent := telegram.ofMethod("sendMessage")
	if len(sent) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(sent))
	}
	if got := sent[0].form.Get("text"); got != "Error: Invalid URL format" {
		t.Fatalf("error text = %q, want %q", got, "Error: Invalid URL format")
	}
	if got := sent[0].form.Get("reply_parameters"); got != `{"message_id":9}` {
		t.Fatalf("error reply = %q, want the command message", got)
	}
	if urls := gotenberg.submittedURLs(); len(urls) != 0 {
		t.Fatalf("Gotenberg URLs = %v, want none", urls)
	}
}

// TestCommandConvertRejectsForeignHost pins the allow-list rejection, which the
// shared service now performs for the command as well.
func TestCommandConvertRejectsForeignHost(t *testing.T) {
	notice := newNoticePageServer(t)
	telegram := newFakeTelegram(t)
	gotenberg := newFakeGotenberg(t)
	botContext := newConversionTestContext(t, noticeHost(notice), telegram.server.URL, gotenberg.server.URL)

	NewCommandsHandler(botContext).ConvertURLToPDFHandler(context.Background(), botContext.Bot,
		newConversionCommandUpdate("/convertpdf https://evil.example.com/notice/1"))

	sent := telegram.ofMethod("sendMessage")
	if len(sent) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(sent))
	}
	if got := sent[0].form.Get("text"); got != "Error: URL domain is not allowed" {
		t.Fatalf("error text = %q, want %q", got, "Error: URL domain is not allowed")
	}
	if urls := gotenberg.submittedURLs(); len(urls) != 0 {
		t.Fatalf("Gotenberg URLs = %v, want none", urls)
	}
}

// TestCommandConvertUsesSharedService proves the command path still produces
// the documented placeholder, file name and reply relation through the shared
// service.
func TestCommandConvertUsesSharedService(t *testing.T) {
	notice := newNoticePageServer(t)
	telegram := newFakeTelegram(t)
	gotenberg := newFakeGotenberg(t)
	botContext := newConversionTestContext(t, noticeHost(notice), telegram.server.URL, gotenberg.server.URL)
	noticeURL := notice.URL + "/notice/42.html"

	NewCommandsHandler(botContext).ConvertURLToPDFHandler(context.Background(), botContext.Bot,
		newConversionCommandUpdate("/convertpdf "+noticeURL))

	sent := telegram.ofMethod("sendMessage")
	if len(sent) != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", len(sent))
	}
	wantText := fmt.Sprintf("Converting URL to PDF(%s.pdf). Please wait...⌛", testConversionNoticeCode)
	if got := sent[0].form.Get("text"); got != wantText {
		t.Fatalf("placeholder = %q, want %q", got, wantText)
	}

	hooks := gotenberg.webhookURLs()
	if len(hooks) != 1 {
		t.Fatalf("Gotenberg submissions = %d, want 1", len(hooks))
	}
	requestID := hooks[0][strings.LastIndex(hooks[0], "/")+1:]
	stored, found := botContext.Store.Get(requestID)
	if !found {
		t.Fatalf("request %q was not stored for the webhook", requestID)
	}
	if stored.ReplyMessageId != 9 {
		t.Fatalf("stored ReplyMessageId = %d, want the command message 9", stored.ReplyMessageId)
	}
	if stored.FileName != testConversionNoticeCode+".pdf" {
		t.Fatalf("stored FileName = %q, want %q", stored.FileName, testConversionNoticeCode+".pdf")
	}
}

// TestDeliverReplyParameters guards the Telegram rejection that used to lose
// Mini App conversions: message_id 0 must not be sent as a reply.
func TestDeliverReplyParameters(t *testing.T) {
	telegram := newFakeTelegram(t)
	gotenberg := newFakeGotenberg(t)
	botContext := newConversionTestContext(t, "notices.example.com", telegram.server.URL, gotenberg.server.URL)
	webhooker := newWebhooker(botContext)
	data := []byte("%PDF-1.7 synthetic notice")

	webhooker.deliver(model.RequestInfo{
		ChatId:         testConversionChatID,
		MessageId:      5,
		Message:        "notice",
		FileName:       "notice.pdf",
		Type:           model.PDF,
		ReplyMessageId: 0,
	}, data)

	sent := telegram.ofMethod("sendDocument")
	if len(sent) != 1 {
		t.Fatalf("sendDocument calls = %d, want 1", len(sent))
	}
	if got, found := sent[0].form["reply_parameters"]; found {
		t.Fatalf("reply_parameters = %v, want it omitted for message_id 0", got)
	}

	webhooker.deliver(model.RequestInfo{
		ChatId:         testConversionChatID,
		MessageId:      6,
		Message:        "notice",
		FileName:       "notice.pdf",
		Type:           model.PDF,
		ReplyMessageId: testConversionReplyID,
	}, data)

	sent = telegram.ofMethod("sendDocument")
	if len(sent) != 2 {
		t.Fatalf("sendDocument calls = %d, want 2", len(sent))
	}
	wantReply := fmt.Sprintf(`{"message_id":%d}`, testConversionReplyID)
	if got := sent[1].form.Get("reply_parameters"); got != wantReply {
		t.Fatalf("reply_parameters = %q, want %q", got, wantReply)
	}
}
