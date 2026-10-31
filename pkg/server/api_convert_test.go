package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gythialy/magnet/pkg/model"
)

const (
	convertTestHost     = "notices.example.com"
	convertTestURL      = "https://notices.example.com/notice/1"
	convertTestOtherURL = "https://notices.example.com/notice/2"
)

// startConversionCall is one recorded submission.
type startConversionCall struct {
	chatID         int64
	url            string
	kind           model.FileType
	replyMessageID int
}

// fakeStarter records submissions and can block them, so the in-flight dedupe
// branch is reachable: the second request must arrive while the first is still
// inside StartConversion.
type fakeStarter struct {
	mu    sync.Mutex
	calls []startConversionCall
	err   error

	started chan struct{}
	release chan struct{}
}

func newFakeStarter() *fakeStarter {
	return &fakeStarter{}
}

func (f *fakeStarter) StartConversion(_ context.Context, chatID int64, noticeURL string,
	kind model.FileType, replyMessageID int,
) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, startConversionCall{
		chatID:         chatID,
		url:            noticeURL,
		kind:           kind,
		replyMessageID: replyMessageID,
	})
	started, release := f.started, f.release
	f.mu.Unlock()

	if started != nil {
		started <- struct{}{}
	}
	if release != nil {
		<-release
	}

	if f.err != nil {
		return "", f.err
	}
	return "request-1", nil
}

func (f *fakeStarter) recorded() []startConversionCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]startConversionCall(nil), f.calls...)
}

// fakeLimiter records the keys it sees and returns a fixed verdict, so a test
// can prove both the 429 branch and the key format.
type fakeLimiter struct {
	mu      sync.Mutex
	allowed bool
	keys    []string
}

func (f *fakeLimiter) Allow(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.keys = append(f.keys, key)
	return f.allowed
}

func (f *fakeLimiter) seenKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.keys...)
}

// newConvertTestHandler mounts the handler with the production host and an
// optional limiter. The handler stays unwrapped on purpose: the tests inject
// identity directly, and the middleware has its own suite.
func newConvertTestHandler(starter ConversionStarter, limiter ConversionLimiter) http.Handler {
	return NewConvertHandler(ConvertConfig{
		Starter:     starter,
		AllowedHost: convertTestHost,
		Limiter:     limiter,
		Now:         func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
}

// seedOwnedNotice prepares the database with one notice that belongs to the
// test user, pointing the DAL at the per-test SQLite file.
func seedOwnedNotice(t *testing.T) {
	t.Helper()

	setupAPITestDB(t)
	seedHistories(t, &model.History{
		UserID:    testUserID,
		URL:       convertTestURL,
		UpdatedAt: time.Unix(1_700_000_000, 0),
		Title:     "Notice",
	})
}

// sendConvert performs one POST /api/convert. When authenticated is false the
// request is sent without the identity the middleware would inject.
func sendConvert(t *testing.T, handler http.Handler, body string, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/convert", strings.NewReader(body))
	if authenticated {
		req = req.WithContext(withInitDataUser(req.Context(), InitDataUser{ID: testUserID}))
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertJSONBody(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantBody string) {
	t.Helper()

	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, wantStatus, rec.Body.String())
	}
	if got := rec.Body.String(); got != wantBody {
		t.Fatalf("body = %q, want %q", got, wantBody)
	}
}

func TestConvertHandlerAcceptsConversion(t *testing.T) {
	tests := []struct {
		name   string
		format string
		kind   model.FileType
	}{
		{name: "pdf", format: "pdf", kind: model.PDF},
		{name: "png", format: "png", kind: model.IMG},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seedOwnedNotice(t)
			starter := newFakeStarter()
			limiter := &fakeLimiter{allowed: true}
			handler := newConvertTestHandler(starter, limiter)

			rec := sendConvert(t, handler,
				`{"url":"`+convertTestURL+`","format":"`+tt.format+`"}`, true)

			assertJSONBody(t, rec, http.StatusAccepted,
				`{"status":"accepted","format":"`+tt.format+`"}`)

			calls := starter.recorded()
			if len(calls) != 1 {
				t.Fatalf("StartConversion calls = %d, want 1", len(calls))
			}
			call := calls[0]
			if call.chatID != testUserID {
				t.Errorf("StartConversion chatID = %d, want %d", call.chatID, testUserID)
			}
			if call.url != convertTestURL {
				t.Errorf("StartConversion url = %q, want %q", call.url, convertTestURL)
			}
			if call.kind != tt.kind {
				t.Errorf("StartConversion kind = %d, want %d", call.kind, tt.kind)
			}
			// A Mini App conversion has no command message to answer.
			if call.replyMessageID != 0 {
				t.Errorf("StartConversion replyMessageID = %d, want 0", call.replyMessageID)
			}

			if keys := limiter.seenKeys(); len(keys) != 1 || keys[0] != strconv.FormatInt(testUserID, 10) {
				t.Errorf("limiter keys = %v, want [%s]", keys, strconv.FormatInt(testUserID, 10))
			}
		})
	}
}

func TestConvertHandlerRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid format", body: `{"url":"` + convertTestURL + `","format":"gif"}`},
		{name: "missing format", body: `{"url":"` + convertTestURL + `"}`},
		{name: "malformed url", body: `{"url":"://not-a-url","format":"pdf"}`},
		{name: "non http scheme", body: `{"url":"ftp://` + convertTestHost + `/notice/1","format":"pdf"}`},
		{name: "missing url", body: `{"format":"pdf"}`},
		{name: "malformed json", body: `{"url":`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupAPITestDB(t)
			starter := newFakeStarter()
			handler := newConvertTestHandler(starter, &fakeLimiter{allowed: true})

			rec := sendConvert(t, handler, tt.body, true)

			assertJSONBody(t, rec, http.StatusBadRequest, `{"error":{"code":"bad_request"}}`)
			if calls := starter.recorded(); len(calls) != 0 {
				t.Fatalf("StartConversion calls = %d, want 0 for a bad request", len(calls))
			}
		})
	}
}

func TestConvertHandlerRejectsForeignHost(t *testing.T) {
	setupAPITestDB(t)
	starter := newFakeStarter()
	handler := newConvertTestHandler(starter, &fakeLimiter{allowed: true})

	rec := sendConvert(t, handler, `{"url":"https://evil.example.com/notice/1","format":"pdf"}`, true)

	assertJSONBody(t, rec, http.StatusForbidden, `{"error":{"code":"forbidden"}}`)
	if calls := starter.recorded(); len(calls) != 0 {
		t.Fatalf("StartConversion calls = %d, want 0 for a foreign host", len(calls))
	}
}

func TestConvertHandlerRejectsUnownedNotice(t *testing.T) {
	setupAPITestDB(t)
	// The user owns a different notice on the same host.
	seedHistories(t, &model.History{
		UserID:    testUserID,
		URL:       convertTestOtherURL,
		UpdatedAt: time.Unix(1_700_000_000, 0),
		Title:     "Other notice",
	})
	starter := newFakeStarter()
	handler := newConvertTestHandler(starter, &fakeLimiter{allowed: true})

	rec := sendConvert(t, handler, `{"url":"`+convertTestURL+`","format":"pdf"}`, true)

	assertJSONBody(t, rec, http.StatusForbidden, `{"error":{"code":"forbidden"}}`)
	if calls := starter.recorded(); len(calls) != 0 {
		t.Fatalf("StartConversion calls = %d, want 0 for an unowned notice", len(calls))
	}
}

func TestConvertHandlerRateLimits(t *testing.T) {
	seedOwnedNotice(t)
	starter := newFakeStarter()
	limiter := &fakeLimiter{allowed: false}
	handler := newConvertTestHandler(starter, limiter)

	rec := sendConvert(t, handler, `{"url":"`+convertTestURL+`","format":"pdf"}`, true)

	assertJSONBody(t, rec, http.StatusTooManyRequests, `{"error":{"code":"too_many_requests"}}`)
	if calls := starter.recorded(); len(calls) != 0 {
		t.Fatalf("StartConversion calls = %d, want 0 when rate limited", len(calls))
	}
	if keys := limiter.seenKeys(); len(keys) != 1 || keys[0] != strconv.FormatInt(testUserID, 10) {
		t.Errorf("limiter keys = %v, want [%s]", keys, strconv.FormatInt(testUserID, 10))
	}
}

