package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
)

// adminTestIDs keeps the fixtures readable: three accounts with different roles.
const (
	adminTestAdminID  = int64(10001)
	adminTestUserID   = int64(20002)
	adminTestStranger = int64(30003)
	adminTestOtherID  = int64(30004)
)

// setupAdminAPITestDB opens a private database with the access-control tables as
// well as the content ones, and points the package DAOs at it.
//
// The restore mirrors setupAPITestDB: dal.SetDefault is global, so the previous
// binding must come back after the test, and the user cache is cleared on both
// ends so no authorisation answer survives into the next test.
func setupAdminAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "admin_api.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(
		&model.History{}, &model.Keyword{}, &model.Alarm{},
		&model.User{}, &model.Setting{},
	); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	previous := *dal.Q
	dal.SetDefault(db)
	dal.User.Invalidate()
	t.Cleanup(func() {
		*dal.Q = previous
		dal.Alarm = &dal.Q.Alarm
		dal.History = &dal.Q.History
		dal.Keyword = &dal.Q.Keyword
		dal.User.Invalidate()
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// seedAdminTestUser inserts one account row directly, bypassing the guards: the
// fixtures describe the database state, not an administrator's action.
func seedAdminTestUser(t *testing.T, db *gorm.DB, userID int64, role int32, enabled, fromEnv bool, remark string) {
	t.Helper()

	now := time.Now().Truncate(time.Second)
	row := &model.User{
		UserID:    userID,
		Remark:    remark,
		Role:      role,
		Enabled:   enabled,
		FromEnv:   fromEnv,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("seed user %d: %v", userID, err)
	}
	dal.User.Invalidate()
}

// adminTestRaw signs initData for one account at now.
func adminTestRaw(t *testing.T, now time.Time, userID int64) string {
	t.Helper()
	return apiTestInitData(t, now, userID)
}

// adminTestRawIdentity signs initData carrying a specific Telegram identity, so
// /api/v1/me can be exercised with the name/username it persists.
func adminTestRawIdentity(t *testing.T, authenticatedAt time.Time, userID int64, first, last, username string) string {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		"id":            userID,
		"first_name":    first,
		"last_name":     last,
		"username":      username,
		"language_code": "zh",
	})
	if err != nil {
		t.Fatalf("encode identity user: %v", err)
	}

	fields := withField(baseFields(authenticatedAt.Unix()), "user", string(payload))
	raw, _ := signInitData(testBotToken, fields)
	return raw
}

// assertAdminGuard asserts the 409 guard envelope keeps its code and fixed
// message and carries the machine-readable reason.
func assertAdminGuard(t *testing.T, rec *httptest.ResponseRecorder, message, reason string) {
	t.Helper()

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var payload guardErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode guard envelope %q: %v", rec.Body.String(), err)
	}
	if payload.Error.Code != "guard" || payload.Error.Message != message || payload.Error.Reason != reason {
		t.Fatalf("guard envelope = %+v, want code=guard message=%q reason=%q", payload.Error, message, reason)
	}
}

// TestAPIAccessGate covers the requireAllowed layer: a restricted deployment
// serves listed accounts and rejects everyone else before any handler runs,
// including the JSON 404 fallback.
func TestAPIAccessGate(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)

	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, true, "admin")
	seedAdminTestUser(t, db, adminTestUserID, model.RoleUser, true, false, "allowed")
	if err := dal.SetSetting(model.SettingAccessMode, model.AccessModeRestricted); err != nil {
		t.Fatalf("SetSetting() error: %v", err)
	}

	rawAdmin := adminTestRaw(t, now, adminTestAdminID)
	rawAllowed := adminTestRaw(t, now, adminTestUserID)
	rawStranger := adminTestRaw(t, now, adminTestStranger)

	for _, target := range []string{apiStatisticsPath, apiMePath, "/api/v1/unknown-endpoint"} {
		t.Run("stranger "+target, func(t *testing.T) {
			assertWriteEnvelope(t, sendAPI(t, handler, target, rawStranger), http.StatusForbidden, "forbidden")
		})
	}

	for _, raw := range []string{rawAdmin, rawAllowed} {
		t.Run("allowed account reads statistics", func(t *testing.T) {
			rec := sendAPI(t, handler, apiStatisticsPath, raw)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
			}
		})
	}

	t.Run("unauthenticated me", func(t *testing.T) {
		assertWriteEnvelope(t, sendAPI(t, handler, apiMePath, ""),
			http.StatusUnauthorized, "unauthorized")
	})

	t.Run("the convert route is gated too", func(t *testing.T) {
		// Registered only with a starter, so it gets its own handler here. The
		// administrator owns the notice, otherwise the convert handler's own
		// ownership rule would answer 403 and hide what this asserts.
		if err := dal.History.Insert([]*model.History{{
			UserID: adminTestAdminID, URL: convertTestURL, Title: "notice", UpdatedAt: now,
		}}); err != nil {
			t.Fatalf("seed history: %v", err)
		}

		convertHandler := apiTestHandler(APIConfig{
			BotToken:    testBotToken,
			Conversion:  newFakeStarter(),
			AllowedHost: convertTestHost,
		}, now)
		body := `{"url":"` + convertTestURL + `","format":"pdf"}`

		assertWriteEnvelope(t,
			writeAPIRequest(t, convertHandler, http.MethodPost, apiConvertPath, rawStranger, body),
			http.StatusForbidden, "forbidden")
		if rec := writeAPIRequest(t, convertHandler, http.MethodPost, apiConvertPath, rawAdmin, body); rec.Code != http.StatusAccepted {
			t.Fatalf("admin convert: status = %d, want %d (body %q)", rec.Code, http.StatusAccepted, rec.Body.String())
		}
	})
}

