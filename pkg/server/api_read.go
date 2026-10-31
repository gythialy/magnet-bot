package server

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
)

// Paging bounds. The default keeps a cold app load small; the cap protects the
// bot's SQLite database and the phone's memory from a hand-written
// "?limit=100000". A larger request is clamped (not rejected) so a caller that
// asks for everything still gets a valid page and simply pages through it.
const (
	defaultPageLimit = 20
	maxPageLimit     = 50
)

// Values of the notices "range" parameter.
const (
	noticesRangeToday = "today"
	noticesRangeWeek  = "7d"
	noticesRangeAll   = "all"
)

// alarmStartPrefix is the /start payload prefix the bot's alarm handler
// recognises. Client and server share the literal through the URL, so it lives
// here rather than being formatted twice.
const alarmStartPrefix = "alarm_"

// errInvalidPaging reports a malformed or out-of-range offset/limit. It is
// mapped to 400 by the endpoints; the sentinel keeps the parser free of HTTP
// concerns.
var errInvalidPaging = errors.New("invalid pagination parameter")

// statisticsResponse is the payload of GET /api/statistics. Field names are
// part of the contract with pkg/server/web/js (api.js and the Today view), so
// they are lowerCamelCase exactly as documented there.
type statisticsResponse struct {
	History        int64  `json:"history"`
	Keywords       int    `json:"keywords"`
	RelatedNotices int64  `json:"relatedNotices"`
	AlarmKeywords  int    `json:"alarmKeywords"`
	BotUsername    string `json:"botUsername"`
}

// noticesResponse is the payload of GET /api/notices.
type noticesResponse struct {
	Items      []noticeItem `json:"items"`
	Total      int64        `json:"total"`
	NextOffset *int         `json:"nextOffset"`
}

type noticeItem struct {
	URL           string `json:"url"`
	Title         string `json:"title"`
	UpdatedAt     string `json:"updatedAt"`
	HasTenderCode bool   `json:"hasTenderCode"`
}

// keywordsResponse is the payload of GET /api/keywords.
type keywordsResponse struct {
	Items []keywordItem `json:"items"`
}

type keywordItem struct {
	ID        int32  `json:"id"`
	Keyword   string `json:"keyword"`
	Counter   int32  `json:"counter"`
	CreatedAt string `json:"createdAt"`
}

// alarmsResponse is the payload of GET /api/alarms.
type alarmsResponse struct {
	Items []alarmKeywordItem `json:"items"`
}

type alarmKeywordItem struct {
	ID        int32  `json:"id"`
	Keyword   string `json:"keyword"`
	CreatedAt string `json:"createdAt"`
}

// alarmRecordsResponse is the payload of GET /api/alarm-records.
type alarmRecordsResponse struct {
	Items      []alarmRecordItem `json:"items"`
	Total      int64             `json:"total"`
	NextOffset *int              `json:"nextOffset"`
}

// alarmRecordItem carries the whole record, not just the row summary: the Mini
// App renders the detail sheet from the page it already loaded, so opening a
// record costs no extra request and never leaves the app.
type alarmRecordItem struct {
	BusinessID       string `json:"businessId"`
	CreditName       string `json:"creditName"`
	CreditCode       string `json:"creditCode,omitempty"`
	StartDate        string `json:"startDate"`
	EndDate          string `json:"endDate,omitempty"`
	Title            string `json:"title,omitempty"`
	DetailReason     string `json:"detailReason,omitempty"`
	HandleDepartment string `json:"handleDepartment,omitempty"`
	HandleUnit       string `json:"handleUnit,omitempty"`
	HandleResult     string `json:"handleResult,omitempty"`
	PageURL1         string `json:"pageUrl1,omitempty"`
	PageURL2         string `json:"pageUrl2,omitempty"`
	NoticeID         string `json:"noticeId,omitempty"`
	OriginNoticeID   string `json:"originNoticeId,omitempty"`
	Link             string `json:"link"`
}

// handleStatistics serves the four counters of the /statistics report plus the
// bot username the client needs for deep links.
//
// Every number is read through the same DAL calls the bot command uses, and
// relatedNotices is summed exactly like statisticsOverview: the two surfaces
// describe one account and must never disagree.
func (a *apiHandler) handleStatistics(w http.ResponseWriter, r *http.Request) {
	user, ok := apiUser(w, r)
	if !ok {
		return
	}

	project := dal.Keyword.GetByUserIdAndType(user.ID, model.PROJECT)
	alarms := dal.Keyword.GetByUserIdAndType(user.ID, model.ALARM)

	var related int64
	for _, keyword := range project {
		related += int64(keyword.Counter)
	}

	WriteJSON(w, http.StatusOK, statisticsResponse{
		History:        dal.History.CountByUserId(user.ID),
		Keywords:       len(project),
		RelatedNotices: related,
		AlarmKeywords:  len(alarms),
		BotUsername:    a.botUsername,
	})
}

