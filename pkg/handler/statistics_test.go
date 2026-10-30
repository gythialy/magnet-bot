package handler

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testKeyword(id int32, text string, counter int32) *model.Keyword {
	return &model.Keyword{ID: &id, Keyword: text, Counter: counter, CreatedAt: time.Date(2025, 6, 12, 0, 0, 0, 0, time.UTC)}
}

func testAlarm(id int32, text string) *model.Keyword {
	return &model.Keyword{ID: &id, Keyword: text, Type: int32(model.ALARM), CreatedAt: time.Date(2025, 6, 12, 0, 0, 0, 0, time.UTC)}
}

// buttonData lists every callback payload of a keyboard, row by row.
func buttonData(kb *models.InlineKeyboardMarkup) []string {
	if kb == nil {
		return nil
	}
	var data []string
	for _, row := range kb.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
		}
	}
	return data
}

// pagerRow returns the pager row of a list keyboard, or nil.
func pagerRow(kb *models.InlineKeyboardMarkup) []models.InlineKeyboardButton {
	if kb == nil {
		return nil
	}
	for _, row := range kb.InlineKeyboard {
		for _, button := range row {
			if strings.HasPrefix(button.CallbackData, "stats:page:") || strings.HasPrefix(button.CallbackData, "stats:apage:") {
				return row
			}
		}
	}
	return nil
}

// buttonByText returns the button with the given label, or nil.
func buttonByText(kb *models.InlineKeyboardMarkup, text string) *models.InlineKeyboardButton {
	if kb == nil {
		return nil
	}
	for _, row := range kb.InlineKeyboard {
		for i := range row {
			if row[i].Text == text {
				return &row[i]
			}
		}
	}
	return nil
}

// TestRenderStatisticsList guards the /statistics report: the counters on top,
// a leaderboard (highest count first) below, one entry per line instead of an
// ASCII table, and user-provided text escaped because the message is HTML.
func TestRenderStatisticsList(t *testing.T) {
	text, kb := renderStatisticsScreen(statisticsScreen{view: statisticsView{
		Version:   "develop-5cbf932",
		BuildTime: "2026-10-30T06:02:33Z",
		History:   12853,
		Keywords: []*model.Keyword{
			testKeyword(8, "+2024-JQ05-F1969", 44),
			testKeyword(3, "车场信息化", 12),
			testKeyword(9, "a<b&c", 0),
		},
		Alarms: []*model.Keyword{testAlarm(5, "消防")},
	}})

	for _, want := range []string{
		"<b>magnet-bot</b> <code>develop-5cbf932</code>",
		"Build <code>2026-10-30T06:02:33Z</code>",
		"<b>Overview</b>",
		"History: <b>12,853</b>",
		"Keywords: <b>3</b>",
		"Related notices: <b>56</b>",
		"Alarm keywords: <b>1</b>",
		"<b>Related notices per keyword</b> (3)",
		"<code>#8</code> +2024-JQ05-F1969 · <b>44</b>",
		"<code>#3</code> 车场信息化 · <b>12</b>",
		"<code>#9</code> a&lt;b&amp;c · <b>0</b>",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("statistics is missing %q\n---\n%s", want, text)
		}
	}

	if strings.Contains(text, "<pre>") {
		t.Errorf("the report still renders an ASCII table:\n%s", text)
	}
	if strings.Index(text, "#8") > strings.Index(text, "#3") {
		t.Errorf("keywords are not sorted by notice count:\n%s", text)
	}

	// One button per keyword, two per row, plus the alarm keyword shortcut.
	if len(kb.InlineKeyboard) != 3 {
		t.Fatalf("got %d keyboard rows, want 3:\n%v", len(kb.InlineKeyboard), buttonData(kb))
	}
	if got := len(kb.InlineKeyboard[0]); got != 2 {
		t.Errorf("first row has %d buttons, want 2", got)
	}
	if buttonByText(kb, "Alarm keywords (1)") == nil {
		t.Errorf("missing the alarm keyword shortcut:\n%v", buttonData(kb))
	}
	if got := buttonData(kb)[0]; got != "stats:kw:1:8" {
		t.Errorf("first keyword button payload = %q, want stats:kw:1:8", got)
	}
}

