package entities

import (
	"strings"

	"gorm.io/gorm"
)

type Keyword struct {
	gorm.Model
	Keyword string
	UserId  int64
}

type KeywordDao struct {
	db *gorm.DB
}

func NewKeywordDao(db *gorm.DB) *KeywordDao {
	return &KeywordDao{db: db}
}

func (k *KeywordDao) Add(keywords []string, userId int64) string {
	var result []string
	for _, keyword := range keywords {
		e := Keyword{
			Keyword: strings.TrimSpace(keyword),
			UserId:  userId,
		}
		if tx := k.db.Where(&e); tx.Error == nil && tx.RowsAffected == 0 {
			k.db.Create(&e)
			result = append(result, keyword)
		}
	}
	return strings.Join(result, ", ")
}

func (k *KeywordDao) Delete(keywords []string, userId int64) string {
	var result []string
	for _, keyword := range keywords {
		if err := k.db.Where("keyword = ? and user_id = ?", strings.TrimSpace(keyword), userId).Delete(&Keyword{}).Error; err == nil {
			result = append(result, keyword)
		}
	}
	return strings.Join(result, ", ")
}

func (k *KeywordDao) List(userId int64) []Keyword {
	var result []Keyword
	if err := k.db.Where("user_id = ?", userId).Find(&result).Error; err == nil {
		return result
	}
	return nil
}

func (k *KeywordDao) ListKeywords(userId int64) []string {
	result := k.List(userId)
	var r []string
	m := make(map[string]bool)
	for _, value := range result {
		if _, ok := m[value.Keyword]; !ok {
			m[value.Keyword] = true
			r = append(r, value.Keyword)
		}
	}
	return r
}

func (k *KeywordDao) Ids() []int64 {
	var ids []int64
	if err := k.db.Model(&Keyword{}).Distinct("user_id").Find(&ids).Error; err == nil {
		return ids
	}
	return nil
}
