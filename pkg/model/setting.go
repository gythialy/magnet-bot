package model

import "time"

const TableNameSetting = "settings"

// Keys and values of the settings table.
//
// Only the access mode exists so far. The constants live next to the model so a
// handler and the bootstrap code cannot drift apart on the spelling of a stored
// value: "off" written as "Off" would silently be treated as restricted.
const (
	SettingAccessMode = "access_mode"

	// AccessModeOff lets every Telegram account use the bot. It is the default
	// for a database with no administrators, so a fresh deployment is never
	// locked out before MANAGER_ID is filled in.
	AccessModeOff = "off"

	// AccessModeRestricted only serves accounts that have an enabled row in the
	// users table.
	AccessModeRestricted = "restricted"
)

// Setting is one persisted configuration value.
//
// It is a plain key/value row instead of one column per setting so adding a
// setting never needs a migration, and so an operator can inspect and edit the
// current mode with sqlite3 without knowing the schema.
type Setting struct {
	Key string `gorm:"column:key;primaryKey" json:"key"`

	Value string `gorm:"column:value;not null;default:''" json:"value"`

	UpdatedAt time.Time `gorm:"column:updated_at;not null" json:"updatedAt"`
}

// TableName Setting's table name.
func (*Setting) TableName() string {
	return TableNameSetting
}