// TestAPIMeUnprovisionedDatabase covers a database that predates the
// access-control migration: /api/v1/me must report the mode the bootstrap would
// write (off) instead of failing for every client.
func TestAPIMeUnprovisionedDatabase(t *testing.T) {
	// Deliberately the helper that migrates the content tables only.
	setupAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)

	payload := decodeAPI[meResponse](t, sendAPI(t, handler, apiMePath, adminTestRaw(t, now, adminTestStranger)))
	if payload.AccessMode != model.AccessModeOff {
		t.Fatalf("accessMode = %q, want off", payload.AccessMode)
	}
	if payload.Role != userRoleName || payload.Remark != "" {
		t.Fatalf("me = %+v, want an unlisted regular user", payload)
	}
}

// TestAPIAdminGate proves every /api/admin route is unreachable for an allowed
// non-administrator, and unreachable without initData at all.
func TestAPIAdminGate(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)

	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, true, "admin")
	seedAdminTestUser(t, db, adminTestUserID, model.RoleUser, true, false, "allowed")
	if err := dal.SetSetting(model.SettingAccessMode, model.AccessModeRestricted); err != nil {
		t.Fatalf("SetSetting() error: %v", err)
	}

	rawUser := adminTestRaw(t, now, adminTestUserID)
	rawAdmin := adminTestRaw(t, now, adminTestAdminID)

	cases := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{"list users", http.MethodGet, adminUsersPath, ""},
		{"add users", http.MethodPost, adminUsersPath, `{"userIds":"30004"}`},
		{"update user", http.MethodPatch, adminUsersPath + "/30004", `{"remark":"x"}`},
		{"delete user", http.MethodDelete, adminUsersPath + "/30004", ""},
		{"get settings", http.MethodGet, adminSettingsPath, ""},
		{"update settings", http.MethodPatch, adminSettingsPath, `{"accessMode":"off"}`},
		{"status", http.MethodGet, adminStatusPath, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name+" denies non-admin", func(t *testing.T) {
			assertWriteEnvelope(t,
				writeAPIRequest(t, handler, tc.method, tc.target, rawUser, tc.body),
				http.StatusForbidden, "forbidden_admin")
		})
		t.Run(tc.name+" denies unauthenticated", func(t *testing.T) {
			assertWriteEnvelope(t,
				writeAPIRequest(t, handler, tc.method, tc.target, "", tc.body),
				http.StatusUnauthorized, "unauthorized")
		})
	}

	// An administrator reaches every read; assertion here is only that the gate
	// lets them through (each endpoint has its own happy-path test below).
	for _, target := range []string{adminUsersPath, adminSettingsPath, adminStatusPath} {
		if rec := sendAPI(t, handler, target, rawAdmin); rec.Code != http.StatusOK {
			t.Fatalf("admin GET %s: status = %d, want 200 (body %q)", target, rec.Code, rec.Body.String())
		}
	}
}

// TestAPIMe covers the identity endpoint for both roles, for an account outside
// the users table under off mode, and its own gate under restricted mode.
func TestAPIMe(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)

	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, true, "boss")
	seedAdminTestUser(t, db, adminTestUserID, model.RoleUser, true, false, "colleague")
	if err := dal.SetSetting(model.SettingAccessMode, model.AccessModeRestricted); err != nil {
		t.Fatalf("SetSetting() error: %v", err)
	}

	admin := decodeAPI[meResponse](t, sendAPI(t, handler, apiMePath, adminTestRaw(t, now, adminTestAdminID)))
	if admin.UserID != adminTestAdminID || admin.Role != adminRoleName {
		t.Errorf("admin me = %+v, want admin role for %d", admin, adminTestAdminID)
	}
	if admin.AccessMode != model.AccessModeRestricted || admin.Remark != "boss" {
		t.Errorf("admin me = %+v, want restricted mode and the stored remark", admin)
	}

	user := decodeAPI[meResponse](t, sendAPI(t, handler, apiMePath, adminTestRaw(t, now, adminTestUserID)))
	if user.Role != userRoleName || user.Remark != "colleague" {
		t.Errorf("user me = %+v, want user role and remark", user)
	}
	// Build facts are public in #429's About block: a regular account must carry
	// the same values /api/admin/status reports.
	if user.Version != constant.Version || user.BuildTime != constant.BuildTime {
		t.Errorf("user build facts = (%q, %q), want (%q, %q)",
			user.Version, user.BuildTime, constant.Version, constant.BuildTime)
	}

	// Off mode: an account with no row is served and learns it is a plain user.
	if err := dal.SetSetting(model.SettingAccessMode, model.AccessModeOff); err != nil {
		t.Fatalf("SetSetting() error: %v", err)
	}
	stranger := decodeAPI[meResponse](t, sendAPI(t, handler, apiMePath, adminTestRaw(t, now, adminTestStranger)))
	if stranger.UserID != adminTestStranger || stranger.Role != userRoleName || stranger.Remark != "" {
		t.Errorf("stranger me = %+v, want an unlisted regular user", stranger)
	}
	if stranger.AccessMode != model.AccessModeOff {
		t.Errorf("accessMode = %q, want %q", stranger.AccessMode, model.AccessModeOff)
	}
}

