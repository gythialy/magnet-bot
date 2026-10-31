package model

import "time"

const TableNameUser = "users"

// Roles of an access-control row.
//
// The values are stable on the wire: the Mini App and the env parser both map
// them to 0/1, so reordering them would silently swap "user" and "admin".
const (
	RoleUser  int32 = 0
	RoleAdmin int32 = 1
)

// User is one person the bot can serve: a Telegram account with an authorisation
// row.
//
// The row is keyed by the Telegram user id itself (autoIncrement:false) because
// there is exactly one row per account and no synthetic identity to leak. It is
// deliberately not soft-deleted: a removed row must stop matching immediately,
// and the access cache is invalidated on every write.
type User struct {
	UserID int64 `gorm:"column:user_id;primaryKey;autoIncrement:false" json:"userId"`

	// Remark is the human-readable label an administrator may edit. It never
	// affects authorisation.
	Remark string `gorm:"column:remark;not null;default:''" json:"remark"`

	// Role is RoleUser or RoleAdmin.
	Role int32 `gorm:"column:role;not null;default:0" json:"role"`

	// Enabled gates access while access_mode=restricted. A disabled row keeps
	// its role and remark, so re-enabling restores the previous state.
	//
	// There is deliberately no `default:true` tag: GORM omits a zero-valued
	// field that carries a default, so creating a User with Enabled=false would
	// silently store true. Every insert path sets the value explicitly instead.
	Enabled bool `gorm:"column:enabled;not null" json:"enabled"`

	// FromEnv marks a row seeded by MANAGER_ID or ALLOWED_USER_IDS. Those rows
	// may only be renamed by hand while the environment variable is the source
	// of truth for role and enabled.
	FromEnv bool `gorm:"column:from_env;not null;default:false" json:"fromEnv"`

	CreatedAt time.Time `gorm:"column:created_at;not null" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null" json:"updatedAt"`

	// LastSeen is written opportunistically on request handling and is therefore
	// nullable: a seeded user who never wrote to the bot has no value.
	LastSeen *time.Time `gorm:"column:last_seen" json:"lastSeen"`
}

// TableName User's table name.
func (*User) TableName() string {
	return TableNameUser
}
