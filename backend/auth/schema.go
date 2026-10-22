package auth

import (
	"errors"

	"gorm.io/gorm"
)

func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("auth database is nil")
	}

	if err := db.AutoMigrate(&User{}, &Session{}); err != nil {
		return err
	}

	return nil
}
