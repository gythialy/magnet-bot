package dal

import (
	"time"

	"github.com/gythialy/magnet/pkg/model"
	"gorm.io/gorm/clause"
)

const (
	batchSize = 30
	// defaultDays is the cleanup window (in days) for history records when
	// Clean is invoked without an explicit value.
	defaultDays = 30
)

func (h *history) IsUrlExist(userId int64, url string) (bool, error) {
	count, err := h.Where(h.UserID.Eq(userId), h.URL.Eq(url)).Count()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (h *history) Clean(days int) error {
	if days == 0 {
		days = defaultDays
	}
	date := time.Now().AddDate(0, 0, -days)
	_, err := h.Where(h.UpdatedAt.Lt(date)).Delete()
	return err
}

func (h *history) GetByUserId(userId int64) ([]*model.History, error) {
	return h.Where(h.UserID.Eq(userId)).Find()
}

func (h *history) Insert(data []*model.History) error {
	return h.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: h.UserID.ColumnName().String()}, {Name: h.URL.ColumnName().String()}},
		DoUpdates: clause.AssignmentColumns([]string{h.Title.ColumnName().String(), h.UpdatedAt.ColumnName().String()}),
	}).CreateInBatches(data, batchSize)
}

// InsertIfAbsent claims a URL via the (user_id, url) unique constraint: it
// inserts the row only when none exists and reports whether this call created
// it, so concurrent callers can never both push the same project. See
// dal.InsertIfAbsent.
func (h *history) InsertIfAbsent(history *model.History) (bool, error) {
	return InsertIfAbsent(h.UnderlyingDB(), history,
		h.UserID.ColumnName().String(), h.URL.ColumnName().String())
}

// Remove deletes a history record. It is used to roll back a claim when the
// project message failed to send, so the next run can retry it.
func (h *history) Remove(userId int64, url string) error {
	_, err := h.Where(h.UserID.Eq(userId), h.URL.Eq(url)).Delete()
	return err
}

func (h *history) SearchByTitle(userId int64, term string, page, pageSize int) ([]*model.History, int64) {
	query := h.Where(h.UserID.Eq(userId))
	if term != "" {
		// Where returns a new query; assigning keeps the filter explicit.
		query = query.Where(h.Title.Like("%" + term + "%"))
	}
	offset := (page - 1) * pageSize
	result, total, err := query.Order(h.UpdatedAt.Desc()).FindByPage(offset, pageSize)
	if err != nil {
		return nil, 0
	}
	return result, total
}

// NormalizeURLs gives the history rows written before links were absolute a
// scheme. Those rows are the (user_id, url) claim key of past pushes: leaving
// them scheme-less would make every notice pushed in the last crawl window
// look new after the upgrade and be pushed a second time.
//
// It is idempotent — rows that already contain "://" are untouched — so it is
// a cheap no-op on every start once the backfill has happened.
func (h *history) NormalizeURLs() (int64, error) {
	db := h.UnderlyingDB()

	// db.Name() is the dialector name: SQLite concatenates with "||", MySQL
	// needs CONCAT (it reads "||" as OR).
	expr := "url = 'https://' || url"
	if db.Name() == "mysql" {
		expr = "url = CONCAT('https://', url)"
	}
	res := db.Exec("UPDATE " + h.TableName() + " SET " + expr + " WHERE url NOT LIKE '%://%'")
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (h *history) CountByUserId(userId int64) int64 {
	count, err := h.Where(h.UserID.Eq(userId)).Count()
	if err != nil {
		return 0
	}
	return count
}

func (h *history) GetTodayByUserId(userId int64, page, pageSize int) ([]*model.History, int64) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	query := h.Where(h.UserID.Eq(userId), h.UpdatedAt.Gte(startOfDay))

	offset := (page - 1) * pageSize
	result, total, err := query.Order(h.UpdatedAt.Desc()).FindByPage(offset, pageSize)
	if err != nil {
		return nil, 0
	}
	return result, total
}
