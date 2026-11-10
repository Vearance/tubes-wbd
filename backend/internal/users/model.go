package users

import "time"

type User struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Username     string     `gorm:"uniqueIndex;not null" json:"username"`
	PasswordHash string     `gorm:"not null" json:"-"`
	IsAdmin      bool       `gorm:"not null;default:false" json:"is_admin"`
	DisabledAt   *time.Time `json:"-"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
