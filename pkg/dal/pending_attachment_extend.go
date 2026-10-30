package dal

import (
	"time"

	"github.com/gythialy/magnet/pkg/model"
	"gorm.io/gorm/clause"
)

// EnqueueAttachment records a notice whose PDF attachment still has to be
// delivered.
//
// The (user_id, url) primary key makes this an upsert: a notice that fails
// again — a forced re-push, for instance — refreshes the existing row, resets
// its retry budget and points at the newest announced message instead of
// queueing a second attachment for the same page.
func EnqueueAttachment(a *model.PendingAttachment) error {
	return Q.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "url"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"reply_to",
			"title",
			"tender_code",
			"attempts",
			"next_attempt_at",
			"locked_until",
			"last_error",
		}),
	}).Create(a).Error
}

// DueAttachments returns the pending attachments that are due at now and not
// claimed by another worker, oldest deadline first.
func DueAttachments(now time.Time, limit int) ([]model.PendingAttachment, error) {
	rows := make([]model.PendingAttachment, 0, limit)
	err := Q.db.
		Where("next_attempt_at <= ? AND locked_until <= ?", now, now).
		Order("next_attempt_at").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// ClaimAttachment takes the lease on one pending attachment. It reports whether
// this caller won the row; a loser must leave it alone. The conditional update
// is what keeps a shared database free of duplicate attachments.
func ClaimAttachment(userID int64, url string, now, leaseUntil time.Time) (bool, error) {
	res := Q.db.Model(&model.PendingAttachment{}).
		Where("user_id = ? AND url = ? AND locked_until <= ?", userID, url, now).
		Update("locked_until", leaseUntil)
	return res.RowsAffected == 1, res.Error
}

// CompleteAttachment drops the row once the attachment has been delivered.
func CompleteAttachment(userID int64, url string) error {
	return Q.db.Where("user_id = ? AND url = ?", userID, url).
		Delete(&model.PendingAttachment{}).Error
}

// RescheduleAttachment records a failed attempt and releases the lease so the
// row becomes due again at next.
func RescheduleAttachment(userID int64, url string, next time.Time, attempts int, lastErr string) error {
	res := Q.db.Model(&model.PendingAttachment{}).
		Where("user_id = ? AND url = ?", userID, url).
		Updates(map[string]any{
			"attempts":        attempts,
			"next_attempt_at": next,
			"locked_until":    time.Time{},
			"last_error":      lastErr,
		})
	return res.Error
}
