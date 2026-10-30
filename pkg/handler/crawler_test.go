package handler

import (
	"os"
	"testing"

	"github.com/gythialy/magnet/pkg/config"
	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/model"

	"github.com/glebarez/sqlite"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/utils"
)

func testBotContext(serverUrl string) *BotContext {
	nopLogger := zerolog.Nop()
	return &BotContext{
		Logger: &utils.Logger{Logger: &nopLogger},
		Config: &config.ServiceConfig{
			MessageServerUrl: serverUrl,
		},
	}
}

// TestCrawlerCrawlDays guards the look-back window: an invalid value must not
// reach the API query, where it would produce an empty window.
func TestCrawlerCrawlDays(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{"unset", "", crawlDays},
		{"valid", "3", 3},
		{"zero", "0", crawlDays},
		{"negative", "-2", crawlDays},
		{"not a number", "week", crawlDays},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(constant.CrawlDays, tt.value)
			if got := (&Crawler{}).crawlDays(); got != tt.want {
				t.Fatalf("crawlDays() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestCrawler_absoluteURL guards the link building: notice paths must become
// absolute URLs, otherwise the pushed links are not clickable.
func TestCrawler_absoluteURL(t *testing.T) {
	crawler := NewCrawler(testBotContext("example.com"))

	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/site/page.html", "https://example.com/site/page.html"},
		{"site/page.html", "https://example.com/site/page.html"},
		{"http://example.com/x", "http://example.com/x"},
		{"https://other.example.com/x", "https://other.example.com/x"},
	}
	for _, tt := range tests {
		if got := crawler.absoluteURL(tt.in); got != tt.want {
			t.Errorf("absoluteURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The two tests below talk to the live procurement API, so they only run when
// SERVER_URL is set explicitly.
func requireLiveServer(t *testing.T) {
	t.Helper()
	if os.Getenv("SERVER_URL") == "" {
		t.Skip("SERVER_URL not set; skipping live crawler test")
	}
}

func TestCrawler_Get(t *testing.T) {
	requireLiveServer(t)

	results := NewCrawler(testBotContext(config.MessageServerUrl())).Projects()
	t.Logf("fetched %d projects", len(results))
}

func TestCrawler_Fetch(t *testing.T) {
	requireLiveServer(t)

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/alarm.db"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Alarm{}); err != nil {
		t.Fatal(err)
	}
	dal.SetDefault(db)

	crawler := NewCrawler(testBotContext(config.MessageServerUrl()))
	userId := int64(1111)
	result := crawler.Alarms([]string{"中国"}, userId)
	t.Logf("fetched %d alarms", len(result))

	if len(result) == 0 {
		return
	}
	if err := dal.Alarm.Insert(result); err != nil {
		t.Fatalf("insert alarms: %v", err)
	}
	// Inserting the same alarms twice must upsert, not fail.
	if err := dal.Alarm.Insert(result); err != nil {
		t.Fatalf("re-insert alarms: %v", err)
	}

	list, total := dal.Alarm.SearchByName(userId, "", 1, 20)
	if total == 0 || len(list) == 0 {
		t.Fatalf("expected stored alarms, got total=%d len=%d", total, len(list))
	}
}
