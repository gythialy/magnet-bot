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

// KeywordLastHits returns the newest match time per keyword for one account.
//
// The dedupe log is pruned after keywordNoticeRetention, so a keyword that is
// absent has had no match since the window opened — not "never matched". The
// caller must say so in those words; the row's lifetime counter is what answers
// "never".
func KeywordLastHits(userID int64) (map[int32]time.Time, error) {
	type hit struct {
		KeywordID int32
		CreatedAt time.Time
	}

	var rows []hit
	err := Q.db.Table("keyword_notices AS kn").
		Select("kn.keyword_id AS keyword_id, kn.created_at AS created_at").
		Joins("JOIN keywords AS k ON k.id = kn.keyword_id").
		Where("k.user_id = ?", userID).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	latest := make(map[int32]time.Time, len(rows))
	for _, row := range rows {
		if row.CreatedAt.After(latest[row.KeywordID]) {
			latest[row.KeywordID] = row.CreatedAt
		}
	}
	return latest, nil
}
