package manageddb

import (
	"context"
	"testing"
	"tubes-wbd/internal/database/targetdb"

	"github.com/stretchr/testify/require"
)

func TestNewSourceRejectsNilDependencies(t *testing.T) {
	_, err := NewSource(nil, newTestCipher(t, 1))
	require.Error(t, err)
	_, err = NewSource(newFakeRepo(), nil)
	require.Error(t, err)
}

func TestSourceConnConfig(t *testing.T) {
	svc, repo, _ := newTestManagedDBService(t)
	d, err := svc.Create(context.Background(), validInput())
	require.NoError(t, err)

	src, err := NewSource(repo, svc.cipher)
	require.NoError(t, err)

	cfg, err := src.ConnConfig(context.Background(), d.ID)
	require.NoError(t, err)
	require.Equal(t, targetdb.ConnConfig{
		Host:     "db-a",
		Port:     5432,
		User:     "target",
		Password: "target_password",
		DBName:   "shop",
		SSLMode:  "disable",
	}, cfg)
}

func TestSourceEncryptedSnapshotKeepsOriginalConnectionConfig(t *testing.T) {
	ctx := context.Background()
	service, repo, _ := newTestManagedDBService(t)
	database, err := service.Create(ctx, validInput())
	require.NoError(t, err)
	source, err := NewSource(repo, service.cipher)
	require.NoError(t, err)

	snapshot, err := source.EncryptedSnapshot(ctx, database.ID)
	require.NoError(t, err)
	require.NotContains(t, string(snapshot), "target_password")

	updated, err := repo.FindByID(ctx, database.ID)
	require.NoError(t, err)
	updated.Host = "changed-host"
	updated.Port = 6432
	updated.Username = "changed-user"
	updated.DBName = "changed-db"
	updated.SSLMode = "require"
	require.NoError(t, repo.Update(ctx, updated))

	config, err := source.ResolveSnapshot(snapshot)
	require.NoError(t, err)
	require.Equal(t, targetdb.ConnConfig{
		Host: "db-a", Port: 5432, User: "target", Password: "target_password",
		DBName: "shop", SSLMode: "disable",
	}, config)
}

func TestSourceUnknownDatabase(t *testing.T) {
	src, err := NewSource(newFakeRepo(), newTestCipher(t, 1))
	require.NoError(t, err)

	_, err = src.ConnConfig(context.Background(), 42)
	require.ErrorIs(t, err, targetdb.ErrUnknownDatabase)
}

func TestSourceCorruptedPassword(t *testing.T) {
	repo := newFakeRepo()
	d := &ManagedDatabase{Name: "shop", EncryptedPassword: []byte("not-a-ciphertext")}
	require.NoError(t, repo.Create(context.Background(), d))

	src, err := NewSource(repo, newTestCipher(t, 1))
	require.NoError(t, err)

	_, err = src.ConnConfig(context.Background(), d.ID)
	require.ErrorIs(t, err, ErrDecrypt)
	require.NotContains(t, err.Error(), "not-a-ciphertext")
}
