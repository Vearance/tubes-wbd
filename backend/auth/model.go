package auth

import "time"

type User struct {
	ID        				uint       `gorm:"primaryKey" json:"id"`
	Username  				string     `gorm:"uniqueIndex;not null" json:"username"`
	PasswordHash 			string     `gorm:"not null" json:"-"`
	IsAdmin      			bool       `gorm:"not null;default:false" json:"is_admin"`
	DisabledAt  			*time.Time `json:"-"`
	CreatedAt    			time.Time  `json:"created_at"`
	UpdatedAt    			time.Time  `json:"updated_at"`
}

type Session struct {
	ID                     	uint       `gorm:"primaryKey" json:"-"`
	UserID                 	uint       `gorm:"index;not null" json:"user_id"`
	TokenHash              	string     `gorm:"uniqueIndex;not null;size:43" json:"-"`
	PreviousTokenHash      	*string    `gorm:"size:43" json:"-"`
	PreviousTokenExpiresAt 	*time.Time `json:"-"`
	CreatedAt              	time.Time  `json:"created_at"`
	LastActivityAt         	time.Time  `json:"last_activity_at"`
	LastRotatedAt          	time.Time  `json:"last_rotated_at"`
	ExpiresAt              	time.Time  `json:"expires_at"`
	RevokedAt              	*time.Time `json:"-"`
	CSRFTokenHash          	string     `gorm:"not null;size:43" json:"-"`
}
