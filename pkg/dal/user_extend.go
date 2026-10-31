package dal

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/gythialy/magnet/pkg/model"
)

// User is the hand-written DAO for the access-control tables. It is a package
// singleton rather than a field of Query because the Query type is generated and
// must not be edited; every method reads the current Q.db, so SetDefault binds
// it exactly like the generated DAOs.
var User = &user{}

// Sentinel errors of the access-control guards. They are exported so the
// transport layer can map them to a status code without matching on message
// text: ErrNotFound -> 404, the other three -> 409 (or 403).
var (
	// ErrNotFound reports an id with no users row.
	ErrNotFound = errors.New("dal: user not found")

	// ErrSelfTarget reports an operation that would remove or restrict the
	// actor's own access: deleting themselves, disabling themselves, or
	// demoting themselves from administrator.
	ErrSelfTarget = errors.New("dal: an actor cannot remove or restrict their own access")

	// ErrEnvManaged reports a row whose role/enabled state is owned by
	// MANAGER_ID or ALLOWED_USER_IDS. Only the remark may be edited by hand.
	ErrEnvManaged = errors.New("dal: user's role and access are managed by environment variables")

	// ErrLastAdmin reports an operation that would leave the deployment without
	// any enabled administrator, which would make the bot unadministrable.
	ErrLastAdmin = errors.New("dal: operation would leave no enabled administrator")
)

// userCacheTTL bounds how stale an access decision may be. It is the same for
// user rows and for the access mode so one Invalidate covers both.
const userCacheTTL = 30 * time.Second

// defaultUserListLimit keeps List pageable even when a caller forgets the limit:
// an accidental "SELECT * FROM users" is not a useful answer for an admin screen.
const defaultUserListLimit = 20

// accessClock is the clock behind cache expiry. It is a package variable so
// tests can advance time instead of sleeping for userCacheTTL.
var accessClock = time.Now

// userCacheEntry is one cached authorisation row. user is nil for a cached
// "does not exist", which keeps a stream of unknown ids from hammering the DB.
type userCacheEntry struct {
	user    *model.User
	expires time.Time
}

// user is the DAO for users. Its cache is invalidated by every write, and it is
// keyed by the database it was filled from: tests call SetDefault per case, and
// rows from a previous database must never answer for the current one.
type user struct {
	mu sync.RWMutex

	db       *gorm.DB
	cache    map[int64]userCacheEntry
	mode     string
	modeTill time.Time
}

// UserPatch carries the fields an administrator may change. A nil field means
// "leave as is", which is why these are pointers rather than zero values: false
// and 0 are meaningful updates.
type UserPatch struct {
	Remark  *string
	Role    *int32
	Enabled *bool
}

// Invalidate drops every cached authorisation decision. Every write path calls
// it after a successful write so a role change, an enable/disable or an access
// mode switch is effective on the next request instead of up to 30s later.
func (u *user) Invalidate() {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.resetLocked()
	u.cache = nil
	u.mode = ""
	u.modeTill = time.Time{}
}

// resetLocked drops the cache when the underlying database changed. It must be
// called with u.mu held.
func (u *user) resetLocked() {
	if u.db != Q.db {
		u.cache = nil
		u.mode = ""
		u.modeTill = time.Time{}
		u.db = Q.db
	}
}

// Get reads one users row straight from the database. It is the uncached view
// used by the admin screens; authorisation decisions go through the cache.
//
// The boolean is false for both "no such user" and a read error, matching the
// other DAO getters in this package that cannot report an error. Write paths use
// loadRow instead so a database failure is not mistaken for a missing row.
func (u *user) Get(userID int64) (*model.User, bool) {
	row, err := u.loadRow(userID)
	if err != nil {
		return nil, false
	}
	return row, true
}