// TestAPIAdminListUsers covers the paging, the remark/id search and the wire
// shape of one account.
func TestAPIAdminListUsers(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := adminTestRaw(t, now, adminTestAdminID)

	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, true, "primary admin")
	seedAdminTestUser(t, db, adminTestUserID, model.RoleUser, true, false, "corp user")
	seedAdminTestUser(t, db, adminTestOtherID, model.RoleUser, false, false, "another corp")
	seen := now.Add(-time.Hour)
	if err := dal.User.SetLastSeen(adminTestUserID, seen); err != nil {
		t.Fatalf("SetLastSeen() error: %v", err)
	}

	t.Run("first page", func(t *testing.T) {
		payload := decodeAPI[adminUsersResponse](t, sendAPI(t, handler, adminUsersPath+"?limit=2", raw))

		if payload.Total != 3 || len(payload.Items) != 2 {
			t.Fatalf("payload = (%d items, %d total), want (2, 3)", len(payload.Items), payload.Total)
		}
		if payload.NextOffset == nil || *payload.NextOffset != 2 {
			t.Fatalf("nextOffset = %v, want 2", payload.NextOffset)
		}

		first := payload.Items[0]
		if first.UserID != adminTestAdminID || first.Role != adminRoleName || !first.Enabled || !first.FromEnv {
			t.Errorf("first item = %+v, want the enabled environment administrator", first)
		}
		if first.Remark != "primary admin" {
			t.Errorf("remark = %q, want primary admin", first.Remark)
		}
		if _, err := time.Parse(time.RFC3339, first.CreatedAt); err != nil {
			t.Errorf("createdAt = %q is not RFC3339: %v", first.CreatedAt, err)
		}
		if first.LastSeen != nil {
			t.Errorf("lastSeen = %v, want null", *first.LastSeen)
		}

		second := payload.Items[1]
		if second.LastSeen == nil {
			t.Fatal("lastSeen = null, want the recorded timestamp")
		}
		parsed, err := time.Parse(time.RFC3339, *second.LastSeen)
		if err != nil {
			t.Fatalf("lastSeen = %q is not RFC3339: %v", *second.LastSeen, err)
		}
		if !parsed.Equal(seen) {
			t.Errorf("lastSeen = %v, want %v", parsed, seen)
		}
	})

	t.Run("last page has no next offset", func(t *testing.T) {
		payload := decodeAPI[adminUsersResponse](t, sendAPI(t, handler, adminUsersPath+"?limit=2&offset=2", raw))
		if len(payload.Items) != 1 || payload.NextOffset != nil {
			t.Fatalf("payload = (%d items, nextOffset=%v), want 1 item and null", len(payload.Items), payload.NextOffset)
		}
	})

	t.Run("remark search", func(t *testing.T) {
		payload := decodeAPI[adminUsersResponse](t, sendAPI(t, handler, adminUsersPath+"?q=corp", raw))
		if payload.Total != 2 || len(payload.Items) != 2 {
			t.Fatalf("payload = (%d items, %d total), want the two corp rows", len(payload.Items), payload.Total)
		}
	})

	t.Run("id search", func(t *testing.T) {
		payload := decodeAPI[adminUsersResponse](t, sendAPI(t, handler, adminUsersPath+"?q=20002", raw))
		if payload.Total != 1 || len(payload.Items) != 1 || payload.Items[0].UserID != adminTestUserID {
			t.Fatalf("payload = %+v, want the single id match", payload)
		}
	})

	t.Run("invalid paging", func(t *testing.T) {
		assertWriteEnvelope(t, sendAPI(t, handler, adminUsersPath+"?limit=0", raw),
			http.StatusBadRequest, "bad_request")
	})
}

// TestAPIAdminAddUsers covers the batch add: de-duplication, ignored entries,
// the batch limits, the role/remark application and the guard on env-managed
// rows.
func TestAPIAdminAddUsers(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)

	// The actor is a manually managed administrator so the env-managed target
	// below is a different account: otherwise the self-target guard would answer
	// first and the environment rule would never be exercised.
	seedAdminTestUser(t, db, adminTestOtherID, model.RoleAdmin, true, false, "actor")
	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, true, "env admin")
	raw := adminTestRaw(t, now, adminTestOtherID)

	if err := dal.SetSetting(model.SettingAccessMode, model.AccessModeRestricted); err != nil {
		t.Fatalf("SetSetting() error: %v", err)
	}

	post := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		return writeAPIRequest(t, handler, http.MethodPost, adminUsersPath, raw, body)
	}

	t.Run("adds new accounts and reports the ignored entry", func(t *testing.T) {
		payload := decodeAPI[adminAddUsersResponse](t, post(t,
			`{"userIds":"10010, 10011, abc, 10010","role":"user","remark":"seeded"}`))

		if len(payload.Added) != 2 || payload.Added[0] != 10010 || payload.Added[1] != 10011 {
			t.Fatalf("added = %v, want [10010 10011]", payload.Added)
		}
		if payload.Updated == nil || len(payload.Updated) != 0 {
			t.Fatalf("updated = %v, want an empty array", payload.Updated)
		}
		if len(payload.Ignored) != 1 || payload.Ignored[0] != "abc" {
			t.Fatalf("ignored = %v, want [abc]", payload.Ignored)
		}

		row, ok := dal.User.Get(10010)
		if !ok {
			t.Fatal("added account 10010 is missing")
		}
		if row.Role != model.RoleUser || !row.Enabled || row.FromEnv || row.Remark != "seeded" {
			t.Fatalf("row = %+v, want an enabled manually managed user with the remark", row)
		}
	})

	t.Run("an existing account is updated, not duplicated", func(t *testing.T) {
		payload := decodeAPI[adminAddUsersResponse](t, post(t, `{"userIds":"10010","role":"admin"}`))

		if len(payload.Updated) != 1 || payload.Updated[0] != 10010 || len(payload.Added) != 0 {
			t.Fatalf("payload = %+v, want 10010 updated and nothing added", payload)
		}
		row, _ := dal.User.Get(10010)
		if row.Role != model.RoleAdmin {
			t.Errorf("role = %d, want admin", row.Role)
		}
		// An empty remark must not wipe the label set by the earlier batch.
		if row.Remark != "seeded" {
			t.Errorf("remark = %q, want seeded", row.Remark)
		}
	})

	t.Run("an env-managed account cannot be demoted", func(t *testing.T) {
		assertAdminGuard(t, post(t, `{"userIds":"10001","role":"user"}`), guardEnvManagedMessage, guardEnvManagedReason)
	})

	for _, tc := range []struct {
		name string
		body string
	}{
		{"no ids", `{"userIds":"  "}`},
		{"all invalid", `{"userIds":"abc,def"}`},
		{"invalid role", `{"userIds":"10020","role":"root"}`},
		{"too many ids", `{"userIds":"` + manyIDList(51) + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertWriteEnvelope(t, post(t, tc.body), http.StatusBadRequest, "bad_request")
		})
	}

	t.Run("the batch limit is accepted", func(t *testing.T) {
		payload := decodeAPI[adminAddUsersResponse](t, post(t,
			`{"userIds":"`+manyIDList(50)+`"}`))
		if len(payload.Added) != 50 {
			t.Fatalf("added %d accounts, want 50", len(payload.Added))
		}
	})
}

