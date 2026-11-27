package access

import (
	"fmt"
	"strconv"

	"gorm.io/gorm"
)

func LockDatabaseAccess(tx *gorm.DB, managedDatabaseID uint, exclusive bool) error {
	fn := "pg_advisory_xact_lock_shared"
	if exclusive {
		fn = "pg_advisory_xact_lock"
	}
	if err := tx.Exec("SELECT "+fn+"(hashtextextended(?, 0))", "dbm.database_access:"+strconv.FormatUint(uint64(managedDatabaseID), 10)).Error; err != nil {
		return fmt.Errorf("locking database access for managed database %d: %w", managedDatabaseID, err)
	}
	return nil
}
