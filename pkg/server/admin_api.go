package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
)

const (
	apiMePath         = "/api/me"
	adminUsersPath    = "/api/admin/users"
	adminSettingsPath = "/api/admin/settings"
	adminStatusPath   = "/api/admin/status"

	// maxAdminUserBatch caps one POST /api/admin/users batch. It bounds both the
	// per-id work and the response size, and matches the keyword batch limit so
	// one number describes every bulk endpoint.
	maxAdminUserBatch = 50

	// Role names on the wire. The database stores int32 (model.RoleUser /
	// model.RoleAdmin); the API speaks the names so the client never depends on
	// the numeric encoding.
	adminRoleName = "admin"
	userRoleName  = "user"

	// Guard messages returned with 409 guard. They are fixed English phrases,
	// not fmt.Errorf output: the client shows them verbatim and the DAL sentinel
	// text is an internal detail.
	guardSelfTargetMessage = "cannot change your own access"
	guardLastAdminMessage  = "cannot remove the last administrator"
	guardEnvManagedMessage = "managed by MANAGER_ID / ALLOWED_USER_IDS"
)

// AdminStatusProvider supplies the deployment facts the status screen cannot
// read from the database. It is implemented by the handler layer (which knows
// the webhook state) and injected through APIConfig.Status; nil means the
// deployment has nothing extra to report.
type AdminStatusProvider interface {
	SystemStatus(ctx context.Context) AdminStatus
}

// AdminStatus is one snapshot of the optional deployment facts.
type AdminStatus struct {
	// Webhook is nil when webhook mode is not configured.
	Webhook *WebhookStatus
	// RecentDenied lists recently rejected accounts. Callers may return nil.
	RecentDenied []DeniedUser
}

// WebhookStatus describes the Telegram webhook, when one is configured.
type WebhookStatus struct {
	URL       string
	LastError string
	Pending   int
}

// DeniedUser is one account rejected by the access policy.
type DeniedUser struct {
	UserID   int64
	Username string
	Name     string
	At       time.Time
}

// meResponse is the payload of GET /api/me.
type meResponse struct {
	UserID     int64  `json:"userId"`
	Role       string `json:"role"`
	AccessMode string `json:"accessMode"`
	Remark     string `json:"remark"`
}

// adminUserItem is how one account appears in the admin list and in the answer
// of a write.
type adminUserItem struct {
	UserID    int64   `json:"userId"`
	Remark    string  `json:"remark"`
	Role      string  `json:"role"`
	Enabled   bool    `json:"enabled"`
	FromEnv   bool    `json:"fromEnv"`
	CreatedAt string  `json:"createdAt"`
	LastSeen  *string `json:"lastSeen"`
}

// adminUsersResponse is the payload of GET /api/admin/users.
type adminUsersResponse struct {
	Items      []adminUserItem `json:"items"`
	Total      int64           `json:"total"`
	NextOffset *int            `json:"nextOffset"`
}

// adminAddUsersRequest is the body of POST /api/admin/users.
type adminAddUsersRequest struct {
	UserIDs string `json:"userIds"`
	Role    string `json:"role"`
	Remark  string `json:"remark"`
}

// adminAddUsersResponse reports what a batch created, what it updated and which
// entries could not be parsed.
type adminAddUsersResponse struct {
	Added   []int64  `json:"added"`
	Updated []int64  `json:"updated"`
	Ignored []string `json:"ignored"`
}

// adminUpdateUserRequest is the body of PATCH /api/admin/users/{id}. Every field
// is a pointer so "absent" and "false"/"0" stay distinguishable.
type adminUpdateUserRequest struct {
	Remark  *string `json:"remark"`
	Role    *string `json:"role"`
	Enabled *bool   `json:"enabled"`
}

// adminSettingsResponse is the payload of GET and PATCH /api/admin/settings.
type adminSettingsResponse struct {
	AccessMode string `json:"accessMode"`
}

// adminSettingsRequest is the body of PATCH /api/admin/settings.
type adminSettingsRequest struct {
	AccessMode string `json:"accessMode"`
}

