//go:build e2e

package server

// Browser-driven specs for the v5 redesign. They assert behaviour a restyle
// must preserve — the gate, the head's freshness and new count, search as a
// mode of the head, one 44px row action, the sheet's field list, the watchlist
// cleanup, month groups and the tab badge — rather than class names, so the
// same specs keep their meaning while the layout changes.

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
)

// e2eClock matches a rendered time of day; the surrounding wording is the
// view's business, not the spec's.
var e2eClock = regexp.MustCompile(`\d{1,2}:\d{2}`)

// Without credentials every request would be rejected, so the shell must stop
// at the gate and say what to do instead of showing an empty app.
func TestE2EGateWithoutInitData(t *testing.T) {
	base, _, _ := e2eApp(t)
	page := e2ePage(t, base, "")

	e2eWaitTrue(t, page, "() => !document.getElementById('app-gate').hidden")

	if shellHidden := page.MustEval("() => document.getElementById('app-shell').hidden").Bool(); !shellHidden {
		t.Error("the app shell stays visible behind the gate")
	}
	if message := e2eText(t, page, "#app-gate"); message == "" {
		t.Error("the gate renders no message")
	}
}

// The Today column must arrive with today's notices, a freshness stamp and a
// count that agrees with what is on screen. The header is the part the
// redesign moves around, so the spec pins the invariant (head count equals rows
// shown) and not the place the number sits.
func TestE2ETodayRendersTodayNotices(t *testing.T) {
	base, initData, now := e2eApp(t)
	titles := e2eSeedToday(t, now)

	page := e2ePage(t, base, initData)
	e2eWaitCount(t, page, e2eRowSelector, len(titles))

	if updated := e2eText(t, page, `[data-slot="updated"]`); !e2eClock.MatchString(updated) {
		t.Errorf("freshness stamp = %q, want a time of day", updated)
	}

	if raw := e2eText(t, page, `[data-slot="head-count"]`); raw != "" {
		count, ok := e2eAtoi(t, raw)
		if !ok {
			t.Errorf("head count %q is not a number", raw)
		} else if count != len(titles) {
			t.Errorf("head count = %d, want %d (the rows actually shown)", count, len(titles))
		}
	}

	// Newest first: the seeded order is the order the column must read in.
	if first := e2eText(t, page, e2eRowSelector); !strings.Contains(first, titles[0]) {
		t.Errorf("first row = %q, want it to start with %q", first, titles[0])
	}

	if label := e2eText(t, page, `#tabbar [aria-current="page"]`); !strings.Contains(label, "今日") && !strings.Contains(label, "Today") {
		t.Errorf("active tab = %q, want Today", label)
	}
}

// Search is the interaction the redesign moves into the header, so the spec
// opens it the way either layout allows and then only judges the result: the
// list narrows to the matching notice and comes back when the term is cleared.
func TestE2ESearchNarrowsTheList(t *testing.T) {
	base, initData, now := e2eApp(t)
	titles := e2eSeedToday(t, now)

	page := e2ePage(t, base, initData)
	e2eWaitCount(t, page, e2eRowSelector, len(titles))

	input := e2eOpenSearch(t, page)
	input.MustInput("耗材")
	e2eWaitCount(t, page, e2eRowSelector, 1)

	if row := e2eText(t, page, e2eRowSelector); !strings.Contains(row, "耗材") {
		t.Errorf("the surviving row = %q, want the 耗材 notice", row)
	}

	e2eSetValue(t, page, `[data-role="search"]`, "")
	e2eWaitCount(t, page, e2eRowSelector, len(titles))
}

// Escape leaves search without keeping the query: the column comes back, the
// toggle is focused again, and a reload is the view's business, not the key's.
func TestE2ESearchEscapeRestoresList(t *testing.T) {
	base, initData, now := e2eApp(t)
	titles := e2eSeedToday(t, now)

	page := e2ePage(t, base, initData)
	e2eWaitCount(t, page, e2eRowSelector, len(titles))

	input := e2eOpenSearch(t, page)
	input.MustInput("耗材")
	e2eWaitCount(t, page, e2eRowSelector, 1)

	page.Keyboard.MustType(inputKeyEscape)

	e2eWaitCount(t, page, e2eRowSelector, len(titles))
	if hidden := page.MustEval(`() => document.querySelector('[data-slot="search-mode"]').hidden`).Bool(); !hidden {
		t.Error("Escape left the search field on screen")
	}
	if expanded := page.MustEval(`() => document.querySelector('[data-role="search-toggle"]').getAttribute('aria-expanded')`).Str(); expanded != "false" {
		t.Errorf("search toggle aria-expanded = %q after Escape, want false", expanded)
	}
	if focused := page.MustEval(`() => document.activeElement === document.querySelector('[data-role="search-toggle"]')`).Bool(); !focused {
		t.Error("Escape did not return focus to the search toggle")
	}
}

