package dal

import (
	"bytes"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/gythialy/magnet/pkg/model"
)

// setupAccessTestDB opens a private database holding the access-control tables
// and points the package DAOs (including User) at it.
//
// dal.SetDefault mutates global handles, so the previous binding is restored on
// cleanup. User.Invalidate runs on both ends: the cache is keyed by database, but
// clearing it makes a cross-test leak impossible rather than merely unlikely.
func setupAccessTestDB(t *testing.T) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "access.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Setting{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	previous := *Q
	SetDefault(db)
	User.Invalidate()
	t.Cleanup(func() {
		*Q = previous
		Alarm = &Q.Alarm
		History = &Q.History
		Keyword = &Q.Keyword
		User.Invalidate()
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

// accessTestClock is the injected clock behind the access cache.
type accessTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *accessTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *accessTestClock) Advance(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(delta)
}

// useAccessTestClock replaces the package clock for one test, so cache expiry can
// be observed without sleeping for userCacheTTL.
func useAccessTestClock(t *testing.T) *accessTestClock {
	t.Helper()

	clock := &accessTestClock{now: time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)}
	previous := accessClock
	accessClock = clock.Now
	t.Cleanup(func() {
		accessClock = previous
		User.Invalidate()
	})
	return clock
}

// seedAccessUser inserts one row directly, which is how a manually managed
// account (from_env=false) is created in tests.
func seedAccessUser(t *testing.T, userID int64, role int32, enabled, fromEnv bool, remark string) {
	t.Helper()

	now := time.Now()
	row := &model.User{
		UserID:    userID,
		Remark:    remark,
		Role:      role,
		Enabled:   enabled,
		FromEnv:   fromEnv,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := Q.db.Create(row).Error; err != nil {
		t.Fatalf("seed user %d: %v", userID, err)
	}
	User.Invalidate()
}

// mustUser returns one row and fails the test when it is missing.
func mustUser(t *testing.T, userID int64) *model.User {
	t.Helper()

	row, ok := User.Get(userID)
	if !ok {
		t.Fatalf("user %d not found", userID)
	}
	return row
}

// captureAccessLogs redirects slog for one test so a warning can be asserted.
func captureAccessLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	previous := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return &logs
}

// TestEnsureBootstrapEmptyDatabase pins the fail-open fresh start: no
// administrators means access mode off, and nobody is blocked before MANAGER_ID
// is configured.
func TestEnsureBootstrapEmptyDatabase(t *testing.T) {
	setupAccessTestDB(t)

	mode, err := EnsureBootstrap(nil, nil)
	if err != nil {
		t.Fatalf("EnsureBootstrap() error: %v", err)
	}
	if mode != model.AccessModeOff {
		t.Fatalf("mode = %q, want %q", mode, model.AccessModeOff)
	}
	if got := User.CountActiveAdmins(); got != 0 {
		t.Fatalf("active admins = %d, want 0", got)
	}
	if !User.IsAllowed(123456789) {
		t.Fatal("an empty database in off mode must allow anyone")
	}

	value, ok, err := GetSetting(model.SettingAccessMode)
	if err != nil || !ok || value != model.AccessModeOff {
		t.Fatalf("stored access mode = (%q, %v, %v), want (%q, true, nil)",
			value, ok, err, model.AccessModeOff)
	}
}

