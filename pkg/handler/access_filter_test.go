package handler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Synthetic ids used by the access-filter tests. They are deliberately not the
// ids other handler tests use, so a bug that leaks rows between cases is easier
// to spot.
const (
	accessAdminID    int64 = 1001
	accessAllowedID  int64 = 1002
	accessDisabledID int64 = 1003
	accessUnknownID  int64 = 1004
)

// setupAccessFilterDB builds a throwaway database carrying every table the
// background pipelines consult: users and settings for the access decision,
// keywords for the scheduled sweep and pending attachments for the retrier.
func setupAccessFilterDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/access_filter.db"), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{},
		&model.Setting{},
		&model.Keyword{},
		&model.PendingAttachment{},
	); err != nil {
		t.Fatal(err)
	}
	dal.SetDefault(db)
	return db
}

// seedAccessUser inserts one account row and drops the authorisation cache so
// the next decision reads the new state instead of a cached earlier one.
func seedAccessUser(t *testing.T, db *gorm.DB, userID int64, role int32, enabled bool) {
	t.Helper()

	now := time.Now().Truncate(time.Second)
	row := &model.User{
		UserID:    userID,
		Role:      role,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create user %d: %v", userID, err)
	}
	dal.User.Invalidate()
}

// seedAccessKeyword gives one account a project keyword, which is what makes
// dal.Keyword.Ids() return it for the scheduled sweep.
func seedAccessKeyword(t *testing.T, db *gorm.DB, userID int64, keyword string) {
	t.Helper()

	row := &model.Keyword{
		UserID:    userID,
		Keyword:   keyword,
		Type:      int32(model.PROJECT),
		CreatedAt: time.Now().Truncate(time.Second),
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create keyword for user %d: %v", userID, err)
	}
}

func setAccessMode(t *testing.T, mode string) {
	t.Helper()

	if err := dal.SetSetting(model.SettingAccessMode, mode); err != nil {
		t.Fatalf("set access mode %q: %v", mode, err)
	}
}

// TestInfoProcessorConfigSkipsUsersWithoutAccess guards the scheduled sweep:
// keywords outlive a revoke, so config() must drop accounts that may no longer
// use the bot instead of pushing notices to them.
func TestInfoProcessorConfigSkipsUsersWithoutAccess(t *testing.T) {
	db := setupAccessFilterDB(t)
	seedAccessUser(t, db, accessAdminID, model.RoleAdmin, true)
	seedAccessUser(t, db, accessAllowedID, model.RoleUser, true)
	seedAccessUser(t, db, accessDisabledID, model.RoleUser, false)

	// The unknown account has keywords but no users row: in restricted mode
	// that is a denial, not an implicit allow.
	for _, userID := range []int64{accessAdminID, accessAllowedID, accessDisabledID, accessUnknownID} {
		seedAccessKeyword(t, db, userID, fmt.Sprintf("keyword-%d", userID))
	}
	setAccessMode(t, model.AccessModeRestricted)

	processor := &InfoProcessor{ctx: testBotContext("example.com")}
	config := processor.config()

	if len(config) != 2 {
		t.Fatalf("config users = %d, want only the admin and the allowed user", len(config))
	}
	for _, userID := range []int64{accessAdminID, accessAllowedID} {
		data, ok := config[userID]
		if !ok {
			t.Fatalf("config is missing allowed user %d", userID)
		}
		if data.UserId != userID {
			t.Fatalf("config[%d].UserId = %d", userID, data.UserId)
		}
		if len(data.ProjectRules) != 1 {
			t.Fatalf("config[%d] has %d project rules, want 1 (keyword expansion must only run for allowed users)",
				userID, len(data.ProjectRules))
		}
	}
	for _, userID := range []int64{accessDisabledID, accessUnknownID} {
		if _, ok := config[userID]; ok {
			t.Fatalf("config produced ProcessData for denied user %d", userID)
		}
	}
}