// loadRow reads one row and keeps the error, so the guards can tell a real
// "not found" (gorm.ErrRecordNotFound) from a storage failure.
func (u *user) loadRow(userID int64) (*model.User, error) {
	var row model.User
	if err := Q.db.Where("user_id = ?", userID).Take(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// List returns one page of users plus the total matching the filter.
//
// q is matched against the remark with a LIKE, and when it parses as a number
// also against the user id exactly. An exact id match (rather than a substring)
// keeps the query portable: casting an integer column to text differs between
// SQLite and MySQL, and an id typed into the admin search box is normally the
// whole id from a report.
func (u *user) List(q string, offset, limit int) ([]*model.User, int64) {
	term := strings.TrimSpace(q)
	if limit <= 0 {
		limit = defaultUserListLimit
	}
	if offset < 0 {
		offset = 0
	}

	filter := func(db *gorm.DB) *gorm.DB {
		if term == "" {
			return db
		}
		// LIKE is applied to a user-supplied value; SQL wildcards inside it are
		// treated as wildcards, which matches the other search endpoints of the
		// bot (and keeps the SQL identical across dialects).
		if id, err := strconv.ParseInt(term, 10, 64); err == nil {
			return db.Where("user_id = ? OR remark LIKE ?", id, "%"+term+"%")
		}
		return db.Where("remark LIKE ?", "%"+term+"%")
	}

	var total int64
	if err := filter(Q.db.Model(&model.User{})).Count(&total).Error; err != nil {
		return nil, 0
	}

	var rows []*model.User
	if err := filter(Q.db.Model(&model.User{})).
		Order("user_id ASC").
		Limit(limit).
		Offset(offset).
		Find(&rows).Error; err != nil {
		return nil, 0
	}
	return rows, total
}

// UpsertFromEnv creates or promotes one environment-seeded user. It is
// idempotent, so calling it on every start is safe.
//
// An existing row is never demoted here: role=RoleUser means "this account is
// listed as a regular user", and an account that is already an administrator
// keeps that role. A promotion also re-enables the row, because an administrator
// disabled before being listed in MANAGER_ID would otherwise stay powerless.
func (u *user) UpsertFromEnv(userID int64, role int32) error {
	if userID <= 0 {
		return fmt.Errorf("dal: invalid user id %d", userID)
	}
	if role != model.RoleUser && role != model.RoleAdmin {
		return fmt.Errorf("dal: invalid role %d", role)
	}

	if _, err := applyEnvUser(Q.db, userID, role); err != nil {
		return err
	}
	u.Invalidate()
	return nil
}

// applyEnvUser is the shared create-or-promote step of UpsertFromEnv and
// SyncFromEnv. It reports whether a row was created.
func applyEnvUser(db *gorm.DB, userID int64, role int32) (bool, error) {
	now := time.Now()

	var row model.User
	err := db.Where("user_id = ?", userID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		created := &model.User{
			UserID:    userID,
			Role:      role,
			Enabled:   true,
			FromEnv:   true,
			CreatedAt: now,
			UpdatedAt: now,
		}
		return true, db.Create(created).Error
	}
	if err != nil {
		return false, err
	}

	updates := make(map[string]any, 3)
	if !row.FromEnv {
		updates["from_env"] = true
	}
	if role == model.RoleAdmin && row.Role != model.RoleAdmin {
		updates["role"] = model.RoleAdmin
		if !row.Enabled {
			updates["enabled"] = true
		}
	}
	if len(updates) == 0 {
		// Already exactly what the environment asks for: do not touch the row,
		// so UpdatedAt keeps recording real changes.
		return false, nil
	}

	updates["updated_at"] = now
	return false, db.Model(&model.User{}).Where("user_id = ?", userID).Updates(updates).Error
}

// SyncFromEnv aligns the users table with the two environment lists and reports
// how many rows it created and how many it stopped managing.
//
// The negotiation is deliberately conservative, because this runs unattended on
// every start:
//   - managers become enabled administrators;
//   - allowed accounts become regular users, and an account that is already an
//     administrator is not demoted by the allowed list;
//   - rows that were managed by the environment and are no longer listed lose
//     from_env, and an administrator among them is demoted only when another
//     enabled administrator remains. The last one keeps the role and a warning
//     is logged: a typo in MANAGER_ID must not lock the deployment out.
//
// added counts created rows. removed counts rows that stopped being
// environment-managed; it is not a row deletion.
func (u *user) SyncFromEnv(managers, allowed []int64) (added, removed int64, err error) {
	managerIDs := uniqueUserIDs(managers)
	allowedIDs := uniqueUserIDs(allowed)

	managed := make(map[int64]struct{}, len(managerIDs)+len(allowedIDs))
	for _, id := range managerIDs {
		managed[id] = struct{}{}
	}
	for _, id := range allowedIDs {
		managed[id] = struct{}{}
	}

	txErr := Q.db.Transaction(func(tx *gorm.DB) error {
		for _, id := range managerIDs {
			created, err := applyEnvUser(tx, id, model.RoleAdmin)
			if err != nil {
				return err
			}
			if created {
				added++
			}
		}
		for _, id := range allowedIDs {
			created, err := applyEnvUser(tx, id, model.RoleUser)
			if err != nil {
				return err
			}
			if created {
				added++
			}
		}

		var stale []model.User
		if err := tx.Where("from_env = ?", true).Find(&stale).Error; err != nil {
			return err
		}
		for i := range stale {
			row := &stale[i]
			if _, ok := managed[row.UserID]; ok {
				continue
			}

			updates := map[string]any{
				"from_env":   false,
				"updated_at": time.Now(),
			}

			if row.Role == model.RoleAdmin && countEnabledAdminsExcept(tx, row.UserID) == 0 {
				// Keeping the role is the anti-lockout rule. The id is only
				// logged at debug: a warning is shown to operators, and ids do
				// not belong in shared logs.
				slog.Warn("kept administrator role for an account no longer listed in the environment")
				slog.Debug("kept environment administrator", "user_id", row.UserID)
			} else if row.Role == model.RoleAdmin {
				updates["role"] = model.RoleUser
			}

			if err := tx.Model(&model.User{}).Where("user_id = ?", row.UserID).Updates(updates).Error; err != nil {
				return err
			}
			removed++
		}
		return nil
	})
	if txErr != nil {
		return 0, 0, txErr
	}

	u.Invalidate()
	return added, removed, nil
}

// EnsureBootstrap seeds users from the environment and makes sure an access mode
// exists. It runs once at startup, before the bot serves requests.
//
// The mode is only written when the settings row is absent or empty: an operator
// who switched the bot to "off" (or back to "restricted") must keep that choice
// across restarts. A database whose first start already has an administrator row
// starts restricted; an empty one starts open, so a fresh deployment cannot lock
// itself out before MANAGER_ID is configured.
func EnsureBootstrap(managers, allowed []int64) (string, error) {
	if _, _, err := User.SyncFromEnv(managers, allowed); err != nil {
		return "", err
	}

	if value, ok, err := GetSetting(model.SettingAccessMode); err != nil {
		return "", err
	} else if ok && value != "" {
		return value, nil
	}

	mode := model.AccessModeOff

	var admins int64
	if err := Q.db.Model(&model.User{}).Where("role = ?", model.RoleAdmin).Count(&admins).Error; err != nil {
		return "", err
	}
	if admins > 0 {
		mode = model.AccessModeRestricted
	}

	if err := SetSetting(model.SettingAccessMode, mode); err != nil {
		return "", err
	}
	return mode, nil
}

// Update applies an administrator's edit to one account. It returns the stored
// row so the caller can render exactly what was written.
//
// The guard order is: the target must exist (ErrNotFound), the actor must not be
// restricting themselves (ErrSelfTarget), an environment-managed row may only be
// renamed (ErrEnvManaged), and the last enabled administrator cannot be demoted
// or disabled (ErrLastAdmin). Self-target is checked before the environment rule
// because "you cannot do this to yourself" is the clearer explanation.
func (u *user) Update(actorID, targetID int64, patch UserPatch) (*model.User, error) {
	if patch.Role != nil && *patch.Role != model.RoleUser && *patch.Role != model.RoleAdmin {
		return nil, fmt.Errorf("dal: invalid role %d", *patch.Role)
	}

	target, err := u.loadRow(targetID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	updates := make(map[string]any, 4)

	if patch.Remark != nil && *patch.Remark != target.Remark {
		updates["remark"] = *patch.Remark
	}

	demoted := patch.Role != nil && *patch.Role == model.RoleUser && target.Role == model.RoleAdmin
	promoted := patch.Role != nil && *patch.Role == model.RoleAdmin && target.Role != model.RoleAdmin
	if demoted || promoted {
		updates["role"] = *patch.Role
	}

	disabled := patch.Enabled != nil && !*patch.Enabled && target.Enabled
	reenabled := patch.Enabled != nil && *patch.Enabled && !target.Enabled
	if disabled || reenabled {
		updates["enabled"] = *patch.Enabled
	}

	// Only a reduction of authority is guarded. Promoting, re-enabling or
	// renaming is always allowed (except for the environment rule below).
	reducesAccess := demoted || disabled

	if reducesAccess && actorID == targetID {
		return nil, ErrSelfTarget
	}
	if target.FromEnv && (demoted || promoted || disabled || reenabled) {
		return nil, ErrEnvManaged
	}
	if reducesAccess && target.Role == model.RoleAdmin && target.Enabled && u.CountActiveAdmins() <= 1 {
		return nil, ErrLastAdmin
	}

	if len(updates) == 0 {
		// Nothing changed (including a patch that repeats the stored values):
		// return the row without bumping UpdatedAt.
		return target, nil
	}

	updates["updated_at"] = time.Now()
	result := Q.db.Model(&model.User{}).Where("user_id = ?", targetID).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}

	u.Invalidate()
	row, ok := u.Get(targetID)
	if !ok {
		return nil, ErrNotFound
	}
	return row, nil
}

// Delete removes one account. The same guards as Update apply: an actor cannot
// delete themselves, an environment-managed row cannot be deleted by hand, and
// the last enabled administrator must survive.
func (u *user) Delete(actorID, targetID int64) error {
	target, err := u.loadRow(targetID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if actorID == targetID {
		return ErrSelfTarget
	}
	if target.FromEnv {
		return ErrEnvManaged
	}
	if target.Role == model.RoleAdmin && target.Enabled && u.CountActiveAdmins() <= 1 {
		return ErrLastAdmin
	}

	result := Q.db.Where("user_id = ?", targetID).Delete(&model.User{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}

	u.Invalidate()
	return nil
}

// SetLastSeen records when an account was last seen. It deliberately does not
// touch updated_at: that column tracks administrative edits, and a per-request
// write would drown them.
//
// It returns ErrNotFound when no row matched, so a caller can ignore the error
// (a stranger has no row) without silently believing the write landed.
func (u *user) SetLastSeen(userID int64, at time.Time) error {
	result := Q.db.Model(&model.User{}).Where("user_id = ?", userID).UpdateColumn("last_seen", at)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}

	u.Invalidate()
	return nil
}

// CountActiveAdmins returns how many administrators are currently enabled. An
// error is reported as zero, which makes the destructive guards above fail
// closed: a broken count must not be read as "there are plenty of admins".
func (u *user) CountActiveAdmins() int64 {
	var count int64
	if err := Q.db.Model(&model.User{}).Where("role = ? AND enabled = ?", model.RoleAdmin, true).Count(&count).Error; err != nil {
		return 0
	}
	return count
}

// IsAllowed reports whether the account may use the bot.
//
// In "off" mode everyone is allowed without touching the users table. In
// "restricted" mode the account must exist, be enabled, and be either an
// administrator or covered by the restricted mode itself. An unknown mode value
// fails closed to "administrators only" rather than open.
func (u *user) IsAllowed(userID int64) bool {
	mode := u.accessMode()
	if mode == model.AccessModeOff {
		return true
	}

	row, ok := u.cachedUser(userID)
	if !ok || row == nil || !row.Enabled {
		return false
	}
	return row.Role == model.RoleAdmin || mode == model.AccessModeRestricted
}

// IsAdmin reports whether the account is an enabled administrator. Admin rights
// are independent of the access mode: "off" does not make every account an
// administrator.
func (u *user) IsAdmin(userID int64) bool {
	row, ok := u.cachedUser(userID)
	return ok && row != nil && row.Enabled && row.Role == model.RoleAdmin
}

// accessMode returns the effective access mode, cached for userCacheTTL.
//
// A database error while reading the setting returns "restricted": a settings
// read failure must not silently open the bot to everybody.
func (u *user) accessMode() string {
	now := accessClock()

	u.mu.RLock()
	sameDB := u.db == Q.db
	mode, until := u.mode, u.modeTill
	u.mu.RUnlock()

	if sameDB && now.Before(until) {
		return mode
	}

	// A missing row means the bot has not bootstrapped yet; treat it as "off",
	// the same value EnsureBootstrap would write for an empty database.
	mode = model.AccessModeOff

	value, ok, err := GetSetting(model.SettingAccessMode)
	switch {
	case err != nil:
		mode = model.AccessModeRestricted
	case ok && value != "":
		mode = value
	}

	u.mu.Lock()
	u.resetLocked()
	u.mode = mode
	u.modeTill = now.Add(userCacheTTL)
	u.mu.Unlock()

	return mode
}

// cachedUser returns one authorisation row from the cache, loading it when the
// entry is missing or expired. Missing rows are cached too, so repeated requests
// from an unknown account do not become repeated queries.
func (u *user) cachedUser(userID int64) (*model.User, bool) {
	now := accessClock()

	u.mu.RLock()
	sameDB := u.db == Q.db
	entry, ok := u.cache[userID]
	u.mu.RUnlock()

	// A cache filled from another database is not an answer for this one; the
	// load below replaces it (resetLocked) before storing the fresh row.
	if sameDB && ok && now.Before(entry.expires) {
		return entry.user, entry.user != nil
	}

	return u.loadUser(userID)
}

// loadUser reads one row from the database and caches the result, including a
// miss.
func (u *user) loadUser(userID int64) (*model.User, bool) {
	row, ok := u.Get(userID)
	now := accessClock()

	u.mu.Lock()
	u.resetLocked()
	if u.cache == nil {
		u.cache = make(map[int64]userCacheEntry)
	}
	u.cache[userID] = userCacheEntry{user: row, expires: now.Add(userCacheTTL)}
	u.mu.Unlock()

	return row, ok
}

// countEnabledAdminsExcept counts enabled administrators other than userID. It
// takes a transaction so SyncFromEnv sees its own writes.
func countEnabledAdminsExcept(db *gorm.DB, userID int64) int64 {
	var count int64
	if err := db.Model(&model.User{}).
		Where("role = ? AND enabled = ? AND user_id <> ?", model.RoleAdmin, true, userID).
		Count(&count).Error; err != nil {
		return 0
	}
	return count
}

// uniqueUserIDs de-duplicates and sanity-checks an environment list. The config
// parser already does this; repeating it here keeps the DAL safe for any caller.
func uniqueUserIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	unique := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}
