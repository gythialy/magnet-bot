package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/gythialy/magnet/pkg/config"
	"github.com/gythialy/magnet/pkg/utils"
	"github.com/rs/zerolog"
)

const testWebhookSecret = "test-secret"

// newTelegramWebhookTestContext builds the minimal BotContext the wrapper
// reads: only the secret and a logger.
func newTelegramWebhookTestContext(secret string) *BotContext {
	nopLogger := zerolog.Nop()
	return &BotContext{
		Logger: &utils.Logger{Logger: &nopLogger},
		Config: &config.ServiceConfig{WebhookSecret: secret},
	}
}

// webhookProbe stands in for the library handler. It records how often it was
// reached and which bytes it saw once the wrapper replayed the body, so the
// tests can pin both the rejection branches and the body hand-off.
type webhookProbe struct {
	calls int
	body  []byte
}

func (p *webhookProbe) handler(w http.ResponseWriter, r *http.Request) {
	p.calls++
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "probe could not read the body", http.StatusInternalServerError)
		return
	}
	p.body = body
	w.WriteHeader(http.StatusOK)
}

// countingBody counts reads, which proves the 403 branch never touches the
// request body.
type countingBody struct {
	reads int
	r     io.Reader
}

func (b *countingBody) Read(p []byte) (int, error) {
	b.reads++
	return b.r.Read(p)
}

func (b *countingBody) Close() error {
	return nil
}

func TestTelegramWebhookWithoutSecret(t *testing.T) {
	probe := &webhookProbe{}
	handler := newTelegramWebhookTestContext("").telegramWebhook(probe.handler)

	const payload = `{"update_id":1}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/telegram/webhook", strings.NewReader(payload))
	// Any header value must be ignored when no secret is configured.
	req.Header.Set(telegramSecretHeader, "not-the-secret")

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if probe.calls != 1 {
		t.Fatalf("next called %d times, want 1", probe.calls)
	}
	if string(probe.body) != payload {
		t.Fatalf("next read %q, want %q", probe.body, payload)
	}
}

func TestTelegramWebhookEnforcesSecret(t *testing.T) {
	const payload = `{"update_id":1}`

	tests := []struct {
		name       string
		header     string
		wantStatus int
		wantCalls  int
	}{
		{name: "missing header", header: "", wantStatus: http.StatusForbidden, wantCalls: 0},
		{name: "wrong secret", header: "wrong-secret", wantStatus: http.StatusForbidden, wantCalls: 0},
		{name: "correct secret", header: testWebhookSecret, wantStatus: http.StatusOK, wantCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := &webhookProbe{}
			handler := newTelegramWebhookTestContext(testWebhookSecret).telegramWebhook(probe.handler)

			body := &countingBody{r: strings.NewReader(payload)}
			req := httptest.NewRequest(http.MethodPost, "/telegram/webhook", nil)
			req.Body = body
			if tt.header != "" {
				req.Header.Set(telegramSecretHeader, tt.header)
			}

			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if probe.calls != tt.wantCalls {
				t.Fatalf("next called %d times, want %d", probe.calls, tt.wantCalls)
			}
			if tt.wantCalls == 0 && body.reads != 0 {
				t.Fatalf("body read %d times on a rejected request, want 0", body.reads)
			}
			if tt.wantCalls == 1 && string(probe.body) != payload {
				t.Fatalf("next read %q, want %q", probe.body, payload)
			}
		})
	}
}

func TestTelegramWebhookRejectsOversizedBody(t *testing.T) {
	probe := &webhookProbe{}
	handler := newTelegramWebhookTestContext(testWebhookSecret).telegramWebhook(probe.handler)

	body := bytes.Repeat([]byte("a"), telegramWebhookMaxBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/telegram/webhook", bytes.NewReader(body))
	req.Header.Set(telegramSecretHeader, testWebhookSecret)

	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if probe.calls != 0 {
		t.Fatalf("next called %d times for an oversized body, want 0", probe.calls)
	}
}

func TestTelegramWebhookAcceptsBodyAtLimit(t *testing.T) {
	probe := &webhookProbe{}
	handler := newTelegramWebhookTestContext(testWebhookSecret).telegramWebhook(probe.handler)

	body := bytes.Repeat([]byte("a"), telegramWebhookMaxBodyBytes)
	req := httptest.NewRequest(http.MethodPost, "/telegram/webhook", bytes.NewReader(body))
	req.Header.Set(telegramSecretHeader, testWebhookSecret)

	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(probe.body) != telegramWebhookMaxBodyBytes {
		t.Fatalf("next read %d bytes, want %d", len(probe.body), telegramWebhookMaxBodyBytes)
	}
}

// TestTelegramWebhookChecksSecretBeforeSize pins the order of the two checks:
// an unauthorised caller must not be able to make the process buffer a body,
// even an oversized one.
func TestTelegramWebhookChecksSecretBeforeSize(t *testing.T) {
	probe := &webhookProbe{}
	handler := newTelegramWebhookTestContext(testWebhookSecret).telegramWebhook(probe.handler)

	oversized := &countingBody{r: bytes.NewReader(bytes.Repeat([]byte("a"), telegramWebhookMaxBodyBytes+1))}
	req := httptest.NewRequest(http.MethodPost, "/telegram/webhook", nil)
	req.Body = oversized
	req.Header.Set(telegramSecretHeader, "wrong-secret")

	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if oversized.reads != 0 {
		t.Fatalf("body read %d times before the secret check, want 0", oversized.reads)
	}
	if probe.calls != 0 {
		t.Fatalf("next called %d times on a rejected request, want 0", probe.calls)
	}
}

// TestRegisterWebhookPublishesExpectedParams drives the registration loop
// against a local fake Bot API. It pins the parameter mapping and proves the
// loop returns on the first success instead of retrying a registration that
// already took effect.
func TestRegisterWebhookPublishesExpectedParams(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
		form  url.Values
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		calls++
		form = r.MultipartForm.Value
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}))
	defer srv.Close()

	telegramBot, err := bot.New("123:token", bot.WithServerURL(srv.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatalf("bot.New: %v", err)
	}

	nopLogger := zerolog.Nop()
	loopCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	webhookCtx := &BotContext{
		Bot:    telegramBot,
		Logger: &utils.Logger{Logger: &nopLogger},
		Config: &config.ServiceConfig{
			PublicBaseURL:          "https://bot.example.com",
			WebhookSecret:          testWebhookSecret,
			TelegramWebhookEnabled: true,
		},
		ctx:    loopCtx,
		cancel: cancel,
	}

	done := make(chan struct{})
	go func() {
		webhookCtx.registerWebhook()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		// A failed registration would retry every 30s; fail quickly instead of
		// letting the test time out.
		t.Fatal("registerWebhook kept retrying after setWebhook succeeded")
	}

	mu.Lock()
	defer mu.Unlock()

	if calls != 1 {
		t.Fatalf("setWebhook called %d times, want 1", calls)
	}

	wantForm := map[string]string{
		"url":             "https://bot.example.com/telegram/webhook",
		"secret_token":    testWebhookSecret,
		"max_connections": "40",
		"allowed_updates": `["message","callback_query","edited_message"]`,
	}
	for field, want := range wantForm {
		if got := form.Get(field); got != want {
			t.Errorf("setWebhook %s = %q, want %q", field, got, want)
		}
	}
}
