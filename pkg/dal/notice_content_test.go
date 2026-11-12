package dal

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gythialy/magnet/pkg/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupNoticeContentTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "notice_content.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.NoticeContent{}, &model.History{}); err != nil {
		t.Fatal(err)
	}

	previous := *Q
	SetDefault(db)
	t.Cleanup(func() {
		*Q = previous
		Alarm = &Q.Alarm
		History = &Q.History
		Keyword = &Q.Keyword
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// TestNoticeContentRoundTrip covers the normal read path and the explicit miss.
func TestNoticeContentRoundTrip(t *testing.T) {
	setupNoticeContentTestDB(t)

	const url = "https://notices.example.com/1.html"
	if _, err := GetNoticeContent(url); !errors.Is(err, ErrNoticeContentMissing) {
		t.Fatalf("GetNoticeContent() error = %v, want ErrNoticeContentMissing", err)
	}

	now := time.Now().Truncate(time.Second)
	if err := SaveNoticeContent(url, "<p>正文</p>", now); err != nil {
		t.Fatalf("SaveNoticeContent() error = %v", err)
	}

	row, err := GetNoticeContent(url)
	if err != nil {
		t.Fatalf("GetNoticeContent() error = %v", err)
	}
	if row.HTML != "<p>正文</p>" {
		t.Errorf("HTML = %q, want %q", row.HTML, "<p>正文</p>")
	}
	if !row.FetchedAt.Equal(now) {
		t.Errorf("FetchedAt = %v, want %v", row.FetchedAt, now)
	}
}

// TestSaveNoticeContentReplacesOnRefresh pins that a forced refresh wins: the
// reader asked for the notice as it is now, so the newer body must be what the
// next open serves.
func TestSaveNoticeContentReplacesOnRefresh(t *testing.T) {
	setupNoticeContentTestDB(t)

	const url = "https://notices.example.com/1.html"
	now := time.Now().Truncate(time.Second)
	if err := SaveNoticeContent(url, "<p>旧</p>", now); err != nil {
		t.Fatalf("SaveNoticeContent() error = %v", err)
	}
	if err := SaveNoticeContent(url, "<p>新</p>", now.Add(time.Hour)); err != nil {
		t.Fatalf("SaveNoticeContent() error = %v", err)
	}

	row, err := GetNoticeContent(url)
	if err != nil {
		t.Fatalf("GetNoticeContent() error = %v", err)
	}
	if row.HTML != "<p>新</p>" {
		t.Errorf("HTML = %q, want the refreshed body", row.HTML)
	}
}

// TestSaveNoticeContentIfAbsentKeepsTheRefreshedCopy guards the warm-up path: a
// crawl that happens to fetch the notice again must not roll back a body the
// reader refreshed.
func TestSaveNoticeContentIfAbsentKeepsTheRefreshedCopy(t *testing.T) {
	setupNoticeContentTestDB(t)

	const url = "https://notices.example.com/1.html"
	now := time.Now().Truncate(time.Second)
	if err := SaveNoticeContent(url, "<p>刷新后</p>", now); err != nil {
		t.Fatalf("SaveNoticeContent() error = %v", err)
	}

	inserted, err := SaveNoticeContentIfAbsent(url, "<p>抓取时</p>", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("SaveNoticeContentIfAbsent() error = %v", err)
	}
	if inserted {
		t.Error("SaveNoticeContentIfAbsent() inserted over an existing row")
	}

	row, err := GetNoticeContent(url)
	if err != nil {
		t.Fatalf("GetNoticeContent() error = %v", err)
	}
	if row.HTML != "<p>刷新后</p>" {
		t.Errorf("HTML = %q, want the refreshed body to survive", row.HTML)
	}
}

// TestPruneNoticeContentsDropsOrphansAndExpired covers both retention rules: a
// body whose notice is gone, and a body past the window.
func TestPruneNoticeContentsDropsOrphansAndExpired(t *testing.T) {
	setupNoticeContentTestDB(t)

	now := time.Now().Truncate(time.Second)
	const kept = "https://notices.example.com/kept.html"
	const orphan = "https://notices.example.com/orphan.html"

	if err := History.Insert([]*model.History{{UserID: 1, URL: kept, Title: "保留", UpdatedAt: now}}); err != nil {
		t.Fatalf("insert history: %v", err)
	}
	if err := SaveNoticeContent(kept, "<p>保留</p>", now); err != nil {
		t.Fatalf("SaveNoticeContent() error = %v", err)
	}
	// Cached while the row existed, then the history was cleaned.
	if err := SaveNoticeContent(orphan, "<p>孤儿</p>", now); err != nil {
		t.Fatalf("SaveNoticeContent() error = %v", err)
	}

	removed, err := PruneNoticeContents(now)
	if err != nil {
		t.Fatalf("PruneNoticeContents() error = %v", err)
	}
	if removed != 1 {
		t.Errorf("PruneNoticeContents() removed %d rows, want 1", removed)
	}
	if _, err := GetNoticeContent(kept); err != nil {
		t.Errorf("GetNoticeContent(kept) error = %v, want the row to survive", err)
	}
	if _, err := GetNoticeContent(orphan); !errors.Is(err, ErrNoticeContentMissing) {
		t.Errorf("GetNoticeContent(orphan) error = %v, want a miss", err)
	}
}

// TestPruneNoticeContentsDropsExpired keeps the retention window honest even
// when the notice is still in the list.
func TestPruneNoticeContentsDropsExpired(t *testing.T) {
	setupNoticeContentTestDB(t)

	now := time.Now().Truncate(time.Second)
	const url = "https://notices.example.com/old.html"
	if err := History.Insert([]*model.History{{UserID: 1, URL: url, Title: "旧", UpdatedAt: now}}); err != nil {
		t.Fatalf("insert history: %v", err)
	}
	if err := SaveNoticeContent(url, "<p>旧</p>", now.Add(-31*24*time.Hour)); err != nil {
		t.Fatalf("SaveNoticeContent() error = %v", err)
	}

	removed, err := PruneNoticeContents(now)
	if err != nil {
		t.Fatalf("PruneNoticeContents() error = %v", err)
	}
	if removed != 1 {
		t.Errorf("PruneNoticeContents() removed %d rows, want 1", removed)
	}
}