// e2eOpenSearch reveals the search field when the page keeps it behind an
// action, and returns it either way.
func e2eOpenSearch(t *testing.T, page *rod.Page) *rod.Element {
	t.Helper()

	if field, err := page.Timeout(2 * time.Second).Element(`[data-role="search"]`); err == nil {
		if visible, _ := field.Visible(); visible {
			return field
		}
	}

	toggle, err := page.Timeout(2 * time.Second).Element(
		`[data-role="search-toggle"], button[aria-label*="搜索"], button[aria-label*="Search"]`)
	if err != nil {
		t.Fatalf("no search field and no search action on the page: %v", err)
	}
	toggle.MustClick()

	field, err := page.Timeout(5 * time.Second).Element(`[data-role="search"]`)
	if err != nil {
		t.Fatalf("the search action did not reveal a field: %v", err)
	}
	return field
}

// The header answers "what arrived since I last looked". A notice stored after
// that visit must raise the badge on the next load; the first load has no
// watermark to compare against, so it must not claim anything is new.
func TestE2EHeaderCountsNewNotices(t *testing.T) {
	base, initData, now := e2eApp(t)
	titles := e2eSeedToday(t, now)

	page := e2ePage(t, base, initData)
	e2eWaitCount(t, page, e2eRowSelector, len(titles))

	if hidden := page.MustEval("() => document.querySelector('[data-slot=\"new-count\"]').hidden").Bool(); !hidden {
		t.Error("a first visit claims unseen notices")
	}

	// A notice stored after the visit: the column remembers the newest row it
	// showed, so this one is past the watermark.
	seedHistories(t, &model.History{
		UserID:    testUserID,
		URL:       "https://notice.example/late",
		Title:     "云陵区人民医院医用耗材配送服务（三次）招标公告（2026-GC-0449）",
		UpdatedAt: now.Add(2 * time.Minute),
	})

	page.MustReload()
	e2eWaitCount(t, page, e2eRowSelector, len(titles)+1)
	e2eWaitTrue(t, page, "() => !document.querySelector('[data-slot=\"new-count\"]').hidden")

	badge := e2eText(t, page, `[data-slot="new-count"]`)
	if count, ok := e2eAtoi(t, firstNumber(badge)); !ok || count != 1 {
		t.Fatalf("new badge = %q, want it to report exactly one new notice", badge)
	}
}

// One action per row, and it is large enough to hit: the redesign moved
// conversion into the sheet, so the list must not keep three small targets.
// The sheet itself is the place the project code and the copy action live.
func TestE2ERowActionAndNoticeFields(t *testing.T) {
	base, initData, now := e2eApp(t)
	titles := e2eSeedToday(t, now)

	page := e2ePage(t, base, initData)
	e2eWaitCount(t, page, e2eRowSelector, len(titles))

	// Compact means "not the title button": the title spans the text column,
	// the action is the icon-only control at the right edge.
	probe := page.MustEval(`() => {
		const row = document.querySelector('#view-today [data-slot="list"] article');
		const buttons = Array.from(row.querySelectorAll('button'));
		const compact = buttons.filter((b) => b.getBoundingClientRect().width <= 60);
		const small = compact.filter((b) => {
			const r = b.getBoundingClientRect();
			return r.width < 44 || r.height < 44;
		});
		return compact.length + "|" + small.length + "|" + (compact[0] ? compact[0].getAttribute("aria-label") : "");
	}`).Str()

	parts := strings.Split(probe, "|")
	if len(parts) != 3 {
		t.Fatalf("probe = %q, want count|small|label", probe)
	}
	if parts[0] != "1" {
		t.Fatalf("row has %s compact actions, want exactly 1", parts[0])
	}
	if parts[1] != "0" {
		t.Fatalf("%s row action(s) are smaller than 44x44", parts[1])
	}

	page.MustElement(`#view-today [data-slot="list"] article button`).MustClick()
	e2eWaitTrue(t, page, `() => { const el = document.querySelector('[data-slot="detail"]'); return el && !el.hidden; }`)

	fields := e2eCount(t, page, `[data-slot="detail-fields"] .field-row`)
	if fields < 2 {
		t.Fatalf("detail fields = %d, want the project code, the date and the source", fields)
	}
	if !strings.Contains(e2eText(t, page, `[data-slot="detail-fields"]`), "2026-ZB-1187") {
		t.Error("the sheet does not show the notice's project code")
	}
	if !strings.Contains(e2eText(t, page, `[data-slot="detail"] .sheet-actions`), "打开") &&
		!strings.Contains(e2eText(t, page, `[data-slot="detail"] .sheet-actions`), "Open") {
		t.Error("the sheet's primary action is not the one that opens the notice")
	}
}

