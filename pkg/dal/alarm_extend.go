package dal

import (
	"time"

	"github.com/gythialy/magnet/pkg/model"
	"gorm.io/gorm/clause"
)

// emptyTime is how a punishment without an end date is stored: year 1, UTC.
//
// It is deliberately parsed from the exact text the SQLite driver writes for a
// zero time.Time instead of using time.Time{} directly. Both values are the
// same instant and the driver serialises them identically today, but they are
// not == (the zero value carries no location), and an earlier incident came
// from assuming the two were interchangeable.
var emptyTime, _ = time.Parse("2006-01-02 15:04:05-07:00", "0001-01-01 00:00:00+00:00")

func (a *alarm) Clean() error {
	now := time.Now()
	_, err := a.Where(a.EndDate.Neq(emptyTime), a.EndDate.Lt(now)).Delete()
	return err
}

func (a *alarm) IsExist(userId int64, creditCode string) (bool, error) {
	now := time.Now()
	count, err := a.Where(a.UserID.Eq(userId),
		a.CreditCode.Eq(creditCode),
		a.Where(a.EndDate.Gt(now)).Or(a.EndDate.Eq(emptyTime))).Count()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *alarm) GetById(userId int64, businessId string) (*model.Alarm, error) {
	return a.Where(a.BusinessID.Eq(businessId)).Where(a.UserID.Eq(userId)).First()
}

func (a *alarm) SearchByName(userId int64, term string, page, pageSize int) ([]*model.Alarm, int64) {
	offset := (page - 1) * pageSize

	query := a.Where(a.UserID.Eq(userId))
	if term != "" {
		query = query.Where(a.CreditName.Like("%" + term + "%"))
	}
	result, total, err := query.Order(a.StartDate.Desc()).FindByPage(offset, pageSize)
	if err != nil {
		return nil, 0
	}
	return result, total
}

// SearchPaged returns one page of a user's alarm records, most recent start
// date first, plus the total number matching the same filter.
//
// term is an optional substring of credit_name, the same field SearchByName
// searches; empty means no filter. It exists next to SearchByName because the
// Mini App pages with an offset while the command path pages with a 1-based
// page number, and it returns the error instead of swallowing it so the
// endpoint can distinguish "empty" from "broken".
func (a *alarm) SearchPaged(userId int64, term string, offset, limit int) ([]*model.Alarm, int64, error) {
	query := a.Where(a.UserID.Eq(userId))
	if term != "" {
		query = query.Where(a.CreditName.Like("%" + term + "%"))
	}

	result, total, err := query.Order(a.StartDate.Desc()).FindByPage(offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

func (a *alarm) Insert(data []*model.Alarm) error {
	return a.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: a.UserID.ColumnName().String()}, {Name: a.CreditCode.ColumnName().String()}},
		UpdateAll: true,
	}).CreateInBatches(data, batchSize)
}

// InsertIfAbsent claims an alarm via the (user_id, credit_code) unique
// constraint: it inserts the row only when none exists and reports whether
// this call created it, so concurrent callers can never both push the same
// alarm. See dal.InsertIfAbsent.
func (a *alarm) InsertIfAbsent(alarm *model.Alarm) (bool, error) {
	return InsertIfAbsent(a.UnderlyingDB(), alarm,
		a.UserID.ColumnName().String(), a.CreditCode.ColumnName().String())
}

// Remove deletes an alarm record. It is used to roll back a claim when the
// message failed to send, so the next run can retry it.
func (a *alarm) Remove(userId int64, creditCode string) error {
	_, err := a.Where(a.UserID.Eq(userId), a.CreditCode.Eq(creditCode)).Delete()
	return err
}
