package dal

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gythialy/magnet/pkg/model"
	"gorm.io/gorm"
)

func setupPendingAttachmentTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/pending_attachment.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.PendingAttachment{}); err != nil {
		t.Fatal(err)
	}
	SetDefault(db)
	return db
}

func countPendingAttachments(t *testing.T, db *gorm.DB) int64 {
	t.Helper()

	var count int64
	if err := db.Model(&model.PendingAttachment{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

// TestEnqueueAttachmentUpserts guards the retry queue's dedupe: a notice has at
// most one pending attachment per user, and enqueueing it again points at the
// newest announced message with a fresh retry budget.
func TestEnqueueAttachmentUpserts(t *testing.T) {
	db := setupPendingAttachmentTestDB(t)
	now := time.Now().Truncate(time.Second)

	first := &model.PendingAttachment{
		UserID:        1,
		URL:           "https://example.com/notice/1",
		ReplyTo:       10,
		Title:         "合成公告 A",
		TenderCode:    "SYN-1",
		Attempts:      3,
		NextAttemptAt: now.Add(time.Minute),
		LastError:     "connection refused",
		CreatedAt:     now,
	}
	if err := EnqueueAttachment(first); err != nil {
		t.Fatal(err)
	}

	second := &model.PendingAttachment{
		UserID:        1,
		URL:           "https://example.com/notice/1",
		ReplyTo:       20,
		Title:         "合成公告 A",
		TenderCode:    "SYN-1",
		NextAttemptAt: now.Add(2 * time.Minute),
		CreatedAt:     now,
	}
	if err := EnqueueAttachment(second); err != nil {
		t.Fatal(err)
	}

	if got := countPendingAttachments(t, db); got != 1 {
		t.Fatalf("rows = %d, want 1", got)
	}

	var rows []model.PendingAttachment
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row.ReplyTo != 20 {
		t.Errorf("reply_to = %d, want 20", row.ReplyTo)
	}
	if row.Attempts != 0 {
		t.Errorf("attempts = %d, want the retry budget reset to 0", row.Attempts)
	}
	if !row.NextAttemptAt.Equal(now.Add(2 * time.Minute)) {
		t.Errorf("next_attempt_at = %s, want %s", row.NextAttemptAt, now.Add(2*time.Minute))
	}
}

// TestDueAttachmentsSkipsFutureAndLeasedRows keeps the sweep from retrying too
// early or stealing a row another instance is already delivering.
func TestDueAttachmentsSkipsFutureAndLeasedRows(t *testing.T) {
	setupPendingAttachmentTestDB(t)
	now := time.Now()

	rows := []*model.PendingAttachment{
		{UserID: 1, URL: "https://example.com/due", NextAttemptAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Hour)},
		{UserID: 2, URL: "https://example.com/future", NextAttemptAt: now.Add(time.Hour), CreatedAt: now},
		{
			UserID: 3, URL: "https://example.com/leased",
			NextAttemptAt: now.Add(-time.Minute), LockedUntil: now.Add(time.Minute), CreatedAt: now,
		},
	}
	for _, row := range rows {
		if err := EnqueueAttachment(row); err != nil {
			t.Fatal(err)
		}
	}

	due, err := DueAttachments(now, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].URL != "https://example.com/due" {
		t.Fatalf("due = %+v, want only https://example.com/due", due)
	}
}

// TestClaimAttachmentIsExclusive guards the lease: two workers must never
// deliver the same attachment twice, and an expired lease must become
// claimable again.
func TestClaimAttachmentIsExclusive(t *testing.T) {
	setupPendingAttachmentTestDB(t)
	now := time.Now()

	if err := EnqueueAttachment(&model.PendingAttachment{
		UserID: 1, URL: "https://example.com/notice/1",
		NextAttemptAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	claimed, err := ClaimAttachment(1, "https://example.com/notice/1", now, now.Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("first ClaimAttachment() = %v, %v; want true, nil", claimed, err)
	}

	claimed, err = ClaimAttachment(1, "https://example.com/notice/1", now, now.Add(time.Minute))
	if err != nil || claimed {
		t.Fatalf("second ClaimAttachment() = %v, %v; want false, nil", claimed, err)
	}

	later := now.Add(2 * time.Minute)
	claimed, err = ClaimAttachment(1, "https://example.com/notice/1", later, later.Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("ClaimAttachment() after the lease = %v, %v; want true, nil", claimed, err)
	}
}

// TestRescheduleAndCompleteAttachment keeps the queue consistent after a failed
// attempt and clears it once the attachment is delivered.
func TestRescheduleAndCompleteAttachment(t *testing.T) {
	db := setupPendingAttachmentTestDB(t)
	now := time.Now()

	if err := EnqueueAttachment(&model.PendingAttachment{
		UserID: 1, URL: "https://example.com/notice/1",
		NextAttemptAt: now, LockedUntil: now.Add(time.Minute), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	next := now.Add(5 * time.Minute)
	if err := RescheduleAttachment(1, "https://example.com/notice/1", next, 1, "boom"); err != nil {
		t.Fatal(err)
	}

	var row model.PendingAttachment
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", row.Attempts)
	}
	if row.LastError != "boom" {
		t.Errorf("last_error = %q, want boom", row.LastError)
	}
	if !row.NextAttemptAt.Equal(next) {
		t.Errorf("next_attempt_at = %s, want %s", row.NextAttemptAt, next)
	}
	if row.LockedUntil.After(now) {
		t.Errorf("locked_until = %s, want the lease released", row.LockedUntil)
	}

	if err := CompleteAttachment(1, "https://example.com/notice/1"); err != nil {
		t.Fatal(err)
	}
	if got := countPendingAttachments(t, db); got != 0 {
		t.Fatalf("rows = %d, want 0", got)
	}
}