// adminCounts is the row-count block of the status screen.
type adminCounts struct {
	Users     int64 `json:"users"`
	Admins    int64 `json:"admins"`
	Keywords  int64 `json:"keywords"`
	Histories int64 `json:"histories"`
	Alarms    int64 `json:"alarms"`
}

// adminWebhookStatus is the wire form of WebhookStatus.
type adminWebhookStatus struct {
	URL       string `json:"url"`
	LastError string `json:"lastError"`
	Pending   int    `json:"pending"`
}

// adminDeniedUser is the wire form of DeniedUser.
type adminDeniedUser struct {
	UserID   int64  `json:"userId"`
	Username string `json:"username"`
	Name     string `json:"name"`
	At       string `json:"at"`
}

// adminStatusResponse is the payload of GET /api/admin/status.
type adminStatusResponse struct {
	Version      string              `json:"version"`
	BuildTime    string              `json:"buildTime"`
	AccessMode   string              `json:"accessMode"`
	Counts       adminCounts         `json:"counts"`
	Webhook      *adminWebhookStatus `json:"webhook"`
	RecentDenied []adminDeniedUser   `json:"recentDenied"`
}

// guardErrorDetail is the error detail of a 409 guard answer. It is the only
// place the API adds a message to the shared {"error":{"code":"..."}} envelope:
// the Mini App shows the guard explanation to the administrator.
type guardErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type guardErrorResponse struct {
	Error guardErrorDetail `json:"error"`
}

// adminAPI owns the account, settings and status endpoints.
//
// It shares the writeAPI created for the keyword/alarm CRUD instead of building
// its own limiter: "30 writes per minute per user" is one budget across every
// write endpoint, so a client cannot double its allowance by mixing admin and
// content edits.
type adminAPI struct {
	write  *writeAPI
	status AdminStatusProvider
}

func newAdminAPI(status AdminStatusProvider, write *writeAPI) *adminAPI {
	return &adminAPI{write: write, status: status}
}

// register adds the account and admin routes to mux.
//
// /api/me is only behind the access gate (it is how a rejected client learns
// why it is rejected, so it must stay reachable for allowed users); everything
// under /api/admin/ additionally requires an enabled administrator.
func (a *adminAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+apiMePath, a.handleMe)

	mux.Handle("GET "+adminUsersPath, requireAdmin(http.HandlerFunc(a.handleListUsers)))
	mux.Handle("POST "+adminUsersPath, requireAdmin(http.HandlerFunc(a.handleAddUsers)))
	mux.Handle("PATCH "+adminUsersPath+"/{id}", requireAdmin(http.HandlerFunc(a.handleUpdateUser)))
	mux.Handle("DELETE "+adminUsersPath+"/{id}", requireAdmin(http.HandlerFunc(a.handleDeleteUser)))
	mux.Handle("GET "+adminSettingsPath, requireAdmin(http.HandlerFunc(a.handleGetSettings)))
	mux.Handle("PATCH "+adminSettingsPath, requireAdmin(http.HandlerFunc(a.handleUpdateSettings)))
	mux.Handle("GET "+adminStatusPath, requireAdmin(http.HandlerFunc(a.handleStatus)))
}

// requireAdmin rejects authenticated callers that are not enabled
// administrators. It runs after initData and requireAllowed, so the request
// already carries a trusted identity.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !dal.User.IsAdmin(user.ID) {
			WriteError(w, http.StatusForbidden, "forbidden_admin")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorize is the write budget shared with the keyword endpoints. Admin reads
// do not call it: only mutations are rate limited.
func (a *adminAPI) authorize(w http.ResponseWriter, r *http.Request) (InitDataUser, bool) {
	return a.write.authorize(w, r)
}

