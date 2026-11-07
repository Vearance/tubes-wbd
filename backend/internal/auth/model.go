package auth

import (
	"time"

	"tubes-wbd/internal/users"
)

type User = users.User

type Session struct {
	ID                     uint       `gorm:"primaryKey" json:"-"`
	UserID                 uint       `gorm:"index;not null" json:"user_id"`
	TokenHash              string     `gorm:"uniqueIndex;not null;size:43" json:"-"`
	PreviousTokenHash      *string    `gorm:"size:43" json:"-"`
	PreviousTokenExpiresAt *time.Time `json:"-"`
	CreatedAt              time.Time  `json:"created_at"`
	LastActivityAt         time.Time  `json:"last_activity_at"`
	LastRotatedAt          time.Time  `json:"last_rotated_at"`
	ExpiresAt              time.Time  `json:"expires_at"`
	RevokedAt              *time.Time `json:"-"`
	RotationRequired       bool       `gorm:"not null;default:false" json:"-"`
	CSRFTokenHash          string     `gorm:"not null;size:43" json:"-"`
}
