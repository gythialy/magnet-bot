package entities

import (
	"time"

	"gorm.io/gorm/clause"

	"gorm.io/gorm"
)

type History struct {
	UserId    int64  `gorm:"primaryKey;autoIncrement:false"`
	Url       string `gorm:"primaryKey;autoIncrement:false"`
	Title     string
	UpdatedAt time.Time
}

type HistoryDao struct {
	db *gorm.DB
}

func NewHistoryDao(db *gorm.DB) *HistoryDao {
	return &HistoryDao{db: db}
}

func (h *HistoryDao) Clean() error {
	date := time.Now().AddDate(0, 0, -7)
	if err := h.db.Where("updated_at < ?", date).Delete(&History{}).Error; err != nil {
		return err
	}
	return nil
}

//func (h *HistoryDao) Cache(userId int64) map[string]History {
//	result := make(map[string]History)
//	var tmp []History
//	if err := h.db.Where("user_id = ? and updated_at > ?", userId, time.Now().Add(-24*time.Hour)).Find(&tmp).Error; err == nil {
//		for _, history := range tmp {
//			result[history.Url] = history
//		}
//	}
//	return result
//}

func (h *HistoryDao) List(userId int64) []History {
	var result []History
	if err := h.db.Where("user_id = ?", userId).Find(&result).Error; err == nil {
		return result
	}
	return nil
}

// IsUrlExist checks if the given URL exists in the history table for the given user.
//
// This function returns true if the URL exists, false otherwise.
func (h *HistoryDao) IsUrlExist(userId int64, url string) bool {
	var count int64
	if err := h.db.Model(&History{}).
		Where("user_id = ? AND url = ?", userId, url).
		Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func (h *HistoryDao) Insert(data []*History) (error, int64) {
	if tx := h.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "url"}},
		DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
	}).Create(&data); tx.Error != nil {
		return tx.Error, 0
	} else {
		return nil, tx.RowsAffected
	}
}

func (h *HistoryDao) SearchByTitle(userId int64, title string, page, pageSize int) ([]History, int64) {
	var result []History
	var total int64

	query := h.db.Where("user_id = ? AND title LIKE ?", userId, "%"+title+"%")

	// Get total count
	query.Model(&History{}).Count(&total)

	// Get paginated results
	if err := query.Order("updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&result).Error; err != nil {
		return nil, 0
	}

	return result, total
}

func (h *HistoryDao) Count(userId int64) int64 {
	var count int64
	h.db.Model(&History{}).Where("user_id = ?", userId).Count(&count)
	return count
}
