package manageddb

import (
	"time"
	"tubes-wbd/backend/database/targetdb"
)

type ManagedDatabase struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	Name              string    `gorm:"uniqueIndex;not null" json:"name"`
	Host              string    `gorm:"not null" json:"host"`
	Port              uint16    `gorm:"not null" json:"port"`
	Username          string    `gorm:"not null" json:"username"`
	EncryptedPassword []byte    `gorm:"not null" json:"-"`
	DBName            string    `gorm:"not null" json:"db_name"`
	SSLMode           string    `gorm:"not null;default:prefer" json:"ssl_mode"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (d *ManagedDatabase) connConfig(password string) targetdb.ConnConfig {
	return targetdb.ConnConfig{
		Host:     d.Host,
		Port:     d.Port,
		User:     d.Username,
		Password: password,
		DBName:   d.DBName,
		SSLMode:  d.SSLMode,
	}
}