// TestRenderStatisticsPaging guards the page window and the pager arrows.
func TestRenderStatisticsPaging(t *testing.T) {
	var keywords []*model.Keyword
	for i := range 20 {
		keywords = append(keywords, testKeyword(int32(i+1), fmt.Sprintf("kw%02d", i+1), int32(20-i)))
	}
	view := statisticsView{Keywords: keywords}

	first, kb := renderStatisticsScreen(statisticsScreen{view: view})
	if got := strings.Count(first, " · <b>"); got != statisticsPageSize {
		t.Errorf("first page lists %d entries, want %d", got, statisticsPageSize)
	}
	if !strings.Contains(first, "kw01") || strings.Contains(first, "kw09") {
		t.Errorf("first page shows the wrong slice:\n%s", first)
	}
	if !strings.Contains(first, "(20 · 1/3)") {
		t.Errorf("missing the page indicator:\n%s", first)
	}
	// No previous arrow on the first page; the label and next arrow are there.
	pager := pagerRow(kb)
	if len(pager) != 2 {
		t.Fatalf("first-page pager has %d buttons, want 2", len(pager))
	}
	if pager[0].Text != "1/3" || pager[0].CallbackData != "stats:page:1:0" {
		t.Errorf("unexpected pager label: %+v", pager[0])
	}
	if pager[1].Text != "›" || pager[1].CallbackData != "stats:page:2:0" {
		t.Errorf("unexpected next button: %+v", pager[1])
	}

	last, kb := renderStatisticsScreen(statisticsScreen{view: view, page: 2})
	if !strings.Contains(last, "kw17") || !strings.Contains(last, "kw20") || strings.Contains(last, "kw16") {
		t.Errorf("last page shows the wrong slice:\n%s", last)
	}
	pager = pagerRow(kb)
	if len(pager) != 2 {
		t.Fatalf("last-page pager has %d buttons, want 2", len(pager))
	}
	if pager[0].Text != "‹" || pager[0].CallbackData != "stats:page:2:0" {
		t.Errorf("unexpected previous button: %+v", pager[0])
	}
	if pager[1].Text != "3/3" {
		t.Errorf("unexpected pager label: %+v", pager[1])
	}

	// A stale page past the end is clamped instead of rendering nothing.
	stale, _ := renderStatisticsScreen(statisticsScreen{view: view, page: 99})
	if !strings.Contains(stale, "kw17") {
		t.Errorf("stale page is not clamped to the last page:\n%s", stale)
	}
}

// TestRenderStatisticsEmpty covers a user without any keyword.
func TestRenderStatisticsEmpty(t *testing.T) {
	text, kb := renderStatisticsScreen(statisticsScreen{view: statisticsView{Version: "v", BuildTime: "t"}})
	if !strings.Contains(text, "Use /add_keywords to subscribe to a keyword.") {
		t.Errorf("missing the hint for a user without keywords:\n%s", text)
	}
	if !strings.Contains(text, "Keywords: <b>0</b>") {
		t.Errorf("missing the empty counters:\n%s", text)
	}
	// Telegram rejects an empty inline keyboard, so a user without keywords and
	// without alarm keywords gets no keyboard at all.
	if kb != nil {
		t.Errorf("empty report still renders a keyboard: %v", buttonData(kb))
	}
}

// TestRenderStatisticsShortensLongKeywords guards the message budget: a pasted
// paragraph is shortened, and a newline cannot break the one-entry-per-line
// layout.
func TestRenderStatisticsShortensLongKeywords(t *testing.T) {
	long := strings.Repeat("长", 120)
	text, kb := renderStatisticsScreen(statisticsScreen{view: statisticsView{
		Keywords: []*model.Keyword{testKeyword(1, long+"\nsecond line", 1)},
	}})

	if strings.Contains(text, "second line") {
		t.Errorf("a newline in a keyword leaked into a second line:\n%s", text)
	}
	if strings.Contains(text, strings.Repeat("长", statisticsTextLimit+1)) {
		t.Errorf("the long keyword is not shortened:\n%s", text)
	}
	if !strings.Contains(text, "…") {
		t.Errorf("the shortened keyword is missing its ellipsis:\n%s", text)
	}

	button := kb.InlineKeyboard[0][0]
	if got := len([]rune(button.Text)); got > statisticsButtonLimit+len("#2147483647 ")+1 {
		t.Errorf("button label is %d runes, want a bounded label: %q", got, button.Text)
	}
}

