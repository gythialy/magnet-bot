// Package server owns the single plaintext HTTP listener that fronts the bot:
// the Telegram webhook, the Mini App (static assets + JSON API) and the
// internal Gotenberg callback. It deliberately knows nothing about the
// handlers it serves, so packages that implement them can depend on this one
// without creating an import cycle. Business handlers are injected through
// Config by the composition root (cmd/server).
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gythialy/magnet/pkg/utils"
)

const (
	// readHeaderTimeout bounds the request line and headers phase, so a client
	// trickling bytes cannot pin a connection before the body is even read.
	readHeaderTimeout = 10 * time.Second
	// readTimeout and writeTimeout cover the whole request and response
	// exchange. They keep the values the previous webhook-only server used, so
	// moving to the unified listener does not silently change the latency any
	// existing client tolerates.
	readTimeout  = time.Minute
	writeTimeout = time.Minute
	// idleTimeout keeps keep-alive connections alive long enough for the Mini
	// App to reuse them without holding dead sockets hostage.
	idleTimeout = 2 * time.Minute
)

// Config describes the capabilities the unified server exposes. Every handler
// is optional: a nil handler means "this deployment does not provide that
// capability" and its routes are not registered at all, so callers receive a
// plain 404 instead of a half-working endpoint.
type Config struct {
	// Addr is the plaintext listen address, for example ":8080".
	Addr string
	// TelegramWebhook receives updates pushed by Telegram.
	TelegramWebhook http.HandlerFunc
	// PDFCallback receives Gotenberg's conversion result at POST /pdf/{id}.
	PDFCallback http.HandlerFunc
	// Static serves the Mini App assets below /app/.
	Static http.Handler
	// API serves the Mini App JSON endpoints below /api/.
	API http.Handler
	// Logger is optional. When nil request logging is skipped entirely, so
	// tests and embedders do not have to construct a logger first.
	Logger *utils.Logger
}

// Server is the unified HTTP server. New builds it, Start binds and serves it,
// Shutdown drains it.
type Server struct {
	cfg        Config
	httpServer *http.Server
	// boundAddr is the address actually bound once Start succeeded. It differs
	// from cfg.Addr when the caller asked for port 0.
	boundAddr string
}

// New builds the server and its routing table. Listening is deferred to Start
// so that a bind failure is reported synchronously to the caller: the plan
// (§4.1) requires the process to fail fast rather than serve a partial surface
// that Caddy would happily route traffic to.
func New(cfg Config) *Server {
	srv := &Server{cfg: cfg}
	srv.httpServer = &http.Server{
		Addr:              cfg.Addr,
		Handler:           newRouter(cfg),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
	return srv
}

// Start binds Addr and then serves in the background. Bind errors (port in
// use, malformed address) are returned immediately. Errors observed later by
// Serve are not returned: shutdown is signalled through Shutdown, where
// http.ErrServerClosed is the expected outcome rather than a failure.
func (s *Server) Start() error {
	if s.cfg.Addr == "" {
		// net.Listen("tcp", "") would silently pick an ephemeral port, turning
		// a missing WEBHOOK_SERVER_PORT into a server nobody can reach.
		return errors.New("server: listen address is empty")
	}

	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("server: listen on %q: %w", s.cfg.Addr, err)
	}
	s.boundAddr = ln.Addr().String()

	go func() {
		// Serve returns ErrServerClosed after a graceful Shutdown; that path is
		// the normal one and must not be reported as a server error.
		if err := s.httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if s.cfg.Logger != nil {
				s.cfg.Logger.Error().
					Err(err).
					Str("addr", s.boundAddr).
					Msg("HTTP server stopped unexpectedly")
			}
		}
	}()

	return nil
}

// Addr returns the address the server is actually listening on. It is useful
// when Config.Addr requested port 0, which is how tests and sandboxed runs
// avoid colliding with an already running instance.
func (s *Server) Addr() string {
	if s.boundAddr != "" {
		return s.boundAddr
	}
	return s.cfg.Addr
}

// Shutdown gracefully drains in-flight requests and closes the listener. It is
// safe even when Start never ran: http.Server.Shutdown on a non-serving server
// is a no-op, and the bot's stop path must not depend on whether the listener
// came up.
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("server: shutdown: %w", err)
	}
	return nil
}
