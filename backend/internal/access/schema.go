package access

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

const GroupLeaderMembershipConstraint = "fk_groups_leader_membership"
const ensureGroupLeaderMembershipSQL = `
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1 FROM pg_constraint WHERE conname = '` + GroupLeaderMembershipConstraint + `'
	) THEN
		ALTER TABLE groups
			ADD CONSTRAINT ` + GroupLeaderMembershipConstraint + `
			FOREIGN KEY (id, leader_user_id)
			REFERENCES user_groups (group_id, user_id)
			ON DELETE RESTRICT;
	END IF;
END
$$`

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
	if err := db.Exec(ensureGroupLeaderMembershipSQL).Error; err != nil {
		return fmt.Errorf("adding group leader membership constraint: %w", err)
	}

	return nil
}
