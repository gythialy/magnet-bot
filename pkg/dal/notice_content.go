package dal

import (
	"errors"
	"time"

	"github.com/gythialy/magnet/pkg/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// noticeContentRetention matches the history retention window: a cached body is
// only useful while the notice it belongs to is still in the reader's list.
const noticeContentRetention = 30 * 24 * time.Hour

// ErrNoticeContentMissing reports that no cached body exists for a URL. It is a
// normal cache miss, not a failure, so callers branch on it instead of logging.
var ErrNoticeContentMissing = errors.New("notice content is not cached")

// GetNoticeContent returns the cached body for url. A miss is reported with
// ErrNoticeContentMissing so a caller can distinguish "never cached" from a
// database error without string matching.
func GetNoticeContent(url string) (*model.NoticeContent, error) {
	if Q.db == nil {
		return nil, ErrNoticeContentMissing
	}

	var row model.NoticeContent
	err := Q.db.Where("url = ?", url).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNoticeContentMissing
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// SaveNoticeContent inserts or replaces one cached body. A forced refresh
// overwrites the previous copy on purpose: the reader asked for the notice as
// it is now, and keeping the old body would make the next open serve it again.
func SaveNoticeContent(url, body string, fetchedAt time.Time) error {
	if Q.db == nil {
		return errors.New("dal: database is not configured")
	}

	return Q.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "url"}},
		DoUpdates: clause.AssignmentColumns([]string{"html", "fetched_at"}),
	}).Create(&model.NoticeContent{URL: url, HTML: body, FetchedAt: fetchedAt}).Error
}

// SaveNoticeContentIfAbsent caches a body only when the URL has no row yet.
//
// The crawl-time warm-up uses this: a body fetched by a later crawl must not
// overwrite one a reader forced a refresh of, so an existing row always wins.
func SaveNoticeContentIfAbsent(url, body string, fetchedAt time.Time) (bool, error) {
	if Q.db == nil {
		return false, errors.New("dal: database is not configured")
	}

	return InsertIfAbsent(Q.db, &model.NoticeContent{
		URL:       url,
		HTML:      body,
		FetchedAt: fetchedAt,
	}, "url")
}

// PruneNoticeContents drops cached bodies that can no longer be read: those past
// the retention window, and those whose URL is in no history row (the notice was
// cleaned, and access requires ownership). It returns the number of rows removed.
//
// The second condition is what keeps the cache bounded without a second
// schedule: History.Clean deletes histories, and the next prune call collects
// the bodies those rows used to serve.
func PruneNoticeContents(now time.Time) (int64, error) {
	if Q.db == nil {
		return 0, nil
	}

	res := Q.db.Where("fetched_at < ?", now.Add(-noticeContentRetention)).
		Or("url NOT IN (SELECT url FROM histories)").
		Delete(&model.NoticeContent{})
	return res.RowsAffected, res.Error
}
