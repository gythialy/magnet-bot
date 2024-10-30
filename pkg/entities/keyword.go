package entities

import (
	"strings"

	"gorm.io/gorm"
)

type KeywordType int

const (
	PROJECT KeywordType = iota
	ALARM
)

func (k KeywordType) String() string {
	names := [...]string{"PROJECT", "ALARM"}
	if k < PROJECT || k > ALARM {
		return "Unknown"
	}
	return names[k]
}

type Keyword struct {
	gorm.Model
	Keyword string
	UserId  int64
	Type    KeywordType
	Counter int `gorm:"default:0"`
}

type KeywordDao struct {
	db *gorm.DB
}

func NewKeywordDao(db *gorm.DB) *KeywordDao {
	return &KeywordDao{db: db}
}

func (k *KeywordDao) Add(keywords []string, userId int64, t KeywordType) string {
	var result []string
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		e := Keyword{
			Keyword: keyword,
			UserId:  userId,
			Type:    t,
		}
		if tx := k.db.Where(&e); tx.Error == nil && tx.RowsAffected == 0 {
			k.db.Create(&e)
			result = append(result, keyword)
		}
	}
	return strings.Join(result, ", ")
}

func (k *KeywordDao) Delete(keywords []string, userId int64, t KeywordType) string {
	var result []string
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		if err := k.db.Where("keyword = ? and user_id = ? and type = ?", keyword, userId, t).Delete(&Keyword{}).Error; err == nil {
			result = append(result, keyword)
		}
	}
	return strings.Join(result, ", ")
}

func (k *KeywordDao) List(userId int64, t KeywordType) []Keyword {
	var result []Keyword
	if err := k.db.Where("user_id = ? and type = ?", userId, t).Find(&result).Error; err == nil {
		return result
	}
	return nil
}

func (k *KeywordDao) ListKeywords(userId int64, t KeywordType) []string {
	result := k.List(userId, t)
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

// UpdateCounter  increment counter
func (k *KeywordDao) UpdateCounter(id uint, counter int64) error {
	if err := k.db.Model(&Keyword{}).
		Where("id = ?", id).
		Update("counter", gorm.Expr("counter + ?", counter)).Error; err != nil {
		return err
	}

	return nil
}

// Count Add method to get keyword stats
func (k *KeywordDao) Count(userId int64, t KeywordType) int64 {
	var count int64
	k.db.Model(&Keyword{}).Where("user_id = ? AND type = ?", userId, t).Count(&count)
	return count
}
