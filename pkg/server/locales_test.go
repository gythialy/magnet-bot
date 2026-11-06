package server

import (
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The Mini App ships two flat dictionaries and one document plus view scripts.
// The mechanical checks here are the ones nobody can do by eye in a Telegram
// client: both dictionaries carry the same keys, no key is declared twice, and
// every literal key referenced by the shell or a view exists in both.
//
// Dynamic keys (t("role." + role)) cannot be checked statically; the views only
// build keys from small closed sets, which the tests below keep out of the scan.
var (
	localeKeyPattern = regexp.MustCompile(`(?m)^\s*"([A-Za-z0-9_.]+)":`)
	assetKeyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`data-i18n(?:-ph|-aria|-title)?="([A-Za-z0-9_.]+)"`),
		regexp.MustCompile(`\bt\(\s*"([A-Za-z0-9_.]+)"`),
		regexp.MustCompile(`\bplural\(\s*"([A-Za-z0-9_.]+)"`),
	}
)

func localeKeys(t *testing.T, content []byte) []string {
	t.Helper()

	var keys []string
	for _, match := range localeKeyPattern.FindAllStringSubmatch(string(content), -1) {
		keys = append(keys, match[1])
	}
	return keys
}

func TestLocaleDictionariesMatch(t *testing.T) {
	en := localeKeys(t, mustReadAsset(t, "js/locales/en.js"))
	zh := localeKeys(t, mustReadAsset(t, "js/locales/zh.js"))

	if len(en) == 0 || len(zh) == 0 {
		t.Fatalf("locale dictionaries are empty (en=%d, zh=%d)", len(en), len(zh))
	}

	for name, keys := range map[string][]string{"en": en, "zh": zh} {
		seen := make(map[string]struct{}, len(keys))
		for _, key := range keys {
			if _, duplicate := seen[key]; duplicate {
				t.Errorf("%s.js declares %q more than once", name, key)
			}
			seen[key] = struct{}{}
		}
	}

	for _, key := range en {
		if !slices.Contains(zh, key) {
			t.Errorf("zh.js is missing %q", key)
		}
	}
	for _, key := range zh {
		if !slices.Contains(en, key) {
			t.Errorf("en.js is missing %q", key)
		}
	}
}

// TestAssetReferencesKnownLocaleKeys walks the embedded bundle and fails when a
// view refers to a key neither dictionary defines. That is exactly the failure a
// language switch produces at runtime: the raw key appears as the label.
func TestAssetReferencesKnownLocaleKeys(t *testing.T) {
	en := localeKeys(t, mustReadAsset(t, "js/locales/en.js"))
	known := make(map[string]struct{}, len(en))
	for _, key := range en {
		known[key] = struct{}{}
	}

	referenced := make(map[string]string)
	collect := func(asset, text string) {
		for _, pattern := range assetKeyPatterns {
			for _, match := range pattern.FindAllStringSubmatch(text, -1) {
				referenced[match[1]] = asset
			}
		}
	}

	collect("index.html", string(mustReadAsset(t, "index.html")))

	err := fs.WalkDir(webAssets, "web/js", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.TrimPrefix(path, "web/")
		if strings.HasPrefix(name, "js/locales/") || strings.HasSuffix(name, "telegram-web-app.js") {
			return nil
		}
		content, readErr := fs.ReadFile(webAssets, path)
		if readErr != nil {
			return readErr
		}
		collect(name, string(content))
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded assets: %v", err)
	}

	if len(referenced) == 0 {
		t.Fatal("no locale keys referenced by the embedded bundle; the scan is broken")
	}

	for key, asset := range referenced {
		if _, ok := known[key]; !ok {
			t.Errorf("%s references undefined locale key %q", asset, key)
		}
	}
}

func mustReadAsset(t *testing.T, name string) []byte {
	t.Helper()

	content, err := fs.ReadFile(webAssets, "web/"+name)
	if err != nil {
		t.Fatalf("read embedded web/%s: %v", name, err)
	}
	return content
}
