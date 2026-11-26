//go:build e2e

package server

// Harness for the browser-driven end-to-end specs.
//
// The specs drive the real embedded bundle through the production router: the
// static handler serves index.html and its assets, and /api/v1 runs behind the
// real initData middleware. Nothing in the client is stubbed. The one seam is
// the launch URL: a Telegram client hands its payload over in the hash, so
// "#tgWebAppData=<payload>" makes the vendored SDK parse real initData itself,
// exactly as it does on a phone.
//
// Run them with:
//
//	go test -tags e2e -run TestE2E ./pkg/server
//
// A Chromium binary is required. E2E_CHROME_BIN names one; otherwise common
// install locations (including the Playwright cache) are probed. When nothing
// is found the specs skip instead of failing, so a plain checkout stays green.

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/gythialy/magnet/pkg/model"
)

const (
	// e2ePageTimeout bounds every wait: a request that hangs must fail the spec
	// with a readable message instead of blocking the suite.
	e2ePageTimeout = 15 * time.Second
	// e2eRowSelector is the contract the specs assert on: the list region the
	// view renders into, and one <article> per notice. Both survive a
	// restyle; a redesign that renames them has to update the specs on purpose.
	e2eRowSelector = `#view-today [data-slot="list"] article`
)

// errNoChrome means "no browser installed", which is a skip rather than a
// failure: the specs are about the app, not about the developer's machine.
var errNoChrome = errors.New("no Chromium or Chrome found")

var (
	e2eOnce     sync.Once
	e2eBrowserR *rod.Browser
	e2eBootErr  error
	e2eProfile  string
)

func TestMain(m *testing.M) {
	code := m.Run()
	if e2eBrowserR != nil {
		_ = e2eBrowserR.Close()
	}
	// The throwaway HOME holds the browser profile; leaving it behind would leak
	// a few megabytes per run.
	if e2eProfile != "" {
		_ = os.RemoveAll(e2eProfile)
	}
	os.Exit(code)
}

// e2eApp boots the whole HTTP surface on a throwaway port with a private
// SQLite database. now is pinned so "today" boundaries do not move under the
// test while a burst of requests is in flight.
func e2eApp(t *testing.T) (baseURL, initData string, now time.Time) {
	t.Helper()

	setupAPITestDB(t)

	now = time.Now().Truncate(time.Second)
	router := newRouter(Config{
		Static: NewStaticHandler(),
		API: apiTestHandler(APIConfig{
			BotToken:    testBotToken,
			BotUsername: apiTestBotUsername,
			InitDataTTL: time.Hour,
		}, now),
	})

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	return srv.URL, apiTestInitData(t, now, testUserID), now
}

// e2eSeedToday stores three notices published today plus two older ones, and
// returns today's titles newest first. Stamps stay inside the local day even
// when the suite runs just after midnight.
func e2eSeedToday(t *testing.T, now time.Time) []string {
	t.Helper()

	// The trailing code is what the client extracts as the project code, so the
	// detail sheet has one real field to render.
	today := []struct{ slug, title string }{
		{"a", "星海市档案馆 2027 年度馆藏数字化服务采购公告（2026-ZB-1187）"},
		{"b", "云陵区人民医院医用耗材配送服务（二次）招标公告（2026-GC-0442）"},
		{"c", "临港新区市政道路照明设备更新项目（更正公告）（2026-CQ-0091）"},
	}

	local := now.Location()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, local)

	rows := make([]*model.History, 0, len(today)+2)
	titles := make([]string, 0, len(today))
	for i, item := range today {
		stamp := now.Add(-time.Duration(i+1) * time.Minute)
		if stamp.Before(startOfDay) {
			// Just after midnight the offsets above land on yesterday. Fall back
			// to milliseconds inside today, still newest first.
			stamp = startOfDay.Add(time.Duration(len(today)-i) * time.Millisecond)
		}
		rows = append(rows, &model.History{
			UserID:    testUserID,
			URL:       "https://notice.example/" + item.slug,
			Title:     item.title,
			UpdatedAt: stamp,
		})
		titles = append(titles, item.title)
	}

	// Outside the default "today" range: the specs prove the filter, not just
	// that rows exist.
	rows = append(rows,
		&model.History{
			UserID:    testUserID,
			URL:       "https://notice.example/older",
			Title:     "北岸新区污水处理厂提标改造工程监理服务招标公告",
			UpdatedAt: now.Add(-26 * time.Hour),
		},
		&model.History{
			UserID:    testUserID,
			URL:       "https://notice.example/oldest",
			Title:     "星海市第一中学实验室设备采购（三次）公告",
			UpdatedAt: now.Add(-30 * time.Hour),
		},
	)

	seedHistories(t, rows...)
	return titles
}

