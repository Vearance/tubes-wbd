package access

import (
	"errors"

	"gorm.io/gorm"
)

func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("access database is nil")
	}

	if err := db.AutoMigrate(
		&Group{},
		&UserGroup{},
		&GroupDatabasePermission{},
	); err != nil {
		return err
	}

	return nil
}
