package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// These tests call a handler built by NewStaticHandler directly, so the request
// paths carry the /app/ prefix exactly as they do in production: newRouter
// mounts the handler with mux.Handle(appPrefix, handler) and net/http passes the
// full path to a subtree handler (it does not strip the mount point).
func newStaticTestHandler(t *testing.T) http.Handler {
	t.Helper()

	handler := NewStaticHandler()
	if handler == nil {
		t.Fatal("NewStaticHandler() = nil, want a handler")
	}

	return handler
}

func serveStatic(t *testing.T, handler http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(method, target, nil))

	return rec
}

// End-to-end over a real listener: the client sends /app/... and no middleware
// rewrites it, which is the same shape the Telegram webview produces.
func TestNewStaticHandlerOverHTTPServer(t *testing.T) {
	srv := httptest.NewServer(NewStaticHandler())
	t.Cleanup(srv.Close)

	cases := []struct {
		target      string
		wantStatus  int
		wantContent string
	}{
		{appPrefix, http.StatusOK, "text/html"},
		{appPrefix + "js/core.js", http.StatusOK, "javascript"},
		{appPrefix + "js/api.js", http.StatusOK, "javascript"},
		{appPrefix + "nope.js", http.StatusNotFound, "text/plain"},
	}

	for _, tc := range cases {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+tc.target, nil)
		if err != nil {
			t.Fatalf("build request for %s: %v", tc.target, err)
		}

		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.target, err)
		}

		_ = resp.Body.Close()

		if resp.StatusCode != tc.wantStatus {
			t.Fatalf("GET %s status = %d, want %d", tc.target, resp.StatusCode, tc.wantStatus)
		}
		if contentType := resp.Header.Get("Content-Type"); !strings.Contains(contentType, tc.wantContent) {
			t.Fatalf("GET %s Content-Type = %q, want %q", tc.target, contentType, tc.wantContent)
		}
	}
}

// The production mount is the contract this handler is written against:
// newRouter registers appPrefix as a subtree and passes the full path through.
// If that ever becomes StripPrefix, every asset below breaks, so the wiring is
// asserted here rather than left implicit.
func TestNewStaticHandlerUnderRouter(t *testing.T) {
	router := newRouter(Config{Static: NewStaticHandler()})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, appPrefix, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("router GET %s status = %d, want %d", appPrefix, rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("router GET %s Content-Type = %q, want text/html", appPrefix, contentType)
	}

	// /app is redirected to /app/ by the router, so the handler only sees the
	// trailing-slash form in production.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, appRootPath, nil))

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("router GET %s status = %d, want %d", appRootPath, rec.Code, http.StatusMovedPermanently)
	}
}

func TestNewStaticHandlerServesIndex(t *testing.T) {
	rec := serveStatic(t, newStaticTestHandler(t), http.MethodGet, appPrefix)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", contentType)
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != indexCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", cacheControl, indexCacheControl)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `name="viewport"`) {
		t.Fatal("index document has no viewport meta tag")
	}
	if !strings.Contains(body, `id="view-today"`) {
		t.Fatal("index document is missing the today view")
	}
	// Telegram provides the native bridge but not the window.Telegram object, so
	// the page has to load the SDK itself. It must be the vendored copy: pulling
	// it from telegram.org would break a strict CSP deployment and an offline
	// one, and every script the page runs must come from this origin.
	if strings.Contains(body, "telegram.org/js/telegram-web-app.js") {
		t.Fatal("index document must not load the Telegram SDK from telegram.org")
	}
	if !strings.Contains(body, "js/telegram-web-app.js") {
		t.Fatal("index document must load the vendored Telegram SDK")
	}
}

// /app without the trailing slash is redirected by newRouter, but the handler
// is also embedded on its own, so it treats the bare prefix as the bundle root.
func TestNewStaticHandlerServesIndexForRootWithoutSlash(t *testing.T) {
	rec := serveStatic(t, newStaticTestHandler(t), http.MethodGet, appRootPath)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", contentType)
	}
}

func TestNewStaticHandlerServesAssetWithETag(t *testing.T) {
	handler := newStaticTestHandler(t)
	rec := serveStatic(t, handler, http.MethodGet, appPrefix+"js/core.js")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/javascript") && !strings.HasPrefix(contentType, "application/javascript") {
		t.Fatalf("Content-Type = %q, want text/javascript or application/javascript", contentType)
	}
	if got, want := rec.Header().Get("Cache-Control"), assetCacheControl; got != want {
		t.Fatalf("Cache-Control = %q, want %q", got, want)
	}

	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag is empty, want a content hash")
	}
	if !strings.Contains(rec.Body.String(), "window.MagnetApp") {
		t.Fatal("core.js body does not define window.MagnetApp")
	}

	// A validator that never revalidates is pointless: a conditional request
	// carrying the same ETag must not resend the body.
	req := httptest.NewRequest(http.MethodGet, appPrefix+"js/core.js", nil)
	req.Header.Set("If-None-Match", etag)
	conditional := httptest.NewRecorder()
	handler.ServeHTTP(conditional, req)

	if conditional.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want %d", conditional.Code, http.StatusNotModified)
	}
	if conditional.Body.Len() != 0 {
		t.Fatalf("conditional body = %d bytes, want empty", conditional.Body.Len())
	}
}

