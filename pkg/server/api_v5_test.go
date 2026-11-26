package server

import (
	"net/url"
	"testing"
	"time"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
)

// The watchlist shows when a word last matched. The dedupe log only spans its
// retention window, so the API must report absence for a word that has not
// matched inside it instead of implying "never".
func TestAPIKeywordsExposeLastHit(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := apiTestInitData(t, now, testUserID)

	seedKeywords(t,
		&model.Keyword{
			ID: new(int32(1)), UserID: testUserID, Type: int32(model.PROJECT),
			Keyword: "matched", Counter: 2, CreatedAt: now.AddDate(0, 0, -40),
		},
		&model.Keyword{
			ID: new(int32(2)), UserID: testUserID, Type: int32(model.PROJECT),
			Keyword: "quiet", Counter: 1, CreatedAt: now.AddDate(0, 0, -40),
		},
	)
	if _, err := dal.MarkKeywordMatch(1, "https://notice.example/a"); err != nil {
		t.Fatalf("record match: %v", err)
	}

	payload := decodeAPI[keywordsResponse](t, sendAPI(t, handler, apiKeywordsPath, raw))

	byID := make(map[int32]keywordItem, len(payload.Items))
	for _, item := range payload.Items {
		byID[item.ID] = item
	}

	matched, ok := byID[1]
	if !ok {
		t.Fatalf("keyword 1 missing from the payload")
	}
	if matched.LastHitAt == nil {
		t.Fatalf("keyword 1 lastHitAt = nil, want the match just recorded")
	}
	at, err := time.Parse(time.RFC3339, *matched.LastHitAt)
	if err != nil {
		t.Fatalf("lastHitAt = %q, want RFC3339: %v", *matched.LastHitAt, err)
	}
	if delta := now.Sub(at); delta < -time.Minute || delta > time.Minute {
		t.Fatalf("lastHitAt = %s, want about now (%s)", at, now)
	}

	quiet, ok := byID[2]
	if !ok {
		t.Fatalf("keyword 2 missing from the payload")
	}
	if quiet.LastHitAt != nil {
		t.Fatalf("keyword 2 lastHitAt = %q, want absent", *quiet.LastHitAt)
	}
}

// The tab badge asks how many punishment records start after the client's
// watermark. Records stored with different UTC offsets must still answer by
// instant, which is why the count never compares the stored text.
func TestAPINewRecordsSince(t *testing.T) {
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := apiTestInitData(t, now, testUserID)

	cst := time.FixedZone("CST", 8*60*60)
	seedAlarms(t,
		&model.Alarm{
			UserID: testUserID, BusinessID: "b1", CreditName: "Alpha", CreditCode: "c1",
			StartDate: now.Add(-2 * time.Hour).In(cst),
			PageUrl1:  "https://notice.example/1", NoticeID: "n1",
		},
		&model.Alarm{
			UserID: testUserID, BusinessID: "b2", CreditName: "Beta", CreditCode: "c2",
			StartDate: now.Add(-30 * time.Hour),
			PageUrl1:  "https://notice.example/2", NoticeID: "n2",
		},
	)

	since := now.Add(-24 * time.Hour)
	query := apiStatisticsPath + "?since=" + url.QueryEscape(since.Format(time.RFC3339))
	payload := decodeAPI[statisticsResponse](t, sendAPI(t, handler, query, raw))
	if payload.NewRecords != 1 {
		t.Fatalf("newRecords = %d, want 1", payload.NewRecords)
	}

	// Without a watermark the field stays zero: the server never guesses what
	// the client has already read.
	plain := decodeAPI[statisticsResponse](t, sendAPI(t, handler, apiStatisticsPath, raw))
	if plain.NewRecords != 0 {
		t.Fatalf("newRecords without since = %d, want 0", plain.NewRecords)
	}
}
