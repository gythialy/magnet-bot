package dal

import (
	"time"

	"github.com/gythialy/magnet/pkg/model"
)

// keywordNoticeRetention bounds the dedupe log: a notice that has left the
// crawl window can no longer be matched again, so older rows are useless.
const keywordNoticeRetention = 30 * 24 * time.Hour

// MarkKeywordMatch records that keywordID matched url and reports whether the
// pair is new. The primary key makes it idempotent, so the caller can count
// distinct notices per keyword without a read-modify-write race.
func MarkKeywordMatch(keywordID int32, url string) (bool, error) {
	return InsertIfAbsent(Q.db, &model.KeywordNotice{
		KeywordID: keywordID,
		URL:       url,
		CreatedAt: time.Now(),
	}, "keyword_id", "url")
}

// PruneKeywordMatches drops dedupe rows older than the retention window and
// reports how many were removed.
func PruneKeywordMatches(now time.Time) (int64, error) {
	res := Q.db.Where("created_at < ?", now.Add(-keywordNoticeRetention)).
		Delete(&model.KeywordNotice{})
	return res.RowsAffected, res.Error
}
