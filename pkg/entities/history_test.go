package entities

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gythialy/magnet/pkg/utils"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) (*gorm.DB, *HistoryDao, func()) {
	f := "./history.db"
	db, err := gorm.Open(sqlite.Open(f), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = db.AutoMigrate(&History{})
	db.Debug()
	dao := NewHistoryDao(db)

	cleanup := func() {
		_ = os.Remove(f)
	}

	return db, dao, cleanup
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
//	data1 := dao.List(userId)
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
//	data2 := dao.List(userId)
//	t.Log(utils.ToString(data2))
//
//	if err := dao.Clean(); err != nil {
//		t.Fatal(err)
//	}
//
//	data3 := dao.List(userId)
//	t.Log(utils.ToString(data3))
//}

func TestHistoryDao_SearchByTitle(t *testing.T) {
	_, dao, cleanup := setupTestDB(t)
	defer cleanup()

	userId := int64(1)
	now := time.Now()

	// Insert test data
	testData := []*History{
		{UserId: userId, Url: "https://test.com/1", Title: "Test Title 1", UpdatedAt: now},
		{UserId: userId, Url: "https://test.com/2", Title: "Another Test", UpdatedAt: now},
		{UserId: userId, Url: "https://test.com/3", Title: "Something Else", UpdatedAt: now},
		{UserId: userId, Url: "https://test.com/4", Title: "Final Test Title", UpdatedAt: now},
		{UserId: userId, Url: "https://test.com/5", Title: "中文测试标题", UpdatedAt: now},
		{UserId: userId, Url: "https://test.com/6", Title: "Another 中文 Test", UpdatedAt: now},
	}

	if err, count := dao.Insert(testData); err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	} else {
		t.Logf("Inserted %d rows", count)
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
			results, total := dao.SearchByTitle(userId, tc.searchTitle, 1, 10) // page 1, 10 items per page
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
		results1, total := dao.SearchByTitle(userId, "Test", 1, pageSize)
		if int(total) != 4 {
			t.Errorf("Expected 4 total results, got %d", total)
		}
		if len(results1) != pageSize {
			t.Errorf("Expected %d results on first page, got %d", pageSize, len(results1))
		}

		results2, _ := dao.SearchByTitle(userId, "Test", 2, pageSize)
		if len(results2) != pageSize {
			t.Errorf("Expected %d results on second page, got %d", pageSize, len(results2))
		}

		// Check that results are different on each page
		if results1[0].Url == results2[0].Url || results1[1].Url == results2[1].Url {
			t.Errorf("Expected different results on different pages")
		}
	})
}

func TestHistoryDao_IsUrlExist(t *testing.T) {
	_, dao, cleanup := setupTestDB(t)
	defer cleanup()

	// Test data
	userId := int64(1)
	existingUrl := "https://example.com"
	nonExistingUrl := "https://nonexistent.com"

	// Insert a test record
	testHistory := &History{
		UserId:    userId,
		Url:       existingUrl,
		Title:     "Example Website",
		UpdatedAt: time.Now(),
	}
	err, _ := dao.Insert([]*History{testHistory})
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
			result := dao.IsUrlExist(tt.userId, tt.url)
			if result != tt.expected {
				t.Errorf("IsUrlExist(%d, %s) = %v, want %v", tt.userId, tt.url, result, tt.expected)
			}
		})
	}
}

func containsInsensitive(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
