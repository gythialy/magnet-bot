package server

import (
	"net/http"
	"strings"
	"time"
)

// The Mini App API is mounted by pkg/server as a subtree: mux.Handle("/api/",
// cfg.API). net/http does not strip the mount point for a subtree handler — it
// only rewrites the URL for its own canonical-path/redirect handling — so the
// handler receives "/api/statistics", prefix included. The patterns below
// therefore repeat the prefix instead of assuming a rewrite that never happens;
// a caller that does strip it would have to change these constants and the
// client together, which is exactly the contract this file documents.
const (
	apiStatisticsPath   = "/api/statistics"
	apiNoticesPath      = "/api/notices"
	apiKeywordsPath     = "/api/keywords"
	apiAlarmsPath       = "/api/alarms"
	apiAlarmRecordsPath = "/api/alarm-records"
	apiConvertPath      = "/api/convert"

	// apiMountPrefix catches every other /api/* request. Without it an unknown
	// endpoint falls through to net/http's plain-text 404, which the Mini App
	// cannot parse into its typed request error (js/core.js expects the JSON
	// envelope on every non-2xx response).
	apiMountPrefix = "/api/"

	// convertRateLimit and convertRateWindow bound how many conversions one
	// user may start per minute. Each request renders a whole page in a shared
	// Gotenberg sidecar, so the budget protects that sidecar rather than this
	// process.
	convertRateLimit  = 5
	convertRateWindow = time.Minute
)

// APIConfig configures the read-only Mini App API.
type APIConfig struct {
	// BotToken authenticates every initData signature. An empty token makes
	// the middleware fail closed, which is what a deployment that lost this
	// setting must do instead of trusting a publicly known HMAC key.
	BotToken string

	// BotUsername is embedded in t.me deep links. Empty means "links are not
	// available": the endpoints then serve "" and the client hides the action
	// instead of producing "https://t.me/?start=...".
	BotUsername string

	// InitDataTTL bounds how old initData may be. A non-positive value means
	// one hour, matching NewInitDataMiddleware.
	InitDataTTL time.Duration

	// Conversion starts a PDF/PNG conversion for the authenticated user. It is
	// optional: a deployment without Gotenberg leaves /api/convert unregistered,
	// so the client gets the same JSON 404 as for any other absent endpoint.
	Conversion ConversionStarter

	// AllowedHost is the only notice host the conversion endpoint accepts. It
	// comes from SERVER_URL, so a crafted request cannot turn the bot into a
	// screenshot service for arbitrary pages.
	AllowedHost string
}

// apiHandler holds the per-deployment constants shared by every endpoint.
// Nothing in it changes after construction, so one instance serves concurrent
// requests without locking.
type apiHandler struct {
	// botUsername is normalised once (no "@", no padding) so a deployment
	// configured as "@name" still produces a valid deep link.
	botUsername string

	// now is the clock behind the relative notice ranges. It is a seam for
	// tests: pinning it makes "today" and "7d" deterministic instead of
	// depending on when the suite runs.
	now func() time.Time
}

// NewAPI builds the read-only Mini App API: five JSON endpoints behind the
// initData authentication middleware. Mount the result at "/api/" — it expects
// the full, unstripped request path.
func NewAPI(cfg APIConfig) http.Handler {
	return newAPI(cfg, time.Now)
}

// newAPI is NewAPI with an injectable clock. Production always passes time.Now;
// tests pass a fixed instant so range boundaries do not move under them.
func newAPI(cfg APIConfig, now func() time.Time) http.Handler {
	if now == nil {
		now = time.Now
	}

	api := &apiHandler{
		botUsername: normalizeBotUsername(cfg.BotUsername),
		now:         now,
	}

	mux := http.NewServeMux()
	// Method-scoped patterns: a POST to a read endpoint is answered with 405 by
	// the mux, so handlers never have to re-check the verb.
	mux.HandleFunc("GET "+apiStatisticsPath, api.handleStatistics)
	mux.HandleFunc("GET "+apiNoticesPath, api.handleNotices)
	mux.HandleFunc("GET "+apiKeywordsPath, api.handleKeywords)
	mux.HandleFunc("GET "+apiAlarmsPath, api.handleAlarms)
	mux.HandleFunc("GET "+apiAlarmRecordsPath, api.handleAlarmRecords)
	// Method-scoped like the others so a write verb on a read endpoint is still
	// answered with 405 by the mux: the mux reports a method mismatch when some
	// pattern matches the path, and an unrestricted catch-all would turn that
	// into a bare 404.
	mux.HandleFunc("GET "+apiMountPrefix, handleAPINotFound)

	// Write (CRUD) routes and their per-user rate limiter live in api_write.go;
	// this call keeps the read table above focused on GETs.
	newWriteAPI().register(mux)

	// Conversion is registered only when a starter is configured. Gotenberg is
	// an optional sidecar, and a route that answers 502 for every request would
	// be worse than a plain 404 the client can interpret as "not available".
	if cfg.Conversion != nil {
		mux.Handle("POST "+apiConvertPath, NewConvertHandler(ConvertConfig{
			Starter:     cfg.Conversion,
			AllowedHost: cfg.AllowedHost,
			Limiter:     NewLimiter(convertRateLimit, convertRateWindow),
		}))
	}

	// Authentication wraps the whole subtree, including the not-found handler:
	// an unauthenticated caller must not be able to probe which endpoints exist.
	return NewInitDataMiddleware(cfg.BotToken, cfg.InitDataTTL)(mux)
}

// handleAPINotFound answers a path under /api/ that has no handler.
func handleAPINotFound(w http.ResponseWriter, _ *http.Request) {
	WriteError(w, http.StatusNotFound, "not_found")
}

// normalizeBotUsername strips the cosmetic "@" and surrounding padding from a
// configured bot username. "https://t.me/@name" is not a valid deep link, and
// the same value is returned to clients for display, so normalising once here
// keeps links and the statistics payload in agreement.
func normalizeBotUsername(username string) string {
	return strings.TrimPrefix(strings.TrimSpace(username), "@")
}
