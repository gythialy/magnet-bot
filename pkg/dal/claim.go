package dal

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InsertIfAbsent inserts value only when no row conflicting on the given
// unique columns exists, reporting whether the row was actually created.
//
// The table's unique constraint therefore acts as a distributed lock:
// concurrent callers — even across bot processes sharing the same DB — can
// never both claim the same row. The caller that gets a true return is the
// only one allowed to proceed (e.g. send a message); everyone else must skip.
//
// conflictColumns must form (a subset of) a unique index on the table, e.g.
// "user_id", "credit_code" for alarms or "user_id", "url" for histories.
func InsertIfAbsent(db *gorm.DB, value interface{}, conflictColumns ...string) (bool, error) {
	columns := make([]clause.Column, 0, len(conflictColumns))
	for _, c := range conflictColumns {
		columns = append(columns, clause.Column{Name: c})
	}

	res := db.Clauses(clause.OnConflict{
		Columns:   columns,
		DoNothing: true,
	}).Create(value)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}
