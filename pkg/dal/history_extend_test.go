package dal

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDB opens a throwaway database for the test and points the global DAO
// at it. t.TempDir is removed automatically, so there is nothing to clean up.
func setupTestDB(t *testing.T) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/history.db"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := db.AutoMigrate(&model.History{}); err != nil {
		t.Fatal(err)
	}
	SetDefault(db)
}

//func TestHistoryDao_Cache(t *testing.T) {
//	_, dao, cleanup := setupTestDB(t)
//	defer cleanup()
//
//	var histories []*History
//	userId := int64(0)
//	now := time.Now()
//	for i := userId; i < 10; i++ {
//		histories = append(histories, &History{
//			UserId:    userId,
//			Url:       fmt.Sprintf("https://test.com/content%d", i),
//			UpdatedAt: now,
//		})
//	}
//
//	if err, i := dao.Insert(histories); err != nil {
//		t.Fatal(err)
//	} else {
//		t.Logf("insert %d rows", i)
//	}
//
//	data1 := dao.SearchByName(userId)
//	t.Log(utils.ToString(data1))
//
//	date1 := now.AddDate(0, 0, -7)
//	if err, i := dao.Insert([]*History{{
//		UserId:    userId,
//		Url:       fmt.Sprintf("https://test.com/content%d", 2),
//		UpdatedAt: date1,
//	}, {
//		UserId:    userId,
//		Url:       fmt.Sprintf("https://test.com/content%d", 4),
//		UpdatedAt: date1,
//	}}); err != nil {
//		t.Fatal(err)
//	} else {
//		t.Logf("insert %d rows", i)
//	}
//	data2 := dao.SearchByName(userId)
//	t.Log(utils.ToString(data2))
//
//	if err := dao.Clean(); err != nil {
//		t.Fatal(err)
//	}
//
//	data3 := dao.SearchByName(userId)
//	t.Log(utils.ToString(data3))
//}

func TestHistoryDao_SearchByTitle(t *testing.T) {
	setupTestDB(t)

	userId := int64(1)
	now := time.Now()

	// Insert test data
	testData := []*model.History{
		{UserID: userId, URL: "https://test.com/1", Title: "Test Title 1", UpdatedAt: now},
		{UserID: userId, URL: "https://test.com/2", Title: "Another Test", UpdatedAt: now},
		{UserID: userId, URL: "https://test.com/3", Title: "Something Else", UpdatedAt: now},
		{UserID: userId, URL: "https://test.com/4", Title: "Final Test Title", UpdatedAt: now},
		{UserID: userId, URL: "https://test.com/5", Title: "中文测试标题", UpdatedAt: now},
		{UserID: userId, URL: "https://test.com/6", Title: "Another 中文 Test", UpdatedAt: now},
	}

	if err := History.Insert(testData); err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Test cases
	testCases := []struct {
		searchTitle string
		expectedLen int
	}{
		{"Test", 4},
		{"Title", 2},
		{"Something", 1},
		{"Nonexistent", 0},
		{"中文", 2},
		{"测试", 1},
		{"标题", 1},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Search for '%s'", tc.searchTitle), func(t *testing.T) {
			results, total := History.SearchByTitle(userId, tc.searchTitle, 1, 10) // page 1, 10 items per page
			if int(total) != tc.expectedLen {
				t.Errorf("Expected %d total results, got %d for search title '%s'", tc.expectedLen, total, tc.searchTitle)
			}

			if len(results) != tc.expectedLen {
				t.Errorf("Expected %d results, got %d for search title '%s'", tc.expectedLen, len(results), tc.searchTitle)
			}

			for _, result := range results {
				if !containsInsensitive(result.Title, tc.searchTitle) {
					t.Errorf("Result title '%s' does not contain search term '%s'", result.Title, tc.searchTitle)
				}
			}

			t.Logf("Search results for '%s': %s", tc.searchTitle, utils.ToString(results))
		})
	}

	// Test pagination
	t.Run("Test pagination", func(t *testing.T) {
		pageSize := 2
		results1, total := History.SearchByTitle(userId, "Test", 1, pageSize)
		if int(total) != 4 {
			t.Errorf("Expected 4 total results, got %d", total)
		}
		if len(results1) != pageSize {
			t.Errorf("Expected %d results on first page, got %d", pageSize, len(results1))
		}

		results2, _ := History.SearchByTitle(userId, "Test", 2, pageSize)
		if len(results2) != pageSize {
			t.Errorf("Expected %d results on second page, got %d", pageSize, len(results2))
		}

		// Check that results are different on each page
		if results1[0].URL == results2[0].URL || results1[1].URL == results2[1].URL {
			t.Errorf("Expected different results on different pages")
		}
	})
}