func TestConvertHandlerDeduplicatesInFlightConversion(t *testing.T) {
	seedOwnedNotice(t)
	starter := newFakeStarter()
	starter.started = make(chan struct{}, 1)
	starter.release = make(chan struct{})
	handler := newConvertTestHandler(starter, &fakeLimiter{allowed: true})
	body := `{"url":"` + convertTestURL + `","format":"pdf"}`

	// The first request blocks inside StartConversion, so it is still in
	// flight when the duplicate arrives.
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		first <- sendConvert(t, handler, body, true)
	}()

	select {
	case <-starter.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first conversion never reached StartConversion")
	}

	duplicate := sendConvert(t, handler, body, true)
	assertJSONBody(t, duplicate, http.StatusAccepted, `{"status":"converting"}`)

	close(starter.release)
	select {
	case rec := <-first:
		assertJSONBody(t, rec, http.StatusAccepted, `{"status":"accepted","format":"pdf"}`)
	case <-time.After(5 * time.Second):
		t.Fatal("the first request did not finish after the conversion returned")
	}

	if calls := starter.recorded(); len(calls) != 1 {
		t.Fatalf("StartConversion calls = %d, want 1 for a duplicate", len(calls))
	}
}

func TestConvertHandlerAllowsRepeatAfterSubmission(t *testing.T) {
	seedOwnedNotice(t)
	starter := newFakeStarter()
	handler := newConvertTestHandler(starter, &fakeLimiter{allowed: true})
	body := `{"url":"` + convertTestURL + `","format":"pdf"}`

	for range 2 {
		rec := sendConvert(t, handler, body, true)
		assertJSONBody(t, rec, http.StatusAccepted, `{"status":"accepted","format":"pdf"}`)
	}

	// The in-flight entry is released once the submission returns, so an
	// identical later request is a new conversion rather than a stale lockout.
	if calls := starter.recorded(); len(calls) != 2 {
		t.Fatalf("StartConversion calls = %d, want 2", len(calls))
	}
}

func TestConvertHandlerReportsConversionFailure(t *testing.T) {
	seedOwnedNotice(t)
	starter := newFakeStarter()
	starter.err = context.Canceled
	handler := newConvertTestHandler(starter, &fakeLimiter{allowed: true})

	rec := sendConvert(t, handler, `{"url":"`+convertTestURL+`","format":"pdf"}`, true)

	assertJSONBody(t, rec, http.StatusBadGateway, `{"error":{"code":"conversion_failed"}}`)
}

func TestConvertHandlerWithoutStarter(t *testing.T) {
	setupAPITestDB(t)
	handler := newConvertTestHandler(nil, nil)

	rec := sendConvert(t, handler, `{"url":"`+convertTestURL+`","format":"pdf"}`, true)

	assertJSONBody(t, rec, http.StatusBadGateway, `{"error":{"code":"conversion_failed"}}`)
}

func TestConvertHandlerRequiresAuthenticatedUser(t *testing.T) {
	setupAPITestDB(t)
	starter := newFakeStarter()
	handler := newConvertTestHandler(starter, &fakeLimiter{allowed: true})

	rec := sendConvert(t, handler, `{"url":"`+convertTestURL+`","format":"pdf"}`, false)

	assertJSONBody(t, rec, http.StatusUnauthorized, `{"error":{"code":"unauthorized"}}`)
	if calls := starter.recorded(); len(calls) != 0 {
		t.Fatalf("StartConversion calls = %d, want 0 without an authenticated user", len(calls))
	}
}

// TestConversionTrackerExpiresReservations pins the leak protection: an entry
// whose request died before releasing it must not block that conversion
// forever, and the sweep must remove it instead of leaving it in the map.
func TestConversionTrackerExpiresReservations(t *testing.T) {
	tracker := newConversionTracker()
	key := conversionKey{userID: testUserID, url: convertTestURL, format: convertFormatPDF}
	start := time.Unix(1_700_000_000, 0)

	if !tracker.begin(key, start) {
		t.Fatal("first reservation was rejected")
	}
	if tracker.begin(key, start.Add(convertInFlightTTL-time.Second)) {
		t.Fatal("a duplicate was accepted before the deadline")
	}

	// At the deadline the old entry is expired and must be swept, so the same
	// conversion can start again.
	if !tracker.begin(key, start.Add(convertInFlightTTL)) {
		t.Fatal("an expired reservation still blocked the conversion")
	}

	tracker.end(key)
	if !tracker.begin(key, start) {
		t.Fatal("releasing a reservation did not free the key")
	}

	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if len(tracker.active) != 1 {
		t.Fatalf("tracker holds %d entries, want 1 (expired entries must not accumulate)", len(tracker.active))
	}
}