// manyIDList renders count comma-separated ids starting at 50000.
func manyIDList(count int) string {
	ids := make([]string, 0, count)
	for i := range count {
		ids = append(ids, strconv.Itoa(50000+i))
	}
	return strings.Join(ids, ",")
}

// TestAPIAdminUpdateAndDeleteUser covers PATCH and DELETE happy paths, their
// 400/404 cases and the guard mapping.
func TestAPIAdminUpdateAndDeleteUser(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := adminTestRaw(t, now, adminTestAdminID)

	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, false, "actor")
	seedAdminTestUser(t, db, adminTestUserID, model.RoleUser, true, false, "target")
	seedAdminTestUser(t, db, adminTestOtherID, model.RoleAdmin, true, true, "env admin")
	if err := dal.SetSetting(model.SettingAccessMode, model.AccessModeRestricted); err != nil {
		t.Fatalf("SetSetting() error: %v", err)
	}

	patch := func(t *testing.T, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		return writeAPIRequest(t, handler, http.MethodPatch, target, raw, body)
	}
	del := func(t *testing.T, target string) *httptest.ResponseRecorder {
		t.Helper()
		return writeAPIRequest(t, handler, http.MethodDelete, target, raw, "")
	}

	t.Run("renames, promotes and disables", func(t *testing.T) {
		payload := decodeAPI[adminUserItem](t, patch(t, adminUsersPath+"/20002", `{"remark":"  renamed  "}`))
		if payload.Remark != "renamed" {
			t.Fatalf("remark = %q, want the trimmed value", payload.Remark)
		}

		payload = decodeAPI[adminUserItem](t, patch(t, adminUsersPath+"/20002", `{"role":"admin"}`))
		if payload.Role != adminRoleName {
			t.Fatalf("role = %q, want admin", payload.Role)
		}

		payload = decodeAPI[adminUserItem](t, patch(t, adminUsersPath+"/20002", `{"enabled":false}`))
		if payload.Enabled {
			t.Fatal("enabled = true, want false")
		}
	})

	t.Run("bad requests and missing targets", func(t *testing.T) {
		assertWriteEnvelope(t, patch(t, adminUsersPath+"/20002", `{"role":"root"}`),
			http.StatusBadRequest, "bad_request")
		assertWriteEnvelope(t, patch(t, adminUsersPath+"/abc", `{"remark":"x"}`),
			http.StatusBadRequest, "bad_request")
		assertWriteEnvelope(t, patch(t, adminUsersPath+"/999999", `{"remark":"x"}`),
			http.StatusNotFound, "not_found")
		assertWriteEnvelope(t, del(t, adminUsersPath+"/999999"), http.StatusNotFound, "not_found")
	})

	t.Run("guards", func(t *testing.T) {
		// Renaming the actor is allowed; disabling themselves is not.
		if rec := patch(t, adminUsersPath+"/10001", `{"remark":"me"}`); rec.Code != http.StatusOK {
			t.Fatalf("self rename: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
		assertAdminGuard(t, patch(t, adminUsersPath+"/10001", `{"enabled":false}`), guardSelfTargetMessage, guardSelfReason)
		assertAdminGuard(t, del(t, adminUsersPath+"/10001"), guardSelfTargetMessage, guardSelfReason)

		// The environment owns the role/access of its rows, but not the remark.
		assertAdminGuard(t, patch(t, adminUsersPath+"/30004", `{"role":"user"}`), guardEnvManagedMessage, guardEnvManagedReason)
		assertAdminGuard(t, del(t, adminUsersPath+"/30004"), guardEnvManagedMessage, guardEnvManagedReason)
		if rec := patch(t, adminUsersPath+"/30004", `{"remark":"renamed env"}`); rec.Code != http.StatusOK {
			t.Fatalf("env remark: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
		}
	})

	t.Run("deletes and then forgets", func(t *testing.T) {
		if rec := del(t, adminUsersPath+"/20002"); rec.Code != http.StatusNoContent {
			t.Fatalf("delete: status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
		}
		if _, ok := dal.User.Get(adminTestUserID); ok {
			t.Fatal("deleted account is still readable")
		}
		assertWriteEnvelope(t, del(t, adminUsersPath+"/20002"), http.StatusNotFound, "not_found")
	})
}

// TestAdminGuardMessages pins the 409 mapping for every sentinel, including
// ErrLastAdmin: an API caller always acts as an enabled administrator, so a
// last-administrator refusal can only come from the DAL directly.
func TestAdminGuardMessages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		message string
		reason  string
	}{
		{"self target", dal.ErrSelfTarget, guardSelfTargetMessage, guardSelfReason},
		{"last admin", dal.ErrLastAdmin, guardLastAdminMessage, guardLastAdminReason},
		{"env managed", dal.ErrEnvManaged, guardEnvManagedMessage, guardEnvManagedReason},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeAdminUserError(rec, tc.err)
			assertAdminGuard(t, rec, tc.message, tc.reason)
		})
	}

	t.Run("not found", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeAdminUserError(rec, dal.ErrNotFound)
		assertWriteEnvelope(t, rec, http.StatusNotFound, "not_found")
	})
}