// e2ePage opens a fresh browser context per test: localStorage carries the
// "seen" watermark and sessionStorage caches launch params, so a shared context
// would leak state between specs.
func e2ePage(t *testing.T, baseURL, initData string) *rod.Page {
	t.Helper()

	browser := e2eBrowser(t)

	target := baseURL + "/app/"
	if initData != "" {
		// QueryEscape encodes spaces as "+", which the SDK's decoder does not
		// turn back into a space; %20 is the form it round-trips.
		target += "#tgWebAppData=" + strings.ReplaceAll(url.QueryEscape(initData), "+", "%20")
	}

	// A fresh context keeps localStorage (the "seen" watermark) out of the next
	// spec; a build without context support still gets a page.
	contextBrowser := browser
	if ctx, err := browser.Incognito(); err == nil {
		contextBrowser = ctx
	} else {
		t.Logf("e2e: incognito unavailable (%v), reusing one context", err)
	}

	page, err := contextBrowser.Timeout(e2ePageTimeout).Page(proto.TargetCreateTarget{URL: target})
	if err != nil {
		t.Fatalf("open %s: %v", target, err)
	}

	// A phone viewport: the app is laid out for the Telegram webview, and a
	// desktop-sized page would pass rows that a 390px screen wraps.
	if err := (proto.EmulationSetDeviceMetricsOverride{
		Width:             390,
		Height:            844,
		DeviceScaleFactor: 2,
		Mobile:            true,
	}).Call(page); err != nil {
		t.Logf("e2e: viewport override failed: %v", err)
	}
	t.Cleanup(func() { _ = page.Close() })
	e2eAttachArtifacts(t, page)

	return page
}

func e2eBrowser(t *testing.T) *rod.Browser {
	t.Helper()

	e2eOnce.Do(func() {
		bin, err := e2eFindChrome()
		if err != nil {
			e2eBootErr = err
			return
		}

		t.Logf("e2e: browser %s", bin)

		profile, err := os.MkdirTemp("", "magnet-e2e-profile-")
		if err != nil {
			e2eBootErr = fmt.Errorf("create browser profile: %w", err)
			return
		}
		e2eProfile = profile

		// The browser must not touch the login keychain or the real home
		// directory: --use-mock-keychain and --password-store=basic keep Chrome
		// away from Keychain, and HOME points its profile, crashpad data and
		// caches at a throwaway directory instead of ~/Library.
		env := make([]string, 0, len(os.Environ())+1)
		for _, entry := range os.Environ() {
			if strings.HasPrefix(entry, "HOME=") || strings.HasPrefix(entry, "TMPDIR=") {
				continue
			}
			env = append(env, entry)
		}
		env = append(env, "HOME="+profile, "TMPDIR="+profile)

		control, launchErr := launcher.New().
			Bin(bin).
			HeadlessNew(true).
			// The test process already runs inside a sandbox; Chrome's own
			// sandbox cannot start nested and is not needed for a local page.
			NoSandbox(true).
			Set("user-data-dir", filepath.Join(profile, "profile")).
			Set("crash-dumps-dir", filepath.Join(profile, "crashes")).
			Set("disable-breakpad").
			Set("use-mock-keychain").
			Set("password-store", "basic").
			Set("disable-gpu").
			Set("disable-dev-shm-usage").
			Set("window-size", "390,844").
			Set("no-first-run").
			Set("no-default-browser-check").
			Env(env...).
			Launch()
		if launchErr != nil {
			e2eBootErr = fmt.Errorf("launch %s: %w", bin, launchErr)
			return
		}

		b := rod.New().ControlURL(control)
		if err := b.Connect(); err != nil {
			e2eBootErr = fmt.Errorf("connect to %s: %w", bin, err)
			return
		}
		e2eBrowserR = b
	})

	if errors.Is(e2eBootErr, errNoChrome) {
		t.Skipf("%v; set E2E_CHROME_BIN to a Chromium or Chrome binary", e2eBootErr)
	}
	if e2eBootErr != nil {
		t.Fatalf("start headless browser: %v", e2eBootErr)
	}

	return e2eBrowserR
}

