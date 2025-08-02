package dal

import (
	"time"

	"github.com/gythialy/magnet/pkg/model"
	"gorm.io/gorm/clause"
)

const (
	batchSize = 30
	defaultDays
)

func (h *history) IsUrlExist(userId int64, url string) (bool, error) {
	if count, err := h.Where(h.UserID.Eq(userId), h.URL.Eq(url)).Count(); err == nil {
		return count > 0, nil
	} else {
		return false, err
	}
}

func (h *history) Clean(days int) error {
	if days == 0 {
		days = defaultDays
	}
	date := time.Now().AddDate(0, 0, -days)
	if _, err := h.Where(h.UpdatedAt.Lt(date)).Delete(); err != nil {
		return err
	}
	return nil
}

func (h *history) GetByUserId(userId int64) ([]*model.History, error) {
	if result, err := h.Where(h.UserID.Eq(userId)).Find(); err == nil {
		return result, nil
	} else {
		return nil, err
	}
}

func (h *history) Insert(data []*model.History) error {
	if err := h.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: h.UserID.ColumnName().String()}, {Name: h.URL.ColumnName().String()}},
		DoUpdates: clause.AssignmentColumns([]string{h.Title.ColumnName().String(), h.UpdatedAt.ColumnName().String()}),
	}).CreateInBatches(data, batchSize); err == nil {
		return nil
	} else {
		return err
	}
}

func (h *history) SearchByTitle(userId int64, term string, page, pageSize int) ([]*model.History, int64) {
	query := h.Where(h.UserID.Eq(userId))
	if term != "" {
		query.Where(h.Title.Like("%" + term + "%"))
	}
	offset := (page - 1) * pageSize
	if result, total, err := query.Order(h.UpdatedAt.Desc()).FindByPage(offset, pageSize); err == nil {
		return result, total
	} else {
		return nil, 0
	}
}

func (h *history) CountByUserId(userId int64) int64 {
	if count, err := h.Where(h.UserID.Eq(userId)).Count(); err == nil {
		return count
	} else {
		return 0
	}
}
