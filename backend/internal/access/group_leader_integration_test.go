//go:build integration

package access_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"tubes-wbd/internal/access"
	userdomain "tubes-wbd/internal/users"
)

func requireForeignKeyViolation(t *testing.T, err error, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "expected a PostgreSQL error, got %v", err)
	require.Equal(t, "23503", pgErr.Code)
	require.Equal(t, constraint, pgErr.ConstraintName)
}

func createLeaderFixture(t *testing.T, db *gorm.DB) (access.Group, userdomain.User) {
	t.Helper()
	user := userdomain.User{Username: "leader", PasswordHash: "x"}
	require.NoError(t, db.Create(&user).Error)
	group := access.Group{Name: "Student Council"}
	require.NoError(t, db.Create(&group).Error)
	return group, user
}

func TestIntegrationGroupLeaderMustBeMember(t *testing.T) {
	db := openPlatformDB(t)
	group, user := createLeaderFixture(t, db)

	err := db.Model(&group).Update("leader_user_id", user.ID).Error
	requireForeignKeyViolation(t, err, access.GroupLeaderMembershipConstraint)

	require.NoError(t, db.Create(&access.UserGroup{UserID: user.ID, GroupID: group.ID}).Error)
	require.NoError(t, db.Model(&group).Update("leader_user_id", user.ID).Error)

	var stored access.Group
	require.NoError(t, db.First(&stored, group.ID).Error)
	require.NotNil(t, stored.LeaderUserID)
	require.Equal(t, user.ID, *stored.LeaderUserID)
}

func TestIntegrationGroupLeaderMembershipCannotBeRemovedWhileLeader(t *testing.T) {
	db := openPlatformDB(t)
	group, user := createLeaderFixture(t, db)
	membership := access.UserGroup{UserID: user.ID, GroupID: group.ID}
	require.NoError(t, db.Create(&membership).Error)
	require.NoError(t, db.Model(&group).Update("leader_user_id", user.ID).Error)

	err := db.Delete(&access.UserGroup{}, membership.ID).Error
	requireForeignKeyViolation(t, err, access.GroupLeaderMembershipConstraint)

	require.NoError(t, db.Model(&group).Update("leader_user_id", nil).Error)
	require.NoError(t, db.Delete(&access.UserGroup{}, membership.ID).Error)
}

func TestIntegrationGroupLeaderIsScopedToItsGroup(t *testing.T) {
	db := openPlatformDB(t)
	group, user := createLeaderFixture(t, db)
	other := access.Group{Name: "Team Spica"}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&access.UserGroup{UserID: user.ID, GroupID: other.ID}).Error)

	// Membership of another group does not satisfy the constraint.
	err := db.Model(&group).Update("leader_user_id", user.ID).Error
	requireForeignKeyViolation(t, err, access.GroupLeaderMembershipConstraint)
}

func TestIntegrationAccessAutoMigrateIsRepeatable(t *testing.T) {
	db := openPlatformDB(t)
	require.NoError(t, access.AutoMigrate(db))
}
