package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/utils"
)

const (
	// noticeContentFetchTimeout bounds one notice page fetch. The endpoint is
	// synchronous — the sheet shows a skeleton until the body arrives — so the
	// bound has to stay inside a phone's patience, not a crawler's.
	noticeContentFetchTimeout = 12 * time.Second

	// noticeContentBodyLimit bounds the fetched page before it is parsed. Notice
	// pages are a few tens of kilobytes; this stops a wrong host or a
	// pathological response from being read into memory.
	noticeContentBodyLimit = 4 << 20

	// noticeContentRateLimit and noticeContentRateWindow bound outbound fetches
	// per user: every cache miss and every forced refresh is one request to the
	// notice site. Cache hits are free and do not consume the budget.
	noticeContentRateLimit  = 10
	noticeContentRateWindow = time.Minute
)

// noticeContentUserAgent identifies the bot like the crawler does, so the notice
// server sees one client rather than an anonymous scraper.
const noticeContentUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:98.0) Gecko/20100101 Firefox/98.0"

// noticeContentResponse is the payload of GET /api/v1/notices/content.
//
// Cached distinguishes a stored body from the one fetched for this request, and
// Stale marks a stored body served after a failed refresh. FetchedAt is RFC3339
// so the client can say how old the copy is.
type noticeContentResponse struct {
	URL       string `json:"url"`
	HTML      string `json:"html"`
	Cached    bool   `json:"cached"`
	Stale     bool   `json:"stale"`
	FetchedAt string `json:"fetchedAt,omitempty"`
}

// noticeContentFetcher retrieves and sanitizes one notice page. It is a field on
// apiHandler so tests can stand in for the notice site without network access.
type noticeContentFetcher func(ctx context.Context, noticeURL, baseURL string) (string, error)

// noticeContentStatusError carries the notice server's HTTP status.
type noticeContentStatusError struct{ status int }

func (e noticeContentStatusError) Error() string {
	return "notice page answered " + http.StatusText(e.status)
}

// keepNoticeHostRedirect refuses a redirect that leaves the host the request
// started on. The allow-list names the notice server, not wherever that server
// redirects to; following one would let it point this fetch at an unrelated and
// possibly internal address.
//
// It duplicates pkg/handler's guard on purpose: this package must not import
// pkg/handler (the composition root imports both), and the two entry points
// must not drift to different rules.
func keepNoticeHostRedirect(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return errors.New("redirect to a non-http scheme is not allowed")
	}
	if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
		return errors.New("redirect to another host is not allowed")
	}
	return nil
}

// fetchNoticePage is the production fetcher: the caller has already validated
// the URL against the allow-list, so this only has to fetch and sanitize.
func fetchNoticePage(ctx context.Context, noticeURL, baseURL string) (string, error) {
	client := &http.Client{
		Timeout: noticeContentFetchTimeout,
		// Reuse the conversion fetch's guard: the allow-list names the host that
		// may be contacted, not wherever it redirects to.
		CheckRedirect: keepNoticeHostRedirect,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, noticeURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", noticeContentUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", noticeContentStatusError{status: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, noticeContentBodyLimit+1))
	if err != nil {
		return "", err
	}
	if len(body) > noticeContentBodyLimit {
		return "", utils.ErrNoticeBodyTooLarge
	}

	return utils.ExtractNoticeBody(string(body), baseURL)
}

// handleNoticeContent serves the full text of one notice the caller received.
//
// The order is a security contract, not an optimisation:
//  1. the URL must be an absolute http(s) URL on the configured notice host;
//  2. it must exist in the caller's own history. The cache is keyed by URL
//     alone, so this ownership check is what stops one account from reading
//     another's notice, or using the endpoint as a general-purpose fetcher;
//  3. the cache answers unless refresh=1 asked for a re-fetch.
//
// A failed refresh still serves a stored copy with stale=true: the sheet is
// already open, and replacing readable text with an error would be worse than
// saying the copy may be outdated.
func (a *apiHandler) handleNoticeContent(w http.ResponseWriter, r *http.Request) {
	user, ok := apiUser(w, r)
	if !ok {
		return
	}

	query := r.URL.Query()
	noticeURL := strings.TrimSpace(query.Get("url"))
	refresh := query.Get("refresh") == "1"

	// A malformed URL and a foreign host get the same 400: the response must
	// not reveal which hosts this deployment serves.
	parsed, err := utils.ValidateNoticeURL(noticeURL, a.allowedHost)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}

	owned, err := dal.History.IsUrlExist(user.ID, noticeURL)
	if err != nil {
		slog.Error("mini app api query failed", "endpoint", "notice-content", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if !owned {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	if !refresh {
		if cached, err := dal.GetNoticeContent(noticeURL); err == nil {
			a.writeNoticeContent(w, noticeURL, cached.HTML, cached.FetchedAt, true, false)
			return
		} else if !errors.Is(err, dal.ErrNoticeContentMissing) {
			// A broken cache read is not a reason to fail the sheet: fall
			// through to a fetch, which also repairs the row on success.
			slog.Error("mini app api query failed", "endpoint", "notice-content", "error", err)
		}
	}

	if a.contentLimiter != nil && !a.contentLimiter.Allow(strconv.FormatInt(user.ID, 10)) {
		WriteError(w, http.StatusTooManyRequests, "too_many_requests")
		return
	}

	now := a.now()
	body, fetchErr := a.fetchNoticeContent(r.Context(), parsed.String(), a.noticeBaseURL)
	if fetchErr != nil {
		// Only a stable label is logged: the error can carry the notice URL,
		// which is user data.
		slog.Warn("mini app notice content fetch failed",
			"endpoint", "notice-content", "reason", noticeContentFailureReason(fetchErr))

		if cached, err := dal.GetNoticeContent(noticeURL); err == nil {
			a.writeNoticeContent(w, noticeURL, cached.HTML, cached.FetchedAt, true, true)
			return
		}
		WriteError(w, http.StatusBadGateway, "fetch_failed")
		return
	}

	if err := dal.SaveNoticeContent(noticeURL, body, now); err != nil {
		// The reader still gets the body; only the next open pays for a fetch.
		slog.Error("mini app notice content cache write failed", "endpoint", "notice-content", "error", err)
	}

	a.writeNoticeContent(w, noticeURL, body, now, false, false)
}

func (a *apiHandler) writeNoticeContent(w http.ResponseWriter, noticeURL, body string,
	fetchedAt time.Time, cached, stale bool,
) {
	WriteJSON(w, http.StatusOK, noticeContentResponse{
		URL:       noticeURL,
		HTML:      body,
		Cached:    cached,
		Stale:     stale,
		FetchedAt: formatAPITime(fetchedAt),
	})
}

// noticeContentFailureReason maps a fetch error to a stable,
// low-cardinality log label. The error itself is never logged.
func noticeContentFailureReason(err error) string {
	var statusErr noticeContentStatusError
	switch {
	case errors.Is(err, utils.ErrNoticeBodyMissing):
		return "no_body"
	case errors.Is(err, utils.ErrNoticeBodyTooLarge):
		return "too_large"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.As(err, &statusErr):
		if statusErr.status == http.StatusNotFound {
			return "not_found"
		}
		return "bad_status"
	default:
		return "error"
	}
}
