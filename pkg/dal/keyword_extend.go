package dal

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gen/field"

	"github.com/gythialy/magnet/pkg/model"
)

// Insert adds the keywords that do not exist yet and returns the ones it
// actually stored.
func (k *keyword) Insert(keywords []string, userId int64, t model.KeywordType) string {
	var data []*model.Keyword
	var result []string
	seen := make(map[string]struct{}, len(keywords))
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		// Drop duplicates inside the same command before hitting the DB.
		if _, ok := seen[kw]; ok {
			continue
		}
		seen[kw] = struct{}{}

		e := &model.Keyword{
			Keyword: kw,
			UserID:  userId,
			Type:    int32(t),
		}
		count, err := k.Where(field.Attrs(e)).Count()
		if err != nil || count > 0 {
			continue
		}
		data = append(data, e)
		result = append(result, kw)
	}
	if len(data) == 0 {
		return ""
	}
	if err := k.CreateInBatches(data, batchSize); err != nil {
		return ""
	}
	return strings.Join(result, ", ")
}

// DeleteByIds deletes the user's keywords by ID and returns the deleted
// "id:keyword" pairs. IDs owned by another user are ignored, so a crafted
// command cannot delete someone else's keywords.
func (k *keyword) DeleteByIds(userId int64, ids string) (string, error) {
	var dbIds []int32
	for raw := range strings.SplitSeq(ids, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if i, err := strconv.ParseInt(raw, 10, 32); err == nil {
			dbIds = append(dbIds, int32(i))
		}
	}
	if len(dbIds) == 0 {
		return "", nil
	}

	records, err := k.Where(k.UserID.Eq(userId), k.ID.In(dbIds...)).Find()
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "", nil
	}

	result := make([]string, 0, len(records))
	for _, r := range records {
		result = append(result, fmt.Sprintf("%d:%s", *r.ID, r.Keyword))
	}
	if _, err := k.Delete(records...); err != nil {
		return "", err
	}
	return strings.Join(result, ";"), nil
}

func (k *keyword) GetByUserIdAndType(userId int64, t model.KeywordType) []*model.Keyword {
	result, err := k.Where(k.UserID.Eq(userId), k.Type.Eq(int32(t))).Find()
	if err != nil {
		return nil
	}
	return result
}

func (k *keyword) GetKeywords(userId int64, t model.KeywordType) []string {
	result := k.GetByUserIdAndType(userId, t)
	seen := make(map[string]struct{}, len(result))
	keywords := make([]string, 0, len(result))
	for _, value := range result {
		if _, ok := seen[value.Keyword]; ok {
			continue
		}
		seen[value.Keyword] = struct{}{}
		keywords = append(keywords, value.Keyword)
	}
	return keywords
}

func (k *keyword) Ids() []int64 {
	result, err := k.Distinct(k.UserID).Find()
	if err != nil {
		return nil
	}
	ids := make([]int64, 0, len(result))
	for _, m := range result {
		ids = append(ids, m.UserID)
	}
	return ids
}

// UpdateCounter increments counter of the given keyword.
func (k *keyword) UpdateCounter(id, counter int32) error {
	_, err := k.Where(k.ID.Eq(id)).UpdateSimple(k.Counter, k.Counter.Add(counter))
	return err
}

// CountByUserId returns how many keywords of one type the user has.
func (k *keyword) CountByUserId(userId int64, t model.KeywordType) int64 {
	c, err := k.Where(k.UserID.Eq(userId), k.Type.Eq(int32(t))).Count()
	if err != nil {
		return 0
	}
	return c
}

// EditById renames the user's keywords. Every entry is "id=keyword"; entries
// that do not parse, or whose ID belongs to another user, are reported back.
func (k *keyword) EditById(userId int64, content []string) error {
	var errs []error
	for idx, entry := range content {
		idText, keyword, ok := strings.Cut(entry, "=")
		if !ok {
			errs = append(errs, fmt.Errorf("invalid entry %d: %s", idx, entry))
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSpace(idText), 10, 32)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid id at entry %d: %s", idx, entry))
			continue
		}
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		info, err := k.Where(k.UserID.Eq(userId), k.ID.Eq(int32(id))).Update(k.Keyword, keyword)
		if err != nil {
			errs = append(errs, fmt.Errorf("update keyword %d: %w", id, err))
			continue
		}
		if info.RowsAffected == 0 {
			errs = append(errs, fmt.Errorf("keyword %d not found", id))
		}
	}

	return errors.Join(errs...)
}