// TestEnsureBootstrapSeedsAdministrators covers the first start with MANAGER_ID:
// restricted mode, an enabled administrator row and a stranger denied.
func TestEnsureBootstrapSeedsAdministrators(t *testing.T) {
	setupAccessTestDB(t)

	mode, err := EnsureBootstrap([]int64{10001}, nil)
	if err != nil {
		t.Fatalf("EnsureBootstrap() error: %v", err)
	}
	if mode != model.AccessModeRestricted {
		t.Fatalf("mode = %q, want %q", mode, model.AccessModeRestricted)
	}

	admin := mustUser(t, 10001)
	if admin.Role != model.RoleAdmin {
		t.Errorf("role = %d, want admin", admin.Role)
	}
	if !admin.Enabled {
		t.Error("seeded administrator is disabled")
	}
	if !admin.FromEnv {
		t.Error("from_env = false, want the row to be owned by MANAGER_ID")
	}

	if !User.IsAllowed(10001) {
		t.Error("IsAllowed(admin) = false, want true")
	}
	if !User.IsAdmin(10001) {
		t.Error("IsAdmin(admin) = false, want true")
	}
	if User.IsAllowed(20002) {
		t.Error("IsAllowed(stranger) = true in restricted mode, want false")
	}
	if User.IsAdmin(20002) {
		t.Error("IsAdmin(stranger) = true, want false")
	}

	// A second bootstrap must be idempotent: no duplicate row, same mode.
	if mode, err := EnsureBootstrap([]int64{10001}, nil); err != nil || mode != model.AccessModeRestricted {
		t.Fatalf("second EnsureBootstrap() = (%q, %v), want (%q, nil)", mode, err, model.AccessModeRestricted)
	}
	var count int64
	if err := Q.db.Model(&model.User{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("user rows = %d, want 1 after an idempotent bootstrap", count)
	}
}

// TestEnsureBootstrapKeepsExplicitMode proves an operator's choice survives a
// restart: the bootstrap only writes the mode when none is stored.
func TestEnsureBootstrapKeepsExplicitMode(t *testing.T) {
	setupAccessTestDB(t)

	if _, err := EnsureBootstrap([]int64{10001}, nil); err != nil {
		t.Fatalf("first EnsureBootstrap() error: %v", err)
	}
	if err := SetSetting(model.SettingAccessMode, model.AccessModeOff); err != nil {
		t.Fatalf("SetSetting() error: %v", err)
	}

	mode, err := EnsureBootstrap([]int64{10001}, nil)
	if err != nil {
		t.Fatalf("second EnsureBootstrap() error: %v", err)
	}
	if mode != model.AccessModeOff {
		t.Fatalf("mode = %q, want the explicitly stored %q", mode, model.AccessModeOff)
	}
	if !User.IsAllowed(20002) {
		t.Fatal("off mode must allow a stranger after the restart")
	}
}

// TestEnsureBootstrapRefillsEmptyMode: an empty value is not a mode, so the
// bootstrap derives and writes one instead of returning "".
func TestEnsureBootstrapRefillsEmptyMode(t *testing.T) {
	setupAccessTestDB(t)

	if err := SetSetting(model.SettingAccessMode, ""); err != nil {
		t.Fatalf("SetSetting() error: %v", err)
	}

	mode, err := EnsureBootstrap([]int64{10001}, nil)
	if err != nil {
		t.Fatalf("EnsureBootstrap() error: %v", err)
	}
	if mode != model.AccessModeRestricted {
		t.Fatalf("mode = %q, want %q for a database with an administrator", mode, model.AccessModeRestricted)
	}
}

// TestEnsureBootstrapSeedsAllowedUsers proves ALLOWED_USER_IDS seeds regular
// users that are allowed in restricted mode but are not administrators.
func TestEnsureBootstrapSeedsAllowedUsers(t *testing.T) {
	setupAccessTestDB(t)

	mode, err := EnsureBootstrap([]int64{10001}, []int64{20002, 20003})
	if err != nil {
		t.Fatalf("EnsureBootstrap() error: %v", err)
	}
	if mode != model.AccessModeRestricted {
		t.Fatalf("mode = %q, want restricted", mode)
	}

	for _, userID := range []int64{20002, 20003} {
		row := mustUser(t, userID)
		if row.Role != model.RoleUser {
			t.Errorf("user %d role = %d, want user", userID, row.Role)
		}
		if !row.Enabled || !row.FromEnv {
			t.Errorf("user %d = (enabled=%v, from_env=%v), want both true", userID, row.Enabled, row.FromEnv)
		}
		if !User.IsAllowed(userID) {
			t.Errorf("IsAllowed(%d) = false, want true", userID)
		}
		if User.IsAdmin(userID) {
			t.Errorf("IsAdmin(%d) = true, want false", userID)
		}
	}

	if User.IsAllowed(20004) {
		t.Fatal("an unlisted account must be denied in restricted mode")
	}
}

// TestEnsureBootstrapAllowedOnlyIsOpen: without any administrator the bot stays
// in off mode even when regular users are seeded.
func TestEnsureBootstrapAllowedOnlyIsOpen(t *testing.T) {
	setupAccessTestDB(t)

	mode, err := EnsureBootstrap(nil, []int64{20002})
	if err != nil {
		t.Fatalf("EnsureBootstrap() error: %v", err)
	}
	if mode != model.AccessModeOff {
		t.Fatalf("mode = %q, want off when there is no administrator", mode)
	}
	if !User.IsAllowed(99999) {
		t.Fatal("off mode must allow anyone, including an unseeded account")
	}
	if row := mustUser(t, 20002); !row.FromEnv || row.Role != model.RoleUser {
		t.Fatalf("seeded allowed user = %+v, want a from_env regular user", row)
	}
}

// TestSyncFromEnvCountsAndUnmanages covers the counters and the un-managing of
// rows that disappeared from the environment.
func TestSyncFromEnvCountsAndUnmanages(t *testing.T) {
	setupAccessTestDB(t)

	added, removed, err := User.SyncFromEnv([]int64{10001}, []int64{20002})
	if err != nil {
		t.Fatalf("SyncFromEnv() error: %v", err)
	}
	if added != 2 || removed != 0 {
		t.Fatalf("first sync = (%d, %d), want (2, 0)", added, removed)
	}

	// Re-running the same lists changes nothing.
	added, removed, err = User.SyncFromEnv([]int64{10001}, []int64{20002})
	if err != nil {
		t.Fatalf("second SyncFromEnv() error: %v", err)
	}
	if added != 0 || removed != 0 {
		t.Fatalf("second sync = (%d, %d), want (0, 0)", added, removed)
	}

	// Dropping the allowed user stops managing it but does not delete it.
	added, removed, err = User.SyncFromEnv([]int64{10001}, nil)
	if err != nil {
		t.Fatalf("third SyncFromEnv() error: %v", err)
	}
	if added != 0 || removed != 1 {
		t.Fatalf("third sync = (%d, %d), want (0, 1)", added, removed)
	}
	row := mustUser(t, 20002)
	if row.FromEnv {
		t.Error("from_env = true after the account left ALLOWED_USER_IDS")
	}
	if row.Role != model.RoleUser || !row.Enabled {
		t.Errorf("row = (role=%d, enabled=%v), want the account kept as a regular enabled user", row.Role, row.Enabled)
	}
}

// TestSyncFromEnvDemotesRemovedAdministrator proves a removed administrator is
// demoted once another enabled administrator remains.
func TestSyncFromEnvDemotesRemovedAdministrator(t *testing.T) {
	setupAccessTestDB(t)

	if _, _, err := User.SyncFromEnv([]int64{10001, 10002}, nil); err != nil {
		t.Fatalf("SyncFromEnv() error: %v", err)
	}

	_, removed, err := User.SyncFromEnv([]int64{10001}, nil)
	if err != nil {
		t.Fatalf("SyncFromEnv() error: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	demoted := mustUser(t, 10002)
	if demoted.Role != model.RoleUser {
		t.Errorf("removed administrator role = %d, want user", demoted.Role)
	}
	if demoted.FromEnv {
		t.Error("removed administrator still has from_env=true")
	}

	kept := mustUser(t, 10001)
	if kept.Role != model.RoleAdmin || !kept.FromEnv {
		t.Errorf("remaining administrator = (role=%d, from_env=%v), want admin and managed", kept.Role, kept.FromEnv)
	}
}

// TestSyncFromEnvKeepsLastAdministrator is the anti-lockout rule: removing the
// only administrator from the environment leaves the role in place.
func TestSyncFromEnvKeepsLastAdministrator(t *testing.T) {
	setupAccessTestDB(t)
	logs := captureAccessLogs(t)

	if _, _, err := User.SyncFromEnv([]int64{10001}, nil); err != nil {
		t.Fatalf("SyncFromEnv() error: %v", err)
	}

	_, removed, err := User.SyncFromEnv(nil, nil)
	if err != nil {
		t.Fatalf("SyncFromEnv() error: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	last := mustUser(t, 10001)
	if last.Role != model.RoleAdmin {
		t.Errorf("role = %d, want the last administrator to keep admin", last.Role)
	}
	if last.FromEnv {
		t.Error("from_env = true, want the row to be un-managed")
	}
	if got := User.CountActiveAdmins(); got != 1 {
		t.Fatalf("active admins = %d, want the last one preserved", got)
	}
	if !bytes.Contains(logs.Bytes(), []byte("kept administrator role")) {
		t.Fatalf("logs = %q, want an anti-lockout warning", logs.String())
	}
}

// TestSyncFromEnvDoesNotDemoteAdministratorListedAsUser: putting an existing
// administrator into ALLOWED_USER_IDS must not silently demote them.
func TestSyncFromEnvDoesNotDemoteAdministratorListedAsUser(t *testing.T) {
	setupAccessTestDB(t)

	if _, _, err := User.SyncFromEnv([]int64{10001}, nil); err != nil {
		t.Fatalf("SyncFromEnv() error: %v", err)
	}
	if _, _, err := User.SyncFromEnv(nil, []int64{10001}); err != nil {
		t.Fatalf("SyncFromEnv() error: %v", err)
	}

	row := mustUser(t, 10001)
	if row.Role != model.RoleAdmin {
		t.Errorf("role = %d, want the administrator to keep the role", row.Role)
	}
	if !row.FromEnv {
		t.Error("from_env = false, want the account to stay managed")
	}
}

// TestUpsertFromEnv covers the single-account upsert: create, idempotency,
// promotion without demotion, and re-enabling a disabled account that becomes an
// administrator.
func TestUpsertFromEnv(t *testing.T) {
	setupAccessTestDB(t)

	if err := User.UpsertFromEnv(10001, model.RoleAdmin); err != nil {
		t.Fatalf("UpsertFromEnv() error: %v", err)
	}
	row := mustUser(t, 10001)
	if row.Role != model.RoleAdmin || !row.Enabled || !row.FromEnv {
		t.Fatalf("created row = (role=%d, enabled=%v, from_env=%v), want admin/enabled/managed",
			row.Role, row.Enabled, row.FromEnv)
	}

	// A second call with the same role changes nothing.
	if err := User.UpsertFromEnv(10001, model.RoleAdmin); err != nil {
		t.Fatalf("second UpsertFromEnv() error: %v", err)
	}
	// The regular-user role must not demote an administrator.
	if err := User.UpsertFromEnv(10001, model.RoleUser); err != nil {
		t.Fatalf("UpsertFromEnv(user) error: %v", err)
	}
	if got := mustUser(t, 10001); got.Role != model.RoleAdmin {
		t.Fatalf("role = %d, want the administrator not to be demoted by the allowed list", got.Role)
	}

	// A disabled regular user promoted to administrator becomes usable again.
	seedAccessUser(t, 20002, model.RoleUser, false, false, "")
	if err := User.UpsertFromEnv(20002, model.RoleAdmin); err != nil {
		t.Fatalf("UpsertFromEnv() error: %v", err)
	}
	promoted := mustUser(t, 20002)
	if promoted.Role != model.RoleAdmin || !promoted.Enabled || !promoted.FromEnv {
		t.Fatalf("promoted row = (role=%d, enabled=%v, from_env=%v), want admin/enabled/managed",
			promoted.Role, promoted.Enabled, promoted.FromEnv)
	}

	if err := User.UpsertFromEnv(-1, model.RoleAdmin); err == nil {
		t.Error("UpsertFromEnv with a negative id returned no error")
	}
	if err := User.UpsertFromEnv(1, 7); err == nil {
		t.Error("UpsertFromEnv with an invalid role returned no error")
	}
}

// TestUpdateGuardOrder walks the guard sequence one rule at a time: not found,
// self-target, environment-managed, and the remark exception.
func TestUpdateGuardOrder(t *testing.T) {
	setupAccessTestDB(t)

	if _, err := EnsureBootstrap([]int64{10001}, []int64{20002}); err != nil {
		t.Fatalf("EnsureBootstrap() error: %v", err)
	}
	seedAccessUser(t, 30003, model.RoleAdmin, true, false, "manual admin")

	t.Run("missing target", func(t *testing.T) {
		if _, err := User.Update(30003, 99999, UserPatch{Remark: new("x")}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update(missing) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("self-target", func(t *testing.T) {
		cases := []UserPatch{
			{Enabled: new(false)},
			{Role: new(model.RoleUser)},
		}
		for _, patch := range cases {
			if _, err := User.Update(10001, 10001, patch); !errors.Is(err, ErrSelfTarget) {
				t.Fatalf("Update(self, %+v) error = %v, want ErrSelfTarget", patch, err)
			}
		}

		// Renaming yourself is not restricting yourself.
		if _, err := User.Update(10001, 10001, UserPatch{Remark: new("primary")}); err != nil {
			t.Fatalf("Update(self, remark) error = %v, want nil", err)
		}
	})

	t.Run("environment-managed", func(t *testing.T) {
		for _, patch := range []UserPatch{
			{Enabled: new(false)},
			{Role: new(model.RoleUser)},
		} {
			if _, err := User.Update(30003, 10001, patch); !errors.Is(err, ErrEnvManaged) {
				t.Fatalf("Update(env-managed, %+v) error = %v, want ErrEnvManaged", patch, err)
			}
		}

		// Repeating the value the environment already stored is not a change, so
		// the environment rule does not fire.
		if _, err := User.Update(30003, 10001, UserPatch{Role: new(model.RoleAdmin)}); err != nil {
			t.Fatalf("Update(env-managed, unchanged role) error = %v, want nil", err)
		}

		// The remark is the one field an administrator may still edit.
		if _, err := User.Update(30003, 10001, UserPatch{Remark: new("boss")}); err != nil {
			t.Fatalf("Update(env-managed, remark) error = %v, want nil", err)
		}
		if got := mustUser(t, 10001); got.Remark != "boss" {
			t.Fatalf("remark = %q, want boss", got.Remark)
		}

		// The regular user seeded from ALLOWED_USER_IDS is managed too.
		if _, err := User.Update(30003, 20002, UserPatch{Enabled: new(false)}); !errors.Is(err, ErrEnvManaged) {
			t.Fatalf("Update(env-managed regular user) error = %v, want ErrEnvManaged", err)
		}
	})

	t.Run("invalid role", func(t *testing.T) {
		if _, err := User.Update(30003, 30003, UserPatch{Role: new(int32(7))}); err == nil {
			t.Fatal("Update with an invalid role returned no error")
		}
	})
}

// TestUpdateAndDeleteApplyPatch covers the successful edit and the no-op case.
func TestUpdateAndDeleteApplyPatch(t *testing.T) {
	setupAccessTestDB(t)

	seedAccessUser(t, 30003, model.RoleAdmin, true, false, "old remark")
	seedAccessUser(t, 40004, model.RoleUser, true, false, "")

	updated, err := User.Update(30003, 40004, UserPatch{
		Remark:  new("renamed"),
		Role:    new(model.RoleAdmin),
		Enabled: new(false),
	})
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if updated.Remark != "renamed" || updated.Role != model.RoleAdmin || updated.Enabled {
		t.Fatalf("updated row = %+v, want renamed/admin/disabled", updated)
	}

	// Repeating the same values is a no-op and still succeeds.
	same, err := User.Update(30003, 40004, UserPatch{Remark: new("renamed")})
	if err != nil {
		t.Fatalf("no-op Update() error: %v", err)
	}
	if same.Remark != "renamed" {
		t.Fatalf("remark = %q, want renamed", same.Remark)
	}

	// Deleting a non-administrator is allowed.
	if err := User.Delete(30003, 40004); err != nil {
		t.Fatalf("Delete(non-admin) error: %v", err)
	}
	if _, ok := User.Get(40004); ok {
		t.Fatal("deleted row is still readable")
	}
	if err := User.Delete(30003, 40004); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete() error = %v, want ErrNotFound", err)
	}
}

// TestDestructiveGuardsProtectLastAdministrator covers demotion, disabling and
// deletion of the final enabled administrator, and the same operations once a
// second administrator exists.
func TestDestructiveGuardsProtectLastAdministrator(t *testing.T) {
	setupAccessTestDB(t)

	seedAccessUser(t, 30003, model.RoleAdmin, true, false, "only admin")
	seedAccessUser(t, 40004, model.RoleUser, true, false, "")

	if _, err := User.Update(40004, 30003, UserPatch{Enabled: new(false)}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("disable last admin error = %v, want ErrLastAdmin", err)
	}
	if _, err := User.Update(40004, 30003, UserPatch{Role: new(model.RoleUser)}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demote last admin error = %v, want ErrLastAdmin", err)
	}
	if err := User.Delete(40004, 30003); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("delete last admin error = %v, want ErrLastAdmin", err)
	}

	// A second enabled administrator makes the operations safe.
	seedAccessUser(t, 30005, model.RoleAdmin, true, false, "second admin")
	if _, err := User.Update(40004, 30003, UserPatch{Role: new(model.RoleUser)}); err != nil {
		t.Fatalf("demote with two admins error = %v, want nil", err)
	}
	if got := mustUser(t, 30003); got.Role != model.RoleUser {
		t.Fatalf("role = %d, want the demotion to stick", got.Role)
	}
	if err := User.Delete(40004, 30003); err != nil {
		t.Fatalf("delete demoted user error = %v, want nil", err)
	}
}

// TestDeleteGuards covers the self-target and environment-managed delete rules,
// and that the environment rule wins over the last-administrator rule.
func TestDeleteGuards(t *testing.T) {
	setupAccessTestDB(t)

	if _, err := EnsureBootstrap([]int64{10001}, []int64{20002}); err != nil {
		t.Fatalf("EnsureBootstrap() error: %v", err)
	}
	seedAccessUser(t, 30003, model.RoleAdmin, true, false, "manual admin")

	if err := User.Delete(10001, 10001); !errors.Is(err, ErrSelfTarget) {
		t.Fatalf("Delete(self) error = %v, want ErrSelfTarget", err)
	}
	if err := User.Delete(30003, 10001); !errors.Is(err, ErrEnvManaged) {
		t.Fatalf("Delete(environment admin) error = %v, want ErrEnvManaged", err)
	}
	if err := User.Delete(30003, 20002); !errors.Is(err, ErrEnvManaged) {
		t.Fatalf("Delete(environment user) error = %v, want ErrEnvManaged", err)
	}
	if err := User.Delete(30003, 99999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete(missing) error = %v, want ErrNotFound", err)
	}
}

// TestAccessCacheServesUntilTTL proves the 30s cache is actually consulted and
// that an expired entry is re-read.
func TestAccessCacheServesUntilTTL(t *testing.T) {
	setupAccessTestDB(t)
	clock := useAccessTestClock(t)

	if _, err := EnsureBootstrap([]int64{10001}, nil); err != nil {
		t.Fatalf("EnsureBootstrap() error: %v", err)
	}
	seedAccessUser(t, 20002, model.RoleUser, true, false, "manual user")

	if !User.IsAllowed(20002) {
		t.Fatal("IsAllowed() = false, want true before the row is changed")
	}

	// Change the row behind the DAL's back: without an Invalidate the cached
	// decision must survive until its TTL.
	if err := Q.db.Model(&model.User{}).Where("user_id = ?", 20002).Update("enabled", false).Error; err != nil {
		t.Fatalf("direct update: %v", err)
	}
	if !User.IsAllowed(20002) {
		t.Fatal("cached decision changed before its TTL expired")
	}

	clock.Advance(userCacheTTL + time.Second)
	if User.IsAllowed(20002) {
		t.Fatal("cached decision survived its TTL, want a re-read")
	}
}

// TestAccessCacheInvalidatedByWrites proves role/enabled edits are effective on
// the next call rather than one TTL later.
func TestAccessCacheInvalidatedByWrites(t *testing.T) {
	setupAccessTestDB(t)
	useAccessTestClock(t)

	if _, err := EnsureBootstrap([]int64{10001}, nil); err != nil {
		t.Fatalf("EnsureBootstrap() error: %v", err)
	}
	seedAccessUser(t, 20002, model.RoleUser, true, false, "manual user")

	if !User.IsAllowed(20002) || User.IsAdmin(20002) {
		t.Fatal("precondition failed: a regular user must be allowed but not an admin")
	}

	if _, err := User.Update(10001, 20002, UserPatch{Enabled: new(false)}); err != nil {
		t.Fatalf("disable error: %v", err)
	}
	if User.IsAllowed(20002) {
		t.Fatal("disable was not effective immediately")
	}

	if _, err := User.Update(10001, 20002, UserPatch{Enabled: new(true)}); err != nil {
		t.Fatalf("re-enable error: %v", err)
	}
	if !User.IsAllowed(20002) {
		t.Fatal("re-enable was not effective immediately")
	}

	if _, err := User.Update(10001, 20002, UserPatch{Role: new(model.RoleAdmin)}); err != nil {
		t.Fatalf("promote error: %v", err)
	}
	if !User.IsAdmin(20002) {
		t.Fatal("promotion was not effective immediately")
	}

	if err := User.Delete(10001, 20002); err != nil {
		t.Fatalf("delete error: %v", err)
	}
	if User.IsAdmin(20002) || User.IsAllowed(20002) {
		t.Fatal("delete was not effective immediately")
	}
}

// TestAccessModeSwitchIsImmediate proves a settings write invalidates the mode
// cache too.
func TestAccessModeSwitchIsImmediate(t *testing.T) {
	setupAccessTestDB(t)
	useAccessTestClock(t)

	if _, err := EnsureBootstrap([]int64{10001}, nil); err != nil {
		t.Fatalf("EnsureBootstrap() error: %v", err)
	}
	if User.IsAllowed(29999) {
		t.Fatal("restricted mode must deny a stranger")
	}

	if err := SetSetting(model.SettingAccessMode, model.AccessModeOff); err != nil {
		t.Fatalf("SetSetting(off) error: %v", err)
	}
	if !User.IsAllowed(29999) {
		t.Fatal("off mode must allow everyone immediately")
	}

	if err := SetSetting(model.SettingAccessMode, model.AccessModeRestricted); err != nil {
		t.Fatalf("SetSetting(restricted) error: %v", err)
	}
	if User.IsAllowed(29999) {
		t.Fatal("restricted mode must deny again immediately")
	}
}

// TestAccessCacheInvalidateReloads covers the explicit Invalidate used by the
// bootstrap and by writes outside the DAL.
func TestAccessCacheInvalidateReloads(t *testing.T) {
	setupAccessTestDB(t)
	useAccessTestClock(t)

	if _, err := EnsureBootstrap([]int64{10001}, nil); err != nil {
		t.Fatalf("EnsureBootstrap() error: %v", err)
	}
	seedAccessUser(t, 20002, model.RoleUser, true, false, "manual user")

	if !User.IsAllowed(20002) {
		t.Fatal("IsAllowed() = false, want true")
	}
	if err := Q.db.Model(&model.User{}).Where("user_id = ?", 20002).Update("enabled", false).Error; err != nil {
		t.Fatalf("direct update: %v", err)
	}
	if !User.IsAllowed(20002) {
		t.Fatal("cache was expected to hide the direct change")
	}

	User.Invalidate()
	if User.IsAllowed(20002) {
		t.Fatal("Invalidate() did not force a re-read")
	}
}

// TestUserList covers the remark/id search and the paging.
func TestUserList(t *testing.T) {
	setupAccessTestDB(t)

	seedAccessUser(t, 10001, model.RoleAdmin, true, true, "primary admin")
	seedAccessUser(t, 10002, model.RoleUser, true, true, "corp user")
	seedAccessUser(t, 20002, model.RoleUser, false, false, "another corp")

	rows, total := User.List("", 0, 10)
	if total != 3 || len(rows) != 3 {
		t.Fatalf("List(all) = (%d rows, %d total), want (3, 3)", len(rows), total)
	}
	wantOrder := []int64{10001, 10002, 20002}
	for i, want := range wantOrder {
		if rows[i].UserID != want {
			t.Errorf("rows[%d].UserID = %d, want %d", i, rows[i].UserID, want)
		}
	}

	rows, total = User.List("corp", 0, 10)
	if total != 2 || len(rows) != 2 {
		t.Fatalf("List(remark) = (%d rows, %d total), want (2, 2)", len(rows), total)
	}

	rows, total = User.List("10002", 0, 10)
	if total != 1 || len(rows) != 1 || rows[0].UserID != 10002 {
		t.Fatalf("List(exact id) = (%d rows, %d total), want the single matched row", len(rows), total)
	}

	rows, total = User.List("nobody", 0, 10)
	if total != 0 || len(rows) != 0 {
		t.Fatalf("List(no match) = (%d rows, %d total), want empty", len(rows), total)
	}

	rows, total = User.List("", 1, 1)
	if total != 3 || len(rows) != 1 || rows[0].UserID != 10002 {
		t.Fatalf("List(page 2) = (%d rows, %d total), want the second row and total 3", len(rows), total)
	}

	rows, total = User.List("", 0, 0)
	if total != 3 || len(rows) != 3 {
		t.Fatalf("List(default limit) = (%d rows, %d total), want all 3 rows", len(rows), total)
	}
}

// TestSetLastSeen covers the write and the missing-row answer.
func TestSetLastSeen(t *testing.T) {
	setupAccessTestDB(t)

	seedAccessUser(t, 20002, model.RoleUser, true, false, "")
	seen := time.Date(2026, 3, 15, 12, 30, 0, 0, time.UTC)

	if err := User.SetLastSeen(20002, seen); err != nil {
		t.Fatalf("SetLastSeen() error: %v", err)
	}
	row := mustUser(t, 20002)
	if row.LastSeen == nil || !row.LastSeen.Equal(seen) {
		t.Fatalf("last_seen = %v, want %v", row.LastSeen, seen)
	}

	if err := User.SetLastSeen(99999, seen); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetLastSeen(missing) error = %v, want ErrNotFound", err)
	}
}

// TestSettingRoundTrip covers the generic settings read/write and the empty-key
// rejection.
func TestSettingRoundTrip(t *testing.T) {
	setupAccessTestDB(t)

	if value, ok, err := GetSetting("missing"); err != nil || ok || value != "" {
		t.Fatalf("GetSetting(missing) = (%q, %v, %v), want (\"\", false, nil)", value, ok, err)
	}

	if err := SetSetting("feature", "on"); err != nil {
		t.Fatalf("SetSetting() error: %v", err)
	}
	if value, ok, err := GetSetting("feature"); err != nil || !ok || value != "on" {
		t.Fatalf("GetSetting() = (%q, %v, %v), want (\"on\", true, nil)", value, ok, err)
	}

	// A second write updates in place rather than inserting a duplicate.
	if err := SetSetting("feature", "off"); err != nil {
		t.Fatalf("second SetSetting() error: %v", err)
	}
	if value, ok, _ := GetSetting("feature"); !ok || value != "off" {
		t.Fatalf("GetSetting() = (%q, %v), want (\"off\", true)", value, ok)
	}

	if err := SetSetting("", "x"); err == nil {
		t.Fatal("SetSetting with an empty key returned no error")
	}
}

// TestAccessCacheConcurrent exercises the cache from several goroutines while
// another one invalidates it: the RWMutex must keep reads and the write lock
// consistent under -race, and no read may observe a half-cleared cache as a
// wrong authorisation answer.
func TestAccessCacheConcurrent(t *testing.T) {
	setupAccessTestDB(t)
	useAccessTestClock(t)

	if _, err := EnsureBootstrap([]int64{10001}, []int64{20002}); err != nil {
		t.Fatalf("EnsureBootstrap() error: %v", err)
	}

	var wg sync.WaitGroup
	for i := range 60 {
		wg.Go(func() {
			switch i % 3 {
			case 0:
				if !User.IsAllowed(20002) {
					t.Error("IsAllowed(allowed user) = false during concurrent access")
				}
			case 1:
				if User.IsAdmin(20002) {
					t.Error("IsAdmin(regular user) = true during concurrent access")
				}
			case 2:
				User.Invalidate()
			}
		})
	}
	wg.Wait()

	if !User.IsAllowed(20002) || !User.IsAdmin(10001) {
		t.Fatal("cache lost the seeded rows after concurrent usage")
	}
}
