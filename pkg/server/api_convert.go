package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/utils"
)

const (
	// convertBodyLimit bounds POST /api/convert. The body only carries a URL
	// and a format, so anything larger is abuse rather than a real request.
	convertBodyLimit = 16 << 10

	// convertInFlightTTL is the safety net for the in-flight dedupe entries.
	// The normal path releases the entry as soon as the submission returns;
	// the deadline only matters if a request dies without running its cleanup.
	convertInFlightTTL = 5 * time.Minute

	convertFormatPDF = "pdf"
	convertFormatPNG = "png"
)

// ConversionStarter submits one asynchronous conversion and returns the
// request id the Gotenberg callback will present. It is implemented by
// *handler.BotContext.StartConversion.
type ConversionStarter interface {
	StartConversion(ctx context.Context, chatID int64, noticeURL string, kind model.FileType, replyMessageID int) (string, error)
}

// ConversionLimiter reports whether key still has budget in the current rate
// window. It is the subset of *Limiter this endpoint uses, so the endpoint does
// not depend on the limiter's storage or its window bookkeeping.
type ConversionLimiter interface {
	Allow(key string) bool
}

// ConvertConfig configures POST /api/convert.
type ConvertConfig struct {
	// Starter performs the conversion. It is required.
	Starter ConversionStarter
	// AllowedHost is the notice host conversions may target, normally
	// config.ServiceConfig.MessageServerUrl. An empty value rejects every
	// request: the allow-list must never fail open.
	AllowedHost string
	// Limiter enforces the per-user budget. A nil limiter skips the check;
	// that is only acceptable for tests or before the composition root wires
	// the real limiter, and production always passes one.
	Limiter ConversionLimiter
	// Now is the clock behind the dedupe deadline. Tests pin it; production
	// leaves it nil so time.Now is used.
	Now func() time.Time
}

// convertRequest is the POST /api/convert body.
type convertRequest struct {
	URL    string `json:"url"`
	Format string `json:"format"`
}

// convertAcceptedResponse answers a freshly submitted conversion.
type convertAcceptedResponse struct {
	Status string `json:"status"`
	Format string `json:"format"`
}

// convertInFlightResponse answers a request that duplicated an in-flight
// conversion. It deliberately omits the format: the caller did not cause a new
// submission, so there is nothing new to report about it.
type convertInFlightResponse struct {
	Status string `json:"status"`
}

// conversionKey identifies one conversion for the in-flight dedupe.
type conversionKey struct {
	userID int64
	url    string
	format string
}

// conversionTracker remembers the conversions currently being submitted.
//
// The entry is released as soon as the submission returns, so the map only
// holds concurrent conversions; convertInFlightTTL is the backstop for a
// request that dies between reserving and releasing.
type conversionTracker struct {
	mu     sync.Mutex
	active map[conversionKey]time.Time
}

func newConversionTracker() *conversionTracker {
	return &conversionTracker{active: make(map[conversionKey]time.Time)}
}

// begin reserves key unless a live reservation already exists. Every call also
// sweeps expired entries, which keeps the map bounded by the number of
// conversions actually in flight instead of growing with request history.
func (t *conversionTracker) begin(key conversionKey, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	for candidate, expires := range t.active {
		if !now.Before(expires) {
			delete(t.active, candidate)
		}
	}

	if _, found := t.active[key]; found {
		return false
	}
	t.active[key] = now.Add(convertInFlightTTL)
	return true
}

// end releases a reservation. It is safe to call after the deadline expired:
// deleting an absent key is a no-op.
func (t *conversionTracker) end(key conversionKey) {
	t.mu.Lock()
	delete(t.active, key)
	t.mu.Unlock()
}

// convertHandler serves POST /api/convert.
type convertHandler struct {
	starter     ConversionStarter
	allowedHost string
	limiter     ConversionLimiter
	now         func() time.Time
	tracker     *conversionTracker
}

// NewConvertHandler builds the POST /api/convert handler.
//
// The returned handler is NOT wrapped in NewInitDataMiddleware: the route
// owner must mount it behind that middleware (and behind a POST-only pattern),
// exactly like the read API. It reads the authenticated user from the request
// context with UserFromContext, so serving it without the middleware answers
// 401 instead of trusting an unauthenticated caller.
func NewConvertHandler(cfg ConvertConfig) http.Handler {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &convertHandler{
		starter:     cfg.Starter,
		allowedHost: cfg.AllowedHost,
		limiter:     cfg.Limiter,
		now:         now,
		tracker:     newConversionTracker(),
	}
}

// ServeHTTP validates and submits one conversion.
//
// The checks run cheapest-and-most-fundamental first: request shape, URL
// syntax, the notice host allow-list, ownership of the notice, the per-user
// rate limit, and finally the in-flight dedupe. Nothing reaches Gotenberg until
// all of them passed.
func (h *convertHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, ok := apiUser(w, r)
	if !ok {
		return
	}
	if h.starter == nil {
		// A deployment without a conversion service must not panic on the
		// first request; it has the same effect on the caller as a failed
		// submission.
		slog.Error("mini app convert is not configured")
		WriteError(w, http.StatusBadGateway, "conversion_failed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, convertBodyLimit)
	var body convertRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}

	format, kind, ok := parseConversionFormat(body.Format)
	if !ok {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}

	noticeURL := strings.TrimSpace(body.URL)
	// Host allow-list first, ownership second: an attacker learns nothing
	// about which URLs exist for other users.
	if _, err := utils.ValidateNoticeURL(noticeURL, h.allowedHost); err != nil {
		if errors.Is(err, utils.ErrNoticeURLHost) {
			WriteError(w, http.StatusForbidden, "forbidden")
			return
		}
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}

	owned, err := dal.History.IsUrlExist(user.ID, noticeURL)
	if err != nil {
		slog.Error("mini app api query failed", "endpoint", "convert", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if !owned {
		// Only notices already delivered to this user may be converted, so
		// the endpoint cannot be used as a general-purpose URL fetcher.
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	// The limiter is optional in the type so tests can exercise the endpoint
	// without a window; wiring always supplies the real one.
	if h.limiter != nil && !h.limiter.Allow(strconv.FormatInt(user.ID, 10)) {
		WriteError(w, http.StatusTooManyRequests, "too_many_requests")
		return
	}

	key := conversionKey{userID: user.ID, url: noticeURL, format: format}
	if !h.tracker.begin(key, h.now()) {
		WriteJSON(w, http.StatusAccepted, convertInFlightResponse{Status: "converting"})
		return
	}
	// Release as soon as the submission returns: once Telegram/Gotenberg own
	// the job, an identical request is a new conversion, not a duplicate.
	defer h.tracker.end(key)

	if _, err := h.starter.StartConversion(r.Context(), user.ID, noticeURL, kind, 0); err != nil {
		// The URL is user data and may identify a private notice, so only the
		// endpoint and the error are logged.
		slog.Error("mini app conversion failed", "endpoint", "convert", "error", err)
		WriteError(w, http.StatusBadGateway, "conversion_failed")
		return
	}

	WriteJSON(w, http.StatusAccepted, convertAcceptedResponse{Status: "accepted", Format: format})
}

// parseConversionFormat normalises the wire format and maps it to the
// conversion kind. An unknown value is rejected instead of being coerced: the
// client and the server must agree on what will be produced.
func parseConversionFormat(raw string) (format string, kind model.FileType, ok bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case convertFormatPDF:
		return convertFormatPDF, model.PDF, true
	case convertFormatPNG:
		return convertFormatPNG, model.IMG, true
	default:
		return "", 0, false
	}
}