// TestRenderStatisticsDetail guards the per-keyword screen and its actions.
func TestRenderStatisticsDetail(t *testing.T) {
	view := statisticsView{
		Keywords: []*model.Keyword{testKeyword(8, "车场信息化", 12)},
		Alarms:   []*model.Keyword{testAlarm(5, "消防")},
	}

	text, kb := renderStatisticsScreen(statisticsScreen{view: view, kind: statisticsDetail, id: 8})
	for _, want := range []string{
		"<b>Keyword</b> <code>#8</code>",
		"<b>车场信息化</b>",
		"Type: <b>project</b>",
		"Related notices: <b>12</b>",
		"Created: <b>2025-06-12</b>",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("detail screen is missing %q\n---\n%s", want, text)
		}
	}
	if buttonByText(kb, "✏️ Edit") == nil || buttonByText(kb, "🗑 Delete") == nil {
		t.Errorf("detail screen is missing its actions:\n%v", buttonData(kb))
	}
	if button := buttonByText(kb, "‹ Back"); button == nil || button.CallbackData != "stats:back:1:0" {
		t.Errorf("detail screen back button = %+v", button)
	}

	alarmText, alarmKB := renderStatisticsScreen(statisticsScreen{view: view, kind: statisticsDetail, id: 5})
	if !strings.Contains(alarmText, "<b>Alarm keyword</b> <code>#5</code>") {
		t.Errorf("alarm detail screen has the wrong title:\n%s", alarmText)
	}
	if strings.Contains(alarmText, "Related notices:") {
		t.Errorf("alarm keywords carry no notice counter:\n%s", alarmText)
	}
	if button := buttonByText(alarmKB, "‹ Back"); button == nil || button.CallbackData != "stats:alarms:1:0" {
		t.Errorf("alarm detail back button = %+v", button)
	}
}

// TestRenderStatisticsEditAndDelete guards the two confirmation screens.
func TestRenderStatisticsEditAndDelete(t *testing.T) {
	view := statisticsView{Keywords: []*model.Keyword{testKeyword(8, "车场信息化", 12)}}

	editText, editKB := renderStatisticsScreen(statisticsScreen{view: view, kind: statisticsEdit, id: 8})
	if !strings.Contains(editText, "<b>Edit keyword</b> <code>#8</code>") ||
		!strings.Contains(editText, "Send the new text for <b>车场信息化</b>.") {
		t.Errorf("edit screen is wrong:\n%s", editText)
	}
	if button := buttonByText(editKB, "Cancel"); button == nil || button.CallbackData != "stats:cancel:1:8" {
		t.Errorf("edit screen cancel button = %+v", button)
	}

	deleteText, deleteKB := renderStatisticsScreen(statisticsScreen{view: view, kind: statisticsDelete, id: 8})
	if !strings.Contains(deleteText, "Remove <b>车场信息化</b>? Its <b>12</b> related notices") {
		t.Errorf("delete screen is wrong:\n%s", deleteText)
	}
	if button := buttonByText(deleteKB, "Delete #8"); button == nil || button.CallbackData != "stats:delok:1:8" {
		t.Errorf("delete confirm button = %+v", button)
	}
	if button := buttonByText(deleteKB, "Cancel"); button == nil {
		t.Errorf("delete screen has no cancel button:\n%v", buttonData(deleteKB))
	}

	// An id that disappeared while the message was open falls back to the list.
	fallback, _ := renderStatisticsScreen(statisticsScreen{view: view, kind: statisticsDetail, id: 404})
	if !strings.Contains(fallback, "<b>Overview</b>") {
		t.Errorf("missing keyword does not fall back to the list:\n%s", fallback)
	}
}

// TestRenderStatisticsAlarmList guards the alarm keyword screen.
func TestRenderStatisticsAlarmList(t *testing.T) {
	view := statisticsView{Alarms: []*model.Keyword{testAlarm(16, "中国"), testAlarm(24, "研究所")}}
	text, kb := renderStatisticsScreen(statisticsScreen{view: view, kind: statisticsAlarmList})

	for _, want := range []string{
		"<b>Alarm keywords</b> (2)",
		"<code>#16</code> 中国",
		"<code>#24</code> 研究所",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("alarm list is missing %q\n---\n%s", want, text)
		}
	}
	if button := buttonByText(kb, "‹ Back to statistics"); button == nil || button.CallbackData != "stats:back:1:0" {
		t.Errorf("alarm list back button = %+v", button)
	}
	if button := buttonByText(kb, "#16 中国"); button == nil || button.CallbackData != "stats:kw:1:16" {
		t.Errorf("alarm keyword button = %+v", button)
	}
}

