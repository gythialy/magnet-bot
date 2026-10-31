package server

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/gythialy/magnet/pkg/utils"
)

func TestHealthzReturnsOK(t *testing.T) {
	handler := newRouter(Config{})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, healthPath, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != healthBody {
		t.Fatalf("body = %q, want %q", body, healthBody)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", contentType)
	}
}

// The health route is method-scoped, so a probe that uses the wrong verb must
// be told so instead of hitting a handler that assumes GET.
func TestHealthzRejectsNonGET(t *testing.T) {
	handler := newRouter(Config{})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, healthPath, nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// A capability that was never injected must not answer at all: a 200 from an
// empty stub would make a broken deployment look healthy to Caddy and Telegram.
func TestUnregisteredHandlersReturn404(t *testing.T) {
	handler := newRouter(Config{})

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"telegram webhook", http.MethodPost, "/telegram/webhook"},
		{"pdf callback", http.MethodPost, "/pdf/abc123"},
		{"static root", http.MethodGet, "/app"},
		{"static asset", http.MethodGet, "/app/"},
		{"static nested asset", http.MethodGet, "/app/index.html"},
		{"api", http.MethodGet, "/api/items"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s %s: status = %d, want %d", tc.method, tc.path, rec.Code, http.StatusNotFound)
			}
		})
	}
}

func TestInjectedHandlersAreReached(t *testing.T) {
	var (
		telegramCalls, pdfCalls, staticCalls, apiCalls atomic.Int32
		pdfID                                          atomic.Value
		staticPath, apiPath                            atomic.Value
	)

	cfg := Config{
		TelegramWebhook: func(w http.ResponseWriter, _ *http.Request) {
			telegramCalls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		},
		PDFCallback: func(w http.ResponseWriter, r *http.Request) {
			pdfCalls.Add(1)
			pdfID.Store(r.PathValue("id"))
			w.WriteHeader(http.StatusNoContent)
		},
		Static: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			staticCalls.Add(1)
			staticPath.Store(r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}),
		API: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiCalls.Add(1)
			apiPath.Store(r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	handler := newRouter(cfg)

	cases := []struct {
		name   string
		method string
		path   string
		calls  *atomic.Int32
	}{
		{"telegram webhook", http.MethodPost, "/telegram/webhook", &telegramCalls},
		{"pdf callback", http.MethodPost, "/pdf/abc123", &pdfCalls},
		{"static root asset", http.MethodGet, "/app/", &staticCalls},
		{"static nested asset", http.MethodGet, "/app/index.html", &staticCalls},
		{"api", http.MethodGet, "/api/items", &apiCalls},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.calls.Load()
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			if tc.calls.Load() != before+1 {
				t.Fatalf("%s %s did not reach the injected handler", tc.method, tc.path)
			}
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
			}
		})
	}

	if got, _ := pdfID.Load().(string); got != "abc123" {
		t.Fatalf("pdf callback id = %q, want %q", got, "abc123")
	}
	// The mux must forward the path unchanged: the static and API handlers
	// receive the full path and route on it themselves.
	if got, _ := staticPath.Load().(string); got != "/app/index.html" {
		t.Fatalf("static handler path = %q, want %q", got, "/app/index.html")
	}
	if got, _ := apiPath.Load().(string); got != "/api/items" {
		t.Fatalf("api handler path = %q, want %q", got, "/api/items")
	}
}

// The bare /app root has to land on the trailing-slash subtree, otherwise the
// embedded static handler would resolve assets relative to the wrong base.
func TestAppRedirectsToTrailingSlash(t *testing.T) {
	handler := newRouter(Config{Static: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, appRootPath, nil))

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	if location := rec.Header().Get("Location"); location != appPrefix {
		t.Fatalf("Location = %q, want %q", location, appPrefix)
	}
}

// Request logging is the one place where a credential could leak into the log,
// so assert positively on the fields that must exist and negatively on a
// secret that appears in both the query string and a header.
func TestRequestLoggingRecordsStatusWithoutSecrets(t *testing.T) {
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	logger := &utils.Logger{Logger: &zl}

	handler := withRequestLogging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}), logger)

	req := httptest.NewRequest(http.MethodGet, "/api/items?token=super-secret", nil)
	req.Header.Set("Authorization", "Bearer super-secret")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	line := buf.String()
	for _, want := range []string{"method", "path", "status", "duration", "/api/items", "418"} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line %q is missing %q", line, want)
		}
	}
	if strings.Contains(line, "super-secret") {
		t.Fatalf("log line leaked a query or header secret: %q", line)
	}
}

// A nil logger is the common case in tests and embedded uses; logging must be
// skipped entirely rather than dereferenced.
func TestRequestLoggingWithoutLogger(t *testing.T) {
	called := false
	handler := withRequestLogging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	}), nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if !called {
		t.Fatal("wrapped handler was not invoked")
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
}

// net.Listen("tcp", "") would bind an ephemeral port; a missing
// WEBHOOK_SERVER_PORT must fail loudly instead of producing an unreachable
// server.
func TestStartRejectsEmptyAddr(t *testing.T) {
	srv := New(Config{})

	err := srv.Start()
	if err == nil {
		_ = srv.Shutdown(context.Background())
		t.Fatal("Start() with an empty address returned nil error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("error = %v, want it to mention the empty address", err)
	}
}

// A port already taken by another process is the failure mode the plan calls
// out explicitly: Start must report it synchronously so the bot can exit.
func TestStartFailsWhenPortIsInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	srv := New(Config{Addr: ln.Addr().String()})
	if err := srv.Start(); err == nil {
		_ = srv.Shutdown(context.Background())
		t.Fatalf("Start() on an occupied address %s returned nil error", ln.Addr())
	}
}

// End-to-end check over a real socket: the server answers, and after Shutdown
// the listener is closed rather than left half-alive.
func TestServeAndGracefulShutdown(t *testing.T) {
	srv := New(Config{Addr: "127.0.0.1:0"})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	addr := srv.Addr()
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get("http://" + addr + healthPath)
	if err != nil {
		_ = srv.Shutdown(context.Background())
		t.Fatalf("GET /healthz: %v", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		t.Fatalf("read body: %v", readErr)
	}
	if resp.StatusCode != http.StatusOK || string(body) != healthBody {
		_ = srv.Shutdown(context.Background())
		t.Fatalf("GET /healthz = %d %q, want 200 %q", resp.StatusCode, string(body), healthBody)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Shutdown closes the listener before returning, but give the kernel a
	// short window instead of asserting on an exact instant.
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if dialErr != nil {
			return
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatalf("address %s still accepts connections after Shutdown", addr)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
