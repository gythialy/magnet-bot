package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
)

// apiTestBotUsername is used by the deep-link tests. It is synthetic and has
// the shape Telegram requires (letters, digits and one underscore).
const apiTestBotUsername = "magnet_test_bot"

// setupAPITestDB opens a private SQLite database for one test, migrates the
// three tables the read API touches and points the package-level DAO at it.
//
// dal.SetDefault mutates global handles, so the previous binding is restored on
// cleanup: tests in this package run in one binary, and a leaked binding would
// make a later test read another test's rows.
func setupAPITestDB(t *testing.T) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "api.db")), &gorm.Config{
		// The endpoints are asserted on their payloads; SQL echo would only
		// bury a failure under generator-like output.
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&model.History{}, &model.Keyword{}, &model.Alarm{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	previous := *dal.Q
	dal.SetDefault(db)
	t.Cleanup(func() {
		// *Query is a plain struct of DAO handles, so a value copy is enough to
		// undo SetDefault without keeping any database connection open.
		*dal.Q = previous
		dal.Alarm = &dal.Q.Alarm
		dal.History = &dal.Q.History
		dal.Keyword = &dal.Q.Keyword
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

// apiTestHandler builds a handler whose clock is pinned to now, so "today" and
// "7d" boundaries do not move while the test runs.
func apiTestHandler(cfg APIConfig, now time.Time) http.Handler {
	return newAPI(cfg, func() time.Time { return now })
}

// apiTestInitData signs initData for userID at authenticatedAt with the
// independent test signer from auth_test.go. Production signing code is
// deliberately not involved: a bug shared by signer and verifier would
// otherwise be invisible here.
func apiTestInitData(t *testing.T, authenticatedAt time.Time, userID int64) string {
	t.Helper()

	fields := baseFields(authenticatedAt.Unix())
	if userID != testUserID {
		fields = withField(fields, "user", testUserJSON(userID, "other"))
	}
	raw, _ := signInitData(testBotToken, fields)
	return raw
}

// sendAPI performs one GET with optional initData and returns the recorder.
func sendAPI(t *testing.T, handler http.Handler, target, rawInitData string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	if rawInitData != "" {
		req.Header.Set("X-Telegram-Init-Data", rawInitData)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// decodeAPI asserts a 200 and decodes the JSON payload into T.
func decodeAPI[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload T
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return payload
}

// assertBadRequest asserts the 400 bad_request envelope.
func assertBadRequest(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	const want = `{"error":{"code":"bad_request"}}`
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := rec.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// parseAPITime decodes an RFC3339 field and fails the test on any other format.
func parseAPITime(t *testing.T, raw string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("time %q is not RFC3339: %v", raw, err)
	}
	return parsed
}

// seedHistories stores notice history rows through the production DAL, so the
// endpoint is exercised against the same insert path the bot uses.
func seedHistories(t *testing.T, rows ...*model.History) {
	t.Helper()

	if err := dal.History.Insert(rows); err != nil {
		t.Fatalf("seed history: %v", err)
	}
}

// seedAlarms stores alarm records through the production DAL.
func seedAlarms(t *testing.T, rows ...*model.Alarm) {
	t.Helper()

	if err := dal.Alarm.Insert(rows); err != nil {
		t.Fatalf("seed alarms: %v", err)
	}
}

// seedKeywords stores keywords with explicit ids so ordering is assertable.
func seedKeywords(t *testing.T, rows ...*model.Keyword) {
	t.Helper()

	if err := dal.Keyword.Create(rows...); err != nil {
		t.Fatalf("seed keywords: %v", err)
	}
}

// TestAPIRejectsUnauthenticatedRequests proves every endpoint — including the
// catch-all under /api/ — is behind the initData middleware: a request without
// the header must not reveal data or even which routes exist.
func TestAPIRejectsUnauthenticatedRequests(t *testing.T) {
	setupAPITestDB(t)

	handler := NewAPI(APIConfig{BotToken: testBotToken})

	for _, target := range []string{
		apiStatisticsPath,
		apiNoticesPath,
		apiAlarmRecordsPath,
		apiKeywordsPath,
		apiAlarmsPath,
		"/api/no-such-endpoint",
	} {
		t.Run(target, func(t *testing.T) {
			rec := sendAPI(t, handler, target, "")

			const want = `{"error":{"code":"unauthorized"}}`
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
			if got := rec.Body.String(); got != want {
				t.Fatalf("body = %q, want %q", got, want)
			}
		})
	}
}

// TestAPIStatisticsMatchesBotReport pins the counters to the same definitions
// the /statistics command uses: history rows for the user, number of project
// keywords, the sum of their counters, and the number of alarm keywords.
func TestAPIStatisticsMatchesBotReport(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{
		BotToken: testBotToken,
		// Configured with "@" on purpose: the payload and the deep links must
		// carry the bare username.
		BotUsername: "@" + apiTestBotUsername,
	}, now)
	raw := apiTestInitData(t, now, testUserID)

	seedHistories(t,
		&model.History{UserID: testUserID, URL: "https://example.com/a", Title: "A", UpdatedAt: now},
		&model.History{UserID: testUserID, URL: "https://example.com/b", Title: "B", UpdatedAt: now},
		&model.History{UserID: testUserID, URL: "https://example.com/c", Title: "C", UpdatedAt: now},
		// Another account's rows must never count.
		&model.History{UserID: 20002, URL: "https://example.com/z", Title: "Z", UpdatedAt: now},
	)
	seedKeywords(t,
		&model.Keyword{ID: new(int32(1)), UserID: testUserID, Type: int32(model.PROJECT), Keyword: "tender", Counter: 4, CreatedAt: now},
		&model.Keyword{ID: new(int32(2)), UserID: testUserID, Type: int32(model.PROJECT), Keyword: "bridge", Counter: 9, CreatedAt: now},
		&model.Keyword{ID: new(int32(3)), UserID: testUserID, Type: int32(model.ALARM), Keyword: "penalty", CreatedAt: now},
		&model.Keyword{ID: new(int32(4)), UserID: 20002, Type: int32(model.PROJECT), Keyword: "other", Counter: 100, CreatedAt: now},
	)

	payload := decodeAPI[statisticsResponse](t, sendAPI(t, handler, apiStatisticsPath, raw))

	if payload.History != 3 {
		t.Errorf("history = %d, want 3", payload.History)
	}
	if payload.Keywords != 2 {
		t.Errorf("keywords = %d, want 2", payload.Keywords)
	}
	// statisticsOverview sums keyword.Counter over the project keywords.
	if payload.RelatedNotices != 13 {
		t.Errorf("relatedNotices = %d, want 13", payload.RelatedNotices)
	}
	if payload.AlarmKeywords != 1 {
		t.Errorf("alarmKeywords = %d, want 1", payload.AlarmKeywords)
	}
	if payload.BotUsername != apiTestBotUsername {
		t.Errorf("botUsername = %q, want %q", payload.BotUsername, apiTestBotUsername)
	}

	// Guard the agreement directly: if the bot report ever changes how it
	// counts, the app must change with it rather than drift silently.
	if want := dal.History.CountByUserId(testUserID); payload.History != want {
		t.Errorf("history = %d, /statistics would show %d", payload.History, want)
	}
	var related int64
	for _, keyword := range dal.Keyword.GetByUserIdAndType(testUserID, model.PROJECT) {
		related += int64(keyword.Counter)
	}
	if payload.RelatedNotices != related {
		t.Errorf("relatedNotices = %d, /statistics would show %d", payload.RelatedNotices, related)
	}
}

// TestAPINoticesRanges covers the three date windows: default range is today,
// "7d" is a calendar week back and "all" has no bound.
func TestAPINoticesRanges(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := apiTestInitData(t, now, testUserID)

	// Times are anchored to local midnight rather than "now minus a few
	// minutes", so a suite that starts just after 00:00 cannot push a row into
	// the previous day.
	today1 := startOfDay.Add(time.Minute)
	today2 := startOfDay.Add(2 * time.Minute)
	today3 := startOfDay.Add(3 * time.Minute)
	threeDaysAgo := now.AddDate(0, 0, -3)
	tenDaysAgo := now.AddDate(0, 0, -10)

	seedHistories(t,
		&model.History{UserID: testUserID, URL: "https://example.com/today1", Title: "today one", UpdatedAt: today1},
		&model.History{UserID: testUserID, URL: "https://example.com/today2", Title: "today two", UpdatedAt: today2},
		&model.History{UserID: testUserID, URL: "https://example.com/today3", Title: "today three", UpdatedAt: today3},
		&model.History{UserID: testUserID, URL: "https://example.com/3d", Title: "three days", UpdatedAt: threeDaysAgo},
		&model.History{UserID: testUserID, URL: "https://example.com/10d", Title: "ten days", UpdatedAt: tenDaysAgo},
		&model.History{UserID: 20002, URL: "https://example.com/other", Title: "other user today", UpdatedAt: today3},
	)

	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"default is today", "", 3},
		{"explicit today", "?range=today", 3},
		{"seven days", "?range=7d", 4},
		{"all", "?range=all", 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := decodeAPI[noticesResponse](t, sendAPI(t, handler, apiNoticesPath+tc.query, raw))

			if len(payload.Items) != tc.want {
				t.Fatalf("items = %d, want %d", len(payload.Items), tc.want)
			}
			if payload.Total != int64(tc.want) {
				t.Fatalf("total = %d, want %d", payload.Total, tc.want)
			}
			if payload.NextOffset != nil {
				t.Fatalf("nextOffset = %d, want null", *payload.NextOffset)
			}
			// Newest first: the whole window is asserted, not just its size.
			if payload.Items[0].URL != "https://example.com/today3" {
				t.Fatalf("first url = %q, want the newest row", payload.Items[0].URL)
			}
		})
	}

	t.Run("timestamps are RFC3339 instants", func(t *testing.T) {
		payload := decodeAPI[noticesResponse](t, sendAPI(t, handler, apiNoticesPath+"?range=all", raw))

		want := map[string]time.Time{
			"https://example.com/today1": today1,
			"https://example.com/today2": today2,
			"https://example.com/today3": today3,
			"https://example.com/3d":     threeDaysAgo,
			"https://example.com/10d":    tenDaysAgo,
		}
		for _, item := range payload.Items {
			expected, ok := want[item.URL]
			if !ok {
				t.Fatalf("unexpected url %q", item.URL)
			}
			if got := parseAPITime(t, item.UpdatedAt); !got.Equal(expected) {
				t.Errorf("%s updatedAt = %v, want %v", item.URL, got, expected)
			}
		}
	})
}

// TestAPINoticesSearchAndPaging covers the title filter, the offset/limit page
// and the null nextOffset on the last page.
func TestAPINoticesSearchAndPaging(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := apiTestInitData(t, now, testUserID)

	seedHistories(t,
		&model.History{UserID: testUserID, URL: "https://example.com/t1", Title: "Tender notice one", UpdatedAt: startOfDay.Add(time.Minute)},
		&model.History{UserID: testUserID, URL: "https://example.com/t2", Title: "Second tender notice", UpdatedAt: startOfDay.Add(2 * time.Minute)},
		&model.History{UserID: testUserID, URL: "https://example.com/t3", Title: "Tender notice three", UpdatedAt: startOfDay.Add(3 * time.Minute)},
		&model.History{UserID: testUserID, URL: "https://example.com/x", Title: "Unrelated announcement", UpdatedAt: startOfDay.Add(4 * time.Minute)},
	)

	t.Run("first page", func(t *testing.T) {
		payload := decodeAPI[noticesResponse](t, sendAPI(t, handler, apiNoticesPath+"?q=tender&limit=2", raw))

		if payload.Total != 3 {
			t.Fatalf("total = %d, want 3", payload.Total)
		}
		if len(payload.Items) != 2 {
			t.Fatalf("items = %d, want 2", len(payload.Items))
		}
		if payload.NextOffset == nil || *payload.NextOffset != 2 {
			t.Fatalf("nextOffset = %v, want 2", payload.NextOffset)
		}
		if payload.Items[0].URL != "https://example.com/t3" || payload.Items[1].URL != "https://example.com/t2" {
			t.Fatalf("page order = [%q %q], want newest first", payload.Items[0].URL, payload.Items[1].URL)
		}
	})

	t.Run("last page has no next offset", func(t *testing.T) {
		payload := decodeAPI[noticesResponse](t, sendAPI(t, handler, apiNoticesPath+"?q=tender&limit=2&offset=2", raw))

		if payload.Total != 3 {
			t.Fatalf("total = %d, want 3", payload.Total)
		}
		if len(payload.Items) != 1 {
			t.Fatalf("items = %d, want 1", len(payload.Items))
		}
		if payload.NextOffset != nil {
			t.Fatalf("nextOffset = %d, want null", *payload.NextOffset)
		}
		if payload.Items[0].URL != "https://example.com/t1" {
			t.Fatalf("last page url = %q, want t1", payload.Items[0].URL)
		}
	})

	t.Run("blank search term is not a filter", func(t *testing.T) {
		payload := decodeAPI[noticesResponse](t, sendAPI(t, handler, apiNoticesPath+"?q=+++&range=today", raw))

		if payload.Total != 4 {
			t.Fatalf("total = %d, want 4 (the whole day)", payload.Total)
		}
	})

	t.Run("tender flag is a bool", func(t *testing.T) {
		seedHistories(t, &model.History{
			UserID:        testUserID,
			URL:           "https://example.com/flagged",
			Title:         "Tender flagged",
			UpdatedAt:     startOfDay.Add(5 * time.Minute),
			HasTenderCode: 1,
		})

		payload := decodeAPI[noticesResponse](t, sendAPI(t, handler, apiNoticesPath+"?q=flagged", raw))
		if len(payload.Items) != 1 {
			t.Fatalf("items = %d, want 1", len(payload.Items))
		}
		if !payload.Items[0].HasTenderCode {
			t.Fatal("hasTenderCode = false, want true for has_tender_code=1")
		}
	})
}

// TestAPINoticesLimitAndValidation covers the limit cap and every rejected
// parameter: a bad range, a limit below one, a negative offset and any
// non-numeric value must be a client error, never a silent default.
func TestAPINoticesLimitAndValidation(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := apiTestInitData(t, now, testUserID)

	rows := make([]*model.History, 0, 55)
	for i := range 55 {
		rows = append(rows, &model.History{
			UserID:    testUserID,
			URL:       "https://example.com/bulk/" + strconv.Itoa(i),
			Title:     "bulk " + strconv.Itoa(i),
			UpdatedAt: startOfDay.Add(time.Duration(i) * time.Second),
		})
	}
	seedHistories(t, rows...)

	t.Run("limit is capped at 50", func(t *testing.T) {
		payload := decodeAPI[noticesResponse](t, sendAPI(t, handler, apiNoticesPath+"?limit=100", raw))

		if len(payload.Items) != 50 {
			t.Fatalf("items = %d, want 50", len(payload.Items))
		}
		if payload.Total != 55 {
			t.Fatalf("total = %d, want 55", payload.Total)
		}
		if payload.NextOffset == nil || *payload.NextOffset != 50 {
			t.Fatalf("nextOffset = %v, want 50", payload.NextOffset)
		}
	})

	t.Run("default limit is 20", func(t *testing.T) {
		payload := decodeAPI[noticesResponse](t, sendAPI(t, handler, apiNoticesPath, raw))

		if len(payload.Items) != 20 {
			t.Fatalf("items = %d, want 20", len(payload.Items))
		}
		if payload.NextOffset == nil || *payload.NextOffset != 20 {
			t.Fatalf("nextOffset = %v, want 20", payload.NextOffset)
		}
	})

	t.Run("empty range value means today", func(t *testing.T) {
		payload := decodeAPI[noticesResponse](t, sendAPI(t, handler, apiNoticesPath+"?range=", raw))

		if payload.Total != 55 {
			t.Fatalf("total = %d, want 55 for the default range", payload.Total)
		}
	})

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"unknown range", "?range=yesterday"},
		{"limit zero", "?limit=0"},
		{"negative limit", "?limit=-5"},
		{"non-numeric limit", "?limit=abc"},
		{"negative offset", "?offset=-1"},
		{"non-numeric offset", "?offset=abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertBadRequest(t, sendAPI(t, handler, apiNoticesPath+tc.query, raw))
		})
	}
}