// handleNotices serves one page of the user's notice history, newest first,
// optionally restricted to a date range and a title substring.
func (a *apiHandler) handleNotices(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	since, ok := a.noticesSince(query.Get("range"))
	if !ok {
		// An unknown range is a client bug; falling back to "today" would hide
		// it and silently change what the user sees.
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}
	offset, limit, err := parsePaging(query)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}

	user, ok := apiUser(w, r)
	if !ok {
		return
	}

	rows, total, err := dal.History.SearchPaged(user.ID, since, strings.TrimSpace(query.Get("q")), offset, limit)
	if err != nil {
		// Only the endpoint name is logged: a query string may carry the user's
		// search text, and titles are user data.
		slog.Error("mini app api query failed", "endpoint", "notices", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Initialised, not nil: the client iterates items unconditionally, and a
	// JSON null would need a second shape check.
	items := make([]noticeItem, 0, len(rows))
	for _, history := range rows {
		items = append(items, noticeItem{
			URL:           history.URL,
			Title:         history.Title,
			UpdatedAt:     formatAPITime(history.UpdatedAt),
			HasTenderCode: history.HasTenderCode != 0,
		})
	}

	WriteJSON(w, http.StatusOK, noticesResponse{
		Items:      items,
		Total:      total,
		NextOffset: nextOffset(offset, len(items), total),
	})
}

// handleKeywords serves the user's project keywords, the leaderboard order the
// /statistics report uses: most related notices first, id as tie-breaker.
func (a *apiHandler) handleKeywords(w http.ResponseWriter, r *http.Request) {
	user, ok := apiUser(w, r)
	if !ok {
		return
	}

	sorted := slices.Clone(dal.Keyword.GetByUserIdAndType(user.ID, model.PROJECT))
	// Clone before sorting: the DAL returns a fresh slice today, but the
	// endpoint must not depend on that staying true to keep its ordering local.
	slices.SortStableFunc(sorted, compareKeywordsByCounter)

	items := make([]keywordItem, 0, len(sorted))
	for _, keyword := range sorted {
		items = append(items, keywordItem{
			ID:        keywordID(keyword),
			Keyword:   keyword.Keyword,
			Counter:   keyword.Counter,
			CreatedAt: formatAPITime(keyword.CreatedAt),
		})
	}

	WriteJSON(w, http.StatusOK, keywordsResponse{Items: items})
}

// handleAlarms serves the user's alarm keywords in creation (id) order, which
// is how the /statistics alarm list renders them.
func (a *apiHandler) handleAlarms(w http.ResponseWriter, r *http.Request) {
	user, ok := apiUser(w, r)
	if !ok {
		return
	}

	sorted := slices.Clone(dal.Keyword.GetByUserIdAndType(user.ID, model.ALARM))
	slices.SortStableFunc(sorted, compareKeywordsByID)

	items := make([]alarmKeywordItem, 0, len(sorted))
	for _, keyword := range sorted {
		items = append(items, alarmKeywordItem{
			ID:        keywordID(keyword),
			Keyword:   keyword.Keyword,
			CreatedAt: formatAPITime(keyword.CreatedAt),
		})
	}

	WriteJSON(w, http.StatusOK, alarmsResponse{Items: items})
}

// handleAlarmRecords serves one page of the user's alarm records, most recent
// punishment first, each with the deep link that opens its detail in the bot.
func (a *apiHandler) handleAlarmRecords(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	offset, limit, err := parsePaging(query)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}

	user, ok := apiUser(w, r)
	if !ok {
		return
	}

	rows, total, err := dal.Alarm.SearchPaged(user.ID, strings.TrimSpace(query.Get("q")), offset, limit)
	if err != nil {
		slog.Error("mini app api query failed", "endpoint", "alarm-records", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	items := make([]alarmRecordItem, 0, len(rows))
	for _, alarm := range rows {
		item := alarmRecordItem{
			BusinessID:       alarm.BusinessID,
			CreditName:       alarm.CreditName,
			CreditCode:       alarm.CreditCode,
			StartDate:        formatAPITime(alarm.StartDate),
			Title:            apiString(alarm.Title),
			DetailReason:     apiString(alarm.DetailReason),
			HandleDepartment: apiString(alarm.HandleDepartment),
			HandleUnit:       apiString(alarm.HandleUnit),
			HandleResult:     apiString(alarm.HandleResult),
			PageURL1:         alarm.PageUrl1,
			PageURL2:         apiString(alarm.PageUrl2),
			NoticeID:         alarm.NoticeID,
			OriginNoticeID:   apiString(alarm.OriginNoticeID),
			Link:             a.alarmLink(alarm.BusinessID),
		}
		if alarm.EndDate != nil {
			item.EndDate = formatAPITime(*alarm.EndDate)
		}
		items = append(items, item)
	}

	WriteJSON(w, http.StatusOK, alarmRecordsResponse{
		Items:      items,
		Total:      total,
		NextOffset: nextOffset(offset, len(items), total),
	})
}

// apiUser returns the identity the middleware attached. The middleware always
// injects it, so a missing value means this handler was mounted without it;
// answering 401 is safer than operating on a zero user id, which would read
// another account's (empty) data.
func apiUser(w http.ResponseWriter, r *http.Request) (InitDataUser, bool) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return InitDataUser{}, false
	}
	return user, true
}

