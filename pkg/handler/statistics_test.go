package handler

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

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
