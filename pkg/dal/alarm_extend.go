package dal

import (
	"errors"
	"time"

	"github.com/gythialy/magnet/pkg/model"
	"gorm.io/gorm"
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

// alarmClock is the clock behind expiry decisions. It is a package variable so
// tests can pin "now" instead of waiting for a date boundary to pass.
var alarmClock = time.Now

// AlarmInForce reports whether the punishment is still in force at now.
//
// A nil, zero or year-1 end date means "no end date". Otherwise the end date is
// the exclusive boundary the crawler filters on, so the punishment stays in
// force until that instant.
//
// The decision is taken on instants, never by letting SQLite order the stored
// text: older rows carry a UTC offset while the comparison bound is local time,
// and SQLite then compares two strings character by character. That made a
// punishment whose end date was "today" look expired from 00:00 local while the
// crawler still offered it until 08:00 local, so the claim was released and the
// same alarm was pushed again on every run.
func AlarmInForce(alarm *model.Alarm, now time.Time) bool {
	if alarm == nil {
		return false
	}
	if alarm.EndDate == nil || alarm.EndDate.IsZero() || alarm.EndDate.Equal(emptyTime) {
		return true
	}
	return alarm.EndDate.After(now)
}

// Clean removes punishments whose end date has passed.
//
// The comparison stays in SQLite on purpose: julianday() converts the timezone
// offset stored in the text, so this is an instant comparison like AlarmInForce
// while expired rows are removed by one statement. Loading the table into Go
// would cost a full scan plus one DELETE transaction per row, and ordering the
// stored text directly would be off by the offset (the duplicate-push bug).
func (a *alarm) Clean() error {
	now := alarmClock()
	// end_date <> emptyTime keeps open-ended punishments, which have no end
	// date; NULL rows never match either predicate and are kept as well.
	res := a.UnderlyingDB().
		Where("end_date <> ? AND julianday(end_date) < julianday(?)", emptyTime, now).
		Delete(&model.Alarm{})
	return res.Error
}

// GetByUserAndCode returns the claimed punishment for user+creditCode, or nil
// when the pair has no row. The claim uses it to tell one punishment apart from
// another instead of trusting the end date alone.
func (a *alarm) GetByUserAndCode(userId int64, creditCode string) (*model.Alarm, error) {
	row, err := a.Where(a.UserID.Eq(userId), a.CreditCode.Eq(creditCode)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

// IsExist reports whether the user still holds an in-force claim for the
// creditor. See AlarmInForce for why the check runs on instants.
func (a *alarm) IsExist(userId int64, creditCode string) (bool, error) {
	current, err := a.GetByUserAndCode(userId, creditCode)
	if err != nil {
		return false, err
	}
	return AlarmInForce(current, alarmClock()), nil
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
