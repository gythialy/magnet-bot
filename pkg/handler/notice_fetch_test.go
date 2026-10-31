package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gythialy/magnet/pkg/model"
)

// newCountingNoticeServer answers every request with a notice page and counts
// the hits, so a test can prove a URL was rejected before it reached the wire.
func newCountingNoticeServer(t *testing.T, noticeHTML string) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, noticeHTML)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func serverHost(server *httptest.Server) string {
	return strings.TrimPrefix(server.URL, "http://")
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}
	return parsed
}

// TestExtractFileNameRejectsForeignHost proves the fetch re-checks the
// allow-list instead of trusting its caller: a URL for another host never
// reaches the network.
func TestExtractFileNameRejectsForeignHost(t *testing.T) {
	server, hits := newCountingNoticeServer(t, testConversionNoticeTitle)

	name, err := extractFileName(context.Background(), mustParseURL(t, server.URL+"/notice/1"), "notices.example.com")
	if err == nil {
		t.Fatalf("extractFileName() error = nil, name = %q", name)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("notice host hits = %d, want 0 for a rejected host", got)
	}
}

// TestExtractFileNameRefusesCrossHostRedirect proves the redirect guard: the
// allow-list names the server that may be contacted, so a redirect away from it
// is refused rather than followed.
func TestExtractFileNameRefusesCrossHostRedirect(t *testing.T) {
	target, targetHits := newCountingNoticeServer(t, testConversionNoticeTitle)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/notice/1", http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	_, err := extractFileName(context.Background(), mustParseURL(t, redirector.URL+"/notice/1"), serverHost(redirector))
	if err == nil {
		t.Fatal("extractFileName() error = nil, want the redirect to be refused")
	}
	if !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("extractFileName() error = %v, want a redirect rejection", err)
	}
	if got := targetHits.Load(); got != 0 {
		t.Fatalf("redirect target hits = %d, want 0", got)
	}
}

// TestExtractFileNameKeepsPathAndQuery pins that rebuilding the request from
// the checked parts still carries the notice link's path and query.
func TestExtractFileNameKeepsPathAndQuery(t *testing.T) {
	var gotURI atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI.Store(r.URL.RequestURI())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, testConversionNoticeTitle)
	}))
	t.Cleanup(server.Close)

	name, err := extractFileName(context.Background(),
		mustParseURL(t, server.URL+"/notice/42.html?from=list"), serverHost(server))
	if err != nil {
		t.Fatalf("extractFileName() error = %v", err)
	}
	if name != testConversionNoticeCode {
		t.Fatalf("extractFileName() = %q, want %q", name, testConversionNoticeCode)
	}
	if got, _ := gotURI.Load().(string); got != "/notice/42.html?from=list" {
		t.Fatalf("requested URI = %q, want the original path and query", got)
	}
}

// TestStartConversionRejectsCredentialURL pins that a notice URL carrying
// credentials is rejected before anything is fetched or submitted.
func TestStartConversionRejectsCredentialURL(t *testing.T) {
	notice, hits := newCountingNoticeServer(t, testConversionNoticeTitle)
	telegram := newFakeTelegram(t)
	gotenberg := newFakeGotenberg(t)
	botContext := newConversionTestContext(t, serverHost(notice), telegram.server.URL, gotenberg.server.URL)

	noticeURL := strings.Replace(notice.URL, "http://", "http://user:pass@", 1) + "/notice/1"
	requestID, err := botContext.StartConversion(context.Background(),
		testConversionChatID, noticeURL, model.PDF, testConversionReplyID)
	if err == nil {
		t.Fatal("StartConversion() error = nil, want the credentials to be rejected")
	}
	if requestID != "" {
		t.Fatalf("StartConversion() request id = %q, want empty", requestID)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("notice host hits = %d, want 0", got)
	}
	if urls := gotenberg.submittedURLs(); len(urls) != 0 {
		t.Fatalf("Gotenberg URLs = %v, want none", urls)
	}
	if calls := telegram.ofMethod("sendMessage"); len(calls) != 0 {
		t.Fatalf("sendMessage calls = %d, want 0", len(calls))
	}
}