// The watchlist's cleanup list: a word that never matched is offered as its own
// list, so pruning is a decision instead of a scan through 42 rows.
func TestE2EWatchlistNeverMatchedFilter(t *testing.T) {
	base, initData, now := e2eApp(t)

	seedKeywords(t,
		&model.Keyword{
			ID: new(int32(1)), UserID: testUserID, Type: int32(model.PROJECT),
			Keyword: "数字化", Counter: 3, CreatedAt: now.AddDate(0, 0, -60),
		},
		&model.Keyword{
			ID: new(int32(2)), UserID: testUserID, Type: int32(model.PROJECT),
			Keyword: "老旧项目", Counter: 2, CreatedAt: now.AddDate(0, 0, -45),
		},
		&model.Keyword{
			ID: new(int32(3)), UserID: testUserID, Type: int32(model.PROJECT),
			Keyword: "智慧园区", Counter: 0, CreatedAt: now.AddDate(0, 0, -92),
		},
	)
	if _, err := dal.MarkKeywordMatch(1, "https://notice.example/a"); err != nil {
		t.Fatalf("record match: %v", err)
	}

	page := e2ePage(t, base, initData)
	page.MustElement(`#tabbar [data-route="#/library"]`).MustClick()

	e2eWaitCount(t, page, `#view-library [data-slot="list"] .row`, 3)

	// Three row actions and each one a 44px target; the canvas draws the same
	// three, so a future decluttering has to update both on purpose.
	probe := page.MustEval(`() => {
		const row = document.querySelector('#view-library [data-slot="list"] .row');
		const buttons = Array.from(row.querySelectorAll('.row-actions button'));
		const small = buttons.filter((b) => {
			const r = b.getBoundingClientRect();
			return r.width < 44 || r.height < 44;
		});
		return buttons.length + "|" + small.length;
	}`).Str()
	if probe != "3|0" {
		t.Fatalf("row actions = %q (count|small), want 3|0", probe)
	}

	// A word with matches but nothing inside the dedupe window says so in words;
	// only a zero counter earns the tag. Both are asserted because the two
	// states look alike in the API (lastHitAt absent) and must not in the view.
	quiet := e2eText(t, page, `#view-library [data-slot="list"] .row:nth-child(2) .row-meta`)
	if !strings.Contains(quiet, "no match in 30 days") && !strings.Contains(quiet, "30 天内无命中") {
		t.Errorf("quiet row meta = %q, want the no-match-in-window wording", quiet)
	}

	// The tag is the visible "never matched" marker; the copy around it is
	// localised, so the spec asserts the marker, not its wording.
	if tag := e2eText(t, page, `#view-library [data-slot="list"] .row:nth-child(3) .row-meta .tag`); tag == "" {
		t.Error("the never-matched row carries no tag")
	}

	cleanup := page.MustElement(`#view-library [data-slot="cleanup"]`)
	if hidden, _ := cleanup.Visible(); hidden == false {
		t.Fatal("the cleanup row is hidden although a word never matched")
	}
	page.MustElement(`#view-library [data-role="only-never"]`).MustClick()

	e2eWaitCount(t, page, `#view-library [data-slot="list"] .row`, 1)
	if rows := page.MustEval(`() => document.querySelector('#view-library [data-slot="list"]').textContent`).Str(); !strings.Contains(rows, "智慧园区") {
		t.Fatalf("filtered list = %q, want only the never-matched word", rows)
	}
}

