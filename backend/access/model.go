package access

import "time"

type Group struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UserGroup struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"not null;uniqueIndex:idx_user_group" json:"user_id"`
	GroupID   uint      `gorm:"not null;uniqueIndex:idx_user_group" json:"group_id"`
	CreatedAt time.Time `json:"created_at"`
}

type PermissionLevel uint8

const (
	PermissionNone     PermissionLevel = 0
	PermissionViewOnly PermissionLevel = 1
	PermissionViewEdit PermissionLevel = 2
)

func (p PermissionLevel) Valid() bool {
	return p == PermissionNone || p == PermissionViewOnly || p == PermissionViewEdit
}

type GroupDatabasePermission struct {
	ID                uint            `gorm:"primaryKey" json:"id"`
	GroupID           uint            `gorm:"not null;uniqueIndex:idx_group_database" json:"group_id"`
	ManagedDatabaseID uint            `gorm:"not null;uniqueIndex:idx_group_database;index" json:"managed_database_id"`
	Permission        PermissionLevel `gorm:"not null" json:"permission"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}