// TestAPIAdminSettings covers reading and switching the access mode, including
// the immediate effect on an unrelated account (the cache must be invalidated).
func TestAPIAdminSettings(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	rawAdmin := adminTestRaw(t, now, adminTestAdminID)
	rawStranger := adminTestRaw(t, now, adminTestStranger)

	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, true, "admin")

	if payload := decodeAPI[adminSettingsResponse](t, sendAPI(t, handler, adminSettingsPath, rawAdmin)); payload.AccessMode != model.AccessModeOff {
		t.Fatalf("initial accessMode = %q, want off", payload.AccessMode)
	}

	patchMode := func(t *testing.T, mode string) *httptest.ResponseRecorder {
		t.Helper()
		return writeAPIRequest(t, handler, http.MethodPatch, adminSettingsPath, rawAdmin, `{"accessMode":"`+mode+`"}`)
	}

	if payload := decodeAPI[adminSettingsResponse](t, patchMode(t, model.AccessModeRestricted)); payload.AccessMode != model.AccessModeRestricted {
		t.Fatalf("accessMode = %q, want restricted", payload.AccessMode)
	}

	// The stranger has no row, so restricted mode now rejects them.
	assertWriteEnvelope(t, sendAPI(t, handler, apiStatisticsPath, rawStranger),
		http.StatusForbidden, "forbidden")

	// Switching back to off is effective immediately for that same account.
	if payload := decodeAPI[adminSettingsResponse](t, patchMode(t, model.AccessModeOff)); payload.AccessMode != model.AccessModeOff {
		t.Fatalf("accessMode = %q, want off", payload.AccessMode)
	}
	if rec := sendAPI(t, handler, apiStatisticsPath, rawStranger); rec.Code != http.StatusOK {
		t.Fatalf("stranger after off: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}

	assertWriteEnvelope(t, patchMode(t, "bogus"), http.StatusBadRequest, "bad_request")
	if payload := decodeAPI[adminSettingsResponse](t, sendAPI(t, handler, adminSettingsPath, rawAdmin)); payload.AccessMode != model.AccessModeOff {
		t.Fatalf("accessMode after the rejected patch = %q, want off", payload.AccessMode)
	}
}

// adminTestStatusProvider is the injected status source.
type adminTestStatusProvider struct {
	status AdminStatus
}

func (p adminTestStatusProvider) SystemStatus(context.Context) AdminStatus {
	return p.status
}

// TestAPIAdminStatus covers the status screen with and without a provider, and
// the row counts of every content table.
func TestAPIAdminStatus(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	raw := adminTestRaw(t, now, adminTestAdminID)

	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, true, "admin")
	seedAdminTestUser(t, db, adminTestUserID, model.RoleUser, true, false, "user")

	if err := db.Create(&model.Keyword{UserID: adminTestAdminID, Type: int32(model.PROJECT), Keyword: "tender", CreatedAt: now}).Error; err != nil {
		t.Fatalf("seed keyword: %v", err)
	}
	if err := db.Create(&model.Keyword{UserID: adminTestUserID, Type: int32(model.ALARM), Keyword: "penalty", CreatedAt: now}).Error; err != nil {
		t.Fatalf("seed alarm keyword: %v", err)
	}
	if err := dal.History.Insert([]*model.History{
		{UserID: adminTestAdminID, URL: "https://example.com/a", Title: "a", UpdatedAt: now},
		{UserID: adminTestUserID, URL: "https://example.com/b", Title: "b", UpdatedAt: now},
	}); err != nil {
		t.Fatalf("seed history: %v", err)
	}
	if err := dal.Alarm.Insert([]*model.Alarm{{
		UserID:     adminTestAdminID,
		BusinessID: "BIZ-1",
		CreditName: "Example Corp",
		CreditCode: "CODE-1",
		StartDate:  now,
		PageUrl1:   "https://example.com/1",
		NoticeID:   "N-1",
	}}); err != nil {
		t.Fatalf("seed alarm: %v", err)
	}

	t.Run("without a provider", func(t *testing.T) {
		handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
		payload := decodeAPI[adminStatusResponse](t, sendAPI(t, handler, adminStatusPath, raw))

		if payload.Webhook != nil {
			t.Errorf("webhook = %+v, want null without a provider", payload.Webhook)
		}
		if payload.RecentDenied == nil || len(payload.RecentDenied) != 0 {
			t.Errorf("recentDenied = %v, want an empty array", payload.RecentDenied)
		}
		if payload.Version != constant.Version || payload.BuildTime != constant.BuildTime {
			t.Errorf("version/buildTime = (%q, %q), want the build values", payload.Version, payload.BuildTime)
		}

		want := adminCounts{Users: 2, Admins: 1, Keywords: 2, Histories: 2, Alarms: 1}
		if payload.Counts != want {
			t.Errorf("counts = %+v, want %+v", payload.Counts, want)
		}
	})

	t.Run("with a provider", func(t *testing.T) {
		deniedAt := now.Add(-2 * time.Hour)
		handler := apiTestHandler(APIConfig{
			BotToken: testBotToken,
			Status: adminTestStatusProvider{status: AdminStatus{
				Webhook: &WebhookStatus{URL: "https://bot.example.com/telegram", LastError: "timeout", Pending: 3},
				RecentDenied: []DeniedUser{
					{UserID: adminTestStranger, Username: "stranger", Name: "Stranger", At: deniedAt},
				},
			}},
		}, now)

		payload := decodeAPI[adminStatusResponse](t, sendAPI(t, handler, adminStatusPath, raw))

		if payload.Webhook == nil {
			t.Fatal("webhook = null, want the injected status")
		}
		if payload.Webhook.URL != "https://bot.example.com/telegram" ||
			payload.Webhook.LastError != "timeout" || payload.Webhook.Pending != 3 {
			t.Errorf("webhook = %+v, want the injected values", payload.Webhook)
		}
		if len(payload.RecentDenied) != 1 {
			t.Fatalf("recentDenied = %v, want one entry", payload.RecentDenied)
		}
		denied := payload.RecentDenied[0]
		if denied.UserID != adminTestStranger || denied.Username != "stranger" || denied.Name != "Stranger" {
			t.Errorf("denied = %+v, want the injected identity", denied)
		}
		parsed, err := time.Parse(time.RFC3339, denied.At)
		if err != nil {
			t.Fatalf("at = %q is not RFC3339: %v", denied.At, err)
		}
		if !parsed.Equal(deniedAt) {
			t.Errorf("at = %v, want %v", parsed, deniedAt)
		}
	})
}