// Records group by month, and opening the tab clears the shell's badge because
// reading the page is what makes those records seen.
func TestE2ERecordsGroupedByMonth(t *testing.T) {
	base, initData, now := e2eApp(t)

	seedAlarms(t,
		&model.Alarm{
			UserID: testUserID, BusinessID: "b1", CreditName: "星海恒信科技有限公司", CreditCode: "c1",
			StartDate: now.AddDate(0, 0, -3), PageUrl1: "https://notice.example/1", NoticeID: "n1",
		},
		&model.Alarm{
			UserID: testUserID, BusinessID: "b2", CreditName: "云陵建工集团有限公司", CreditCode: "c2",
			StartDate: now.AddDate(0, -2, 0), PageUrl1: "https://notice.example/2", NoticeID: "n2",
		},
	)

	page := e2ePage(t, base, initData)
	page.MustElement(`#tabbar [data-route="#/records"]`).MustClick()

	e2eWaitCount(t, page, `#view-records [data-slot="list"] .row`, 2)
	if groups := e2eCount(t, page, `#view-records .records-group`); groups != 2 {
		t.Fatalf("month groups = %d, want 2 (the two records are in different months)", groups)
	}

	seen := page.MustEval(`() => window.localStorage.getItem('magnet.records.seenAt')`).Str()
	if seen == "" {
		t.Error("opening the records tab did not store the watermark")
	}
}

// The tab badge counts punishments that start after the device's watermark and
// goes away when the reader opens the tab: the count is a hint, not a to-do.
func TestE2ERecordsBadgeCountsAndClears(t *testing.T) {
	base, initData, now := e2eApp(t)

	page := e2ePage(t, base, initData)

	// Read the tab once so the device stores its watermark.
	page.MustElement(`#tabbar [data-route="#/records"]`).MustClick()
	e2eWaitTrue(t, page, `() => !!window.localStorage.getItem('magnet.records.seenAt')`)
	before := page.MustEval(`() => window.localStorage.getItem('magnet.records.seenAt')`).Str()

	page.MustElement(`#tabbar [data-route="#/today"]`).MustClick()

	// A record that starts after that watermark is what the badge counts. The
	// seat is synthetic, so its start time is simply later than now.
	seedAlarms(t, &model.Alarm{
		UserID: testUserID, BusinessID: "b1", CreditName: "星海恒信科技有限公司", CreditCode: "c1",
		StartDate: now.Add(time.Hour), PageUrl1: "https://notice.example/1", NoticeID: "n1",
	})

	page.MustReload()
	e2eWaitTrue(t, page, `() => { const b = document.querySelector('#tabbar [data-route="#/records"] .tab-badge'); return b && !b.hidden; }`)

	if badge := e2eText(t, page, `#tabbar [data-route="#/records"] .tab-badge`); badge != "1" {
		t.Fatalf("tab badge = %q, want 1", badge)
	}
	if hidden := page.MustEval(`() => document.querySelector('#tabbar [data-route="#/records"] .tab-badge').getAttribute('aria-hidden')`).Str(); hidden != "true" {
		t.Errorf("tab badge aria-hidden = %q, want true (decoration for screen readers)", hidden)
	}

	page.MustElement(`#tabbar [data-route="#/records"]`).MustClick()
	e2eWaitTrue(t, page, `() => { const b = document.querySelector('#tabbar [data-route="#/records"] .tab-badge'); return !b || b.hidden; }`)

	after := page.MustEval(`() => window.localStorage.getItem('magnet.records.seenAt')`).Str()
	if after == "" || after == before {
		t.Errorf("watermark after reading = %q, want it to move past %q", after, before)
	}
}

// inputKeyEscape is rod's Escape key; a local `input` variable would otherwise
// shadow the package name in the specs above.
var inputKeyEscape = input.Escape

// firstNumber pulls the leading digits out of a rendered count ("3 new").
func firstNumber(text string) string {
	digits := regexp.MustCompile(`\d+`).FindString(text)
	return digits
}

// Losing the network must explain itself without throwing away the rows: the
// strip names the state and the list stays readable.
func TestE2EOfflineStrip(t *testing.T) {
	base, initData, now := e2eApp(t)
	titles := e2eSeedToday(t, now)

	page := e2ePage(t, base, initData)
	e2eWaitCount(t, page, e2eRowSelector, len(titles))

	if err := (proto.NetworkEnable{}).Call(page); err != nil {
		t.Fatalf("enable network domain: %v", err)
	}
	if err := (proto.NetworkEmulateNetworkConditions{Offline: true}).Call(page); err != nil {
		t.Fatalf("go offline: %v", err)
	}

	e2eWaitTrue(t, page, "() => { const el = document.querySelector('[data-slot=\"offline\"]'); return el && !el.hidden; }")
	if rows := e2eCount(t, page, e2eRowSelector); rows != len(titles) {
		t.Fatalf("offline hid the rows: %d on screen, want %d", rows, len(titles))
	}

	if err := (proto.NetworkEmulateNetworkConditions{Offline: false}).Call(page); err != nil {
		t.Fatalf("go online: %v", err)
	}
	e2eWaitTrue(t, page, "() => document.querySelector('[data-slot=\"offline\"]').hidden")
}
