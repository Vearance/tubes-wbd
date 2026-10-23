package manageddb

import (
	"errors"
	"gorm.io/gorm"
)

func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("managed database schema: db is nil")
	}

	return db.AutoMigrate(&ManagedDatabase{})
}