func TestHistoryDao_IsUrlExist(t *testing.T) {
	setupTestDB(t)

	// Test data
	userId := int64(1)
	existingUrl := "https://example.com"
	nonExistingUrl := "https://nonexistent.com"

	// Insert a test record
	now := time.Now()
	testHistory := &model.History{
		UserID:    userId,
		URL:       existingUrl,
		Title:     "Example Website",
		UpdatedAt: now,
	}
	err := History.Insert([]*model.History{testHistory})
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Test cases
	tests := []struct {
		name     string
		userId   int64
		url      string
		expected bool
	}{
		{
			name:     "Existing URL",
			userId:   userId,
			url:      existingUrl,
			expected: true,
		},
		{
			name:     "Non-existing URL",
			userId:   userId,
			url:      nonExistingUrl,
			expected: false,
		},
		{
			name:     "Existing URL but different user",
			userId:   userId + 1,
			url:      existingUrl,
			expected: false,
		},
	}

	// Run test cases
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := History.IsUrlExist(tt.userId, tt.url)
			if err != nil {
				t.Fatalf("IsUrlExist failed: %v", err)
			}
			if result != tt.expected {
				t.Errorf("IsUrlExist(%d, %s) = %v, want %v", tt.userId, tt.url, result, tt.expected)
			}
		})
	}
}

func TestHistoryDao_CountHistory(t *testing.T) {
	setupTestDB(t)

	userId := int64(1)
	now := time.Now()

	// Insert test data
	testData := []*model.History{
		{UserID: userId, URL: "https://test.com/1", Title: "Test Title 1", UpdatedAt: now},
		{UserID: userId, URL: "https://test.com/2", Title: "Another Test", UpdatedAt: now},
		{UserID: userId, URL: "https://test.com/3", Title: "Something Else", UpdatedAt: now},
	}

	if err := History.Insert(testData); err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	expectedCount := int64(3)
	actualCount := History.CountByUserId(userId)
	if actualCount != expectedCount {
		t.Errorf("Expected %d history records, got %d", expectedCount, actualCount)
	}
}