// TestAPIAdminSharesWriteBudget proves admin mutations spend the same per-user
// budget as the keyword endpoints instead of opening a second one.
func TestAPIAdminSharesWriteBudget(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := adminTestRaw(t, now, adminTestAdminID)

	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, false, "admin")

	body := `{"accessMode":"off"}`
	for attempt := 1; attempt <= writeRateLimit; attempt++ {
		rec := writeAPIRequest(t, handler, http.MethodPatch, adminSettingsPath, raw, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: status = %d, want 200 (body %q)", attempt, rec.Code, rec.Body.String())
		}
	}

	assertWriteEnvelope(t, writeAPIRequest(t, handler, http.MethodPatch, adminSettingsPath, raw, body),
		http.StatusTooManyRequests, "too_many_requests")

	// The shared limiter is already exhausted for this user, so the content
	// write endpoint refuses as well.
	assertWriteEnvelope(t,
		writeAPIRequest(t, handler, http.MethodPost, apiKeywordsPath, raw, `{"keywords":"limit-check"}`),
		http.StatusTooManyRequests, "too_many_requests")
}

// TestAPIAdminUserKeywordsReadOnly covers the administrator's read-only view of
// another account's keywords: the identity block, the active-only counts, the
// archived toggle, the validation answers and the admin gate.
func TestAPIAdminUserKeywordsReadOnly(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, false, "boss")
	seedAdminTestUser(t, db, adminTestUserID, model.RoleUser, true, false, "张工")
	seedAdminTestUser(t, db, adminTestStranger, model.RoleUser, true, false, "stranger")
	if err := db.Model(&model.User{}).Where("user_id = ?", adminTestUserID).
		Updates(map[string]any{"name": "张工", "username": "zhanggong"}).Error; err != nil {
		t.Fatalf("seed identity: %v", err)
	}

	archivedAt := now.Add(-time.Hour)
	seedKeywords(t,
		&model.Keyword{ID: new(int32(11)), UserID: adminTestUserID, Type: int32(model.PROJECT), Keyword: "active", Counter: 2, CreatedAt: now},
		&model.Keyword{ID: new(int32(12)), UserID: adminTestUserID, Type: int32(model.PROJECT), Keyword: "archived", Counter: 7, CreatedAt: now, ArchivedAt: &archivedAt},
		&model.Keyword{ID: new(int32(13)), UserID: adminTestUserID, Type: int32(model.ALARM), Keyword: "alarm-active", CreatedAt: now},
	)

	rawAdmin := adminTestRaw(t, now, adminTestAdminID)
	rawUser := adminTestRaw(t, now, adminTestUserID)
	path := adminUsersPath + "/" + strconv.FormatInt(adminTestUserID, 10) + "/keywords"

	t.Run("active project list and identity block", func(t *testing.T) {
		payload := decodeAPI[adminUserKeywordsResponse](t, sendAPI(t, handler, path+"?type=project", rawAdmin))

		if payload.User.UserID != adminTestUserID || payload.User.Name != "张工" || payload.User.Username != "zhanggong" {
			t.Fatalf("user block = %+v, want the persisted identity", payload.User)
		}
		if payload.User.KeywordCounts != (adminKeywordCounts{Project: 1, Alarm: 1}) {
			t.Fatalf("keywordCounts = %+v, want active-only {1 1}", payload.User.KeywordCounts)
		}
		if payload.User.LastSeen != nil {
			t.Fatalf("lastSeen = %v, want null for a never-seen account", *payload.User.LastSeen)
		}
		if len(payload.Items) != 1 || payload.Items[0].ID != 11 || payload.Items[0].ArchivedAt != nil {
			t.Fatalf("items = %+v, want only the active project keyword", payload.Items)
		}
		if payload.ArchivedCount != 1 {
			t.Fatalf("archivedCount = %d, want 1", payload.ArchivedCount)
		}
	})

	t.Run("includeArchived adds the archived row", func(t *testing.T) {
		payload := decodeAPI[adminUserKeywordsResponse](t, sendAPI(t, handler, path+"?type=project&includeArchived=1", rawAdmin))
		if len(payload.Items) != 2 || payload.ArchivedCount != 1 {
			t.Fatalf("payload = (%d items, count %d), want (2, 1)", len(payload.Items), payload.ArchivedCount)
		}
		if payload.Items[0].ID != 12 || payload.Items[0].ArchivedAt == nil {
			t.Fatalf("first item = %+v, want the archived leaderboard row", payload.Items[0])
		}
	})

	t.Run("alarm type is supported", func(t *testing.T) {
		payload := decodeAPI[adminUserKeywordsResponse](t, sendAPI(t, handler, path+"?type=alarm", rawAdmin))
		if len(payload.Items) != 1 || payload.Items[0].Keyword != "alarm-active" {
			t.Fatalf("items = %+v, want the active alarm keyword", payload.Items)
		}
	})

	t.Run("unknown or missing type is a bad request", func(t *testing.T) {
		for _, target := range []string{path, path + "?type=weird", path + "?type="} {
			assertWriteEnvelope(t, sendAPI(t, handler, target, rawAdmin), http.StatusBadRequest, "bad_request")
		}
	})

	t.Run("an unknown account is not found", func(t *testing.T) {
		assertWriteEnvelope(t, sendAPI(t, handler, adminUsersPath+"/999999/keywords?type=project", rawAdmin),
			http.StatusNotFound, "not_found")
	})

	t.Run("a non-admin is forbidden", func(t *testing.T) {
		assertWriteEnvelope(t, sendAPI(t, handler, path+"?type=project", rawUser),
			http.StatusForbidden, "forbidden_admin")
	})

	t.Run("an anonymous caller is unauthorized", func(t *testing.T) {
		assertWriteEnvelope(t, sendAPI(t, handler, path+"?type=project", ""),
			http.StatusUnauthorized, "unauthorized")
	})
}