// e2eFindChrome resolves a browser without downloading one: CI passes an
// explicit path, and a machine that already has Chrome or a Playwright cache
// should not fetch a second copy.
func e2eFindChrome() (string, error) {
	if bin := strings.TrimSpace(os.Getenv("E2E_CHROME_BIN")); bin != "" {
		info, err := os.Stat(bin)
		if err != nil || info.IsDir() {
			return "", fmt.Errorf("E2E_CHROME_BIN=%s is not an executable file", bin)
		}
		return bin, nil
	}

	candidates := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/usr/bin/google-chrome",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
	}

	if home, err := os.UserHomeDir(); err == nil {
		// The full Chromium comes first: chrome-headless-shell cannot create
		// browser contexts, and every spec needs a fresh one.
		globs := []string{
			filepath.Join(home, "Library/Caches/ms-playwright/chromium-*/chrome-mac*/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing"),
			filepath.Join(home, "Library/Caches/ms-playwright/chromium_headless_shell-*/chrome-headless-shell-mac*/chrome-headless-shell"),
			filepath.Join(home, ".cache/ms-playwright/chromium-*/chrome-linux/chrome"),
			filepath.Join(home, ".cache/ms-playwright/chromium_headless_shell-*/chrome-linux/headless_shell"),
		}
		for _, pattern := range globs {
			matches, _ := filepath.Glob(pattern)
			// Version directories sort lexicographically; newest last.
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			candidates = append(candidates, matches...)
		}
	}

	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}

	return "", errNoChrome
}

func e2eCount(t *testing.T, page *rod.Page, selector string) int {
	t.Helper()

	res, err := page.Eval("(sel) => document.querySelectorAll(sel).length", selector)
	if err != nil {
		return -1
	}
	return int(res.Value.Int())
}

// e2eWaitCount waits for the DOM to settle on want rows. Polling instead of a
// fixed sleep keeps the suite fast on a warm page and patient under -race.
func e2eWaitCount(t *testing.T, page *rod.Page, selector string, want int) {
	t.Helper()

	deadline := time.Now().Add(e2ePageTimeout)
	for {
		if got := e2eCount(t, page, selector); got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s shows %d rows after %s, want %d", selector, e2eCount(t, page, selector), e2ePageTimeout, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// e2eWaitTrue waits for a boolean JS expression.
func e2eWaitTrue(t *testing.T, page *rod.Page, expr string) {
	t.Helper()

	deadline := time.Now().Add(e2ePageTimeout)
	for {
		if res, err := page.Eval(expr); err == nil && res.Value.Bool() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never became true within %s", expr, e2ePageTimeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func e2eText(t *testing.T, page *rod.Page, selector string) string {
	t.Helper()

	el, err := page.Element(selector)
	if err != nil {
		return ""
	}
	text, err := el.Text()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}

// e2eSetValue replaces a field's value and fires the input event the view
// listens to. It exists because selecting all text depends on the platform's
// shortcut; the clearing spec must not depend on that.
func e2eSetValue(t *testing.T, page *rod.Page, selector, value string) {
	t.Helper()

	_, err := page.Eval(`(sel, next) => {
		const el = document.querySelector(sel);
		el.focus();
		el.value = next;
		el.dispatchEvent(new Event('input', { bubbles: true }));
	}`, selector, value)
	if err != nil {
		t.Fatalf("set %s: %v", selector, err)
	}
}

// e2eAttachArtifacts saves a screenshot and the DOM when a spec fails. A
// browser failure is the one thing a CI log cannot explain on its own.
func e2eAttachArtifacts(t *testing.T, page *rod.Page) {
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}

		dir := strings.TrimSpace(os.Getenv("E2E_ARTIFACT_DIR"))
		if dir == "" {
			dir = filepath.Join(os.TempDir(), "magnet-e2e")
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return
		}

		name := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
		if png, err := page.Screenshot(true, nil); err == nil {
			path := filepath.Join(dir, name+".png")
			if os.WriteFile(path, png, 0o644) == nil {
				t.Logf("screenshot: %s", path)
			}
		}
		if html, err := page.HTML(); err == nil {
			path := filepath.Join(dir, name+".html")
			if os.WriteFile(path, []byte(html), 0o644) == nil {
				t.Logf("dom: %s", path)
			}
		}
	})
}

// e2eAtoi parses a rendered count, tolerating grouping separators.
func e2eAtoi(t *testing.T, raw string) (int, bool) {
	t.Helper()

	cleaned := strings.NewReplacer(",", "", " ", "", "\u00a0", "").Replace(raw)
	if cleaned == "" {
		return 0, false
	}
	value, err := strconv.Atoi(cleaned)
	if err != nil {
		return 0, false
	}
	return value, true
}
