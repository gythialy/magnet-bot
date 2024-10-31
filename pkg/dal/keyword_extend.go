package dal

import (
	"strings"

	"gorm.io/gen/field"

	"github.com/gythialy/magnet/pkg/model"
)

func (k *keyword) Insert(keywords []string, userId int64, t model.KeywordType) string {
	var data []*model.Keyword
	var result []string
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		e := &model.Keyword{
			Keyword: kw,
			UserID:  userId,
			Type:    int32(t),
		}
		if count, err := k.Where(field.Attrs(&e)).Count(); err == nil && count == 0 {
			data = append(data, e)
			result = append(result, kw)
		}
	}
	if err := k.CreateInBatches(data, batchSize); err != nil {
		return ""
	}
	return strings.Join(result, ", ")
}

func (k *keyword) Delete(keywords []string, userId int64, t model.KeywordType) (string, error) {
	var result []string
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		result = append(result, kw)
	}
	if _, err := k.Where(k.Keyword.In(result...), k.UserID.Eq(userId), k.Type.Eq(int32(t))).Delete(); err == nil {
		return strings.Join(result, ", "), err
	} else {
		return "", err
	}
}

func (k *keyword) GetByUserIdAndType(userId int64, t model.KeywordType) []*model.Keyword {
	if result, err := k.Where(k.UserID.Eq(userId), k.Type.Eq(int32(t))).Find(); err == nil {
		return result
	}
	return nil
}

func (k *keyword) GetKeywords(userId int64, t model.KeywordType) []string {
	result := k.GetByUserIdAndType(userId, t)
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

func (k *keyword) Ids() []int64 {
	var ids []int64
	if result, err := k.Distinct(k.UserID).Find(); err == nil {
		for _, m := range result {
			ids = append(ids, m.UserID)
		}
	}
	return ids
}

// UpdateCounter  increment counter
func (k *keyword) UpdateCounter(id, counter int32) error {
	if _, err := k.Where(k.ID.Eq(id)).UpdateSimple(k.Counter, k.Counter.Add(counter)); err == nil {
		return nil
	} else {
		return err
	}
}

// CountByUserId Count Insert method to get keyword stats
func (k *keyword) CountByUserId(userId int64, t model.KeywordType) int64 {
	if c, err := k.Where(k.UserID.Eq(userId), k.Type.Eq(int32(t))).Count(); err == nil {
		return c
	} else {
		return 0
	}
}