// TestAPIAdminListUsersCountsAndRecentSort covers the member-picker fields: the
// active keyword counts on every item and the optional recent ordering, while
// the default order stays account id ascending.
func TestAPIAdminListUsersCountsAndRecentSort(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := adminTestRaw(t, now, adminTestAdminID)

	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, false, "boss")
	seedAdminTestUser(t, db, adminTestUserID, model.RoleUser, true, false, "older")
	seedAdminTestUser(t, db, adminTestOtherID, model.RoleUser, true, false, "newer")
	older := now.Add(-2 * time.Hour)
	newer := now.Add(-time.Hour)
	if err := dal.User.SetLastSeen(adminTestUserID, older); err != nil {
		t.Fatalf("SetLastSeen(older) error: %v", err)
	}
	if err := dal.User.SetLastSeen(adminTestOtherID, newer); err != nil {
		t.Fatalf("SetLastSeen(newer) error: %v", err)
	}

	archivedAt := now.Add(-time.Hour)
	seedKeywords(t,
		&model.Keyword{ID: new(int32(21)), UserID: adminTestUserID, Type: int32(model.PROJECT), Keyword: "p1", CreatedAt: now},
		&model.Keyword{ID: new(int32(22)), UserID: adminTestUserID, Type: int32(model.PROJECT), Keyword: "p2", CreatedAt: now},
		&model.Keyword{ID: new(int32(23)), UserID: adminTestUserID, Type: int32(model.PROJECT), Keyword: "p-archived", CreatedAt: now, ArchivedAt: &archivedAt},
		&model.Keyword{ID: new(int32(24)), UserID: adminTestUserID, Type: int32(model.ALARM), Keyword: "a1", CreatedAt: now},
		&model.Keyword{ID: new(int32(25)), UserID: adminTestOtherID, Type: int32(model.PROJECT), Keyword: "other", CreatedAt: now},
	)

	t.Run("default order keeps keyword counts", func(t *testing.T) {
		payload := decodeAPI[adminUsersResponse](t, sendAPI(t, handler, adminUsersPath, raw))
		if payload.Total != 3 || len(payload.Items) != 3 {
			t.Fatalf("payload = (%d items, %d total), want 3", len(payload.Items), payload.Total)
		}
		for i, want := range []int64{adminTestAdminID, adminTestUserID, adminTestOtherID} {
			if payload.Items[i].UserID != want {
				t.Fatalf("items[%d] = %d, want the default id order %d", i, payload.Items[i].UserID, want)
			}
		}

		byID := make(map[int64]adminUserItem, len(payload.Items))
		for _, item := range payload.Items {
			byID[item.UserID] = item
		}
		if got := byID[adminTestUserID].KeywordCounts; got != (adminKeywordCounts{Project: 2, Alarm: 1}) {
			t.Fatalf("colleague counts = %+v, want active-only {2 1}", got)
		}
		if got := byID[adminTestOtherID].KeywordCounts; got != (adminKeywordCounts{Project: 1}) {
			t.Fatalf("other counts = %+v, want {1 0}", got)
		}
		if got := byID[adminTestAdminID].KeywordCounts; got != (adminKeywordCounts{}) {
			t.Fatalf("admin counts = %+v, want zero", got)
		}
	})

	t.Run("sort=recent orders by activity and puts never-seen last", func(t *testing.T) {
		payload := decodeAPI[adminUsersResponse](t, sendAPI(t, handler, adminUsersPath+"?sort=recent", raw))
		for i, want := range []int64{adminTestOtherID, adminTestUserID, adminTestAdminID} {
			if payload.Items[i].UserID != want {
				t.Fatalf("items[%d] = %d, want %d (recent first, null last)", i, payload.Items[i].UserID, want)
			}
		}
	})
}

// TestAPIAdminMeExposesTelegramIdentity covers the identity fields of /api/v1/me:
// the stored name/username are returned for a known account and stay empty for
// an account without a row.
func TestAPIAdminMeExposesTelegramIdentity(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, false, "boss")

	if err := dal.User.Touch(adminTestAdminID, now, "刘工", "liugong"); err != nil {
		t.Fatalf("Touch() error: %v", err)
	}

	raw := adminTestRawIdentity(t, now, adminTestAdminID, "刘工", "", "liugong")
	payload := decodeAPI[meResponse](t, sendAPI(t, handler, apiMePath, raw))
	if payload.Name != "刘工" || payload.Username != "liugong" || payload.Remark != "boss" {
		t.Fatalf("me = %+v, want the stored identity", payload)
	}

	stranger := decodeAPI[meResponse](t, sendAPI(t, handler, apiMePath, adminTestRaw(t, now, adminTestStranger)))
	if stranger.Name != "" || stranger.Username != "" {
		t.Fatalf("stranger me = %+v, want empty identity fields", stranger)
	}
}