// handleMe serves GET /api/me: who the caller is and which access mode the
// deployment runs. It is deliberately reachable for every allowed account,
// including one that is not an administrator.
func (a *adminAPI) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	mode, err := currentAccessMode()
	if err != nil {
		slog.Error("mini app access mode read failed", "endpoint", "me", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	remark := ""
	if row, found := dal.User.Get(user.ID); found {
		remark = row.Remark
	}

	role := userRoleName
	if dal.User.IsAdmin(user.ID) {
		role = adminRoleName
	}

	WriteJSON(w, http.StatusOK, meResponse{
		UserID:     user.ID,
		Role:       role,
		AccessMode: mode,
		Remark:     remark,
	})
}

// handleListUsers serves GET /api/admin/users.
func (a *adminAPI) handleListUsers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	offset, limit, err := parsePaging(query)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}

	rows, total := dal.User.List(strings.TrimSpace(query.Get("q")), offset, limit)

	items := make([]adminUserItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, adminUserItemOf(row))
	}

	WriteJSON(w, http.StatusOK, adminUsersResponse{
		Items:      items,
		Total:      total,
		NextOffset: nextOffset(offset, len(items), total),
	})
}

// handleAddUsers serves POST /api/admin/users: a batch of ids is added when new
// and updated when already known.
//
// Entries that do not parse as a Telegram id are reported in "ignored" instead
// of failing the batch, because a pasted list often carries one stale value. A
// batch with no usable id at all is a client error.
func (a *adminAPI) handleAddUsers(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r)
	if !ok {
		return
	}

	var payload adminAddUsersRequest
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	roleName := strings.TrimSpace(payload.Role)
	if roleName == "" {
		roleName = userRoleName
	}
	role, ok := parseRoleName(roleName)
	if !ok {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}
	remark := strings.TrimSpace(payload.Remark)

	ids, ignored, ok := parseAdminUserIDs(payload.UserIDs)
	if !ok {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}

	added := make([]int64, 0, len(ids))
	updated := make([]int64, 0, len(ids))

	for _, userID := range ids {
		if _, found := dal.User.Get(userID); !found {
			if err := createAdminUser(userID, role, remark); err != nil {
				slog.Error("admin user create failed", "endpoint", "users.add", "error", err)
				WriteError(w, http.StatusInternalServerError, "internal_error")
				return
			}
			added = append(added, userID)
			continue
		}

		patch := dal.UserPatch{Role: &role}
		if remark != "" {
			// An empty remark means "leave the existing label alone": a bulk add
			// must not wipe labels the administrator set earlier.
			patch.Remark = &remark
		}

		if _, err := dal.User.Update(actor.ID, userID, patch); err != nil {
			if errors.Is(err, dal.ErrNotFound) {
				// The row disappeared between the lookup and the update; treat it
				// as a fresh insert rather than failing the whole batch.
				if err := createAdminUser(userID, role, remark); err != nil {
					slog.Error("admin user create failed", "endpoint", "users.add", "error", err)
					WriteError(w, http.StatusInternalServerError, "internal_error")
					return
				}
				added = append(added, userID)
				continue
			}
			writeAdminUserError(w, err)
			return
		}
		updated = append(updated, userID)
	}

	WriteJSON(w, http.StatusOK, adminAddUsersResponse{
		Added:   added,
		Updated: updated,
		Ignored: ignored,
	})
}

// handleUpdateUser serves PATCH /api/admin/users/{id}.
func (a *adminAPI) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r)
	if !ok {
		return
	}

	userID, ok := adminUserIDFromPath(w, r)
	if !ok {
		return
	}

	var payload adminUpdateUserRequest
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	patch := dal.UserPatch{}
	if payload.Remark != nil {
		remark := strings.TrimSpace(*payload.Remark)
		patch.Remark = &remark
	}
	if payload.Role != nil {
		role, ok := parseRoleName(strings.TrimSpace(*payload.Role))
		if !ok {
			WriteError(w, http.StatusBadRequest, "bad_request")
			return
		}
		patch.Role = &role
	}
	if payload.Enabled != nil {
		patch.Enabled = payload.Enabled
	}

	row, err := dal.User.Update(actor.ID, userID, patch)
	if err != nil {
		writeAdminUserError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, adminUserItemOf(row))
}