func containsInsensitive(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func TestHistoryDao_GetTodayByUserId(t *testing.T) {
	setupTestDB(t)

	userId := int64(1)
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	twoDaysAgo := now.AddDate(0, 0, -2)

	// Insert test data with different dates
	testData := []*model.History{
		{UserID: userId, URL: "https://test.com/today1", Title: "Today Record 1", UpdatedAt: now},
		{UserID: userId, URL: "https://test.com/today2", Title: "Today Record 2", UpdatedAt: now},
		{UserID: userId, URL: "https://test.com/today3", Title: "Today Record 3", UpdatedAt: now},
		{UserID: userId, URL: "https://test.com/yesterday1", Title: "Yesterday Record 1", UpdatedAt: yesterday},
		{UserID: userId, URL: "https://test.com/yesterday2", Title: "Yesterday Record 2", UpdatedAt: yesterday},
		{UserID: userId, URL: "https://test.com/twodaysago", Title: "Two Days Ago Record", UpdatedAt: twoDaysAgo},
		{UserID: userId + 1, URL: "https://test.com/otheruser", Title: "Other User Today", UpdatedAt: now},
	}

	if err := History.Insert(testData); err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Test cases
	t.Run("Get today's records for user", func(t *testing.T) {
		results, total := History.GetTodayByUserId(userId, 1, 10)
		expectedTotal := int64(3)

		if total != expectedTotal {
			t.Errorf("Expected %d total results, got %d", expectedTotal, total)
		}

		if len(results) != int(expectedTotal) {
			t.Errorf("Expected %d results, got %d", expectedTotal, len(results))
		}

		// Verify all results are from today
		startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		for _, result := range results {
			if result.UpdatedAt.Before(startOfDay) {
				t.Errorf("Result with URL %s is not from today (updated at: %v)", result.URL, result.UpdatedAt)
			}
			if result.UserID != userId {
				t.Errorf("Result with URL %s belongs to wrong user (userID: %d, expected: %d)", result.URL, result.UserID, userId)
			}
		}
	})

	t.Run("Test pagination", func(t *testing.T) {
		pageSize := 2

		// First page
		results1, total := History.GetTodayByUserId(userId, 1, pageSize)
		if total != 3 {
			t.Errorf("Expected 3 total results, got %d", total)
		}
		if len(results1) != pageSize {
			t.Errorf("Expected %d results on first page, got %d", pageSize, len(results1))
		}

		// Second page
		results2, _ := History.GetTodayByUserId(userId, 2, pageSize)
		if len(results2) != 1 {
			t.Errorf("Expected 1 result on second page, got %d", len(results2))
		}

		// Third page should be empty
		results3, _ := History.GetTodayByUserId(userId, 3, pageSize)
		if len(results3) != 0 {
			t.Errorf("Expected 0 results on third page, got %d", len(results3))
		}
	})

	t.Run("Get today's records for non-existent user", func(t *testing.T) {
		results, total := History.GetTodayByUserId(userId+100, 1, 10)
		if total != 0 {
			t.Errorf("Expected 0 total results for non-existent user, got %d", total)
		}
		if len(results) != 0 {
			t.Errorf("Expected 0 results for non-existent user, got %d", len(results))
		}
	})

	t.Run("Verify no yesterday records returned", func(t *testing.T) {
		results, total := History.GetTodayByUserId(userId, 1, 10)

		for _, result := range results {
			if strings.Contains(result.Title, "Yesterday") || strings.Contains(result.Title, "Two Days Ago") {
				t.Errorf("Found non-today record: %s", result.Title)
			}
		}

		t.Logf("Today's records for user %d: %s, total: %d", userId, utils.ToString(results), total)
	})
}

// TestHistoryNormalizeURLs guards the one-off backfill: rows written before
// links were absolute must gain a scheme so they keep matching the claim key,
// and the backfill must be idempotent.
func TestHistoryNormalizeURLs(t *testing.T) {
	setupTestDB(t)

	now := time.Now()
	legacy := []*model.History{
		{UserID: 1, URL: "www.example.com/a.html", Title: "legacy a", UpdatedAt: now},
		{UserID: 2, URL: "www.example.com/b.html", Title: "legacy b", UpdatedAt: now},
	}
	if err := History.Insert(legacy); err != nil {
		t.Fatal(err)
	}
	if err := History.Insert([]*model.History{
		{UserID: 1, URL: "https://www.example.com/c.html", Title: "absolute c", UpdatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := History.NormalizeURLs()
	if err != nil {
		t.Fatalf("NormalizeURLs() failed: %v", err)
	}
	if rows != 2 {
		t.Fatalf("NormalizeURLs() touched %d rows, want 2", rows)
	}

	for _, tc := range []struct {
		userID int64
		url    string
	}{
		{1, "https://www.example.com/a.html"},
		{2, "https://www.example.com/b.html"},
		{1, "https://www.example.com/c.html"},
	} {
		exist, err := History.IsUrlExist(tc.userID, tc.url)
		if err != nil {
			t.Fatal(err)
		}
		if !exist {
			t.Errorf("expected %q to exist after the backfill", tc.url)
		}
	}
	if exist, _ := History.IsUrlExist(1, "www.example.com/a.html"); exist {
		t.Error("scheme-less URL survived the backfill")
	}

	// A second run must be a no-op.
	if rows, err := History.NormalizeURLs(); err != nil || rows != 0 {
		t.Fatalf("second NormalizeURLs() = (%d, %v), want (0, nil)", rows, err)
	}
}

// setupAlarmTestDB opens a throwaway database that holds only the alarms table.
// setupTestDB migrates histories only, and each test rebinds the global DAO to
// its own file, so the two helpers cannot see each other's rows.
func setupAlarmTestDB(t *testing.T) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/alarm.db"), &gorm.Config{
		// These tests assert on rows, not SQL; the echo would only bury a
		// failure.
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Alarm{}); err != nil {
		t.Fatal(err)
	}
	SetDefault(db)
}

// TestHistoryDao_SearchPaged covers the offset-based query the Mini App API
// uses: optional time bound, optional title filter, newest first, and a total
// that counts the whole filtered set rather than the returned page.
func TestHistoryDao_SearchPaged(t *testing.T) {
	setupTestDB(t)

	const (
		userID      = int64(1)
		otherUserID = int64(2)
	)
	now := time.Now().Truncate(time.Second)

	if err := History.Insert([]*model.History{
		{UserID: userID, URL: "https://example.com/a", Title: "Tender A", UpdatedAt: now},
		{UserID: userID, URL: "https://example.com/b", Title: "Tender B", UpdatedAt: now.Add(-time.Hour)},
		{UserID: userID, URL: "https://example.com/c", Title: "Other C", UpdatedAt: now.Add(-2 * time.Hour)},
		{UserID: userID, URL: "https://example.com/old", Title: "tender old", UpdatedAt: now.AddDate(0, 0, -10)},
		{UserID: otherUserID, URL: "https://example.com/z", Title: "Tender other", UpdatedAt: now},
	}); err != nil {
		t.Fatalf("insert history: %v", err)
	}

	t.Run("no filters returns every row of the user, newest first", func(t *testing.T) {
		result, total, err := History.SearchPaged(userID, nil, "", 0, 10)
		if err != nil {
			t.Fatalf("SearchPaged() error: %v", err)
		}
		if total != 4 {
			t.Fatalf("total = %d, want 4", total)
		}
		want := []string{
			"https://example.com/a",
			"https://example.com/b",
			"https://example.com/c",
			"https://example.com/old",
		}
		if len(result) != len(want) {
			t.Fatalf("rows = %d, want %d", len(result), len(want))
		}
		for i, url := range want {
			if result[i].URL != url {
				t.Errorf("row[%d] = %q, want %q", i, result[i].URL, url)
			}
		}
	})

	t.Run("since excludes older rows", func(t *testing.T) {
		since := now.AddDate(0, 0, -7)

		result, total, err := History.SearchPaged(userID, &since, "", 0, 10)
		if err != nil {
			t.Fatalf("SearchPaged() error: %v", err)
		}
		if total != 3 || len(result) != 3 {
			t.Fatalf("total = %d rows = %d, want 3 and 3", total, len(result))
		}
		for _, row := range result {
			if row.UpdatedAt.Before(since) {
				t.Errorf("row %q is older than the lower bound", row.URL)
			}
		}
	})

	t.Run("term filters the title", func(t *testing.T) {
		// LIKE is case-insensitive for ASCII in SQLite, so this also covers the
		// mixed-case "tender old" row.
		result, total, err := History.SearchPaged(userID, nil, "Tender", 0, 10)
		if err != nil {
			t.Fatalf("SearchPaged() error: %v", err)
		}
		if total != 3 || len(result) != 3 {
			t.Fatalf("total = %d rows = %d, want 3 and 3", total, len(result))
		}
		for _, row := range result {
			if !strings.Contains(strings.ToLower(row.Title), "tender") {
				t.Errorf("row %q does not match the term", row.Title)
			}
		}
	})

	t.Run("term and since combine", func(t *testing.T) {
		since := now.AddDate(0, 0, -7)

		_, total, err := History.SearchPaged(userID, &since, "Tender", 0, 10)
		if err != nil {
			t.Fatalf("SearchPaged() error: %v", err)
		}
		if total != 2 {
			t.Fatalf("total = %d, want 2", total)
		}
	})

	t.Run("offset and limit page the filtered set", func(t *testing.T) {
		result, total, err := History.SearchPaged(userID, nil, "", 1, 2)
		if err != nil {
			t.Fatalf("SearchPaged() error: %v", err)
		}
		if total != 4 {
			t.Fatalf("total = %d, want 4 (the whole filtered set)", total)
		}
		if len(result) != 2 {
			t.Fatalf("rows = %d, want 2", len(result))
		}
		if result[0].URL != "https://example.com/b" || result[1].URL != "https://example.com/c" {
			t.Fatalf("page = [%q %q], want b and c", result[0].URL, result[1].URL)
		}
	})

	t.Run("offset past the end returns no rows but the real total", func(t *testing.T) {
		result, total, err := History.SearchPaged(userID, nil, "", 10, 5)
		if err != nil {
			t.Fatalf("SearchPaged() error: %v", err)
		}
		if len(result) != 0 {
			t.Fatalf("rows = %d, want 0", len(result))
		}
		if total != 4 {
			t.Fatalf("total = %d, want 4", total)
		}
	})

	t.Run("another user sees only their rows", func(t *testing.T) {
		result, total, err := History.SearchPaged(otherUserID, nil, "", 0, 10)
		if err != nil {
			t.Fatalf("SearchPaged() error: %v", err)
		}
		if total != 1 || len(result) != 1 {
			t.Fatalf("total = %d rows = %d, want 1 and 1", total, len(result))
		}
		if result[0].URL != "https://example.com/z" {
			t.Fatalf("row = %q, want the other user's row", result[0].URL)
		}
	})
}

// TestAlarmDao_SearchPaged covers the offset-based alarm query: optional
// credit-name filter, newest start date first, real total, and user scoping.
func TestAlarmDao_SearchPaged(t *testing.T) {
	setupAlarmTestDB(t)

	const (
		userID      = int64(1)
		otherUserID = int64(2)
	)
	now := time.Now().Truncate(time.Second)

	if err := Alarm.Insert([]*model.Alarm{
		{UserID: userID, BusinessID: "BIZ-1", CreditName: "Alpha Corp", CreditCode: "CODE-1", StartDate: now, PageUrl1: "https://example.com/1", NoticeID: "N-1"},
		{UserID: userID, BusinessID: "BIZ-2", CreditName: "Beta Ltd", CreditCode: "CODE-2", StartDate: now.AddDate(0, 0, -1), PageUrl1: "https://example.com/2", NoticeID: "N-2"},
		{UserID: userID, BusinessID: "BIZ-3", CreditName: "Alpha Holdings", CreditCode: "CODE-3", StartDate: now.AddDate(0, 0, -2), PageUrl1: "https://example.com/3", NoticeID: "N-3"},
		{UserID: otherUserID, BusinessID: "BIZ-Z", CreditName: "Alpha Other", CreditCode: "CODE-Z", StartDate: now, PageUrl1: "https://example.com/z", NoticeID: "N-Z"},
	}); err != nil {
		t.Fatalf("insert alarms: %v", err)
	}

	t.Run("newest start date first", func(t *testing.T) {
		result, total, err := Alarm.SearchPaged(userID, "", 0, 10)
		if err != nil {
			t.Fatalf("SearchPaged() error: %v", err)
		}
		if total != 3 {
			t.Fatalf("total = %d, want 3", total)
		}
		want := []string{"BIZ-1", "BIZ-2", "BIZ-3"}
		if len(result) != len(want) {
			t.Fatalf("rows = %d, want %d", len(result), len(want))
		}
		for i, businessID := range want {
			if result[i].BusinessID != businessID {
				t.Errorf("row[%d] = %q, want %q", i, result[i].BusinessID, businessID)
			}
		}
	})

	t.Run("term filters the credit name", func(t *testing.T) {
		result, total, err := Alarm.SearchPaged(userID, "Alpha", 0, 10)
		if err != nil {
			t.Fatalf("SearchPaged() error: %v", err)
		}
		if total != 2 || len(result) != 2 {
			t.Fatalf("total = %d rows = %d, want 2 and 2", total, len(result))
		}
		for _, row := range result {
			if !strings.Contains(strings.ToLower(row.CreditName), "alpha") {
				t.Errorf("row %q does not match the term", row.CreditName)
			}
		}
	})

	t.Run("offset and limit page the filtered set", func(t *testing.T) {
		result, total, err := Alarm.SearchPaged(userID, "", 1, 1)
		if err != nil {
			t.Fatalf("SearchPaged() error: %v", err)
		}
		if total != 3 {
			t.Fatalf("total = %d, want 3", total)
		}
		if len(result) != 1 || result[0].BusinessID != "BIZ-2" {
			t.Fatalf("page = %v, want BIZ-2", result)
		}
	})

	t.Run("another user sees only their rows", func(t *testing.T) {
		result, total, err := Alarm.SearchPaged(otherUserID, "", 0, 10)
		if err != nil {
			t.Fatalf("SearchPaged() error: %v", err)
		}
		if total != 1 || len(result) != 1 {
			t.Fatalf("total = %d rows = %d, want 1 and 1", total, len(result))
		}
		if result[0].BusinessID != "BIZ-Z" {
			t.Fatalf("row = %q, want the other user's row", result[0].BusinessID)
		}
	})
}
