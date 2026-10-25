//go:build integration

package manageddb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"gorm.io/gorm"

	"tubes-wbd/backend/access"
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

	require.NoError(t, manageddb.AutoMigrate(db))
	require.NoError(t, access.AutoMigrate(db))
	return db
}

func startTargetDB(t *testing.T) manageddb.Input {
	t.Helper()
	ctx := context.Background()

	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("shop"),
		tcpostgres.WithUsername("shop_owner"),
		tcpostgres.WithPassword("shop_password"),
		tcpostgres.BasicWaitStrategies(),
	)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	require.NoError(t, err)

	host, err := ctr.Host(ctx)
	require.NoError(t, err)
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	return manageddb.Input{
		Name:     "shop",
		Host:     host,
		Port:     port.Num(),
		Username: "shop_owner",
		Password: "shop_password",
		DBName:   "shop",
		SSLMode:  "disable",
	}
}

func TestIntegrationPermissionsCascadeOnDelete(t *testing.T) {
	db := openPlatformDB(t)

	require.True(t, db.Migrator().HasConstraint(&access.GroupDatabasePermission{}, "ManagedDatabase"))

	target := manageddb.ManagedDatabase{
		Name:              "shop",
		Host:              "db-a",
		Port:              5432,
		Username:          "target",
		EncryptedPassword: []byte("ciphertext"),
		DBName:            "shop",
	}
	require.NoError(t, db.Create(&target).Error)
	require.Equal(t, "prefer", target.SSLMode, "sslmode must default to prefer")

	group := access.Group{Name: "finance"}
	require.NoError(t, db.Create(&group).Error)

	permission := access.GroupDatabasePermission{
		GroupID:           group.ID,
		ManagedDatabaseID: target.ID,
		Permission:        access.PermissionViewOnly,
	}
	require.NoError(t, db.Create(&permission).Error)

	require.NoError(t, db.Delete(&manageddb.ManagedDatabase{}, target.ID).Error)

	var remaining int64
	require.NoError(t, db.Model(&access.GroupDatabasePermission{}).
		Where("managed_database_id = ?", target.ID).
		Count(&remaining).Error)
	require.Zero(t, remaining, "permissions must be deleted with their managed database")
}

func TestIntegrationPermissionRequiresExistingDatabase(t *testing.T) {
	db := openPlatformDB(t)

	group := access.Group{Name: "finance"}
	require.NoError(t, db.Create(&group).Error)

	err := db.Create(&access.GroupDatabasePermission{
		GroupID:           group.ID,
		ManagedDatabaseID: 999,
		Permission:        access.PermissionViewOnly,
	}).Error
	require.Error(t, err)
}
