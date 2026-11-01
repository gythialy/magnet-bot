package dal

import (
	"testing"
	"time"

	"github.com/gythialy/magnet/pkg/model"
)

// cst is the deployment timezone (Asia/Shanghai). The local bound is expressed
// with a fixed zone so these tests behave the same on a UTC CI host.
var cst = time.FixedZone("CST", 8*60*60)

// withAlarmClock pins the alarm expiry clock for the duration of the test.
func withAlarmClock(t *testing.T, now time.Time) {
	t.Helper()

	previous := alarmClock
	alarmClock = func() time.Time { return now }
	t.Cleanup(func() { alarmClock = previous })
}

// newAlarmRow builds the minimal not-null payload for an alarms row.
func newAlarmRow(userId int64, creditCode string, endDate *time.Time) *model.Alarm {
	return &model.Alarm{
		UserID:     userId,
		BusinessID: "BIZ-" + creditCode,
		CreditName: "Example Corp",
		CreditCode: creditCode,
		StartDate:  time.Date(2024, 11, 1, 0, 0, 0, 0, time.Local),
		EndDate:    endDate,
		PageUrl1:   "https://example.com/notice",
		NoticeID:   "N-" + creditCode,
	}
}

// TestAlarmIsExistComparesInstantsNotStoredText reproduces the duplicate-push
// incident: a date-only end date is stored with a UTC offset while the check
// bound is local time. SQLite compares the two as text, so before the fix the
// punishment looked expired at 00:27 local and the claim was released on every
// run, re-pushing the same alarm.
func TestAlarmIsExistComparesInstantsNotStoredText(t *testing.T) {
	setupAlarmTestDB(t)

	endDate := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) // 08:00 CST
	if err := Q.db.Create(newAlarmRow(1, "CODE-UTC", &endDate)).Error; err != nil {
		t.Fatalf("insert alarm: %v", err)
	}

	// Same local day at 00:27: the punishment has not ended yet.
	withAlarmClock(t, time.Date(2026, 11, 1, 0, 27, 40, 0, cst))

	exists, err := Alarm.IsExist(1, "CODE-UTC")
	if err != nil {
		t.Fatalf("IsExist() error: %v", err)
	}
	if !exists {
		t.Fatal("IsExist() = false; a punishment ending at 2026-11-01T00:00Z is still in force at 2026-11-01T00:27+08:00")
	}
}

// TestAlarmIsExistExpiresAtEndInstant pins the boundary itself: the end date is
// the exclusive boundary, so the punishment is no longer in force at that
// instant.
func TestAlarmIsExistExpiresAtEndInstant(t *testing.T) {
	setupAlarmTestDB(t)

	endDate := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if err := Q.db.Create(newAlarmRow(1, "CODE-END", &endDate)).Error; err != nil {
		t.Fatalf("insert alarm: %v", err)
	}
	withAlarmClock(t, endDate)

	exists, err := Alarm.IsExist(1, "CODE-END")
	if err != nil {
		t.Fatalf("IsExist() error: %v", err)
	}
	if exists {
		t.Fatal("IsExist() = true at the end instant, want expired")
	}
}

// TestAlarmIsExistWithoutEndDate keeps open-ended punishments in force.
func TestAlarmIsExistWithoutEndDate(t *testing.T) {
	setupAlarmTestDB(t)

	noEnd := emptyTime
	if err := Q.db.Create(newAlarmRow(1, "CODE-OPEN", &noEnd)).Error; err != nil {
		t.Fatalf("insert alarm: %v", err)
	}
	withAlarmClock(t, time.Date(2026, 11, 1, 0, 27, 0, 0, cst))

	exists, err := Alarm.IsExist(1, "CODE-OPEN")
	if err != nil {
		t.Fatalf("IsExist() error: %v", err)
	}
	if !exists {
		t.Fatal("IsExist() = false for a punishment without an end date, want true")
	}
}

// TestAlarmCleanUsesInstants guards the /clean path: expired rows are removed
// even when their stored text would not sort before a local-time bound, and the
// comparison works for both the UTC offset older rows carry and the local offset
// the crawler writes now.
func TestAlarmCleanUsesInstants(t *testing.T) {
	setupAlarmTestDB(t)

	expiredUTC := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	expiredLocal := time.Date(2026, 10, 15, 0, 0, 0, 0, cst)
	activeUTC := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	activeLocal := time.Date(2026, 12, 1, 0, 0, 0, 0, cst)
	noEnd := emptyTime
	for _, row := range []*model.Alarm{
		newAlarmRow(1, "CODE-EXPIRED-UTC", &expiredUTC),
		newAlarmRow(1, "CODE-EXPIRED-LOCAL", &expiredLocal),
		newAlarmRow(1, "CODE-ACTIVE-UTC", &activeUTC),
		newAlarmRow(1, "CODE-ACTIVE-LOCAL", &activeLocal),
		newAlarmRow(1, "CODE-OPEN", &noEnd),
	} {
		if err := Q.db.Create(row).Error; err != nil {
			t.Fatalf("insert alarm %s: %v", row.CreditCode, err)
		}
	}

	withAlarmClock(t, time.Date(2026, 11, 1, 12, 0, 0, 0, cst))
	if err := Alarm.Clean(); err != nil {
		t.Fatalf("Clean() error: %v", err)
	}

	for code, want := range map[string]bool{
		"CODE-EXPIRED-UTC":   false,
		"CODE-EXPIRED-LOCAL": false,
		"CODE-ACTIVE-UTC":    true,
		"CODE-ACTIVE-LOCAL":  true,
		"CODE-OPEN":          true,
	} {
		exists, err := Alarm.IsExist(1, code)
		if err != nil {
			t.Fatalf("IsExist(%s) error: %v", code, err)
		}
		if exists != want {
			t.Errorf("IsExist(%q) = %t after Clean(), want %t", code, exists, want)
		}
	}
}
