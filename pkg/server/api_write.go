package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
)

// Write-endpoint limits. Every one of them is a documented client contract, so
// they live next to the handlers instead of being repeated as literals.
const (
	// writeRateLimit and writeRateWindow bound how often one user may attempt a
	// write. Every attempt counts, including rejected ones: otherwise a client
	// could probe ids or bodies for free.
	writeRateLimit  = 30
	writeRateWindow = time.Minute

	// maxKeywordsPerRequest caps one add batch. It bounds both the SQL IN list
	// InsertMany builds and the response size.
	maxKeywordsPerRequest = 50

	// maxKeywordRunes caps one keyword. It is measured in runes, not bytes, so a
	// Chinese keyword is not rejected at a third of the visible length.
	maxKeywordRunes = 128

	// maxWriteBodyBytes bounds the request body. The largest legitimate body is
	// 50 keywords of 128 runes (~20 KB UTF-8), so this leaves ample room while
	// keeping a hostile body from being decoded.
	maxWriteBodyBytes = 64 << 10
)

// keywordBatchRequest is the body of POST /api/keywords and /api/alarms.
type keywordBatchRequest struct {
	Keywords string `json:"keywords"`
}

// keywordBatchResponse reports what a batch added and what already existed.
type keywordBatchResponse struct {
	Added   []string `json:"added"`
	Skipped []string `json:"skipped"`
}

// keywordRenameRequest is the body of PATCH /api/keywords/{id}.
type keywordRenameRequest struct {
	Keyword string `json:"keyword"`
}

// keywordRenameResponse echoes the stored keyword so the client can render the
// trimmed value it did not have to compute itself.
type keywordRenameResponse struct {
	ID      int32  `json:"id"`
	Keyword string `json:"keyword"`
}

// writeAPI owns the write endpoints and the one rate limiter shared by them.
// The limiter is keyed per user, so a second account is never slowed down by
// the first.
type writeAPI struct {
	limiter *Limiter
}

// newWriteAPI builds the production write surface with the documented limit.
func newWriteAPI() *writeAPI {
	return newWriteAPIWithLimiter(NewLimiter(writeRateLimit, writeRateWindow))
}

// newWriteAPIWithLimiter is newWriteAPI with an injected limiter, so tests can
// use a smaller window or a fake clock without sleeping.
func newWriteAPIWithLimiter(limiter *Limiter) *writeAPI {
	return &writeAPI{limiter: limiter}
}

// register adds the six write routes to mux. They are registered from api.go,
// which owns the shared mux and wraps it in the initData middleware; keeping
// the list here means the write surface can be read in one place.
//
// The {id} wildcard only matches a non-empty segment, so a trailing slash is
// answered by the mux rather than by a handler that would have to reject "".
func (a *writeAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("POST "+apiKeywordsPath, a.handleCreateKeywords)
	mux.HandleFunc("PATCH "+apiKeywordsPath+"/{id}", a.handleUpdateKeyword)
	mux.HandleFunc("DELETE "+apiKeywordsPath+"/{id}", a.handleDeleteKeyword)
	mux.HandleFunc("POST "+apiAlarmsPath, a.handleCreateAlarms)
	mux.HandleFunc("PATCH "+apiAlarmsPath+"/{id}", a.handleUpdateAlarm)
	mux.HandleFunc("DELETE "+apiAlarmsPath+"/{id}", a.handleDeleteAlarm)
}

// handleCreateKeywords adds project keywords.
func (a *writeAPI) handleCreateKeywords(w http.ResponseWriter, r *http.Request) {
	a.create(w, r, "keywords", model.PROJECT)
}

// handleCreateAlarms adds alarm keywords.
func (a *writeAPI) handleCreateAlarms(w http.ResponseWriter, r *http.Request) {
	a.create(w, r, "alarms", model.ALARM)
}

