package dal

import (
	"time"

	"github.com/gythialy/magnet/pkg/model"
	"gorm.io/gorm/clause"
)

var emptyTime, _ = time.Parse("2006-01-02 15:04:05-07:00", "0001-01-01 00:00:00+00:00")

func (a *alarm) Clean() error {
	now := time.Now()
	if _, err := a.Where(a.EndDate.Neq(emptyTime), a.EndDate.Lt(now)).Delete(); err != nil {
		return err
	}
	return nil
}

func (a *alarm) Cache(userId int64) map[string]*model.Alarm {
	result := make(map[string]*model.Alarm)
	now := time.Now()
	if alarms, err := a.Where(a.UserID.Eq(userId), a.Where(a.EndDate.Gt(now)).Or(a.EndDate.Eq(emptyTime))).Find(); err == nil {
		for _, alarm := range alarms {
			result[alarm.CreditCode] = alarm
		}
	}
	return result
}

func (a *alarm) List(userId int64, page, pageSize int) ([]*model.Alarm, int64) {
	offset := (page - 1) * pageSize

	if result, total, err := a.Where(a.UserID.Eq(userId)).FindByPage(offset, pageSize); err == nil {
		return result, total
	} else {
		return nil, 0
	}
}

func (a *alarm) Insert(data []*model.Alarm) error {
	if err := a.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: a.UserID.ColumnName().String()}, {Name: a.CreditCode.ColumnName().String()}},
		UpdateAll: true,
	}).CreateInBatches(data, batchSize); err != nil {
		return err
	} else {
		return nil
	}
}
