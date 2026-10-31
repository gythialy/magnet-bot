package handler

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/utils"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	// attachmentSweepInterval is how often due attachments are looked up.
	attachmentSweepInterval = time.Minute
	// attachmentClaimLease reserves a row for one sweep. It has to stay above
	// the conversion timeout, otherwise a slow conversion could be stolen and
	// delivered twice.
	attachmentClaimLease = gotenbergHTTPTimeout + time.Minute
	// attachmentSendGap keeps consecutive uploads under Telegram's flood limit.
	attachmentSendGap = 3 * time.Second
	// attachmentBatchSize caps one sweep; a long outage can leave many rows.
	attachmentBatchSize = 10
	// attachmentTTL drops rows that kept failing for longer than this.
	attachmentTTL = 24 * time.Hour
)

// attachmentBackoff is the delay before each retry; its length is also the
// maximum number of attempts, i.e. six tries over roughly four hours.
var attachmentBackoff = []time.Duration{
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
	time.Hour,
	2 * time.Hour,
}

// pdfConverter and documentSender are the only collaborators the retrier needs.
// Keeping them as interfaces lets the tests drive the retry policy without a
// real Gotenberg instance or a Telegram API.
type pdfConverter interface {
	FetchPDF(ctx context.Context, u string) ([]byte, error)
}

type documentSender interface {
	SendDocument(ctx context.Context, params *bot.SendDocumentParams) (*models.Message, error)
}

// AttachmentRetrier re-sends the PDF attachments that could not be delivered
// while a notice's text message was pushed.
//
// A notice whose table is too complex to flatten is announced as a placeholder
// plus an attachment; when the conversion or the upload fails the text has
// already reached the user, so the claim is not rolled back. The pending row is
// retried here with a backoff instead, which turns a Gotenberg outage into a
// delayed attachment instead of a silently missing one.
type AttachmentRetrier struct {
	convert  pdfConverter
	sender   documentSender
	logger   *utils.Logger
	now      func() time.Time
	interval time.Duration
	sendGap  time.Duration
	batch    int
	maxTries int
	ttl      time.Duration
}

// NewAttachmentRetrier wires the retrier to the bot context. A missing bot or
// Gotenberg client leaves the corresponding collaborator nil and disables the
// sweeps, which keeps a conversion-less deployment (or a partial test context)
// from panicking.
func NewAttachmentRetrier(ctx *BotContext) *AttachmentRetrier {
	r := &AttachmentRetrier{
		logger:   ctx.Logger,
		now:      time.Now,
		interval: attachmentSweepInterval,
		sendGap:  attachmentSendGap,
		batch:    attachmentBatchSize,
		maxTries: len(attachmentBackoff),
		ttl:      attachmentTTL,
	}
	if ctx.Gotenberg != nil {
		r.convert = ctx.Gotenberg
	}
	if ctx.Bot != nil {
		r.sender = ctx.Bot
	}
	return r
}

