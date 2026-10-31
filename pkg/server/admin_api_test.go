package server

import (
	"context"
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

// assertAdminGuard asserts the 409 guard envelope with its fixed message.
func assertAdminGuard(t *testing.T, rec *httptest.ResponseRecorder, message string) {
	t.Helper()

	want := `{"error":{"code":"guard","message":"` + message + `"}}`
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if got := rec.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
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

	for _, target := range []string{apiStatisticsPath, apiMePath, "/api/unknown-endpoint"} {
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
// access-control migration: /api/me must report the mode the bootstrap would
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
		assertAdminGuard(t, post(t, `{"userIds":"10001","role":"user"}`), guardEnvManagedMessage)
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
		assertAdminGuard(t, patch(t, adminUsersPath+"/10001", `{"enabled":false}`), guardSelfTargetMessage)
		assertAdminGuard(t, del(t, adminUsersPath+"/10001"), guardSelfTargetMessage)

		// The environment owns the role/access of its rows, but not the remark.
		assertAdminGuard(t, patch(t, adminUsersPath+"/30004", `{"role":"user"}`), guardEnvManagedMessage)
		assertAdminGuard(t, del(t, adminUsersPath+"/30004"), guardEnvManagedMessage)
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
	}{
		{"self target", dal.ErrSelfTarget, guardSelfTargetMessage},
		{"last admin", dal.ErrLastAdmin, guardLastAdminMessage},
		{"env managed", dal.ErrEnvManaged, guardEnvManagedMessage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeAdminUserError(rec, tc.err)
			assertAdminGuard(t, rec, tc.message)
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
