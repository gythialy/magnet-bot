package server

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// webAssets carries the Mini App bundle inside the binary. Keeping the assets
// embedded is what lets the deployment ship a single file: there is no Node
// build step and no directory on disk to mount, so a missing asset is a build
// error rather than a runtime 404 nobody notices until release.
//
//go:embed web
var webAssets embed.FS

const (
	// appIndexName is the document served for the /app/ directory itself. It is
	// deliberately also the only HTML asset: every other path maps to a file
	// that must exist in the embedded tree.
	appIndexName = "index.html"
	// indexCacheControl keeps the document revalidating, so a redeploy is
	// picked up as soon as a client reopens the Mini App. The other assets get
	// assetCacheControl instead: a short public window, after which their ETag
	// brings a conditional revalidation rather than a full redownload.
	indexCacheControl = "no-cache"
	assetCacheControl = "public, max-age=300"
	// etagHexBytes is how much of the SHA-256 digest is exposed. 128 bits leaves
	// no realistic collision room for cache validation while keeping the header
	// short.
	etagHexBytes = 16
)

// staticAsset is one file of the embedded bundle, hashed once at startup so a
// request never has to read or digest it again.
type staticAsset struct {
	content []byte
	etag    string
}

// NewStaticHandler returns the handler for the Mini App static assets.
//
// Path convention: newRouter mounts it with mux.Handle(appPrefix, handler),
// and net/http passes the full request path to a subtree handler, so the
// handler always receives "/app/..." — including the prefix — and strips it
// here. It is not wrapped in http.StripPrefix on purpose: keeping the prefix
// visible makes the routing table and this handler agree on one string
// (appPrefix) instead of relying on a wrapper that is easy to drop.
func NewStaticHandler() http.Handler {
	assets, err := loadStaticAssets()
	if err != nil {
		// The bundle is fixed at compile time, so this can only fail when the
		// embedded layout itself is broken; failing at construction beats
		// serving a half-empty app to users.
		panic(fmt.Sprintf("server: load embedded Mini App assets: %v", err))
	}

	return &staticHandler{assets: assets}
}

// staticHandler serves files that were resolved and hashed during New.
type staticHandler struct {
	assets map[string]staticAsset
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

		return
	}

	name, ok := staticAssetName(r.URL.Path)
	if !ok {
		http.NotFound(w, r)

		return
	}

	asset, ok := h.assets[name]
	if !ok {
		// No SPA fallback: answering index.html here would turn every mistyped
		// or stale asset URL into a 200, which hides broken references.
		http.NotFound(w, r)

		return
	}

	if name == appIndexName {
		w.Header().Set("Cache-Control", indexCacheControl)
	} else {
		w.Header().Set("Cache-Control", assetCacheControl)
	}
	w.Header().Set("ETag", asset.etag)

	// ServeContent handles HEAD, Range and If-None-Match against the ETag set
	// above. The zero modtime suppresses Last-Modified: embedded files have no
	// meaningful mtime, and inventing one would fake a change source.
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(asset.content))
}

// staticAssetName maps a request path to a name inside the embedded web tree.
// It returns false when the path is outside /app/ or is not a clean
// forward-slash relative name. Rejecting instead of cleaning is the point: a
// ".." segment can never be normalised into an asset that does exist, so a
// traversal attempt is a hard 404 rather than a silent alias.
func staticAssetName(urlPath string) (string, bool) {
	// newRouter redirects /app to /app/, but the handler is also exercised on
	// its own (tests, embedding), so the prefix without a trailing slash is
	// treated as the bundle root too.
	if urlPath == appRootPath {
		return appIndexName, true
	}

	rel, found := strings.CutPrefix(urlPath, appPrefix)
	if !found {
		return "", false
	}

	if rel == "" {
		return appIndexName, true
	}

	if !fs.ValidPath(rel) {
		return "", false
	}

	return rel, true
}

// loadStaticAssets walks the embedded tree once and hashes every regular file.
// Directory entries are skipped: they are not servable, which also means this
// handler never renders a directory listing.
func loadStaticAssets() (map[string]staticAsset, error) {
	root, err := fs.Sub(webAssets, "web")
	if err != nil {
		return nil, err
	}

	assets := make(map[string]staticAsset, 16)

	walkErr := fs.WalkDir(root, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		content, err := fs.ReadFile(root, name)
		if err != nil {
			return err
		}

		sum := sha256.Sum256(content)
		assets[name] = staticAsset{
			content: content,
			etag:    `"` + hex.EncodeToString(sum[:etagHexBytes]) + `"`,
		}

		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	if _, ok := assets[appIndexName]; !ok {
		return nil, fmt.Errorf("embedded bundle is missing %s", appIndexName)
	}

	return assets, nil
}
