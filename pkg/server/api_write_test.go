package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
)

// writeTestOtherUserID is a second account used by the isolation tests. It only needs to
// differ from testUserID; Telegram never sees these ids.
const writeTestOtherUserID = int64(20002)

// sendWrite performs one JSON write request with optional initData.
func writeAPIRequest(t *testing.T, handler http.Handler, method, target, rawInitData, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if rawInitData != "" {
		req.Header.Set("X-Telegram-Init-Data", rawInitData)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// writeTestJSON encodes a request payload with the production struct, so the
// tests and the handler cannot disagree about a field name unnoticed.
func writeJSONBody(t *testing.T, payload any) string {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode request body: %v", err)
	}
	return string(encoded)
}

// assertWriteError asserts both the status and the exact error envelope.
func assertWriteEnvelope(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()

	want := `{"error":{"code":"` + code + `"}}`
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, status, rec.Body.String())
	}
	if got := rec.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// newWriteOnlyHandler builds just the write surface with an injected limiter,
// which is how the rate-limit tests avoid spending 30 real requests.
func newWriteOnlyHandler(limiter *Limiter) http.Handler {
	mux := http.NewServeMux()
	newWriteAPIWithLimiter(limiter).register(mux)
	return NewInitDataMiddleware(testBotToken, time.Hour)(mux)
}

// seedOneKeyword stores one keyword through the DAL and returns its id.
func seedOneKeyword(t *testing.T, userID int64, kind model.KeywordType, keyword string) int32 {
	t.Helper()

	added, _, err := dal.Keyword.InsertMany(userID, kind, []string{keyword})
	if err != nil || len(added) != 1 {
		t.Fatalf("seed keyword %q: added=%v err=%v", keyword, added, err)
	}
	for _, row := range dal.Keyword.GetByUserIdAndType(userID, kind) {
		if row.Keyword == keyword && row.ID != nil {
			return *row.ID
		}
	}
	t.Fatalf("seeded keyword %q not found", keyword)
	return 0
}

// keywordList returns the stored keyword strings of one list, in id order.
func storedKeywords(t *testing.T, userID int64, kind model.KeywordType) []string {
	t.Helper()

	rows := dal.Keyword.GetByUserIdAndType(userID, kind)
	keywords := make([]string, 0, len(rows))
	for _, row := range rows {
		keywords = append(keywords, row.Keyword)
	}
	return keywords
}

// TestAPIWriteRequiresInitData proves the six write routes sit behind the same
// initData middleware as the reads.
func TestAPIWriteRequiresInitData(t *testing.T) {
	setupAPITestDB(t)

	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, time.Now())

	for _, tc := range []struct {
		method string
		target string
		body   string
	}{
		{http.MethodPost, apiKeywordsPath, `{"keywords":"a"}`},
		{http.MethodPatch, apiKeywordsPath + "/1", `{"keyword":"a"}`},
		{http.MethodDelete, apiKeywordsPath + "/1", ""},
		{http.MethodPost, apiAlarmsPath, `{"keywords":"a"}`},
		{http.MethodPatch, apiAlarmsPath + "/1", `{"keyword":"a"}`},
		{http.MethodDelete, apiAlarmsPath + "/1", ""},
	} {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			assertWriteEnvelope(t, writeAPIRequest(t, handler, tc.method, tc.target, "", tc.body),
				http.StatusUnauthorized, "unauthorized")
		})
	}
}

