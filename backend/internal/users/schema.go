package users

import (
	"errors"

	"gorm.io/gorm"
)

func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("users database is nil")
	}
	if err := db.AutoMigrate(&User{}); err != nil {
		return err
	}
	return nil
}