// noticesSince maps the range parameter to the lower bound on updated_at. The
// boolean is false for an unknown range so the caller can answer 400.
//
// "today" starts at local midnight, computed exactly like
// dal.GetTodayByUserId: the /today command and the app must select the same
// rows. "7d" is a calendar week back; "all" removes the bound entirely.
func (a *apiHandler) noticesSince(rawRange string) (*time.Time, bool) {
	now := a.now()
	switch rawRange {
	case "", noticesRangeToday:
		startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return &startOfDay, true
	case noticesRangeWeek:
		weekAgo := now.AddDate(0, 0, -7)
		return &weekAgo, true
	case noticesRangeAll:
		return nil, true
	default:
		return nil, false
	}
}

// parsePaging validates offset and limit. limit defaults to defaultPageLimit
// and is clamped to maxPageLimit; offset defaults to 0. A non-numeric or
// out-of-range value is a client bug and is reported, not guessed at.
func parsePaging(query url.Values) (offset, limit int, err error) {
	limit = defaultPageLimit
	if raw := query.Get("limit"); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed < 1 {
			return 0, 0, errInvalidPaging
		}
		limit = min(parsed, maxPageLimit)
	}

	if raw := query.Get("offset"); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed < 0 {
			return 0, 0, errInvalidPaging
		}
		offset = parsed
	}

	return offset, limit, nil
}

// nextOffset reports where the next page starts, or nil when this page reached
// the end. It is a pointer so the JSON has a real null: the client stops paging
// on null, while a bare 0 would make it request the first page again.
func nextOffset(offset, returned int, total int64) *int {
	// An empty page can only mean a concurrent delete moved the end; returning
	// a next offset equal to the current one would loop the client forever.
	if returned == 0 {
		return nil
	}

	next := offset + returned
	if int64(next) >= total {
		return nil
	}
	return &next
}

// formatAPITime renders one instant in the wire format the client parses. UTC
// is used so the same row produces the same string on a server whose local
// zone changes; the instant itself is preserved.
// apiString flattens the nullable columns of a record into the JSON shape the
// client renders: omitted when absent, never the string "null".
func apiString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func formatAPITime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// alarmLink builds the deep link that reopens the bot on one alarm record.
// A deployment without a bot username has no link, and an empty business id
// has nothing to point at.
//
// The start payload is not escaped: Telegram accepts only [A-Za-z0-9_-] there,
// which is exactly what the crawler stores as BusinessID, and the bot parses
// the same literal after "alarm_".
func (a *apiHandler) alarmLink(businessID string) string {
	if a.botUsername == "" || businessID == "" {
		return ""
	}
	return fmt.Sprintf("https://t.me/%s?start=%s%s", a.botUsername, alarmStartPrefix, businessID)
}

// compareKeywordsByCounter is the leaderboard order: most related notices
// first, then the id so equal counters keep a stable order.
func compareKeywordsByCounter(a, b *model.Keyword) int {
	if a.Counter != b.Counter {
		return cmp.Compare(b.Counter, a.Counter)
	}
	return cmp.Compare(keywordID(a), keywordID(b))
}

// compareKeywordsByID is creation order: ids are assigned by SQLite, so
// ascending id is ascending creation time.
func compareKeywordsByID(a, b *model.Keyword) int {
	return cmp.Compare(keywordID(a), keywordID(b))
}

// keywordID dereferences the primary key, tolerating the nil pointer a keyword
// loaded through a projection can carry.
func keywordID(keyword *model.Keyword) int32 {
	if keyword == nil || keyword.ID == nil {
		return 0
	}
	return *keyword.ID
}