// TestAPIWriteCreateKeywords covers the happy path of the batch add, including
// trimming, dropped blanks, batch de-duplication and the added/skipped split
// across two calls.
func TestAPIWriteCreateKeywords(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := apiTestInitData(t, now, testUserID)

	first := decodeAPI[keywordBatchResponse](t, writeAPIRequest(t, handler, http.MethodPost, apiKeywordsPath, raw,
		writeJSONBody(t, keywordBatchRequest{Keywords: " alpha , beta ,, alpha "})))

	if len(first.Added) != 2 || first.Added[0] != "alpha" || first.Added[1] != "beta" {
		t.Fatalf("added = %v, want [alpha beta]", first.Added)
	}
	// An empty array, not null: the client iterates both fields unconditionally.
	if first.Skipped == nil || len(first.Skipped) != 0 {
		t.Fatalf("skipped = %v, want an empty array", first.Skipped)
	}

	second := decodeAPI[keywordBatchResponse](t, writeAPIRequest(t, handler, http.MethodPost, apiKeywordsPath, raw,
		writeJSONBody(t, keywordBatchRequest{Keywords: "alpha, gamma"})))
	if len(second.Added) != 1 || second.Added[0] != "gamma" {
		t.Fatalf("added = %v, want [gamma]", second.Added)
	}
	if len(second.Skipped) != 1 || second.Skipped[0] != "alpha" {
		t.Fatalf("skipped = %v, want [alpha]", second.Skipped)
	}

	// Batch-internal duplicates are collapsed before anything reaches the DAL:
	// "skipped" means "already stored", never "repeated in this request".
	dupes := decodeAPI[keywordBatchResponse](t, writeAPIRequest(t, handler, http.MethodPost, apiKeywordsPath, raw,
		writeJSONBody(t, keywordBatchRequest{Keywords: "dup,dup"})))
	if len(dupes.Added) != 1 || dupes.Added[0] != "dup" {
		t.Fatalf("added = %v, want [dup]", dupes.Added)
	}
	if dupes.Skipped == nil || len(dupes.Skipped) != 0 {
		t.Fatalf("skipped = %v, want an empty array for a collapsed duplicate", dupes.Skipped)
	}

	// Repeating the same batch afterwards does report the stored keyword.
	repeat := decodeAPI[keywordBatchResponse](t, writeAPIRequest(t, handler, http.MethodPost, apiKeywordsPath, raw,
		writeJSONBody(t, keywordBatchRequest{Keywords: "dup,dup"})))
	if len(repeat.Added) != 0 || len(repeat.Skipped) != 1 || repeat.Skipped[0] != "dup" {
		t.Fatalf("second call = (%v, %v), want added empty and skipped [dup]", repeat.Added, repeat.Skipped)
	}

	if got := storedKeywords(t, testUserID, model.PROJECT); len(got) != 4 {
		t.Fatalf("stored keywords = %v, want 4", got)
	}

	// The alarm list is a separate namespace: the same word can be added there.
	alarm := decodeAPI[keywordBatchResponse](t, writeAPIRequest(t, handler, http.MethodPost, apiAlarmsPath, raw,
		writeJSONBody(t, keywordBatchRequest{Keywords: "alpha"})))
	if len(alarm.Added) != 1 || alarm.Added[0] != "alpha" {
		t.Fatalf("alarm added = %v, want [alpha] (separate from the project list)", alarm.Added)
	}
	if got := storedKeywords(t, testUserID, model.PROJECT); len(got) != 4 {
		t.Fatalf("project keywords changed by an alarm add: %v", got)
	}
}

