package handler

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gythialy/magnet/pkg/config"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// noticeContentTestContext builds a BotContext whose config names the synthetic
// notice host, with a private database bound to the package DAL.
func noticeContentTestContext(t *testing.T, host string) *BotContext {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "notice_content.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&model.NoticeContent{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	previous := *dal.Q
	dal.SetDefault(db)
	t.Cleanup(func() {
		*dal.Q = previous
		dal.Alarm = &dal.Q.Alarm
		dal.History = &dal.Q.History
		dal.Keyword = &dal.Q.Keyword
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	return &BotContext{Config: &config.ServiceConfig{MessageServerUrl: host}}
}

// TestWarmNoticeContentCachesSanitizedBody proves the warm-up path stores
// markup the app can insert: the fragment is sanitized on write, so the cache
// can never hold the raw crawl payload.
func TestWarmNoticeContentCachesSanitizedBody(t *testing.T) {
	ctx := noticeContentTestContext(t, "notices.example.com")

	ctx.warmNoticeContent(&Project{
		Pageurl:    "https://notices.example.com/notice/1",
		RawContent: `<div><p onclick="steal()">正文</p><script>alert(1)</script></div>`,
	})

	stored, err := dal.GetNoticeContent("https://notices.example.com/notice/1")
	if err != nil {
		t.Fatalf("GetNoticeContent() error = %v", err)
	}
	for _, want := range []string{"<p>正文</p>"} {
		if !strings.Contains(stored.HTML, want) {
			t.Errorf("stored body is missing %q: %s", want, stored.HTML)
		}
	}
	for _, unwanted := range []string{"onclick", "script", "alert("} {
		if strings.Contains(stored.HTML, unwanted) {
			t.Errorf("stored body carries %q: %s", unwanted, stored.HTML)
		}
	}
}

// TestWarmNoticeContentDoesNotOverwriteRefresh guards the reader's forced
// refresh: the next crawl cycle must not roll the cache back to the older body.
func TestWarmNoticeContentDoesNotOverwriteRefresh(t *testing.T) {
	ctx := noticeContentTestContext(t, "notices.example.com")
	const url = "https://notices.example.com/notice/1"

	if err := dal.SaveNoticeContent(url, "<p>刷新后</p>", time.Unix(1_700_000_000, 0)); err != nil {
		t.Fatalf("SaveNoticeContent() error = %v", err)
	}

	ctx.warmNoticeContent(&Project{Pageurl: url, RawContent: "<p>抓取时</p>"})

	stored, err := dal.GetNoticeContent(url)
	if err != nil {
		t.Fatalf("GetNoticeContent() error = %v", err)
	}
	if stored.HTML != "<p>刷新后</p>" {
		t.Errorf("stored body = %q, want the refreshed copy to survive", stored.HTML)
	}
}

// TestWarmNoticeContentSkipsEmptyPayload keeps the cache from filling with rows
// that only say "there was no body".
func TestWarmNoticeContentSkipsEmptyPayload(t *testing.T) {
	ctx := noticeContentTestContext(t, "notices.example.com")

	ctx.warmNoticeContent(&Project{Pageurl: "https://notices.example.com/notice/1"})
	ctx.warmNoticeContent(&Project{Pageurl: "https://notices.example.com/notice/2", RawContent: "<script>x()</script>"})
	ctx.warmNoticeContent(nil)

	for _, url := range []string{
		"https://notices.example.com/notice/1",
		"https://notices.example.com/notice/2",
	} {
		if _, err := dal.GetNoticeContent(url); err == nil {
			t.Errorf("GetNoticeContent(%q) succeeded, want a miss", url)
		}
	}
}
