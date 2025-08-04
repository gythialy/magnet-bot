package dal

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) func() {
	f := "./history.db"
	db, err := gorm.Open(sqlite.Open(f), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = db.AutoMigrate(&model.History{})
	db.Debug()

	SetDefault(db)

	cleanup := func() {
		_ = os.Remove(f)
	}

	return cleanup
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
	cleanup := setupTestDB(t)
	defer cleanup()

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
	cleanup := setupTestDB(t)
	defer cleanup()

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
	cleanup := setupTestDB(t)
	defer cleanup()

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
	cleanup := setupTestDB(t)
	defer cleanup()

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