// TestAPIWriteCreateValidation covers every rejected batch and the two boundary
// values the contract names (50 entries, 128 runes).
func TestAPIWriteCreateValidation(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := apiTestInitData(t, now, testUserID)

	keywords := func(count int) string {
		items := make([]string, 0, count)
		for i := range count {
			items = append(items, "kw"+strconv.Itoa(i))
		}
		return strings.Join(items, ",")
	}

	for _, tc := range []struct {
		name string
		body string
	}{
		{"empty value", `{"keywords":""}`},
		{"only blanks and separators", `{"keywords":" , ,, "}`},
		{"missing field", `{}`},
		{"malformed json", `{`},
		{"wrong field type", `{"keywords":1}`},
		{"51 keywords", writeJSONBody(t, keywordBatchRequest{Keywords: keywords(51)})},
		{"keyword of 129 runes", writeJSONBody(t, keywordBatchRequest{Keywords: strings.Repeat("字", 129)})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodPost, apiKeywordsPath, raw, tc.body),
				http.StatusBadRequest, "bad_request")
		})
	}

	t.Run("50 keywords are accepted", func(t *testing.T) {
		payload := decodeAPI[keywordBatchResponse](t, writeAPIRequest(t, handler, http.MethodPost, apiKeywordsPath, raw,
			writeJSONBody(t, keywordBatchRequest{Keywords: keywords(50)})))
		if len(payload.Added) != 50 {
			t.Fatalf("added %d keywords, want 50", len(payload.Added))
		}
	})

	t.Run("128 runes are accepted", func(t *testing.T) {
		keyword := strings.Repeat("字", 128)
		payload := decodeAPI[keywordBatchResponse](t, writeAPIRequest(t, handler, http.MethodPost, apiKeywordsPath, raw,
			writeJSONBody(t, keywordBatchRequest{Keywords: keyword})))
		if len(payload.Added) != 1 || payload.Added[0] != keyword {
			t.Fatalf("added = %v, want the 128-rune keyword", payload.Added)
		}
	})

	t.Run("unknown fields are ignored", func(t *testing.T) {
		rec := writeAPIRequest(t, handler, http.MethodPost, apiKeywordsPath, raw, `{"keywords":"forward-compatible","future":true}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
	})

	t.Run("alarms share the same validation", func(t *testing.T) {
		assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodPost, apiAlarmsPath, raw,
			writeJSONBody(t, keywordBatchRequest{Keywords: keywords(51)})),
			http.StatusBadRequest, "bad_request")
	})
}

// TestAPIWriteUpdateKeyword covers rename success, the trimmed echo, empty
// input, length limit, malformed ids and the 404 that hides the difference
// between a foreign and a missing id.
func TestAPIWriteUpdateKeyword(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	rawOwner := apiTestInitData(t, now, testUserID)
	rawOther := apiTestInitData(t, now, writeTestOtherUserID)

	ownerID := seedOneKeyword(t, testUserID, model.PROJECT, "original")
	foreignID := seedOneKeyword(t, writeTestOtherUserID, model.PROJECT, "foreign")

	t.Run("renames the caller's keyword", func(t *testing.T) {
		payload := decodeAPI[keywordRenameResponse](t, writeAPIRequest(t, handler, http.MethodPatch,
			apiKeywordsPath+"/"+strconv.Itoa(int(ownerID)), rawOwner,
			writeJSONBody(t, keywordRenameRequest{Keyword: "  renamed  "})))

		if payload.ID != ownerID {
			t.Errorf("id = %d, want %d", payload.ID, ownerID)
		}
		if payload.Keyword != "renamed" {
			t.Errorf("keyword = %q, want the trimmed value", payload.Keyword)
		}
		if got := storedKeywords(t, testUserID, model.PROJECT); len(got) != 1 || got[0] != "renamed" {
			t.Fatalf("stored keywords = %v, want [renamed]", got)
		}
	})

	t.Run("empty and blank names are rejected", func(t *testing.T) {
		for _, value := range []string{"", "   ", "\t\n"} {
			assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodPatch,
				apiKeywordsPath+"/"+strconv.Itoa(int(ownerID)), rawOwner,
				writeJSONBody(t, keywordRenameRequest{Keyword: value})),
				http.StatusBadRequest, "bad_request")
		}
	})

	t.Run("a name past the limit is rejected", func(t *testing.T) {
		assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodPatch,
			apiKeywordsPath+"/"+strconv.Itoa(int(ownerID)), rawOwner,
			writeJSONBody(t, keywordRenameRequest{Keyword: strings.Repeat("字", 129)})),
			http.StatusBadRequest, "bad_request")
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodPatch, apiKeywordsPath+"/999999", rawOwner,
			writeJSONBody(t, keywordRenameRequest{Keyword: "x"})),
			http.StatusNotFound, "not_found")
	})

	t.Run("another user's id is not found and unchanged", func(t *testing.T) {
		assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodPatch,
			apiKeywordsPath+"/"+strconv.Itoa(int(foreignID)), rawOwner,
			writeJSONBody(t, keywordRenameRequest{Keyword: "stolen"})),
			http.StatusNotFound, "not_found")

		if got := storedKeywords(t, writeTestOtherUserID, model.PROJECT); len(got) != 1 || got[0] != "foreign" {
			t.Fatalf("foreign keyword = %v, want [foreign]", got)
		}
	})

	t.Run("the owner's id is not reachable by another account", func(t *testing.T) {
		assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodPatch,
			apiKeywordsPath+"/"+strconv.Itoa(int(ownerID)), rawOther,
			writeJSONBody(t, keywordRenameRequest{Keyword: "stolen"})),
			http.StatusNotFound, "not_found")

		if got := storedKeywords(t, testUserID, model.PROJECT); len(got) != 1 || got[0] != "renamed" {
			t.Fatalf("owner keyword = %v, want the earlier rename to survive", got)
		}
	})

	t.Run("a non-numeric id is a bad request", func(t *testing.T) {
		for _, id := range []string{"abc", "0", "-1", "1.5"} {
			assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodPatch, apiKeywordsPath+"/"+id, rawOwner,
				writeJSONBody(t, keywordRenameRequest{Keyword: "x"})),
				http.StatusBadRequest, "bad_request")
		}
	})

	t.Run("alarm keywords rename through their own path", func(t *testing.T) {
		alarmID := seedOneKeyword(t, testUserID, model.ALARM, "penalty")
		payload := decodeAPI[keywordRenameResponse](t, writeAPIRequest(t, handler, http.MethodPatch,
			apiAlarmsPath+"/"+strconv.Itoa(int(alarmID)), rawOwner,
			writeJSONBody(t, keywordRenameRequest{Keyword: "lawsuit"})))
		if payload.Keyword != "lawsuit" {
			t.Fatalf("keyword = %q, want lawsuit", payload.Keyword)
		}
		if got := storedKeywords(t, testUserID, model.ALARM); len(got) != 1 || got[0] != "lawsuit" {
			t.Fatalf("alarm keywords = %v, want [lawsuit]", got)
		}
	})
}

// TestAPIWriteDeleteKeyword covers 204, the empty body, the second delete and
// the 404 for another account's id.
func TestAPIWriteDeleteKeyword(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	rawOwner := apiTestInitData(t, now, testUserID)

	ownerID := seedOneKeyword(t, testUserID, model.PROJECT, "remove-me")
	foreignID := seedOneKeyword(t, writeTestOtherUserID, model.PROJECT, "keep-me")

	t.Run("deletes the caller's keyword with 204", func(t *testing.T) {
		rec := writeAPIRequest(t, handler, http.MethodDelete,
			apiKeywordsPath+"/"+strconv.Itoa(int(ownerID)), rawOwner, "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusNoContent, rec.Body.String())
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("body = %q, want empty", rec.Body.String())
		}
		if got := storedKeywords(t, testUserID, model.PROJECT); len(got) != 0 {
			t.Fatalf("keywords after delete = %v, want none", got)
		}
	})

	t.Run("deleting again is not found", func(t *testing.T) {
		assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodDelete,
			apiKeywordsPath+"/"+strconv.Itoa(int(ownerID)), rawOwner, ""),
			http.StatusNotFound, "not_found")
	})

	t.Run("another user's id is not found and unchanged", func(t *testing.T) {
		assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodDelete,
			apiKeywordsPath+"/"+strconv.Itoa(int(foreignID)), rawOwner, ""),
			http.StatusNotFound, "not_found")

		if got := storedKeywords(t, writeTestOtherUserID, model.PROJECT); len(got) != 1 || got[0] != "keep-me" {
			t.Fatalf("foreign keyword = %v, want [keep-me]", got)
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodDelete, apiKeywordsPath+"/999999", rawOwner, ""),
			http.StatusNotFound, "not_found")
	})

	t.Run("alarm keywords delete through their own path", func(t *testing.T) {
		alarmID := seedOneKeyword(t, testUserID, model.ALARM, "penalty")
		rec := writeAPIRequest(t, handler, http.MethodDelete,
			apiAlarmsPath+"/"+strconv.Itoa(int(alarmID)), rawOwner, "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
		}
		if got := storedKeywords(t, testUserID, model.ALARM); len(got) != 0 {
			t.Fatalf("alarm keywords after delete = %v, want none", got)
		}
	})
}

// TestAPIWriteRateLimit spends the documented 30 attempts and expects the 31st
// to be refused, with a second account still allowed: the limiter is per user.
func TestAPIWriteRateLimit(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	rawOwner := apiTestInitData(t, now, testUserID)
	rawOther := apiTestInitData(t, now, writeTestOtherUserID)

	// A well-formed write that reaches the DAL and misses; using 404 responses
	// proves every attempt is counted, not just successful ones.
	target := apiKeywordsPath + "/999999"
	body := writeJSONBody(t, keywordRenameRequest{Keyword: "x"})

	for attempt := 1; attempt <= writeRateLimit; attempt++ {
		rec := writeAPIRequest(t, handler, http.MethodPatch, target, rawOwner, body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("attempt %d: status = %d, want %d (body %q)",
				attempt, rec.Code, http.StatusNotFound, rec.Body.String())
		}
	}

	assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodPatch, target, rawOwner, body),
		http.StatusTooManyRequests, "too_many_requests")

	// The other account has its own budget.
	if rec := writeAPIRequest(t, handler, http.MethodPatch, target, rawOther, body); rec.Code != http.StatusNotFound {
		t.Fatalf("second user status = %d, want %d (throttled by the first user)", rec.Code, http.StatusNotFound)
	}

	// A read is not part of the write budget.
	if rec := sendAPI(t, handler, apiKeywordsPath, rawOwner); rec.Code != http.StatusOK {
		t.Fatalf("read after throttling: status = %d, want 200", rec.Code)
	}
}

// TestAPIWriteSmallWindowLimiter drives the injected limiter: two writes fit,
// the third is refused, and a window worth of time restores the budget.
func TestAPIWriteSmallWindowLimiter(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	clock := newLimiterTestClock(now)
	handler := newWriteOnlyHandler(newLimiterWithClock(2, time.Minute, clock.Now))
	raw := apiTestInitData(t, now, testUserID)

	target := apiKeywordsPath + "/999999"
	body := writeJSONBody(t, keywordRenameRequest{Keyword: "x"})

	for attempt := 1; attempt <= 2; attempt++ {
		if rec := writeAPIRequest(t, handler, http.MethodPatch, target, raw, body); rec.Code != http.StatusNotFound {
			t.Fatalf("attempt %d: status = %d, want %d", attempt, rec.Code, http.StatusNotFound)
		}
	}
	assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodPatch, target, raw, body),
		http.StatusTooManyRequests, "too_many_requests")

	clock.Advance(writeRateWindow + time.Second)
	if rec := writeAPIRequest(t, handler, http.MethodPatch, target, raw, body); rec.Code != http.StatusNotFound {
		t.Fatalf("status after the window = %d, want the request to be processed again", rec.Code)
	}
}

// TestAPIWriteIsImmediatelyVisible proves there is no cache between the write
// endpoints and the read endpoints: each mutation is visible to the next read.
func TestAPIWriteIsImmediatelyVisible(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := apiTestInitData(t, now, testUserID)

	created := decodeAPI[keywordBatchResponse](t, writeAPIRequest(t, handler, http.MethodPost, apiKeywordsPath, raw,
		writeJSONBody(t, keywordBatchRequest{Keywords: "visible"})))
	if len(created.Added) != 1 {
		t.Fatalf("added = %v, want [visible]", created.Added)
	}

	afterCreate := decodeAPI[keywordsResponse](t, sendAPI(t, handler, apiKeywordsPath, raw))
	if len(afterCreate.Items) != 1 || afterCreate.Items[0].Keyword != "visible" {
		t.Fatalf("keywords after create = %+v, want [visible]", afterCreate.Items)
	}

	id := afterCreate.Items[0].ID
	decodeAPI[keywordRenameResponse](t, writeAPIRequest(t, handler, http.MethodPatch,
		apiKeywordsPath+"/"+strconv.Itoa(int(id)), raw,
		writeJSONBody(t, keywordRenameRequest{Keyword: "renamed"})))

	afterRename := decodeAPI[keywordsResponse](t, sendAPI(t, handler, apiKeywordsPath, raw))
	if len(afterRename.Items) != 1 || afterRename.Items[0].Keyword != "renamed" {
		t.Fatalf("keywords after rename = %+v, want [renamed]", afterRename.Items)
	}

	if rec := writeAPIRequest(t, handler, http.MethodDelete, apiKeywordsPath+"/"+strconv.Itoa(int(id)), raw, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", rec.Code)
	}
	afterDelete := decodeAPI[keywordsResponse](t, sendAPI(t, handler, apiKeywordsPath, raw))
	if len(afterDelete.Items) != 0 {
		t.Fatalf("keywords after delete = %+v, want none", afterDelete.Items)
	}
}

// TestAPIWriteCrossUserIsolation proves neither a read nor a guessed id lets one
// account see or change another account's keywords.
func TestAPIWriteCrossUserIsolation(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	rawOwner := apiTestInitData(t, now, testUserID)
	rawOther := apiTestInitData(t, now, writeTestOtherUserID)

	ownerID := seedOneKeyword(t, testUserID, model.PROJECT, "private")

	otherKeywords := decodeAPI[keywordsResponse](t, sendAPI(t, handler, apiKeywordsPath, rawOther))
	if len(otherKeywords.Items) != 0 {
		t.Fatalf("second user sees %+v, want none", otherKeywords.Items)
	}

	assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodPatch,
		apiKeywordsPath+"/"+strconv.Itoa(int(ownerID)), rawOther,
		writeJSONBody(t, keywordRenameRequest{Keyword: "stolen"})),
		http.StatusNotFound, "not_found")
	assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodDelete,
		apiKeywordsPath+"/"+strconv.Itoa(int(ownerID)), rawOther, ""),
		http.StatusNotFound, "not_found")

	if got := storedKeywords(t, testUserID, model.PROJECT); len(got) != 1 || got[0] != "private" {
		t.Fatalf("owner keywords = %v, want [private]", got)
	}

	// The owner still reads their own row after both failed attempts.
	ownerKeywords := decodeAPI[keywordsResponse](t, sendAPI(t, handler, apiKeywordsPath, rawOwner))
	if len(ownerKeywords.Items) != 1 || ownerKeywords.Items[0].Keyword != "private" {
		t.Fatalf("owner reads %+v, want [private]", ownerKeywords.Items)
	}

	// The same word is not "already there" for the second account.
	created := decodeAPI[keywordBatchResponse](t, writeAPIRequest(t, handler, http.MethodPost, apiKeywordsPath, rawOther,
		writeJSONBody(t, keywordBatchRequest{Keywords: "private"})))
	if len(created.Added) != 1 || created.Added[0] != "private" {
		t.Fatalf("second user added = %v, want [private]", created.Added)
	}
}