// TestAPIKeywordsSortedByCounter proves the leaderboard ordering: most related
// notices first, id ascending as the tie-breaker, only project keywords.
func TestAPIKeywordsSortedByCounter(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := apiTestInitData(t, now, testUserID)
	created := now.Add(-24 * time.Hour)

	seedKeywords(t,
		&model.Keyword{ID: new(int32(5)), UserID: testUserID, Type: int32(model.PROJECT), Keyword: "low", Counter: 2, CreatedAt: created},
		&model.Keyword{ID: new(int32(9)), UserID: testUserID, Type: int32(model.PROJECT), Keyword: "high c", Counter: 7, CreatedAt: created},
		&model.Keyword{ID: new(int32(2)), UserID: testUserID, Type: int32(model.PROJECT), Keyword: "high a", Counter: 7, CreatedAt: created},
		&model.Keyword{ID: new(int32(4)), UserID: testUserID, Type: int32(model.PROJECT), Keyword: "high b", Counter: 7, CreatedAt: created},
		// Filtered out: wrong type for this endpoint.
		&model.Keyword{ID: new(int32(1)), UserID: testUserID, Type: int32(model.ALARM), Keyword: "alarm only", Counter: 99, CreatedAt: created},
		// Filtered out: another account.
		&model.Keyword{ID: new(int32(3)), UserID: 20002, Type: int32(model.PROJECT), Keyword: "other user", Counter: 50, CreatedAt: created},
	)

	payload := decodeAPI[keywordsResponse](t, sendAPI(t, handler, apiKeywordsPath, raw))

	wantIDs := []int32{2, 4, 9, 5}
	if len(payload.Items) != len(wantIDs) {
		t.Fatalf("items = %d, want %d", len(payload.Items), len(wantIDs))
	}
	for i, wantID := range wantIDs {
		if payload.Items[i].ID != wantID {
			t.Errorf("items[%d].id = %d, want %d", i, payload.Items[i].ID, wantID)
		}
		if got := parseAPITime(t, payload.Items[i].CreatedAt); !got.Equal(created) {
			t.Errorf("items[%d].createdAt = %v, want %v", i, got, created)
		}
	}
	if payload.Items[0].Keyword != "high a" || payload.Items[0].Counter != 7 {
		t.Errorf("first item = %+v, want the highest counted keyword", payload.Items[0])
	}
}

