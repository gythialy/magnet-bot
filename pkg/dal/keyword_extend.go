package dal

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gythialy/magnet/pkg/model"
)

// Insert adds the keywords that do not exist yet and returns the ones it
// actually stored as a comma-separated list.
//
// It is a compatibility wrapper around InsertMany: the command side has always
// treated a storage failure as "nothing added" and has no error path to report
// it on, so the error is still swallowed here. New callers that need the real
// failure use InsertMany directly.
func (k *keyword) Insert(keywords []string, userId int64, t model.KeywordType) string {
	added, _, err := k.InsertMany(userId, t, keywords)
	if err != nil || len(added) == 0 {
		return ""
	}
	return strings.Join(added, ", ")
}

// InsertMany stores the keywords the user does not have yet under the given
// type and reports what was created and what was already there.
//
// Input is trimmed and blank entries are dropped, which is the behaviour the
// command path has always relied on through Insert. Duplicates inside one batch
// are reported as skipped so `added` never names the same keyword twice. The
// error is returned instead of swallowed so an API caller can answer 500 while
// the command wrapper keeps its old "" result.
func (k *keyword) InsertMany(userId int64, t model.KeywordType, keywords []string) (added, skipped []string, err error) {
	// Initialised (not nil) so JSON encodes [] rather than null even when the
	// batch was entirely duplicates.
	added = make([]string, 0, len(keywords))
	skipped = make([]string, 0, len(keywords))
	if len(keywords) == 0 {
		return added, skipped, nil
	}

	candidates := make([]string, 0, len(keywords))
	seen := make(map[string]struct{}, len(keywords))
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		if _, ok := seen[keyword]; ok {
			skipped = append(skipped, keyword)
			continue
		}
		seen[keyword] = struct{}{}
		candidates = append(candidates, keyword)
	}
	if len(candidates) == 0 {
		return added, skipped, nil
	}

	// One query for the whole batch replaces the per-keyword COUNT the previous
	// implementation issued. Batches are bounded by the API cap, and a round
	// trip per keyword was the expensive part.
	existing, err := k.Where(k.UserID.Eq(userId), k.Type.Eq(int32(t)), k.Keyword.In(candidates...)).Find()
	if err != nil {
		return nil, nil, err
	}
	exists := make(map[string]struct{}, len(existing))
	for _, keyword := range existing {
		exists[keyword.Keyword] = struct{}{}
	}

	data := make([]*model.Keyword, 0, len(candidates))
	for _, keyword := range candidates {
		if _, ok := exists[keyword]; ok {
			skipped = append(skipped, keyword)
			continue
		}
		added = append(added, keyword)
		data = append(data, &model.Keyword{
			Keyword: keyword,
			UserID:  userId,
			Type:    int32(t),
		})
	}
	if len(data) == 0 {
		return added, skipped, nil
	}
	if err := k.CreateInBatches(data, batchSize); err != nil {
		return nil, nil, err
	}
	return added, skipped, nil
}

// DeleteOwned deletes one keyword by id only while it belongs to the user.
// The ownership predicate is part of the DELETE, not a read followed by a
// write, so a crafted id can never remove another account's row.
//
// The boolean reports whether a row was actually removed, which is what lets
// the API answer 404 for a foreign or already-deleted id instead of a
// misleading 204.
func (k *keyword) DeleteOwned(userId int64, id int32) (bool, error) {
	info, err := k.Where(k.UserID.Eq(userId), k.ID.Eq(id)).Delete()
	if err != nil {
		return false, err
	}
	return info.RowsAffected > 0, nil
}

// UpdateOwned renames one keyword by id only while it belongs to the user and
// reports whether a row matched.
//
// A zero RowsAffected cannot be read as "not found": MySQL reports 0 for an
// update that sets the value it already had, so a miss triggers one ownership-
// scoped count to tell "unchanged" from "missing". The count is built from a
// fresh Where chain rather than the one used by the UPDATE: a DAO value must not
// be assumed reusable after a statement ran.
func (k *keyword) UpdateOwned(userId int64, id int32, keyword string) (bool, error) {
	info, err := k.Where(k.UserID.Eq(userId), k.ID.Eq(id)).Update(k.Keyword, keyword)
	if err != nil {
		return false, err
	}
	if info.RowsAffected > 0 {
		return true, nil
	}

	count, err := k.Where(k.UserID.Eq(userId), k.ID.Eq(id)).Count()
	if err != nil {
		return false, err
	}
	return count > 0, nil
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