// TestInfoProcessorConfigIncludesEveryoneWhenAccessIsOff guards the other side
// of the switch: "off" ignores the users table entirely, including disabled
// rows, and the sweep must keep working as before this feature existed.
func TestInfoProcessorConfigIncludesEveryoneWhenAccessIsOff(t *testing.T) {
	db := setupAccessFilterDB(t)
	seedAccessUser(t, db, accessAdminID, model.RoleAdmin, true)
	seedAccessUser(t, db, accessAllowedID, model.RoleUser, true)
	seedAccessUser(t, db, accessDisabledID, model.RoleUser, false)

	for _, userID := range []int64{accessAdminID, accessAllowedID, accessDisabledID, accessUnknownID} {
		seedAccessKeyword(t, db, userID, fmt.Sprintf("keyword-%d", userID))
	}
	setAccessMode(t, model.AccessModeOff)

	if !dal.User.IsAllowed(accessDisabledID) {
		t.Fatal("off mode must allow a disabled row without consulting the users table")
	}

	processor := &InfoProcessor{ctx: testBotContext("example.com")}
	config := processor.config()

	if len(config) != 4 {
		t.Fatalf("config users = %d, want every keyword owner in off mode", len(config))
	}
	for _, userID := range []int64{accessAdminID, accessAllowedID, accessDisabledID, accessUnknownID} {
		if _, ok := config[userID]; !ok {
			t.Fatalf("config is missing user %d in off mode", userID)
		}
	}
}

// TestInfoProcessorGetStopsBeforeCrawlingDeniedUser proves the /retry entry
// point applies the access gate before it does any work. The processor is built
// without a crawler and without a worker pool: reaching either one would panic,
// so surviving the calls below is the assertion that the early return happened.
func TestInfoProcessorGetStopsBeforeCrawlingDeniedUser(t *testing.T) {
	db := setupAccessFilterDB(t)
	seedAccessUser(t, db, accessDisabledID, model.RoleUser, false)
	setAccessMode(t, model.AccessModeRestricted)

	processor := &InfoProcessor{ctx: testBotContext("example.com")}

	// A disabled row and an unknown id are both denials in restricted mode.
	processor.Get(accessDisabledID)
	processor.Get(accessUnknownID)
}

// enqueueAttachmentForUser inserts one due pending attachment for the given
// account.
func enqueueAttachmentForUser(t *testing.T, clock *fakeClock, userID int64, url string) {
	t.Helper()

	row := &model.PendingAttachment{
		UserID:        userID,
		URL:           url,
		Title:         "合成设备采购公告",
		TenderCode:    "SYN-ACCESS",
		NextAttemptAt: clock.Now().Add(-time.Minute),
		CreatedAt:     clock.Now().Add(-time.Hour),
	}
	if err := dal.EnqueueAttachment(row); err != nil {
		t.Fatalf("enqueue attachment for user %d: %v", userID, err)
	}
}

// TestAttachmentRetrierSkipsRevokedUserButKeepsTheRow guards the retrier's
// admission check: a due attachment owned by a revoked account must not be
// sent, converted, counted as a failure or dropped, and the sweep must not take
// its lease. Once the account is allowed again the preserved row is delivered
// by the next sweep.
func TestAttachmentRetrierSkipsRevokedUserButKeepsTheRow(t *testing.T) {
	db := setupAccessFilterDB(t)
	clock := &fakeClock{at: time.Now().Truncate(time.Second)}
	seedAccessUser(t, db, accessAllowedID, model.RoleUser, true)
	seedAccessUser(t, db, accessDisabledID, model.RoleUser, false)
	setAccessMode(t, model.AccessModeRestricted)

	const (
		allowedURL = "https://example.com/allowed"
		revokedURL = "https://example.com/revoked"
	)
	enqueueAttachmentForUser(t, clock, accessAllowedID, allowedURL)
	enqueueAttachmentForUser(t, clock, accessDisabledID, revokedURL)

	convert := &fakeConverter{data: []byte("%PDF-1.4 synthetic")}
	sender := &fakeSender{}
	retrier := newAttachmentRetrierForTest(t, clock, convert, sender)

	retrier.RunOnce(context.Background())

	if len(sender.calls) != 1 {
		t.Fatalf("uploads = %d, want only the allowed user's attachment", len(sender.calls))
	}
	if chatID, ok := sender.calls[0].ChatID.(int64); !ok || chatID != accessAllowedID {
		t.Fatalf("upload chat id = %v, want %d", sender.calls[0].ChatID, accessAllowedID)
	}
	// The revoked account's page must not even be converted: revoking access
	// pauses the whole delivery path, not just the upload.
	if len(convert.calls) != 1 || convert.calls[0] != allowedURL {
		t.Fatalf("converted = %v, want only %q", convert.calls, allowedURL)
	}

	rows := pendingRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("pending rows = %d, want the revoked row preserved", len(rows))
	}
	row := rows[0]
	if row.UserID != accessDisabledID || row.URL != revokedURL {
		t.Fatalf("remaining row = {%d %q}, want {%d %q}", row.UserID, row.URL, accessDisabledID, revokedURL)
	}
	if row.Attempts != 0 {
		t.Errorf("attempts = %d, want 0 (a skip is not a failure)", row.Attempts)
	}
	if row.LastError != "" {
		t.Errorf("last_error = %q, want empty", row.LastError)
	}
	if !row.LockedUntil.IsZero() {
		t.Errorf("locked_until = %s, want the row never claimed", row.LockedUntil)
	}

	// The lease is still free: a claim wins immediately. This is what makes the
	// skip safe — no other sweep is locked out while the account is denied.
	claimed, err := dal.ClaimAttachment(accessDisabledID, revokedURL, clock.Now(), clock.Now())
	if err != nil {
		t.Fatalf("claim skipped row: %v", err)
	}
	if !claimed {
		t.Fatal("skipped row's lease was taken, so a later sweep could not deliver it")
	}

	// Re-authorising the account must flush the preserved row without operator
	// intervention.
	if err := db.Model(&model.User{}).
		Where("user_id = ?", accessDisabledID).
		Update("enabled", true).Error; err != nil {
		t.Fatalf("re-authorise user %d: %v", accessDisabledID, err)
	}
	dal.User.Invalidate()

	retrier.RunOnce(context.Background())

	if len(sender.calls) != 2 {
		t.Fatalf("uploads after re-authorisation = %d, want the preserved row delivered", len(sender.calls))
	}
	if chatID, ok := sender.calls[1].ChatID.(int64); !ok || chatID != accessDisabledID {
		t.Fatalf("second upload chat id = %v, want %d", sender.calls[1].ChatID, accessDisabledID)
	}
	if rows := pendingRows(t, db); len(rows) != 0 {
		t.Fatalf("pending rows = %d after re-authorised delivery, want 0", len(rows))
	}
}