// TestAPIAlarmsSortedByID proves alarm keywords are listed in creation order
// and that project keywords are not mixed in.
func TestAPIAlarmsSortedByID(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := apiTestInitData(t, now, testUserID)

	seedKeywords(t,
		&model.Keyword{ID: new(int32(7)), UserID: testUserID, Type: int32(model.ALARM), Keyword: "penalty", CreatedAt: now},
		&model.Keyword{ID: new(int32(1)), UserID: testUserID, Type: int32(model.ALARM), Keyword: "blacklist", CreatedAt: now},
		&model.Keyword{ID: new(int32(4)), UserID: testUserID, Type: int32(model.ALARM), Keyword: "lawsuit", CreatedAt: now},
		&model.Keyword{ID: new(int32(2)), UserID: testUserID, Type: int32(model.PROJECT), Keyword: "tender", CreatedAt: now},
	)

	payload := decodeAPI[alarmsResponse](t, sendAPI(t, handler, apiAlarmsPath, raw))

	wantIDs := []int32{1, 4, 7}
	if len(payload.Items) != len(wantIDs) {
		t.Fatalf("items = %d, want %d", len(payload.Items), len(wantIDs))
	}
	for i, wantID := range wantIDs {
		if payload.Items[i].ID != wantID {
			t.Errorf("items[%d].id = %d, want %d", i, payload.Items[i].ID, wantID)
		}
	}
	if got := parseAPITime(t, payload.Items[0].CreatedAt); !got.Equal(now) {
		t.Errorf("createdAt = %v, want %v", got, now)
	}
}

