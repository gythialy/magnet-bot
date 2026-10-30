package handler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/utils"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupPendingAttachmentDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/pending_attachment.db"), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.PendingAttachment{}); err != nil {
		t.Fatal(err)
	}
	dal.SetDefault(db)
	return db
}

type fakeClock struct {
	at time.Time
}

func (c *fakeClock) Now() time.Time { return c.at }

func (c *fakeClock) Advance(d time.Duration) { c.at = c.at.Add(d) }

type fakeConverter struct {
	data  []byte
	err   error
	calls []string
}

func (f *fakeConverter) FetchPDF(_ context.Context, u string) ([]byte, error) {
	f.calls = append(f.calls, u)
	return f.data, f.err
}

type fakeSender struct {
	errs  []error
	calls []*bot.SendDocumentParams
}

func (f *fakeSender) SendDocument(_ context.Context, params *bot.SendDocumentParams) (*models.Message, error) {
	f.calls = append(f.calls, params)
	if len(f.errs) == 0 {
		return &models.Message{ID: 42}, nil
	}
	err := f.errs[0]
	f.errs = f.errs[1:]
	if err != nil {
		return nil, err
	}
	return &models.Message{ID: 42}, nil
}

func newAttachmentRetrierForTest(t *testing.T, clock *fakeClock, convert pdfConverter, sender documentSender) *AttachmentRetrier {
	t.Helper()

	nop := zerolog.Nop()
	return &AttachmentRetrier{
		convert:  convert,
		sender:   sender,
		logger:   &utils.Logger{Logger: &nop},
		now:      clock.Now,
		interval: time.Minute,
		sendGap:  0,
		batch:    attachmentBatchSize,
		maxTries: len(attachmentBackoff),
		ttl:      attachmentTTL,
	}
}

// enqueueDue inserts one due notice and fails the test if the queue rejects it.
func enqueueDue(t *testing.T, clock *fakeClock, attempts int) *model.PendingAttachment {
	t.Helper()

	row := &model.PendingAttachment{
		UserID:        7,
		URL:           "https://example.com/notice/1",
		ReplyTo:       99,
		Title:         "合成设备采购公告",
		TenderCode:    "SYN-0001",
		Attempts:      attempts,
		NextAttemptAt: clock.Now().Add(-time.Minute),
		CreatedAt:     clock.Now().Add(-time.Hour),
	}
	if err := dal.EnqueueAttachment(row); err != nil {
		t.Fatal(err)
	}
	return row
}

func pendingRows(t *testing.T, db *gorm.DB) []model.PendingAttachment {
	t.Helper()

	var rows []model.PendingAttachment
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestAttachmentRetrierDeliversDueAttachment covers the happy path: a queued
// attachment is converted, sent as a reply to the announced message and
// removed from the queue.
func TestAttachmentRetrierDeliversDueAttachment(t *testing.T) {
	db := setupPendingAttachmentDB(t)
	clock := &fakeClock{at: time.Now().Truncate(time.Second)}
	enqueueDue(t, clock, 0)

	convert := &fakeConverter{data: []byte("%PDF-1.4 synthetic")}
	sender := &fakeSender{}
	r := newAttachmentRetrierForTest(t, clock, convert, sender)

	r.RunOnce(context.Background())

	if len(convert.calls) != 1 || convert.calls[0] != "https://example.com/notice/1" {
		t.Fatalf("converted = %v, want the notice url once", convert.calls)
	}
	if len(sender.calls) != 1 {
		t.Fatalf("uploads = %d, want 1", len(sender.calls))
	}

	call := sender.calls[0]
	// ChatID is an any: it also accepts @channel usernames, so compare the value.
	if chatID, ok := call.ChatID.(int64); !ok || chatID != 7 {
		t.Errorf("chat id = %v, want 7", call.ChatID)
	}
	if call.ReplyParameters == nil || call.ReplyParameters.MessageID != 99 {
		t.Errorf("reply parameters = %+v, want message 99", call.ReplyParameters)
	}
	if call.Caption != "合成设备采购公告" {
		t.Errorf("caption = %q, want the notice title", call.Caption)
	}
	upload, ok := call.Document.(*models.InputFileUpload)
	if !ok {
		t.Fatalf("document = %T, want *models.InputFileUpload", call.Document)
	}
	if upload.Filename != "SYN-0001.pdf" {
		t.Errorf("filename = %q, want SYN-0001.pdf", upload.Filename)
	}
	if rows := pendingRows(t, db); len(rows) != 0 {
		t.Fatalf("pending rows = %d, want the delivered row removed", len(rows))
	}
}

// TestAttachmentRetrierReschedulesOnFailure guards the backoff: a failed
// delivery stays queued, counts the attempt and releases the lease so the next
// sweep can try again.
func TestAttachmentRetrierReschedulesOnFailure(t *testing.T) {
	db := setupPendingAttachmentDB(t)
	clock := &fakeClock{at: time.Now().Truncate(time.Second)}
	enqueueDue(t, clock, 0)

	convert := &fakeConverter{err: errors.New("dial tcp 10.0.0.5:3000: connect: connection refused")}
	sender := &fakeSender{}
	r := newAttachmentRetrierForTest(t, clock, convert, sender)

	r.RunOnce(context.Background())

	if len(sender.calls) != 0 {
		t.Fatalf("uploads = %d, want none after a failed conversion", len(sender.calls))
	}

	rows := pendingRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("pending rows = %d, want the row kept for a retry", len(rows))
	}
	row := rows[0]
	if row.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", row.Attempts)
	}
	want := clock.Now().Add(attachmentBackoff[1])
	if !row.NextAttemptAt.Equal(want) {
		t.Errorf("next_attempt_at = %s, want %s", row.NextAttemptAt, want)
	}
	if row.LastError == "" {
		t.Error("last_error is empty, want the conversion failure")
	}
	if row.LockedUntil.After(clock.Now()) {
		t.Errorf("locked_until = %s, want the lease released", row.LockedUntil)
	}
}

