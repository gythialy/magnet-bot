package handler

import (
	"testing"
	"time"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupAlarmClaimDB opens a private database holding only the alarms table and
// points the package DAOs at it.
func setupAlarmClaimDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/alarm_claim.db"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(&model.Alarm{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	dal.SetDefault(db)
	return db
}

// alarmForClaim builds the fields a claimed alarm needs.
func alarmForClaim(userId int64, businessId, creditCode string, endDate *time.Time) *model.Alarm {
	return &model.Alarm{
		UserID:     userId,
		BusinessID: businessId,
		CreditName: "Example Corp",
		CreditCode: creditCode,
		StartDate:  time.Now().AddDate(-2, 0, 0).Truncate(time.Second),
		EndDate:    endDate,
		PageUrl1:   "https://example.com/notice",
		NoticeID:   "N-" + businessId,
	}
}

// TestAlarmClaimSkipsAlreadyPushedPunishment is the regression test for the
// duplicate push: after its end date the row stays in the table (only /clean
// removes it), and every later run re-crawls the same notice. The claim must
// stay taken instead of being released and sent again.
func TestAlarmClaimSkipsAlreadyPushedPunishment(t *testing.T) {
	db := setupAlarmClaimDB(t)
	processor := &InfoProcessor{ctx: testBotContext("")}

	past := time.Now().AddDate(-1, 0, 0).Truncate(time.Second)
	if err := db.Create(alarmForClaim(7, "BIZ-SAME", "CODE-SAME", &past)).Error; err != nil {
		t.Fatalf("insert existing alarm: %v", err)
	}

	claimed, err := processor.claimAlarm(alarmForClaim(7, "BIZ-SAME", "CODE-SAME", &past))
	if err != nil {
		t.Fatalf("claimAlarm() error: %v", err)
	}
	if claimed {
		t.Fatal("claimAlarm() = true for a punishment that was already pushed; want false")
	}
}

// TestAlarmClaimReplacesExpiredPunishmentOnce covers the legitimate renewal: a
// new punishment for the same creditor replaces the expired row and is pushed
// exactly once.
func TestAlarmClaimReplacesExpiredPunishmentOnce(t *testing.T) {
	db := setupAlarmClaimDB(t)
	processor := &InfoProcessor{ctx: testBotContext("")}

	past := time.Now().AddDate(-1, 0, 0).Truncate(time.Second)
	future := time.Now().AddDate(1, 0, 0).Truncate(time.Second)
	if err := db.Create(alarmForClaim(7, "BIZ-OLD", "CODE-RENEW", &past)).Error; err != nil {
		t.Fatalf("insert expired alarm: %v", err)
	}

	claimed, err := processor.claimAlarm(alarmForClaim(7, "BIZ-NEW", "CODE-RENEW", &future))
	if err != nil {
		t.Fatalf("claimAlarm() error: %v", err)
	}
	if !claimed {
		t.Fatal("claimAlarm() = false for a new punishment replacing an expired one; want true")
	}

	row, err := dal.Alarm.GetByUserAndCode(7, "CODE-RENEW")
	if err != nil {
		t.Fatalf("GetByUserAndCode() error: %v", err)
	}
	if row == nil || row.BusinessID != "BIZ-NEW" {
		t.Fatalf("stored row = %+v, want business id BIZ-NEW", row)
	}

	// The next run of the same crawl must not push the new punishment again.
	claimed, err = processor.claimAlarm(alarmForClaim(7, "BIZ-NEW", "CODE-RENEW", &future))
	if err != nil {
		t.Fatalf("second claimAlarm() error: %v", err)
	}
	if claimed {
		t.Fatal("second claimAlarm() = true for the same punishment; want false")
	}
}

// TestAlarmClaimKeepsInForcePunishment verifies a different punishment that is
// still in force does not replace the existing claim.
func TestAlarmClaimKeepsInForcePunishment(t *testing.T) {
	db := setupAlarmClaimDB(t)
	processor := &InfoProcessor{ctx: testBotContext("")}

	future := time.Now().AddDate(1, 0, 0).Truncate(time.Second)
	if err := db.Create(alarmForClaim(7, "BIZ-OLD", "CODE-KEEP", &future)).Error; err != nil {
		t.Fatalf("insert in-force alarm: %v", err)
	}

	claimed, err := processor.claimAlarm(alarmForClaim(7, "BIZ-NEW", "CODE-KEEP", &future))
	if err != nil {
		t.Fatalf("claimAlarm() error: %v", err)
	}
	if claimed {
		t.Fatal("claimAlarm() = true while another punishment is still in force; want false")
	}

	row, err := dal.Alarm.GetByUserAndCode(7, "CODE-KEEP")
	if err != nil {
		t.Fatalf("GetByUserAndCode() error: %v", err)
	}
	if row == nil || row.BusinessID != "BIZ-OLD" {
		t.Fatalf("stored row = %+v, want the existing business id BIZ-OLD", row)
	}
}