// TestAPIAlarmRecordsPagingAndLink covers the alarm record page, the newest
// start date first, the credit-name search and the deep-link shape, including
// the deployment without a bot username.
func TestAPIAlarmRecordsPagingAndLink(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	cfg := APIConfig{BotToken: testBotToken, BotUsername: apiTestBotUsername}
	handler := apiTestHandler(cfg, now)
	raw := apiTestInitData(t, now, testUserID)

	newest := now.AddDate(0, 0, -1)
	middle := now.AddDate(0, 0, -2)
	oldest := now.AddDate(0, 0, -3)

	seedAlarms(t,
		&model.Alarm{UserID: testUserID, BusinessID: "BIZ-1", CreditName: "Alpha Corp", CreditCode: "CODE-1", StartDate: newest, PageUrl1: "https://example.com/1", NoticeID: "N-1"},
		&model.Alarm{UserID: testUserID, BusinessID: "BIZ-2", CreditName: "Beta Ltd", CreditCode: "CODE-2", StartDate: middle, PageUrl1: "https://example.com/2", NoticeID: "N-2"},
		&model.Alarm{UserID: testUserID, BusinessID: "BIZ-3", CreditName: "Gamma Inc", CreditCode: "CODE-3", StartDate: oldest, PageUrl1: "https://example.com/3", NoticeID: "N-3"},
		// Another account's record must never appear in this listing.
		&model.Alarm{UserID: 20002, BusinessID: "BIZ-X", CreditName: "Alpha Other", CreditCode: "CODE-X", StartDate: newest, PageUrl1: "https://example.com/x", NoticeID: "N-X"},
	)

	t.Run("first page", func(t *testing.T) {
		payload := decodeAPI[alarmRecordsResponse](t, sendAPI(t, handler, apiAlarmRecordsPath+"?limit=2", raw))

		if payload.Total != 3 {
			t.Fatalf("total = %d, want 3", payload.Total)
		}
		if len(payload.Items) != 2 {
			t.Fatalf("items = %d, want 2", len(payload.Items))
		}
		if payload.NextOffset == nil || *payload.NextOffset != 2 {
			t.Fatalf("nextOffset = %v, want 2", payload.NextOffset)
		}
		if payload.Items[0].BusinessID != "BIZ-1" || payload.Items[1].BusinessID != "BIZ-2" {
			t.Fatalf("order = [%q %q], want newest start date first", payload.Items[0].BusinessID, payload.Items[1].BusinessID)
		}
		if got := parseAPITime(t, payload.Items[0].StartDate); !got.Equal(newest) {
			t.Errorf("startDate = %v, want %v", got, newest)
		}
		wantLink := "https://t.me/" + apiTestBotUsername + "?start=alarm_BIZ-1"
		if payload.Items[0].Link != wantLink {
			t.Errorf("link = %q, want %q", payload.Items[0].Link, wantLink)
		}
	})

	t.Run("last page", func(t *testing.T) {
		payload := decodeAPI[alarmRecordsResponse](t, sendAPI(t, handler, apiAlarmRecordsPath+"?limit=2&offset=2", raw))

		if len(payload.Items) != 1 {
			t.Fatalf("items = %d, want 1", len(payload.Items))
		}
		if payload.Items[0].BusinessID != "BIZ-3" {
			t.Fatalf("item = %q, want BIZ-3", payload.Items[0].BusinessID)
		}
		if payload.NextOffset != nil {
			t.Fatalf("nextOffset = %d, want null", *payload.NextOffset)
		}
	})

	t.Run("credit name search", func(t *testing.T) {
		payload := decodeAPI[alarmRecordsResponse](t, sendAPI(t, handler, apiAlarmRecordsPath+"?q=alpha", raw))

		if payload.Total != 1 || len(payload.Items) != 1 {
			t.Fatalf("total = %d items = %d, want 1 and 1", payload.Total, len(payload.Items))
		}
		if payload.Items[0].BusinessID != "BIZ-1" {
			t.Fatalf("item = %q, want BIZ-1", payload.Items[0].BusinessID)
		}
	})

	t.Run("no bot username means no link", func(t *testing.T) {
		// Same database, a deployment without a username: the link is empty and
		// statistics reports the empty value, so the client hides the action.
		anon := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
		payload := decodeAPI[alarmRecordsResponse](t, sendAPI(t, anon, apiAlarmRecordsPath, raw))

		if len(payload.Items) != 3 {
			t.Fatalf("items = %d, want 3", len(payload.Items))
		}
		for _, item := range payload.Items {
			if item.Link != "" {
				t.Errorf("link = %q, want empty without a bot username", item.Link)
			}
		}

		stats := decodeAPI[statisticsResponse](t, sendAPI(t, anon, apiStatisticsPath, raw))
		if stats.BotUsername != "" {
			t.Errorf("botUsername = %q, want empty", stats.BotUsername)
		}
	})
}