// TestAttachmentRetrierGivesUpAfterMaxAttempts keeps a permanently failing row
// from being retried forever.
func TestAttachmentRetrierGivesUpAfterMaxAttempts(t *testing.T) {
	db := setupPendingAttachmentDB(t)
	clock := &fakeClock{at: time.Now().Truncate(time.Second)}
	enqueueDue(t, clock, len(attachmentBackoff)-1)

	convert := &fakeConverter{err: errors.New("still unavailable")}
	r := newAttachmentRetrierForTest(t, clock, convert, &fakeSender{})

	r.RunOnce(context.Background())

	if len(convert.calls) != 1 {
		t.Fatalf("converted %d times, want the final attempt", len(convert.calls))
	}
	if rows := pendingRows(t, db); len(rows) != 0 {
		t.Fatalf("pending rows = %d, want the row dropped", len(rows))
	}
}

// TestAttachmentRetrierRetriesWithoutReply covers a deleted announcement: the
// attachment is still worth sending, so the reply target is dropped.
func TestAttachmentRetrierRetriesWithoutReply(t *testing.T) {
	db := setupPendingAttachmentDB(t)
	clock := &fakeClock{at: time.Now().Truncate(time.Second)}
	enqueueDue(t, clock, 0)

	convert := &fakeConverter{data: []byte("%PDF-1.4 synthetic")}
	sender := &fakeSender{errs: []error{
		fmt.Errorf("%w, %s", bot.ErrorBadRequest, "message to reply not found"),
		nil,
	}}
	r := newAttachmentRetrierForTest(t, clock, convert, sender)

	r.RunOnce(context.Background())

	if len(sender.calls) != 2 {
		t.Fatalf("uploads = %d, want the reply attempt and the fallback", len(sender.calls))
	}
	if sender.calls[1].ReplyParameters != nil {
		t.Errorf("fallback reply parameters = %+v, want none", sender.calls[1].ReplyParameters)
	}
	if rows := pendingRows(t, db); len(rows) != 0 {
		t.Fatalf("pending rows = %d, want the delivered row removed", len(rows))
	}
}

// TestAttachmentRetrierDropsPermanentlyRejectedUpload covers the uploads that
// can never succeed: Telegram rejected the file or the chat. The row is dropped
// right away instead of converting the notice again for every retry.
func TestAttachmentRetrierDropsPermanentlyRejectedUpload(t *testing.T) {
	cases := map[string]error{
		"file too big":  fmt.Errorf("%w, %s", bot.ErrorBadRequest, "file is too big"),
		"bot blocked":   fmt.Errorf("%w, %s", bot.ErrorForbidden, "bot was blocked by the user"),
		"bad bot token": fmt.Errorf("%w, %s", bot.ErrorUnauthorized, "Unauthorized"),
	}

	for name, failure := range cases {
		t.Run(name, func(t *testing.T) {
			db := setupPendingAttachmentDB(t)
			clock := &fakeClock{at: time.Now().Truncate(time.Second)}
			enqueueDue(t, clock, 0)

			sender := &fakeSender{errs: []error{failure}}
			r := newAttachmentRetrierForTest(t, clock, &fakeConverter{data: []byte("%PDF-1.4 synthetic")}, sender)

			r.RunOnce(context.Background())

			if len(sender.calls) != 1 {
				t.Fatalf("uploads = %d, want a single attempt", len(sender.calls))
			}
			if rows := pendingRows(t, db); len(rows) != 0 {
				t.Fatalf("pending rows = %d, want the row dropped", len(rows))
			}
		})
	}
}

