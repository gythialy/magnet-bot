package server

import (
	"io"
	"net/http"
	"time"

	"github.com/gythialy/magnet/pkg/utils"
)

// Route patterns live here rather than in pkg/constant because this package
// defines the transport surface, while that package primarily carries
// environment variable names. Keeping the strings local also means the
// transport layout can change without touching configuration code.
const (
	healthPath     = "/healthz"
	telegramPath   = "/telegram/webhook"
	pdfCallbackURL = "/pdf/{id}"
	appRootPath    = "/app"
	appPrefix      = "/app/"
	apiPrefix      = "/api/"

	healthBody = "ok"
)

// newRouter assembles the mux and wraps it with request logging. Handlers are
// injected because this package must never import pkg/handler: the handler
// package is what configures the bot, so importing it back would form a cycle.
func newRouter(cfg Config) http.Handler {
	mux := http.NewServeMux()

	// Registered unconditionally: Caddy and Cloudflare need a liveness probe
	// regardless of which optional capabilities this deployment enables.
	mux.HandleFunc("GET "+healthPath, handleHealth)

	if cfg.TelegramWebhook != nil {
		// Method-scoped so a stray GET returns 405 instead of reaching the
		// webhook handler with an empty body.
		mux.HandleFunc("POST "+telegramPath, cfg.TelegramWebhook)
	}

	if cfg.PDFCallback != nil {
		// The {id} wildcard preserves the id as a path variable; validation of
		// that id stays in the callback handler so the transport layer does not
		// need to know the conversion state machine.
		mux.HandleFunc("POST "+pdfCallbackURL, cfg.PDFCallback)
	}

	if cfg.Static != nil {
		// ServeMux would redirect /app to /app/ implicitly once /app/ is
		// registered, but registering the redirect explicitly keeps the
		// contract visible and guarantees it is only offered when the static
		// handler exists (otherwise both paths must 404).
		mux.HandleFunc(appRootPath, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, appPrefix, http.StatusMovedPermanently)
		})
		mux.Handle(appPrefix, cfg.Static)
	}

	if cfg.API != nil {
		// The API owns its own mux: mounting the whole subtree keeps endpoint
		// patterns, method matching and 404/405 semantics in the API package
		// instead of leaking them into this routing table.
		mux.Handle(apiPrefix, cfg.API)
	}

	return withRequestLogging(mux, cfg.Logger)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	// Explicit content type: the body is a constant string, and a probe that
	// sees text/plain is easier to distinguish from a generic error page.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, healthBody)
}

// withRequestLogging records one line per request. Only the URL path is
// reported: a query string may carry an access token and headers or bodies may
// carry credentials, so none of them are ever copied into a log record.
func withRequestLogging(next http.Handler, logger *utils.Logger) http.Handler {
	if logger == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		logger.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", rec.status).
			Dur("duration", time.Since(start)).
			Msg("HTTP request")
	})
}

// statusRecorder captures the response status for logging while keeping the
// original writer reachable through Unwrap, so http.ResponseController still
// supports Flush and Hijack for streaming or upgraded responses.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	// Record only the first header write: net/http commits the first one and
	// logs the rest as superfluous, so logging a later value would lie.
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	// A handler that writes without calling WriteHeader implicitly sends 200;
	// mark the header as written so the recorded status stays truthful.
	r.wroteHeader = true
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
