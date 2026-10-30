package handler

import (
	"fmt"
	"html"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testKeyword(id int32, text string, counter int32) *model.Keyword {
	return &model.Keyword{ID: &id, Keyword: text, Counter: counter}
}

// TestRenderStatistics guards the /statistics layout: keywords are a
// leaderboard (highest count first), the old "[index/id]" pairs are gone, and
// user-provided keyword text is escaped because the message is HTML.
func TestRenderStatistics(t *testing.T) {
	got := renderStatistics(statisticsView{
		Version:   "develop-5cbf932",
		BuildTime: "2026-10-30T06:02:33Z",
		History:   12853,
		Keywords: []*model.Keyword{
			testKeyword(8, "+2024-JQ05-F1969", 44),
			testKeyword(3, "车场信息化", 12),
			testKeyword(9, "a<b&c", 0),
		},
		Alarms: []*model.Keyword{testKeyword(5, "消防", 0)},
	})

	for _, want := range []string{
		"<b>magnet-bot</b> <code>develop-5cbf932</code>",
		"Build <code>2026-10-30T06:02:33Z</code>",
		"| History         | 12,853 |",
		"| Keywords        |      3 |",
		"| Related notices |     56 |",
		"| Alarm keywords  |      1 |",
		"|      44 | #8 | +2024-JQ05-F1969 |",
		"|      12 | #3 | 车场信息化       |",
		"a&lt;b&amp;c", // user input is escaped inside the table
	} {
		if !strings.Contains(got, want) {
			t.Errorf("statistics is missing %q\n---\n%s", want, got)
		}
	}

	if strings.Contains(got, "[1/") {
		t.Errorf("statistics still contains the misleading [index/id] pairs:\n%s", got)
	}
	if strings.Index(got, "#8") > strings.Index(got, "#3") {
		t.Errorf("keywords are not sorted by notice count:\n%s", got)
	}
	assertTablesAligned(t, got)
}

// assertTablesAligned checks every <pre> table: all its lines must occupy the
// same number of monospace columns, otherwise the borders do not line up.
func assertTablesAligned(t *testing.T, message string) {
	t.Helper()

	blocks := strings.Split(message, "<pre>")
	if len(blocks) < 2 {
		t.Fatal("message has no <pre> table")
	}
	for _, block := range blocks[1:] {
		table, _, _ := strings.Cut(block, "</pre>")
		want := -1
		for line := range strings.SplitSeq(table, "\n") {
			width := displayWidth(html.UnescapeString(line))
			if want == -1 {
				want = width
				continue
			}
			if width != want {
				t.Errorf("table line %q is %d columns wide, want %d", line, width, want)
			}
		}
	}
}

// TestRenderStatisticsCapsLongLists keeps the message inside Telegram's limit.
func TestRenderStatisticsCapsLongLists(t *testing.T) {
	var keywords []*model.Keyword
	for i := range statisticsKeywordLimit + 5 {
		keywords = append(keywords, testKeyword(int32(i+1), fmt.Sprintf("kw%d", i+1), int32(i)))
	}

	got := renderStatistics(statisticsView{Keywords: keywords})
	if n := strings.Count(got, " | #"); n != statisticsKeywordLimit {
		t.Errorf("rendered %d keyword rows, want %d\n%s", n, statisticsKeywordLimit, got)
	}
	if !strings.Contains(got, "…and 5 more") {
		t.Errorf("missing the truncation notice:\n%s", got)
	}
	assertTablesAligned(t, got)
}

// TestRenderStatisticsEmpty covers a user without any keyword.
func TestRenderStatisticsEmpty(t *testing.T) {
	got := renderStatistics(statisticsView{Version: "v", BuildTime: "t"})
	if !strings.Contains(got, "| Keywords") || !strings.Contains(got, "| Related notices") {
		t.Errorf("summary table is missing its rows:\n%s", got)
	}
	if strings.Contains(got, "<b>Related notices per keyword</b>") {
		t.Errorf("empty keyword list must not be rendered:\n%s", got)
	}
	if !strings.Contains(got, "/add_keywords") {
		t.Errorf("missing the hint for a user without keywords:\n%s", got)
	}
	if strings.Contains(got, "<b>Alarm keywords</b>") {
		t.Errorf("empty alarm list must not be rendered:\n%s", got)
	}
	assertTablesAligned(t, got)
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