// handleDeleteUser serves DELETE /api/admin/users/{id}.
func (a *adminAPI) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r)
	if !ok {
		return
	}

	userID, ok := adminUserIDFromPath(w, r)
	if !ok {
		return
	}

	if err := dal.User.Delete(actor.ID, userID); err != nil {
		writeAdminUserError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleGetSettings serves GET /api/admin/settings.
func (a *adminAPI) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	mode, err := currentAccessMode()
	if err != nil {
		slog.Error("mini app access mode read failed", "endpoint", "settings.get", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	WriteJSON(w, http.StatusOK, adminSettingsResponse{AccessMode: mode})
}

// handleUpdateSettings serves PATCH /api/admin/settings.
func (a *adminAPI) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.authorize(w, r); !ok {
		return
	}

	var payload adminSettingsRequest
	if !decodeJSONBody(w, r, &payload) {
		return
	}

	mode := strings.TrimSpace(payload.AccessMode)
	if mode != model.AccessModeOff && mode != model.AccessModeRestricted {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}

	if err := dal.SetSetting(model.SettingAccessMode, mode); err != nil {
		slog.Error("mini app access mode write failed", "endpoint", "settings.update", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	WriteJSON(w, http.StatusOK, adminSettingsResponse{AccessMode: mode})
}

// handleStatus serves GET /api/admin/status.
func (a *adminAPI) handleStatus(w http.ResponseWriter, r *http.Request) {
	mode, err := currentAccessMode()
	if err != nil {
		slog.Error("mini app access mode read failed", "endpoint", "status", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// Row counts degrade to zero instead of failing the whole screen: the status
	// page is a diagnostic view, and one unreadable table should not hide the
	// rest.
	_, users := dal.User.List("", 0, 1)
	counts := adminCounts{
		Users:     users,
		Admins:    dal.User.CountActiveAdmins(),
		Keywords:  statusCount("keywords", dal.Keyword.Count),
		Histories: statusCount("histories", dal.History.Count),
		Alarms:    statusCount("alarms", dal.Alarm.Count),
	}

	response := adminStatusResponse{
		Version:    constant.Version,
		BuildTime:  constant.BuildTime,
		AccessMode: mode,
		Counts:     counts,
		// Initialised so a deployment without a provider serves [] and not null.
		RecentDenied: make([]adminDeniedUser, 0),
	}

	if a.status != nil {
		status := a.status.SystemStatus(r.Context())
		if status.Webhook != nil {
			response.Webhook = &adminWebhookStatus{
				URL:       status.Webhook.URL,
				LastError: status.Webhook.LastError,
				Pending:   status.Webhook.Pending,
			}
		}
		for _, denied := range status.RecentDenied {
			response.RecentDenied = append(response.RecentDenied, adminDeniedUser{
				UserID:   denied.UserID,
				Username: denied.Username,
				Name:     denied.Name,
				At:       formatAPITime(denied.At),
			})
		}
	}

	WriteJSON(w, http.StatusOK, response)
}

// currentAccessMode reads the stored access mode. A missing or blank row means
// "off", the value EnsureBootstrap writes for a database without administrators.
func currentAccessMode() (string, error) {
	value, ok, err := dal.GetSetting(model.SettingAccessMode)
	if err != nil {
		// A database that has not run the access-control migration has no
		// settings table. Reporting the mode the bootstrap would write there keeps
		// /api/me usable instead of answering 500 to every client of such a
		// deployment; a genuine read failure on a provisioned database still
		// surfaces as an error.
		if !accessControlProvisioned() {
			return model.AccessModeOff, nil
		}
		return "", err
	}
	if !ok || strings.TrimSpace(value) == "" {
		return model.AccessModeOff, nil
	}
	return value, nil
}

// accessDB reports whether the access-control schema is reachable. It is only
// used to keep a database that predates this feature usable: such a database has
// no settings table, so the DAL cannot answer a policy question about it yet.
func accessDB() *gorm.DB {
	if dal.History == nil {
		return nil
	}
	return dal.History.UnderlyingDB()
}

// createAdminUser inserts one manually managed account (from_env=false).
//
// The insert goes through the DAL so the authorisation cache is invalidated by
// the same rule as every other account change, and so a future creation path
// cannot drift from the guarded one.
func createAdminUser(userID int64, role int32, remark string) error {
	_, err := dal.User.CreateManual(userID, role, remark)
	return err
}

// writeAdminUserError maps the account-operation failures to the API contract:
// guard refusals are 409 with a fixed message, a missing row is 404, and
// anything else is a 500 that is logged without request data.
func writeAdminUserError(w http.ResponseWriter, err error) {
	var message string
	switch {
	case errors.Is(err, dal.ErrSelfTarget):
		message = guardSelfTargetMessage
	case errors.Is(err, dal.ErrLastAdmin):
		message = guardLastAdminMessage
	case errors.Is(err, dal.ErrEnvManaged):
		message = guardEnvManagedMessage
	case errors.Is(err, dal.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found")
		return
	default:
		slog.Error("admin user operation failed", "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	WriteJSON(w, http.StatusConflict, guardErrorResponse{
		Error: guardErrorDetail{Code: "guard", Message: message},
	})
}

// adminUserIDFromPath parses the {id} wildcard as a Telegram user id. A
// non-numeric or non-positive value addresses no account, so it is a bad
// request rather than a missing resource.
func adminUserIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := strings.TrimSpace(r.PathValue("id"))
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return 0, false
	}
	return id, true
}

// parseRoleName maps the wire names to the stored values.
func parseRoleName(name string) (int32, bool) {
	switch name {
	case adminRoleName:
		return model.RoleAdmin, true
	case userRoleName:
		return model.RoleUser, true
	default:
		return 0, false
	}
}

// roleName maps a stored role to its wire name. Unknown values are reported as
// a regular user, which is the safe direction for a corrupted row.
func roleName(role int32) string {
	if role == model.RoleAdmin {
		return adminRoleName
	}
	return userRoleName
}

// adminUserItemOf renders one row for the API.
func adminUserItemOf(row *model.User) adminUserItem {
	item := adminUserItem{
		UserID:    row.UserID,
		Remark:    row.Remark,
		Role:      roleName(row.Role),
		Enabled:   row.Enabled,
		FromEnv:   row.FromEnv,
		CreatedAt: formatAPITime(row.CreatedAt),
	}
	if row.LastSeen != nil {
		seen := formatAPITime(*row.LastSeen)
		item.LastSeen = &seen
	}
	return item
}

// parseAdminUserIDs splits, trims and de-duplicates the userIds field.
//
// The boolean is false when the batch must be rejected as a whole: no entries,
// more entries than maxAdminUserBatch, or no valid id at all. Unparsable and
// non-positive entries are reported in ignored, as the contract requires, so one
// stale id in a pasted list does not reject the rest.
func parseAdminUserIDs(raw string) (ids []int64, ignored []string, ok bool) {
	ids = make([]int64, 0)
	ignored = make([]string, 0)

	parts := strings.Split(raw, ",")
	tokens := make([]string, 0, len(parts))
	seenTokens := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}
		if _, duplicate := seenTokens[token]; duplicate {
			continue
		}
		seenTokens[token] = struct{}{}
		tokens = append(tokens, token)
	}

	if len(tokens) == 0 || len(tokens) > maxAdminUserBatch {
		return nil, nil, false
	}

	seenIDs := make(map[int64]struct{}, len(tokens))
	for _, token := range tokens {
		id, err := strconv.ParseInt(token, 10, 64)
		if err != nil || id <= 0 {
			ignored = append(ignored, token)
			continue
		}
		if _, duplicate := seenIDs[id]; duplicate {
			continue
		}
		seenIDs[id] = struct{}{}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return nil, nil, false
	}
	return ids, ignored, true
}

// statusCount runs one table count, logging a failure and reporting zero. The
// label names the table, never a user value.
func statusCount(label string, read func() (int64, error)) int64 {
	count, err := read()
	if err != nil {
		slog.Warn("admin status count failed", "table", label, "error", err)
		return 0
	}
	return count
}
