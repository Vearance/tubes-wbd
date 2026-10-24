//go:build integration

package access_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"gorm.io/gorm"

	"tubes-wbd/backend/access"
	"tubes-wbd/backend/auth"
	"tubes-wbd/backend/database/platformdb"
	"tubes-wbd/backend/manageddb"
)

func openPlatformDB(t *testing.T) *gorm.DB {
	t.Helper()
	ctx := context.Background()

	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("platform"),
		tcpostgres.WithUsername("platform"),
		tcpostgres.WithPassword("platform_password"),
		tcpostgres.BasicWaitStrategies(),
	)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	require.NoError(t, err)

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := platformdb.Open(ctx, platformdb.Config{DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { _ = platformdb.Close(db) })

	require.NoError(t, auth.AutoMigrate(db))
	require.NoError(t, manageddb.AutoMigrate(db))
	require.NoError(t, access.AutoMigrate(db))
	return db
}

func TestIntegrationDatabasePermissionsDriveEffectivePermission(t *testing.T) {
	db := openPlatformDB(t)
	ctx := context.Background()

	users := auth.NewGormUserRepository(db)
	repo := access.NewGormRepository(db)
	groups, err := access.NewGroupService(users, repo)
	require.NoError(t, err)
	authz, err := access.NewAuthorizationService(users, repo)
	require.NoError(t, err)

	user := &auth.User{Username: "jennifer", PasswordHash: "x"}
	require.NoError(t, users.Create(ctx, user))
	target := &manageddb.ManagedDatabase{
		Name: "shop", Host: "db", Port: 5432, Username: "u",
		EncryptedPassword: []byte("x"), DBName: "shop",
	}
	require.NoError(t, db.Create(target).Error)

	readers, err := groups.CreateGroup(ctx, "readers")
	require.NoError(t, err)
	editors, err := groups.CreateGroup(ctx, "editors")
	require.NoError(t, err)
	require.NoError(t, groups.AddMember(ctx, readers.ID, user.ID))
	require.NoError(t, groups.AddMember(ctx, editors.ID, user.ID))

	effective := func() access.PermissionLevel {
		t.Helper()
		level, err := authz.EffectivePermission(ctx, user.ID, target.ID)
		require.NoError(t, err)
		return level
	}
	require.Equal(t, access.PermissionNone, effective())
	require.ErrorIs(t, authz.RequireView(ctx, user.ID, target.ID), access.ErrInsufficientPermission)

	_, err = groups.SetDatabasePermission(ctx, readers.ID, target.ID, access.PermissionViewOnly)
	require.NoError(t, err)
	require.Equal(t, access.PermissionViewOnly, effective())
	require.NoError(t, authz.RequireView(ctx, user.ID, target.ID))
	require.ErrorIs(t, authz.RequireEdit(ctx, user.ID, target.ID), access.ErrInsufficientPermission)

	_, err = groups.SetDatabasePermission(ctx, editors.ID, target.ID, access.PermissionViewEdit)
	require.NoError(t, err)
	require.Equal(t, access.PermissionViewEdit, effective(), "the highest group permission wins")

	_, err = groups.SetDatabasePermission(ctx, editors.ID, target.ID, access.PermissionViewOnly)
	require.NoError(t, err)
	require.Equal(t, access.PermissionViewOnly, effective(), "setting again must replace, not duplicate")
	list, err := groups.ListDatabasePermissions(ctx, editors.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, access.PermissionViewOnly, list[0].Permission)

	require.NoError(t, groups.RemoveDatabasePermission(ctx, editors.ID, target.ID))
	require.NoError(t, groups.RemoveDatabasePermission(ctx, readers.ID, target.ID))
	require.Equal(t, access.PermissionNone, effective())
	require.ErrorIs(t, groups.RemoveDatabasePermission(ctx, readers.ID, target.ID), access.ErrPermissionNotFound)
}

func TestIntegrationDatabasePermissionForUnknownDatabase(t *testing.T) {
	db := openPlatformDB(t)
	ctx := context.Background()

	users := auth.NewGormUserRepository(db)
	groups, err := access.NewGroupService(users, access.NewGormRepository(db))
	require.NoError(t, err)
	group, err := groups.CreateGroup(ctx, "readers")
	require.NoError(t, err)

	_, err = groups.SetDatabasePermission(ctx, group.ID, 999, access.PermissionViewOnly)

	require.ErrorIs(t, err, manageddb.ErrNotFound)
}
