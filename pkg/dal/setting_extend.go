package dal

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gythialy/magnet/pkg/model"
)

// GetSetting reads one settings row. The boolean is false when the key has never
// been written, which lets a caller distinguish "not configured" from an empty
// value without treating a missing row as an error.
//
// A failed read is returned: silently treating it as "not configured" would let
// a broken database flip the access mode, and the access cache must not cache a
// guess.
func GetSetting(key string) (string, bool, error) {
	var setting model.Setting
	err := Q.db.Where("key = ?", key).Take(&setting).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "", false, nil
	case err != nil:
		return "", false, err
	}
	return setting.Value, true, nil
}

// SetSetting writes one settings row, creating it on first use.
//
// The write invalidates the access cache: the access mode is cached for the same
// TTL as user rows, and switching it must take effect on the next request rather
// than up to TTL later.
func SetSetting(key, value string) error {
	// An empty key can only be a programming error; storing it would create a
	// row no reader could ever find deliberately.
	if key == "" {
		return errors.New("dal: setting key is empty")
	}

	setting := &model.Setting{
		Key:       key,
		Value:     value,
		UpdatedAt: time.Now(),
	}

	// Upsert through the (key) primary key. Create+OnConflict works on both
	// SQLite and MySQL, unlike a dialect-specific "INSERT OR REPLACE".
	err := Q.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(setting).Error
	if err != nil {
		return err
	}

	User.Invalidate()
	return nil
}
