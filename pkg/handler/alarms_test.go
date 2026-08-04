package handler

import (
	"os"
	"sync"
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
	f := "./alarm_claim_test.db"
	defer func() { _ = os.Remove(f) }()
	db, err := gorm.Open(sqlite.Open(f), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(&model.Alarm{})
	dal.SetDefault(db)

	userId := int64(3333)
	makeAlarm := func() *model.Alarm {
		return &model.Alarm{
			UserID:     userId,
			BusinessID: "BIZ-CLAIM",
			CreditName: "测试公司",
			CreditCode: "CODE-CLAIM",
			StartDate:  time.Now(),
			EndDate:    &[]time.Time{time.Time{}}[0],
			PageUrl1:   "http://example.com",
			NoticeID:   "N-CLAIM",
		}
	}

	// claim runs the same critical section used by processAlarms: fast-path
	// IsExist, then claim via InsertIfAbsent; returns how many sends happened.
	claim := func() (sent int) {
		alarm := makeAlarm()
		exist, err := dal.Alarm.IsExist(userId, alarm.CreditCode)
		if err != nil || exist {
			return 0
		}
		inserted, err := dal.Alarm.InsertIfAbsent(alarm)
		if err != nil || !inserted {
			return 0
		}
		return 1 // this instance won the claim and would send the message
	}

	var sentTotal int32
	var wg sync.WaitGroup
	// 30 concurrent "instances" all racing for the same alarm
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if claim() == 1 {
				sentTotal++
			}
		}()
	}
	wg.Wait()

	if sentTotal != 1 {
		t.Fatalf("expected exactly 1 send across instances, got %d", sentTotal)
	}

	// The claim must be visible to later runs: another attempt is skipped.
	if sent := claim(); sent != 0 {
		t.Fatalf("expected the claimed alarm to be skipped on the next run, got send=%d", sent)
	}

	// Rollback (as done on send failure) must allow retry.
	if err := dal.Alarm.Remove(userId, "CODE-CLAIM"); err != nil {
		t.Fatal(err)
	}
	if sent := claim(); sent != 1 {
		t.Fatalf("expected the alarm to be claimable again after rollback, got send=%d", sent)
	}
}

// TestHistoryClaimPreventsDuplicateAcrossInstances verifies that the DB
// primary key (user_id, url) acts as a distributed lock for project pushes:
// many concurrent "instances" can never both send the same project URL.
func TestHistoryClaimPreventsDuplicateAcrossInstances(t *testing.T) {
	f := "./history_claim_test.db"
	defer func() { _ = os.Remove(f) }()
	db, err := gorm.Open(sqlite.Open(f), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(&model.History{})
	dal.SetDefault(db)

	userId := int64(4444)
	url := "http://example.com/project/1"

	// claim runs the same critical section used by processProjects.
	claim := func() (won bool) {
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

	var winners int32
	var wg sync.WaitGroup
	// 30 concurrent "instances" all racing for the same URL
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if claim() {
				winners++
			}
		}()
	}
	wg.Wait()

	if winners != 1 {
		t.Fatalf("expected exactly 1 claim across instances, got %d", winners)
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
