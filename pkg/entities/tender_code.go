package entities

import (
	"gorm.io/gorm"
	"strings"
)

type TenderCode struct {
	gorm.Model
	Code   string
	UserId int64
}
type TenderCodeDao struct {
	db *gorm.DB
}

func NewTenderCodeDao(db *gorm.DB) *TenderCodeDao {
	return &TenderCodeDao{db: db}
}

func (t *TenderCodeDao) Add(codes []string, userId int64) string {
	var result []string
	for _, code := range codes {
		e := TenderCode{
			Code:   strings.TrimSpace(code),
			UserId: userId,
		}
		if tx := t.db.Where(&e); tx.Error == nil && tx.RowsAffected == 0 {
			t.db.Create(&e)
			result = append(result, code)
		}
	}
	return strings.Join(result, ", ")
}

func (t *TenderCodeDao) Delete(codes []string, userId int64) string {
	var result []string
	for _, code := range codes {
		if err := t.db.Where("code = ? and user_id = ?", strings.TrimSpace(code), userId).Delete(&TenderCode{}).Error; err == nil {
			result = append(result, code)
		}
	}
	return strings.Join(result, ", ")
}

func (t *TenderCodeDao) List(userId int64) []TenderCode {
	var result []TenderCode
	if err := t.db.Where("user_id = ?", userId).Find(&result).Error; err == nil {
		return result
	}
	return nil
}
func (t *TenderCodeDao) ListTenderCodes(userId int64) []string {
	var result []string
	r := t.List(userId)
	m := make(map[string]bool)
	for _, value := range r {
		if _, ok := m[value.Code]; !ok {
			result = append(result, value.Code)
			m[value.Code] = true
		}
	}
	return result
}

func (t *TenderCodeDao) Ids() []int64 {
	var ids []int64
	if err := t.db.Model(&TenderCode{}).Distinct("user_id").Find(&ids).Error; err == nil {
		return ids
	}
	return nil
}
