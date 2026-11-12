package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/utils"
)

const (
	contentTestHost = "notices.example.com"
	contentTestURL  = "https://notices.example.com/notice/1"
	contentTestBody = "<p>公告正文</p>"
)

// fakeNoticeFetcher stands in for the notice site: it records every call and
// answers with a fixed body or error, so a test can prove when the server went
// to the network and when it served the cache.
type fakeNoticeFetcher struct {
	calls int
	body  string
	err   error
}

func (f *fakeNoticeFetcher) fetch(_ context.Context, _, _ string) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return f.body, nil
}

// sendContent performs one GET /api/v1/notices/content signed for userID, so the
// real middleware runs. A userID of 0 sends no credentials at all.
func sendContent(t *testing.T, handler http.Handler, target string, userID int64) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	if userID != 0 {
		req.Header.Set("X-Telegram-Init-Data", apiTestInitData(t, time.Now(), userID))
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func contentQuery(noticeURL string, refresh bool) string {
	values := url.Values{}
	values.Set("url", noticeURL)
	if refresh {
		values.Set("refresh", "1")
	}
	return apiNoticeContentPath + "?" + values.Encode()
}

// seedOwnedContent prepares the database with one owned notice.
func seedOwnedContent(t *testing.T) {
	t.Helper()

	setupAPITestDB(t)
	seedHistories(t, &model.History{
		UserID:    testUserID,
		URL:       contentTestURL,
		UpdatedAt: time.Unix(1_700_000_000, 0),
		Title:     "Notice",
	})
}

func decodeContent(t *testing.T, rec *httptest.ResponseRecorder) noticeContentResponse {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload noticeContentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return payload
}

// TestNoticeContentServesCacheOnSecondOpen pins the caching contract: the first
// open fetches and stores, the second answers without touching the network.
func TestNoticeContentServesCacheOnSecondOpen(t *testing.T) {
	seedOwnedContent(t)
	now := time.Unix(1_700_000_000, 0)
	fetcher := &fakeNoticeFetcher{body: contentTestBody}
	handler := newAPIWithFetcher(t, APIConfig{BotToken: testBotToken, AllowedHost: contentTestHost}, fetcher, now)

	first := decodeContent(t, sendContent(t, handler, contentQuery(contentTestURL, false), testUserID))
	if first.Cached {
		t.Error("first open reported cached = true, want a fresh fetch")
	}
	if first.HTML != contentTestBody {
		t.Errorf("html = %q, want %q", first.HTML, contentTestBody)
	}

	second := decodeContent(t, sendContent(t, handler, contentQuery(contentTestURL, false), testUserID))
	if !second.Cached {
		t.Error("second open reported cached = false, want the stored body")
	}
	if second.HTML != contentTestBody {
		t.Errorf("html = %q, want %q", second.HTML, contentTestBody)
	}
	if fetcher.calls != 1 {
		t.Errorf("notice site fetches = %d, want 1", fetcher.calls)
	}
}

// TestNoticeContentRefreshBypassesCache proves refresh=1 re-fetches and replaces
// the stored body.
func TestNoticeContentRefreshBypassesCache(t *testing.T) {
	seedOwnedContent(t)
	now := time.Unix(1_700_000_000, 0)
	fetcher := &fakeNoticeFetcher{body: contentTestBody}
	handler := newAPIWithFetcher(t, APIConfig{BotToken: testBotToken, AllowedHost: contentTestHost}, fetcher, now)

	decodeContent(t, sendContent(t, handler, contentQuery(contentTestURL, false), testUserID))

	fetcher.body = "<p>刷新后的正文</p>"
	refreshed := decodeContent(t, sendContent(t, handler, contentQuery(contentTestURL, true), testUserID))
	if refreshed.Cached {
		t.Error("refresh reported cached = true, want a fresh fetch")
	}
	if refreshed.HTML != "<p>刷新后的正文</p>" {
		t.Errorf("html = %q, want the refreshed body", refreshed.HTML)
	}
	if fetcher.calls != 2 {
		t.Errorf("notice site fetches = %d, want 2", fetcher.calls)
	}

	stored, err := dal.GetNoticeContent(contentTestURL)
	if err != nil {
		t.Fatalf("GetNoticeContent() error = %v", err)
	}
	if stored.HTML != "<p>刷新后的正文</p>" {
		t.Errorf("stored html = %q, want the refreshed body", stored.HTML)
	}
}

// TestNoticeContentRefreshFailureKeepsStaleCopy guards the sheet the reader is
// looking at: a failed refresh answers the stored body marked stale instead of
// an error.
func TestNoticeContentRefreshFailureKeepsStaleCopy(t *testing.T) {
	seedOwnedContent(t)
	now := time.Unix(1_700_000_000, 0)
	fetcher := &fakeNoticeFetcher{body: contentTestBody}
	handler := newAPIWithFetcher(t, APIConfig{BotToken: testBotToken, AllowedHost: contentTestHost}, fetcher, now)

	decodeContent(t, sendContent(t, handler, contentQuery(contentTestURL, false), testUserID))

	fetcher.err = errors.New("upstream is down")
	stale := decodeContent(t, sendContent(t, handler, contentQuery(contentTestURL, true), testUserID))
	if !stale.Cached || !stale.Stale {
		t.Errorf("cached = %t, stale = %t; want true, true", stale.Cached, stale.Stale)
	}
	if stale.HTML != contentTestBody {
		t.Errorf("html = %q, want the stored body", stale.HTML)
	}
}

// TestNoticeContentFetchFailureWithoutCacheIsBadGateway covers the cold miss: no
// stored copy means there is nothing honest to answer with.
func TestNoticeContentFetchFailureWithoutCacheIsBadGateway(t *testing.T) {
	seedOwnedContent(t)
	now := time.Unix(1_700_000_000, 0)
	handler := newAPIWithFetcher(t, APIConfig{BotToken: testBotToken, AllowedHost: contentTestHost},
		&fakeNoticeFetcher{err: errors.New("404")}, now)

	rec := sendContent(t, handler, contentQuery(contentTestURL, false), testUserID)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
}

// TestNoticeContentRejectsUnownedNotice is the ownership check: the cache is
// keyed by URL, so without it one account could read another account's notice.
func TestNoticeContentRejectsUnownedNotice(t *testing.T) {
	setupAPITestDB(t)
	seedHistories(t, &model.History{
		UserID:    testUserID + 1,
		URL:       contentTestURL,
		UpdatedAt: time.Unix(1_700_000_000, 0),
		Title:     "Someone else's notice",
	})
	fetcher := &fakeNoticeFetcher{body: contentTestBody}
	handler := newAPIWithFetcher(t, APIConfig{BotToken: testBotToken, AllowedHost: contentTestHost},
		fetcher, time.Unix(1_700_000_000, 0))

	rec := sendContent(t, handler, contentQuery(contentTestURL, false), testUserID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if fetcher.calls != 0 {
		t.Errorf("notice site fetches = %d, want 0 for an unowned notice", fetcher.calls)
	}
}

// TestNoticeContentRejectsForeignHost keeps the endpoint from becoming a
// general-purpose fetcher.
func TestNoticeContentRejectsForeignHost(t *testing.T) {
	seedOwnedContent(t)
	fetcher := &fakeNoticeFetcher{body: contentTestBody}
	handler := newAPIWithFetcher(t, APIConfig{BotToken: testBotToken, AllowedHost: contentTestHost},
		fetcher, time.Unix(1_700_000_000, 0))

	for name, target := range map[string]string{
		"foreign host": contentQuery("https://evil.example.com/notice/1", false),
		"non-http":     contentQuery("ftp://notices.example.com/notice/1", false),
		"missing url":  apiNoticeContentPath,
	} {
		t.Run(name, func(t *testing.T) {
			rec := sendContent(t, handler, target, testUserID)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
	if fetcher.calls != 0 {
		t.Errorf("notice site fetches = %d, want 0", fetcher.calls)
	}
}

// TestNoticeContentRequiresAuthentication proves the endpoint is behind the
// same middleware as every other read.
func TestNoticeContentRequiresAuthentication(t *testing.T) {
	seedOwnedContent(t)
	handler := newAPIWithFetcher(t, APIConfig{BotToken: testBotToken, AllowedHost: contentTestHost},
		&fakeNoticeFetcher{body: contentTestBody}, time.Unix(1_700_000_000, 0))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, contentQuery(contentTestURL, false), nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// TestNoticeContentRateLimitsBoundsFetches keeps a client from turning the
// endpoint into a crawler: the budget covers outbound fetches only, so a cache
// hit still succeeds while the limit is spent.
func TestNoticeContentRateLimitsBoundsFetches(t *testing.T) {
	setupAPITestDB(t)
	seedHistories(t, &model.History{
		UserID:    testUserID,
		URL:       contentTestURL,
		UpdatedAt: time.Unix(1_700_000_000, 0),
		Title:     "Notice",
	})
	if err := dal.SaveNoticeContent(contentTestURL, contentTestBody, time.Unix(1_700_000_000, 0)); err != nil {
		t.Fatalf("SaveNoticeContent() error = %v", err)
	}

	fetcher := &fakeNoticeFetcher{body: contentTestBody}
	limiter := &fakeLimiter{allowed: false}
	handler := newAPIWithFetcherLimiter(t, APIConfig{BotToken: testBotToken, AllowedHost: contentTestHost},
		fetcher, limiter, time.Unix(1_700_000_000, 0))

	// A cache hit is free.
	if payload := decodeContent(t, sendContent(t, handler, contentQuery(contentTestURL, false), testUserID)); !payload.Cached {
		t.Error("cache hit reported cached = false")
	}

	// A forced refresh is an outbound fetch and is refused once the budget is
	// spent.
	rec := sendContent(t, handler, contentQuery(contentTestURL, true), testUserID)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if fetcher.calls != 0 {
		t.Errorf("notice site fetches = %d, want 0 while rate limited", fetcher.calls)
	}
	if keys := limiter.seenKeys(); len(keys) != 1 || keys[0] != "10001" {
		t.Errorf("limiter keys = %v, want [10001]", keys)
	}
}

// TestNoticeContentSanitizesFetchedPage runs the real fetcher against a fake
// notice site, so the parse-and-sanitize path is exercised end to end.
func TestNoticeContentSanitizesFetchedPage(t *testing.T) {
	setupAPITestDB(t)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><div class="pdfcontent">` +
			`<h1>公告标题</h1><p onclick="steal()">公告正文</p><script>alert(1)</script>` +
			`</div></body></html>`))
	}))
	t.Cleanup(site.Close)

	host := strings.TrimPrefix(site.URL, "http://")
	noticeURL := site.URL + "/notice/1"
	seedHistories(t, &model.History{
		UserID:    testUserID,
		URL:       noticeURL,
		UpdatedAt: time.Unix(1_700_000_000, 0),
		Title:     "Notice",
	})

	handler := newAPI(APIConfig{BotToken: testBotToken, AllowedHost: host},
		func() time.Time { return time.Unix(1_700_000_000, 0) })

	payload := decodeContent(t, sendContent(t, handler, contentQuery(noticeURL, false), testUserID))
	for _, want := range []string{"公告标题", "公告正文"} {
		if !strings.Contains(payload.HTML, want) {
			t.Errorf("html is missing %q: %s", want, payload.HTML)
		}
	}
	for _, unwanted := range []string{"onclick", "script", "alert("} {
		if strings.Contains(payload.HTML, unwanted) {
			t.Errorf("html carries %q: %s", unwanted, payload.HTML)
		}
	}

	// The stored copy is what the next open serves, so it must be sanitized too.
	stored, err := dal.GetNoticeContent(noticeURL)
	if err != nil {
		t.Fatalf("GetNoticeContent() error = %v", err)
	}
	if _, err := utils.ExtractNoticeBody(stored.HTML, ""); err == nil && strings.Contains(stored.HTML, "script") {
		t.Error("stored body is not sanitized")
	}
}

// TestNoticeContentCannotBeShadowedByNoticeList guards the routing: the content
// path is a longer literal than /notices, so ServeMux must prefer it.
func TestNoticeContentCannotBeShadowedByNoticeList(t *testing.T) {
	seedOwnedContent(t)
	now := time.Unix(1_700_000_000, 0)
	fetcher := &fakeNoticeFetcher{body: contentTestBody}
	handler := newAPIWithFetcher(t, APIConfig{BotToken: testBotToken, AllowedHost: contentTestHost}, fetcher, now)

	payload := decodeContent(t, sendContent(t, handler, contentQuery(contentTestURL, false), testUserID))
	if payload.URL != contentTestURL || payload.HTML != contentTestBody {
		t.Fatalf("content route answered %+v, want the notice body", payload)
	}
}

// newAPIWithFetcher builds the API with a stub fetcher so the notice endpoints
// never touch the network.
func newAPIWithFetcher(t *testing.T, cfg APIConfig, fetcher *fakeNoticeFetcher, now time.Time) http.Handler {
	t.Helper()
	return newAPIWithFetcherLimiter(t, cfg, fetcher, nil, now)
}

func newAPIWithFetcherLimiter(t *testing.T, cfg APIConfig, fetcher *fakeNoticeFetcher,
	limiter ConversionLimiter, now time.Time,
) http.Handler {
	t.Helper()

	// The production limiter is replaced only when a test supplies one; a nil
	// limiter would silently disable the fetch budget the endpoint promises.
	if limiter == nil {
		limiter = NewLimiter(noticeContentRateLimit, noticeContentRateWindow)
	}
	return newAPIWithNoticeSeams(cfg, func() time.Time { return now }, fetcher.fetch, limiter)
}