// Start sweeps until ctx is cancelled. The first sweep runs immediately so a
// restart after an outage re-sends the backlog without waiting a full interval.
func (r *AttachmentRetrier) Start(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		r.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce sends every attachment that is due, oldest deadline first.
func (r *AttachmentRetrier) RunOnce(ctx context.Context) {
	if r.convert == nil || r.sender == nil {
		return
	}

	rows, err := dal.DueAttachments(r.now(), r.batch)
	if err != nil {
		r.logger.Error().Stack().Err(err).Msg("list pending attachments")
		return
	}

	enforced := backgroundAccessEnforced()
	skipped := 0
	attempts := 0
	for i := range rows {
		if ctx.Err() != nil {
			return
		}
		// Authorisation is checked before the lease is taken. Claiming a row
		// only to drop it would hold the lease until it expired, hiding the
		// attachment from a later sweep even after the account was allowed
		// again. Leaving the row untouched keeps the queue honest: it stays in
		// pending_attachments, keeps its attempt count and is delivered
		// automatically by the first sweep after access is restored. That is
		// deliberate — revoking access pauses delivery, it does not discard a
		// conversion the user already triggered.
		if enforced && !dal.User.IsAllowed(rows[i].UserID) {
			skipped++
			continue
		}
		if attempts > 0 && !sleepContext(ctx, r.sendGap) {
			return
		}
		r.deliver(ctx, &rows[i])
		attempts++
	}

	// One summary line per sweep, so an operator can see a paused backlog
	// without per-row noise.
	if skipped > 0 {
		r.logger.Debug().
			Int("skipped", skipped).
			Int("due", len(rows)).
			Msg("skipped attachments for users without access")
	}
}

// deliver converts and uploads one pending attachment. It only sends when it
// wins the row's lease, so instances sharing a database never deliver the same
// attachment twice.
func (r *AttachmentRetrier) deliver(ctx context.Context, row *model.PendingAttachment) {
	now := r.now()
	claimed, err := dal.ClaimAttachment(row.UserID, row.URL, now, now.Add(attachmentClaimLease))
	if err != nil {
		r.logger.Error().Stack().Err(err).Str("url", row.URL).Msg("claim pending attachment")
		return
	}
	if !claimed {
		return // another sweep or instance owns it
	}

	convertCtx, cancel := context.WithTimeout(ctx, gotenbergHTTPTimeout)
	defer cancel()

	data, err := r.convert.FetchPDF(convertCtx, row.URL)
	if err != nil {
		r.retryLater(row, err)
		return
	}

	params := &bot.SendDocumentParams{
		ChatID: row.UserID,
		Document: &models.InputFileUpload{
			Filename: attachmentFilename(row.TenderCode, row.Title),
			Data:     bytes.NewReader(data),
		},
		Caption: truncateCaption(row.Title),
	}
	if row.ReplyTo != 0 {
		params.ReplyParameters = &models.ReplyParameters{MessageID: row.ReplyTo}
	}

	sent, err := r.sender.SendDocument(ctx, params)
	if err != nil && row.ReplyTo != 0 && isReplyTargetGone(err) {
		// The announced message is gone; the attachment is still worth sending.
		params.ReplyParameters = nil
		sent, err = r.sender.SendDocument(ctx, params)
	}
	if err != nil {
		r.retryLater(row, err)
		return
	}

	if err := dal.CompleteAttachment(row.UserID, row.URL); err != nil {
		r.logger.Error().Stack().Err(err).Str("url", row.URL).Msg("complete pending attachment")
		return
	}
	r.logger.Info().
		Int64("chatId", row.UserID).
		Str("url", row.URL).
		Int("attempts", row.Attempts+1).
		Int("attachmentId", messageID(sent)).
		Msg("resent notice pdf")
}

// retryLater schedules the next attempt, honours a Telegram rate limit and
// gives up — dropping the row — once the budget or the TTL is exhausted.
func (r *AttachmentRetrier) retryLater(row *model.PendingAttachment, cause error) {
	now := r.now()
	lastErr := cause.Error()

	var limited *bot.TooManyRequestsError
	if errors.As(cause, &limited) && limited.RetryAfter > 0 {
		next := now.Add(time.Duration(limited.RetryAfter) * time.Second)
		if err := dal.RescheduleAttachment(row.UserID, row.URL, next, row.Attempts, lastErr); err != nil {
			r.logger.Error().Stack().Err(err).Str("url", row.URL).Msg("reschedule pending attachment")
		}
		return
	}

	attempts := row.Attempts + 1
	// Telegram rejected the request or the chat itself: the same payload will
	// never succeed, so stop before converting the page again for every retry.
	if errors.Is(cause, bot.ErrorBadRequest) ||
		errors.Is(cause, bot.ErrorForbidden) ||
		errors.Is(cause, bot.ErrorUnauthorized) {
		r.giveUp(row, attempts, lastErr)
		return
	}
	if attempts >= r.maxTries || now.Sub(row.CreatedAt) > r.ttl {
		r.giveUp(row, attempts, lastErr)
		return
	}

	next := now.Add(attachmentBackoff[attempts])
	if err := dal.RescheduleAttachment(row.UserID, row.URL, next, attempts, lastErr); err != nil {
		r.logger.Error().Stack().Err(err).Str("url", row.URL).Msg("reschedule pending attachment")
	}
}

// giveUp drops the row and records why the attachment will never be delivered.
func (r *AttachmentRetrier) giveUp(row *model.PendingAttachment, attempts int, lastErr string) {
	if err := dal.CompleteAttachment(row.UserID, row.URL); err != nil {
		r.logger.Error().Stack().Err(err).Str("url", row.URL).Msg("drop pending attachment")
	}
	r.logger.Warn().
		Int64("chatId", row.UserID).
		Str("url", row.URL).
		Int("attempts", attempts).
		Str("lastError", lastErr).
		Msg("giving up on notice pdf")
}

// isReplyTargetGone reports whether an upload failed only because the message
// it replied to no longer exists. Telegram answers such a request with 400.
func isReplyTargetGone(err error) bool {
	if !errors.Is(err, bot.ErrorBadRequest) {
		return false
	}
	// "Bad Request: message to be replied not found"; keep the match narrow so
	// a permanent 400 (file too big, bad file name) is not retried as if only
	// the reply target had vanished.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "repl") && strings.Contains(msg, "not found")
}

// messageID tolerates a sender that reports success without a message.
func messageID(sent *models.Message) int {
	if sent == nil {
		return 0
	}
	return sent.ID
}

// sleepContext waits for d or until ctx is cancelled, reporting whether the
// wait completed.
func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