// setupLegacyKeywordDB mimics a database created before #412: keywords exist,
// but the access-control tables were never migrated. Background work has to
// keep running there instead of failing closed on a schema that is not part of
// that database.
func setupLegacyKeywordDB(t *testing.T, userIDs ...int64) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/legacy_keywords.db"), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Keyword{}); err != nil {
		t.Fatal(err)
	}
	dal.SetDefault(db)
	for _, userID := range userIDs {
		seedAccessKeyword(t, db, userID, fmt.Sprintf("keyword-%d", userID))
	}
	return db
}

// TestBackgroundAccessEnforcedRequiresSchema pins the compatibility rule: a
// database without the access-control tables is treated like "off" (nobody can
// be denied there), while a migrated database always enforces the decision —
// including when no mode row has been written yet, which IsAllowed reads as
// "off" but still consults the users table in a restricted mode.
func TestBackgroundAccessEnforcedRequiresSchema(t *testing.T) {
	setupLegacyKeywordDB(t, accessAllowedID)
	if backgroundAccessEnforced() {
		t.Fatal("schema-less database must not enforce access control")
	}

	setupAccessFilterDB(t)
	if !backgroundAccessEnforced() {
		t.Fatal("migrated database must enforce access control even before a mode row exists")
	}

	setAccessMode(t, model.AccessModeRestricted)
	if !backgroundAccessEnforced() {
		t.Fatal("restricted mode must enforce access control")
	}
}

// TestInfoProcessorConfigKeepsLegacyDatabaseBehaviour guards the promise that a
// pre-#412 database keeps its historical sweep behaviour: no access tables, no
// filtering, no panic.
func TestInfoProcessorConfigKeepsLegacyDatabaseBehaviour(t *testing.T) {
	setupLegacyKeywordDB(t, accessAllowedID, accessDisabledID)

	processor := &InfoProcessor{ctx: testBotContext("example.com")}
	config := processor.config()

	if len(config) != 2 {
		t.Fatalf("config users = %d, want both keyword owners on a legacy database", len(config))
	}
	for _, userID := range []int64{accessAllowedID, accessDisabledID} {
		if _, ok := config[userID]; !ok {
			t.Fatalf("legacy config is missing user %d", userID)
		}
	}
}

// TestIsMissingTable keeps the fallback narrow: only a genuinely absent table
// may be read as "no access control"; other read failures must keep the gate
// closed.
func TestIsMissingTable(t *testing.T) {
	cases := map[string]bool{
		"SQL logic error: no such table: settings (1)":              true,
		"Error 1146 (42S02): Table 'magnet.settings' doesn't exist": true,
		"database is locked (5) (SQLITE_BUSY)":                      false,
		"disk I/O error":                                            false,
	}
	for message, want := range cases {
		if got := isMissingTable(errors.New(message)); got != want {
			t.Errorf("isMissingTable(%q) = %v, want %v", message, got, want)
		}
	}
}
