package dal

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gythialy/magnet/pkg/model"
	"gorm.io/gorm"
)

func setupKeywordNoticeTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/keyword_notice.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.KeywordNotice{}); err != nil {
		t.Fatal(err)
	}
	SetDefault(db)
	return db
}

// TestMarkKeywordMatchDeduplicates guards the counter's dedupe: the same notice
// must be recorded once per keyword, so re-crawling a window cannot inflate the
// number of related notices.
func TestMarkKeywordMatchDeduplicates(t *testing.T) {
	setupKeywordNoticeTestDB(t)
	const url = "https://example.com/notice/1"

	fresh, err := MarkKeywordMatch(1, url)
	if err != nil || !fresh {
		t.Fatalf("first MarkKeywordMatch() = %v, %v; want true, nil", fresh, err)
	}

	fresh, err = MarkKeywordMatch(1, url)
	if err != nil || fresh {
		t.Fatalf("duplicate MarkKeywordMatch() = %v, %v; want false, nil", fresh, err)
	}

	// Another keyword matching the same notice is a new pair.
	fresh, err = MarkKeywordMatch(2, url)
	if err != nil || !fresh {
		t.Fatalf("other keyword MarkKeywordMatch() = %v, %v; want true, nil", fresh, err)
	}
}

// TestPruneKeywordMatches keeps the dedupe log bounded.
func TestPruneKeywordMatches(t *testing.T) {
	db := setupKeywordNoticeTestDB(t)
	now := time.Now()

	if err := db.Create(&model.KeywordNotice{
		KeywordID: 1,
		URL:       "https://example.com/old",
		CreatedAt: now.Add(-40 * 24 * time.Hour),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := MarkKeywordMatch(1, "https://example.com/fresh"); err != nil {
		t.Fatal(err)
	}

	removed, err := PruneKeywordMatches(now)
	if err != nil {
		t.Fatalf("PruneKeywordMatches() error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("PruneKeywordMatches() removed %d rows, want 1", removed)
	}

	var remaining int64
	if err := db.Model(&model.KeywordNotice{}).Count(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("%d rows left, want 1", remaining)
	}
}