func TestNewStaticHandlerSupportsHEAD(t *testing.T) {
	rec := serveStatic(t, newStaticTestHandler(t), http.MethodHead, appPrefix+"css/base.css")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body = %d bytes, want empty", rec.Body.Len())
	}
	if rec.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD response has no Content-Length")
	}
}

// A missing asset must stay missing: falling back to index.html would answer
// 200 for every broken script or stylesheet reference.
func TestNewStaticHandlerReturns404ForUnknownPath(t *testing.T) {
	rec := serveStatic(t, newStaticTestHandler(t), http.MethodGet, appPrefix+"nope.js")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if strings.Contains(rec.Body.String(), `id="view-today"`) {
		t.Fatal("404 response served the index document")
	}
}

func TestNewStaticHandlerDoesNotListDirectories(t *testing.T) {
	for _, target := range []string{appPrefix + "js/", appPrefix + "css", appPrefix + "js"} {
		rec := serveStatic(t, newStaticTestHandler(t), http.MethodGet, target)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want %d", target, rec.Code, http.StatusNotFound)
		}
	}
}

// Traversal must be rejected, not cleaned: a normalised ".." could alias a real
// asset and hide the attempt. /app/../go.mod would otherwise reach the module
// file if the handler ever served the working directory.
func TestNewStaticHandlerRejectsTraversal(t *testing.T) {
	targets := []string{
		appPrefix + "../go.mod",
		appPrefix + "%2e%2e/go.mod",
		appPrefix + "js/../../go.mod",
		appPrefix + "..%2Fgo.mod",
		appPrefix + "../app/index.html",
		"/app/../server.go",
	}

	handler := newStaticTestHandler(t)
	for _, target := range targets {
		rec := serveStatic(t, handler, http.MethodGet, target)

		if rec.Code == http.StatusOK {
			t.Fatalf("GET %s status = %d, want a rejection", target, rec.Code)
		}
		if body := rec.Body.String(); strings.Contains(body, "module github.com/gythialy/magnet") {
			t.Fatalf("GET %s leaked go.mod", target)
		}
	}
}

// Anything outside /app/ is not this handler's business. newRouter never mounts
// it there, but a wrong mount must fail loudly instead of serving assets from an
// unexpected prefix.
func TestNewStaticHandlerRejectsPathsOutsideAppPrefix(t *testing.T) {
	for _, target := range []string{"/", "/js/core.js", "/appx/js/core.js", "/app/assets/js/core.js"} {
		rec := serveStatic(t, newStaticTestHandler(t), http.MethodGet, target)

		if rec.Code == http.StatusOK {
			t.Fatalf("GET %s status = %d, want a rejection", target, rec.Code)
		}
	}
}

func TestNewStaticHandlerRejectsNonReadMethods(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := serveStatic(t, newStaticTestHandler(t), method, appPrefix)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d, want %d", method, rec.Code, http.StatusMethodNotAllowed)
		}
		if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
			t.Fatalf("%s Allow = %q, want %q", method, allow, "GET, HEAD")
		}
	}
}

// Every asset the document references must exist in the embedded bundle. A typo
// in a <link> or <script> tag would otherwise only show up as a silent 404 in a
// Telegram client, where nobody watches the network panel.
func TestIndexReferencesOnlyEmbeddedAssets(t *testing.T) {
	handler := newStaticTestHandler(t)

	index, err := fs.ReadFile(webAssets, "web/"+appIndexName)
	if err != nil {
		t.Fatalf("read embedded %s: %v", appIndexName, err)
	}

	// The documents the shell must load: four stylesheets (base + one per view)
	// and four scripts (core, api helper, one per view).
	required := []string{
		"css/base.css",
		"css/today.css",
		"css/keywords.css",
		"css/alarms.css",
		"css/records.css",
		"js/core.js",
		"js/api.js",
		"js/today.js",
		"js/keywords.js",
		"js/alarms.js",
		"js/records.js",
	}

	refs := referencedAssets(string(index))
	for _, want := range required {
		if !refs[want] {
			t.Errorf("index document does not reference %s", want)
		}
	}

	for ref := range refs {
		if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "//") {
			t.Errorf("index document references an external resource: %s", ref)
		}

		rec := serveStatic(t, handler, http.MethodGet, appPrefix+ref)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s%s status = %d, want %d", appPrefix, ref, rec.Code, http.StatusOK)
		}
	}
}

var assetRefPattern = regexp.MustCompile(`(?:href|src)="([^"]+)"`)

// referencedAssets collects the local href/src targets of a document, keyed by
// path relative to the bundle root. Fragments and absolute URLs are kept as-is
// so the caller can reject them.
func referencedAssets(document string) map[string]bool {
	refs := make(map[string]bool)

	for _, match := range assetRefPattern.FindAllStringSubmatch(document, -1) {
		ref := match[1]
		if ref == "" || strings.HasPrefix(ref, "#") {
			continue
		}
		ref = strings.TrimPrefix(ref, "./")
		if i := strings.IndexByte(ref, '?'); i >= 0 {
			ref = ref[:i]
		}
		refs[ref] = true
	}

	return refs
}
