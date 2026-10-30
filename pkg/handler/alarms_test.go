package handler

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestAlarmClaimPreventsDuplicateAcrossInstances verifies that the DB primary
// key (user_id, credit_code) acts as a distributed lock: two independent
// processors — each with its own in-memory locks, simulating two bot
// instances — cannot both claim and send the same alarm.
func TestAlarmClaimPreventsDuplicateAcrossInstances(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/alarm_claim_test.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Alarm{}); err != nil {
		t.Fatal(err)
	}
	dal.SetDefault(db)

	const userId = int64(3333)
	makeAlarm := func() *model.Alarm {
		return &model.Alarm{
			UserID:     userId,
			BusinessID: "BIZ-CLAIM",
			CreditName: "测试公司",
			CreditCode: "CODE-CLAIM",
			StartDate:  time.Now(),
			EndDate:    new(time.Time),
			PageUrl1:   "http://example.com",
			NoticeID:   "N-CLAIM",
		}
	}

	// claim runs the same critical section used by processAlarms: fast-path
	// IsExist, then claim via InsertIfAbsent; returns how many sends happened.
	claim := func() (sent bool) {
		alarm := makeAlarm()
		exist, err := dal.Alarm.IsExist(userId, alarm.CreditCode)
		if err != nil || exist {
			return false
		}
		inserted, err := dal.Alarm.InsertIfAbsent(alarm)
		if err != nil || !inserted {
			return false
		}
		return true // this instance won the claim and would send the message
	}

	var sentTotal atomic.Int32
	var wg sync.WaitGroup
	// 30 concurrent "instances" all racing for the same alarm
	for range 30 {
		wg.Go(func() {
			if claim() {
				sentTotal.Add(1)
			}
		})
	}
	wg.Wait()

	if got := sentTotal.Load(); got != 1 {
		t.Fatalf("expected exactly 1 send across instances, got %d", got)
	}

	// The claim must be visible to later runs: another attempt is skipped.
	if claim() {
		t.Fatal("expected the claimed alarm to be skipped on the next run")
	}

	// Rollback (as done on send failure) must allow retry.
	if err := dal.Alarm.Remove(userId, "CODE-CLAIM"); err != nil {
		t.Fatal(err)
	}
	if !claim() {
		t.Fatal("expected the alarm to be claimable again after rollback")
	}
}

// TestHistoryClaimPreventsDuplicateAcrossInstances verifies that the DB
// primary key (user_id, url) acts as a distributed lock for project pushes:
// many concurrent "instances" can never both send the same project URL.
func TestHistoryClaimPreventsDuplicateAcrossInstances(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/history_claim_test.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.History{}); err != nil {
		t.Fatal(err)
	}
	dal.SetDefault(db)

	const (
		userId = int64(4444)
		url    = "http://example.com/project/1"
	)

	// claim runs the same critical section used by processProjects.
	claim := func() bool {
		history := &model.History{
			UserID:    userId,
			URL:       url,
			Title:     "测试项目",
			UpdatedAt: time.Now(),
		}
		inserted, err := dal.History.InsertIfAbsent(history)
		if err != nil {
			t.Error(err)
			return false
		}
		return inserted
	}

	var winners atomic.Int32
	var wg sync.WaitGroup
	// 30 concurrent "instances" all racing for the same URL
	for range 30 {
		wg.Go(func() {
			if claim() {
				winners.Add(1)
			}
		})
	}
	wg.Wait()

	if got := winners.Load(); got != 1 {
		t.Fatalf("expected exactly 1 claim across instances, got %d", got)
	}

	// A later run must see the URL as already claimed.
	if claim() {
		t.Fatal("expected the claimed URL to be skipped on the next run")
	}

	// Rollback (as done on send failure) must allow retry.
	if err := dal.History.Remove(userId, url); err != nil {
		t.Fatal(err)
	}
	if !claim() {
		t.Fatal("expected the URL to be claimable again after rollback")
	}
}