// create serves both add endpoints: they differ only in the keyword type they
// write, and every validation rule is shared.
func (a *writeAPI) create(w http.ResponseWriter, r *http.Request, endpoint string, kind model.KeywordType) {
	user, ok := a.authorize(w, r)
	if !ok {
		return
	}

	var payload keywordBatchRequest
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	keywords, ok := parseKeywordBatch(payload.Keywords)
	if !ok {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}

	added, skipped, err := dal.Keyword.InsertMany(user.ID, kind, keywords)
	if err != nil {
		// Neither the request body nor the keywords are logged: both are user
		// input.
		slog.Error("mini app write failed", "endpoint", endpoint+".create", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	WriteJSON(w, http.StatusOK, keywordBatchResponse{Added: added, Skipped: skipped})
}

// handleUpdateKeyword serves PATCH /api/keywords/{id}.
func (a *writeAPI) handleUpdateKeyword(w http.ResponseWriter, r *http.Request) {
	a.update(w, r, "keywords")
}

// handleUpdateAlarm serves PATCH /api/alarms/{id}.
func (a *writeAPI) handleUpdateAlarm(w http.ResponseWriter, r *http.Request) {
	a.update(w, r, "alarms")
}

// update serves both rename endpoints. Ownership is expressed by the (user, id)
// predicate in the DAL statement, so an id that does not belong to the caller is
// simply not found, whichever URL it was sent to.
func (a *writeAPI) update(w http.ResponseWriter, r *http.Request, endpoint string) {
	user, ok := a.authorize(w, r)
	if !ok {
		return
	}

	id, ok := keywordIDFromPath(w, r)
	if !ok {
		return
	}

	var payload keywordRenameRequest
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	keyword := strings.TrimSpace(payload.Keyword)
	// A blank name or one past the documented length is a client error, not a
	// silent truncation: the user would otherwise not see what was stored.
	if keyword == "" || utf8.RuneCountInString(keyword) > maxKeywordRunes {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}

	updated, err := dal.Keyword.UpdateOwned(user.ID, id, keyword)
	if err != nil {
		slog.Error("mini app write failed", "endpoint", endpoint+".update", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if !updated {
		WriteError(w, http.StatusNotFound, "not_found")
		return
	}

	WriteJSON(w, http.StatusOK, keywordRenameResponse{ID: id, Keyword: keyword})
}

// handleDeleteKeyword serves DELETE /api/keywords/{id}.
func (a *writeAPI) handleDeleteKeyword(w http.ResponseWriter, r *http.Request) {
	a.delete(w, r, "keywords")
}

// handleDeleteAlarm serves DELETE /api/alarms/{id}.
func (a *writeAPI) handleDeleteAlarm(w http.ResponseWriter, r *http.Request) {
	a.delete(w, r, "alarms")
}

// delete serves both removal endpoints, for the same reason update is shared.
func (a *writeAPI) delete(w http.ResponseWriter, r *http.Request, endpoint string) {
	user, ok := a.authorize(w, r)
	if !ok {
		return
	}

	id, ok := keywordIDFromPath(w, r)
	if !ok {
		return
	}

	deleted, err := dal.Keyword.DeleteOwned(user.ID, id)
	if err != nil {
		slog.Error("mini app write failed", "endpoint", endpoint+".delete", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if !deleted {
		// Already gone and never-owned are the same answer: revealing the
		// difference would confirm that someone else's id exists.
		WriteError(w, http.StatusNotFound, "not_found")
		return
	}

	// 204 with no body: the client already knows which id it asked to remove.
	w.WriteHeader(http.StatusNoContent)
}

// authorize resolves the caller and spends one rate-limit token. It answers the
// response itself on failure, so handlers only need to return.
//
// Limiting happens before the body is read: a blocked client must not get free
// JSON parsing, and a malformed body is still an attempt.
func (a *writeAPI) authorize(w http.ResponseWriter, r *http.Request) (InitDataUser, bool) {
	user, ok := apiUser(w, r)
	if !ok {
		return InitDataUser{}, false
	}

	// The id is the key, not the username: a username can change, an id cannot,
	// and it is already the account key for every read endpoint.
	if !a.limiter.Allow(strconv.FormatInt(user.ID, 10)) {
		WriteError(w, http.StatusTooManyRequests, "too_many_requests")
		return InitDataUser{}, false
	}

	return user, true
}

// parseKeywordBatch splits, trims, de-duplicates and validates one comma-
// separated batch. The boolean is false for a batch the endpoint must reject:
// nothing usable was left, an entry is longer than maxKeywordRunes, or the
// distinct entries exceed maxKeywordsPerRequest.
//
// Duplicates are dropped before the cap is applied: otherwise a client could
// smuggle 51 distinct keywords past the limit by sending one of them twice.
func parseKeywordBatch(raw string) ([]string, bool) {
	parts := strings.Split(raw, ",")
	keywords := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))

	for _, part := range parts {
		keyword := strings.TrimSpace(part)
		if keyword == "" {
			continue
		}
		if utf8.RuneCountInString(keyword) > maxKeywordRunes {
			return nil, false
		}
		if _, ok := seen[keyword]; ok {
			continue
		}
		seen[keyword] = struct{}{}
		keywords = append(keywords, keyword)
	}

	if len(keywords) == 0 || len(keywords) > maxKeywordsPerRequest {
		return nil, false
	}
	return keywords, true
}

// keywordIDFromPath parses the {id} wildcard. A non-numeric or non-positive id
// is a malformed request rather than a missing resource: no id was addressed at
// all. A well-formed id that matches nothing is answered 404 by the DAL result.
func keywordIDFromPath(w http.ResponseWriter, r *http.Request) (int32, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return 0, false
	}
	return int32(id), true
}

// decodeJSONBody decodes one JSON object from a size-capped body. Any decode
// failure is a 400: the client sent something this endpoint cannot interpret.
// Unknown fields are ignored so a newer client can add one without breaking an
// older server.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxWriteBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	return true
}