// TestAPIAdminMePersistsInitDataIdentity covers the Mini-App-only path: a
// signed /api/v1/me request fills an empty identity on the caller's row and
// refreshes last_seen without any bot update. A second request with a changed
// name keeps both the identity and last_seen current.
func TestAPIAdminMePersistsInitDataIdentity(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, false, "")
	oldSeen := now.Add(-2 * time.Hour)
	if err := dal.User.SetLastSeen(adminTestAdminID, oldSeen); err != nil {
		t.Fatalf("SetLastSeen() error: %v", err)
	}

	first := adminTestRawIdentity(t, now, adminTestAdminID, "王小明", "", "wangxm")
	payload := decodeAPI[meResponse](t, sendAPI(t, handler, apiMePath, first))
	if payload.Name != "王小明" || payload.Username != "wangxm" {
		t.Fatalf("me = %+v, want the signed identity", payload)
	}

	row, ok := dal.User.Get(adminTestAdminID)
	if !ok {
		t.Fatal("user row disappeared")
	}
	if row.Name != "王小明" || row.Username != "wangxm" {
		t.Fatalf("stored identity = (%q, %q), want the signed identity", row.Name, row.Username)
	}
	if row.LastSeen == nil || !row.LastSeen.After(oldSeen) {
		t.Fatalf("last_seen = %v, want it refreshed past %v", row.LastSeen, oldSeen)
	}
	firstSeen := *row.LastSeen

	// A changed name must be written again, and last_seen must keep moving.
	second := adminTestRawIdentity(t, now, adminTestAdminID, "王大明", "", "wangdm")
	payload = decodeAPI[meResponse](t, sendAPI(t, handler, apiMePath, second))
	if payload.Name != "王大明" || payload.Username != "wangdm" {
		t.Fatalf("me = %+v, want the changed identity", payload)
	}

	row, ok = dal.User.Get(adminTestAdminID)
	if !ok {
		t.Fatal("user row disappeared")
	}
	if row.Name != "王大明" || row.Username != "wangdm" {
		t.Fatalf("stored identity = (%q, %q), want the changed identity", row.Name, row.Username)
	}
	if row.LastSeen == nil || row.LastSeen.Before(firstSeen) {
		t.Fatalf("last_seen = %v, want it not to move backwards from %v", row.LastSeen, firstSeen)
	}
}

// TestAPIAdminListUsersEnabledFilter covers the member-picker filter: enabled
// applies to both the page and the total, composes with sort=recent, keeps the
// unfiltered default, and rejects a non-boolean value.
func TestAPIAdminListUsersEnabledFilter(t *testing.T) {
	db := setupAdminAPITestDB(t)

	now := time.Now().Truncate(time.Second)
	handler := apiTestHandler(APIConfig{BotToken: testBotToken}, now)
	raw := adminTestRaw(t, now, adminTestAdminID)

	seedAdminTestUser(t, db, adminTestAdminID, model.RoleAdmin, true, false, "boss")
	seedAdminTestUser(t, db, adminTestUserID, model.RoleUser, true, false, "active user")
	seedAdminTestUser(t, db, adminTestStranger, model.RoleUser, false, false, "disabled stranger")
	seedAdminTestUser(t, db, adminTestOtherID, model.RoleUser, false, false, "disabled other")

	older := now.Add(-2 * time.Hour)
	newer := now.Add(-time.Hour)
	if err := dal.User.SetLastSeen(adminTestUserID, older); err != nil {
		t.Fatalf("SetLastSeen(active) error: %v", err)
	}
	if err := dal.User.SetLastSeen(adminTestOtherID, newer); err != nil {
		t.Fatalf("SetLastSeen(disabled) error: %v", err)
	}

	t.Run("disabled only", func(t *testing.T) {
		payload := decodeAPI[adminUsersResponse](t, sendAPI(t, handler, adminUsersPath+"?enabled=false", raw))
		if payload.Total != 2 || len(payload.Items) != 2 {
			t.Fatalf("payload = (%d items, %d total), want the two disabled rows", len(payload.Items), payload.Total)
		}
		for i, want := range []int64{adminTestStranger, adminTestOtherID} {
			if payload.Items[i].UserID != want || payload.Items[i].Enabled {
				t.Fatalf("items[%d] = %+v, want the disabled row %d", i, payload.Items[i], want)
			}
		}
	})

	t.Run("enabled only", func(t *testing.T) {
		payload := decodeAPI[adminUsersResponse](t, sendAPI(t, handler, adminUsersPath+"?enabled=true", raw))
		if payload.Total != 2 || len(payload.Items) != 2 {
			t.Fatalf("payload = (%d items, %d total), want the two enabled rows", len(payload.Items), payload.Total)
		}
		for i, want := range []int64{adminTestAdminID, adminTestUserID} {
			if payload.Items[i].UserID != want {
				t.Fatalf("items[%d] = %d, want %d", i, payload.Items[i].UserID, want)
			}
		}
	})

	t.Run("filter composes with the recent order and paging", func(t *testing.T) {
		payload := decodeAPI[adminUsersResponse](t, sendAPI(t, handler, adminUsersPath+"?enabled=false&sort=recent&limit=1", raw))
		if payload.Total != 2 || len(payload.Items) != 1 || payload.Items[0].UserID != adminTestOtherID {
			t.Fatalf("payload = %+v, want the recently active disabled member and total 2", payload)
		}
		if payload.NextOffset == nil || *payload.NextOffset != 1 {
			t.Fatalf("nextOffset = %v, want 1", payload.NextOffset)
		}
	})

	t.Run("no filter keeps every account", func(t *testing.T) {
		payload := decodeAPI[adminUsersResponse](t, sendAPI(t, handler, adminUsersPath, raw))
		if payload.Total != 4 || len(payload.Items) != 4 {
			t.Fatalf("payload = (%d items, %d total), want all 4", len(payload.Items), payload.Total)
		}
	})

	t.Run("a non-boolean value is a bad request", func(t *testing.T) {
		assertWriteEnvelope(t, sendAPI(t, handler, adminUsersPath+"?enabled=maybe", raw),
			http.StatusBadRequest, "bad_request")
	})
}