// TestAPIAlarmRecordsValidation reuses the shared paging parser, so the
// invalid offset/limit cases are asserted here too rather than only for
// notices.
func TestAPIAlarmRecordsValidation(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := apiTestInitData(t, now, testUserID)

	for _, query := range []string{"?limit=0", "?limit=x", "?offset=-2", "?offset=-"} {
		t.Run(query, func(t *testing.T) {
			assertBadRequest(t, sendAPI(t, handler, apiAlarmRecordsPath+query, raw))
		})
	}
}

// TestAPIUnknownEndpoint checks the JSON 404 under /api/ and that a write verb
// on a read endpoint is refused by the mux.
func TestAPIUnknownEndpoint(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := apiTestInitData(t, now, testUserID)

	rec := sendAPI(t, handler, "/api/no-such-endpoint", raw)
	const want = `{"error":{"code":"not_found"}}`
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := rec.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}

	post := httptest.NewRequest(http.MethodPost, apiStatisticsPath, nil)
	post.Header.Set("X-Telegram-Init-Data", raw)
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, post)
	if postRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want %d", postRec.Code, http.StatusMethodNotAllowed)
	}
}

// TestAPIEndpointsAreUserScoped seeds two accounts and reads every endpoint as
// the second one: no row of the first account may appear anywhere.
func TestAPIEndpointsAreUserScoped(t *testing.T) {
	setupAPITestDB(t)

	const otherUserID = int64(20002)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken, BotUsername: apiTestBotUsername}, now)
	otherRaw := apiTestInitData(t, now, otherUserID)

	seedHistories(t, &model.History{UserID: testUserID, URL: "https://example.com/private", Title: "private", UpdatedAt: now})
	seedKeywords(t, &model.Keyword{ID: new(int32(11)), UserID: testUserID, Type: int32(model.PROJECT), Keyword: "private", Counter: 3, CreatedAt: now})
	seedKeywords(t, &model.Keyword{ID: new(int32(12)), UserID: testUserID, Type: int32(model.ALARM), Keyword: "private alarm", CreatedAt: now})
	seedAlarms(t, &model.Alarm{UserID: testUserID, BusinessID: "BIZ-P", CreditName: "Private Corp", CreditCode: "CODE-P", StartDate: now, PageUrl1: "https://example.com/p", NoticeID: "N-P"})

	stats := decodeAPI[statisticsResponse](t, sendAPI(t, handler, apiStatisticsPath, otherRaw))
	if stats.History != 0 || stats.Keywords != 0 || stats.RelatedNotices != 0 || stats.AlarmKeywords != 0 {
		t.Errorf("statistics leaked the other account's counters: %+v", stats)
	}

	notices := decodeAPI[noticesResponse](t, sendAPI(t, handler, apiNoticesPath+"?range=all", otherRaw))
	if notices.Total != 0 || len(notices.Items) != 0 {
		t.Errorf("notices leaked %d rows", notices.Total)
	}

	keywords := decodeAPI[keywordsResponse](t, sendAPI(t, handler, apiKeywordsPath, otherRaw))
	if len(keywords.Items) != 0 {
		t.Errorf("keywords leaked %d rows", len(keywords.Items))
	}

	alarms := decodeAPI[alarmsResponse](t, sendAPI(t, handler, apiAlarmsPath, otherRaw))
	if len(alarms.Items) != 0 {
		t.Errorf("alarms leaked %d rows", len(alarms.Items))
	}

	records := decodeAPI[alarmRecordsResponse](t, sendAPI(t, handler, apiAlarmRecordsPath, otherRaw))
	if records.Total != 0 || len(records.Items) != 0 {
		t.Errorf("alarm records leaked %d rows", records.Total)
	}
}

// TestAPIUnderProductionRouter documents and guards the prefix contract: the
// unified router mounts the API with mux.Handle("/api/", cfg.API) and net/http
// does not strip the mount point, so the handler must register the /api/...
// patterns itself and receives the full path.
func TestAPIUnderProductionRouter(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	router := newRouter(Config{
		API: apiTestHandler(APIConfig{BotToken: testBotToken, BotUsername: apiTestBotUsername}, now),
	})
	raw := apiTestInitData(t, now, testUserID)

	seedHistories(t, &model.History{UserID: testUserID, URL: "https://example.com/a", Title: "A", UpdatedAt: now})

	payload := decodeAPI[statisticsResponse](t, sendAPI(t, router, apiStatisticsPath, raw))
	if payload.History != 1 {
		t.Fatalf("history = %d, want 1", payload.History)
	}

	// And the middleware still sits in front of the mounted subtree.
	unauthorized := sendAPI(t, router, apiStatisticsPath, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}
}