// TestStatisticsCallbackRoundTrip guards the "stats:<action>:<page>:<id>" wire
// format, including the payloads Telegram rejects when they grow past 64 bytes.
func TestStatisticsCallbackRoundTrip(t *testing.T) {
	actions := []string{
		statisticsActionPage, statisticsActionAlarms, statisticsActionAlarmPg,
		statisticsActionBack, statisticsActionOpen, statisticsActionEdit,
		statisticsActionDelete, statisticsActionDelOK, statisticsActionCancel,
	}
	for _, action := range actions {
		data := statisticsCallbackData(action, 12, 2147483647)
		if len(data) > 64 {
			t.Errorf("callback payload %q is %d bytes, Telegram allows 64", data, len(data))
		}
		gotAction, page, id, err := parseStatisticsCallback(data)
		if err != nil {
			t.Fatalf("parseStatisticsCallback(%q) error = %v", data, err)
		}
		if gotAction != action || page != 12 || id != 2147483647 {
			t.Errorf("parseStatisticsCallback(%q) = (%q, %d, %d)", data, gotAction, page, id)
		}
	}
}

// TestParseStatisticsCallbackRejectsGarbage guards against indexing malformed
// payloads blindly: any client can send whatever it wants.
func TestParseStatisticsCallbackRejectsGarbage(t *testing.T) {
	for _, data := range []string{
		"",
		"stats",
		"stats:",
		"stats:page",
		"stats:page:1",
		"stats:page:0:0",
		"stats:page:-1:0",
		"stats:page:x:0",
		"stats:page:1:abc",
		"stats:page:1:-1",
		"stats::1:0",
		"search:1:x",
	} {
		if _, _, _, err := parseStatisticsCallback(data); err == nil {
			t.Errorf("parseStatisticsCallback(%q) accepted malformed data", data)
		}
	}
}

func TestGroupDigits(t *testing.T) {
	tests := map[int64]string{
		0:       "0",
		999:     "999",
		1000:    "1,000",
		12853:   "12,853",
		1234567: "1,234,567",
		-1234:   "-1,234",
	}
	for in, want := range tests {
		if got := groupDigits(in); got != want {
			t.Errorf("groupDigits(%d) = %q, want %q", in, got, want)
		}
	}
}

func setupStatisticsDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/statistics.db"), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Keyword{}, &model.KeywordNotice{}); err != nil {
		t.Fatal(err)
	}
	dal.SetDefault(db)
	return db
}

// TestRecordKeywordMatchesCountsDistinctNotices guards the statistics semantics:
// the recorder is fed the notices that were actually delivered, a keyword's
// counter is the number of distinct related notices, so recording the same
// delivery again must not increase it.
func TestRecordKeywordMatchesCountsDistinctNotices(t *testing.T) {
	db := setupStatisticsDB(t)

	kw1 := &model.Keyword{UserID: 1, Keyword: "k1", Type: int32(model.PROJECT)}
	kw2 := &model.Keyword{UserID: 1, Keyword: "k2", Type: int32(model.PROJECT)}
	for _, kw := range []*model.Keyword{kw1, kw2} {
		if err := db.Create(kw).Error; err != nil {
			t.Fatal(err)
		}
	}
	id1, id2 := *kw1.ID, *kw2.ID

	counterOf := func(id int32) int32 {
		t.Helper()
		var kw model.Keyword
		if err := db.First(&kw, id).Error; err != nil {
			t.Fatal(err)
		}
		return kw.Counter
	}

	processor := &InfoProcessor{ctx: testBotContext("example.com")}
	projects := []*Project{
		{Pageurl: "https://example.com/n1", keywordIDs: []int32{id1}},
		{Pageurl: "https://example.com/n2", keywordIDs: []int32{id1, id2}},
	}
	processor.recordKeywordMatches(projects)

	if got := counterOf(id1); got != 2 {
		t.Fatalf("keyword 1 counter = %d, want 2", got)
	}
	if got := counterOf(id2); got != 1 {
		t.Fatalf("keyword 2 counter = %d, want 1", got)
	}

	// Re-crawling the same window must not count the notices again.
	processor.recordKeywordMatches(projects)
	if got := counterOf(id1); got != 2 {
		t.Fatalf("keyword 1 counter after re-crawl = %d, want 2", got)
	}
	if got := counterOf(id2); got != 1 {
		t.Fatalf("keyword 2 counter after re-crawl = %d, want 1", got)
	}

	// A keyword added later still counts a notice delivered before it existed.
	kw3 := &model.Keyword{UserID: 1, Keyword: "k3", Type: int32(model.PROJECT)}
	if err := db.Create(kw3).Error; err != nil {
		t.Fatal(err)
	}
	processor.recordKeywordMatches([]*Project{
		{Pageurl: "https://example.com/n1", keywordIDs: []int32{*kw3.ID}},
	})
	if got := counterOf(*kw3.ID); got != 1 {
		t.Fatalf("late keyword counter = %d, want 1", got)
	}
}
