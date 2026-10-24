//go:build integration

package manageddb_test

import (
	"bytes"
	"context"
	"testing"
	"time"
	"github.com/stretchr/testify/require"
	"tubes-wbd/backend/database/targetdb"
	"tubes-wbd/backend/manageddb"
)

func newRow(name string) *manageddb.ManagedDatabase {
	return &manageddb.ManagedDatabase{
		Name:              name,
		Host:              "db-a",
		Port:              5432,
		Username:          "target",
		EncryptedPassword: []byte("ciphertext"),
		DBName:            name,
		SSLMode:           "disable",
	}
}

func TestIntegrationRepositoryCRUD(t *testing.T) {
	repo := manageddb.NewGormRepository(openPlatformDB(t))
	ctx := context.Background()

	first := newRow("shop")
	require.NoError(t, repo.Create(ctx, first))
	require.NotZero(t, first.ID)
	second := newRow("warehouse")
	require.NoError(t, repo.Create(ctx, second))

	got, err := repo.FindByID(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, "shop", got.Name)
	require.Equal(t, []byte("ciphertext"), got.EncryptedPassword)

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, first.ID, list[0].ID)
	require.Equal(t, second.ID, list[1].ID)

	createdUpdatedAt := got.UpdatedAt
	time.Sleep(10 * time.Millisecond)
	got.Host = "db-b"
	got.EncryptedPassword = []byte("new-ciphertext")
	require.NoError(t, repo.Update(ctx, got))

	updated, err := repo.FindByID(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, "db-b", updated.Host)
	require.Equal(t, []byte("new-ciphertext"), updated.EncryptedPassword)
	require.True(t, updated.UpdatedAt.After(createdUpdatedAt), "updated_at must change")
	require.True(t, updated.CreatedAt.Equal(got.CreatedAt), "created_at must not change")

	require.NoError(t, repo.Delete(ctx, first.ID))
	_, err = repo.FindByID(ctx, first.ID)
	require.ErrorIs(t, err, manageddb.ErrNotFound)
}

func TestIntegrationRepositoryEmptyList(t *testing.T) {
	repo := manageddb.NewGormRepository(openPlatformDB(t))

	list, err := repo.List(context.Background())
	require.NoError(t, err)
	require.NotNil(t, list)
	require.Empty(t, list)
}

func TestIntegrationRepositoryDuplicateName(t *testing.T) {
	repo := manageddb.NewGormRepository(openPlatformDB(t))
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, newRow("shop")))
	require.ErrorIs(t, repo.Create(ctx, newRow("shop")), manageddb.ErrDuplicateName)

	other := newRow("warehouse")
	require.NoError(t, repo.Create(ctx, other))
	other.Name = "shop"
	require.ErrorIs(t, repo.Update(ctx, other), manageddb.ErrDuplicateName)
}

func TestIntegrationRepositoryUnknownID(t *testing.T) {
	repo := manageddb.NewGormRepository(openPlatformDB(t))
	ctx := context.Background()

	_, err := repo.FindByID(ctx, 999)
	require.ErrorIs(t, err, manageddb.ErrNotFound)
	require.ErrorIs(t, repo.Delete(ctx, 999), manageddb.ErrNotFound)

	ghost := newRow("ghost")
	ghost.ID = 999
	require.ErrorIs(t, repo.Update(ctx, ghost), manageddb.ErrNotFound)

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Empty(t, list, "update of an unknown id must not insert a row")
}

func TestIntegrationServiceToConnector(t *testing.T) {
	db, ctr := startPlatformDB(t)
	ctx := context.Background()

	host, err := ctr.Host(ctx)
	require.NoError(t, err)
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)

	cipher, err := manageddb.NewCipher(bytes.Repeat([]byte{1}, manageddb.KeySize))
	require.NoError(t, err)
	repo := manageddb.NewGormRepository(db)
	source, err := manageddb.NewSource(repo, cipher)
	require.NoError(t, err)
	manager := targetdb.NewManager(source, targetdb.Options{})
	t.Cleanup(manager.Close)
	svc, err := manageddb.NewService(repo, cipher, manager)
	require.NoError(t, err)

	in := manageddb.Input{
		Name:     "self",
		Host:     host,
		Port:     port.Num(),
		Username: "platform",
		Password: "platform_password",
		DBName:   "platform",
		SSLMode:  "disable",
	}
	require.NoError(t, svc.TestConnection(ctx, in))

	d, err := svc.Create(ctx, in)
	require.NoError(t, err)
	require.NotContains(t, string(d.EncryptedPassword), "platform_password")
	require.NoError(t, svc.TestStored(ctx, d.ID))

	pool, err := manager.Pool(ctx, d.ID)
	require.NoError(t, err)
	require.NoError(t, pool.Ping(ctx))

	in.Password = "wrong-password"
	_, err = svc.Update(ctx, d.ID, in)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return pool.Ping(ctx) != nil },
		5*time.Second, 20*time.Millisecond, "update must close the cached pool")
	_, err = manager.Pool(ctx, d.ID)
	require.ErrorIs(t, err, targetdb.ErrAuthFailed)

	require.NoError(t, svc.Delete(ctx, d.ID))
	_, err = manager.Pool(ctx, d.ID)
	require.ErrorIs(t, err, targetdb.ErrUnknownDatabase)
}
