package migration

import (
	"errors"
	"gorm.io/gorm"
)


func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("migration schema: db is nil")
	}
	return db.AutoMigrate(&Draft{}, &Run{})
}