// TestAttachmentRetrierHonoursRateLimit reschedules by Telegram's retry_after
// without spending an attempt.
func TestAttachmentRetrierHonoursRateLimit(t *testing.T) {
	db := setupPendingAttachmentDB(t)
	clock := &fakeClock{at: time.Now().Truncate(time.Second)}
	enqueueDue(t, clock, 0)

	sender := &fakeSender{errs: []error{&bot.TooManyRequestsError{Message: "too many requests", RetryAfter: 30}}}
	r := newAttachmentRetrierForTest(t, clock, &fakeConverter{data: []byte("%PDF-1.4 synthetic")}, sender)

	r.RunOnce(context.Background())

	rows := pendingRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("pending rows = %d, want the row kept", len(rows))
	}
	if rows[0].Attempts != 0 {
		t.Errorf("attempts = %d, want a rate limit to cost no attempt", rows[0].Attempts)
	}
	want := clock.Now().Add(30 * time.Second)
	if !rows[0].NextAttemptAt.Equal(want) {
		t.Errorf("next_attempt_at = %s, want %s", rows[0].NextAttemptAt, want)
	}
}

// TestAttachmentRetrierSkipsLeasedRow proves the sweep never steals a row that
// another instance is delivering.
func TestAttachmentRetrierSkipsLeasedRow(t *testing.T) {
	db := setupPendingAttachmentDB(t)
	clock := &fakeClock{at: time.Now().Truncate(time.Second)}
	row := enqueueDue(t, clock, 0)

	if err := db.Model(&model.PendingAttachment{}).
		Where("user_id = ? AND url = ?", row.UserID, row.URL).
		Update("locked_until", clock.Now().Add(time.Minute)).Error; err != nil {
		t.Fatal(err)
	}

	convert := &fakeConverter{data: []byte("%PDF-1.4 synthetic")}
	sender := &fakeSender{}
	r := newAttachmentRetrierForTest(t, clock, convert, sender)

	r.RunOnce(context.Background())

	if len(convert.calls) != 0 || len(sender.calls) != 0 {
		t.Fatalf("converted = %d, uploaded = %d; want neither for a leased row", len(convert.calls), len(sender.calls))
	}
	if rows := pendingRows(t, db); len(rows) != 1 {
		t.Fatalf("pending rows = %d, want the leased row kept", len(rows))
	}
}

// TestEnqueuePendingPDFQueuesFirstRetry covers the hand-off from the push
// pipeline: a failed attachment is queued with the announced message id and the
// first backoff delay, and is skipped entirely without a converter.
func TestEnqueuePendingPDFQueuesFirstRetry(t *testing.T) {
	db := setupPendingAttachmentDB(t)
	nop := zerolog.Nop()
	processor := &InfoProcessor{ctx: &BotContext{
		Gotenberg: &GotenbergClient{},
		Logger:    &utils.Logger{Logger: &nop},
	}}

	project := &Project{
		Pageurl:        "https://example.com/notice/2",
		Title:          "合成采购公告 B",
		OpenTenderCode: "SYN-0002",
	}

	before := time.Now()
	processor.enqueuePendingPDF(7, 99, project)

	rows := pendingRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("pending rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.UserID != 7 || row.URL != project.Pageurl {
		t.Errorf("row = (%d, %s), want (7, %s)", row.UserID, row.URL, project.Pageurl)
	}
	if row.ReplyTo != 99 {
		t.Errorf("reply_to = %d, want the announced message 99", row.ReplyTo)
	}
	if row.Title != project.Title || row.TenderCode != project.OpenTenderCode {
		t.Errorf("row = (%q, %q), want the notice metadata", row.Title, row.TenderCode)
	}
	if row.Attempts != 0 {
		t.Errorf("attempts = %d, want a fresh budget", row.Attempts)
	}
	delay := row.NextAttemptAt.Sub(before)
	if delay < attachmentBackoff[0] || delay > attachmentBackoff[0]+time.Minute {
		t.Errorf("next attempt in %s, want about %s", delay, attachmentBackoff[0])
	}

	// Without a converter the attachment cannot be retried, so nothing is queued.
	processor.ctx.Gotenberg = nil
	processor.enqueuePendingPDF(8, 100, project)
	if rows := pendingRows(t, db); len(rows) != 1 {
		t.Fatalf("pending rows = %d after a converter-less failure, want 1", len(rows))
	}
}

// TestAttachmentRetrierWithoutConverterIsNoOp keeps a deployment without a
// conversion service from panicking in the background loop.
func TestAttachmentRetrierWithoutConverterIsNoOp(t *testing.T) {
	setupPendingAttachmentDB(t)
	clock := &fakeClock{at: time.Now().Truncate(time.Second)}
	enqueueDue(t, clock, 0)

	sender := &fakeSender{}
	r := newAttachmentRetrierForTest(t, clock, nil, sender)

	r.RunOnce(context.Background())

	if len(sender.calls) != 0 {
		t.Fatalf("uploads = %d, want none without a converter", len(sender.calls))
	}
}
