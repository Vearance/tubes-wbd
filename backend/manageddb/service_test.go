package manageddb

import (
	"context"
	"testing"
	"github.com/stretchr/testify/require"
	"tubes-wbd/backend/database/targetdb"
)

type fakeRepo struct {
	rows   map[uint]ManagedDatabase
	nextID uint
}

var _ Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: map[uint]ManagedDatabase{}, nextID: 1}
}

func (r *fakeRepo) nameTaken(name string, except uint) bool {
	for id, row := range r.rows {
		if id != except && row.Name == name {
			return true
		}
	}
	return false
}

func (r *fakeRepo) Create(_ context.Context, d *ManagedDatabase) error {
	if r.nameTaken(d.Name, 0) {
		return ErrDuplicateName
	}
	d.ID = r.nextID
	r.nextID++
	r.rows[d.ID] = *d
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, id uint) (*ManagedDatabase, error) {
	row, ok := r.rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &row, nil
}

func (r *fakeRepo) List(_ context.Context) ([]ManagedDatabase, error) {
	list := []ManagedDatabase{}
	for id := uint(1); id < r.nextID; id++ {
		if row, ok := r.rows[id]; ok {
			list = append(list, row)
		}
	}
	return list, nil
}

func (r *fakeRepo) Update(_ context.Context, d *ManagedDatabase) error {
	if _, ok := r.rows[d.ID]; !ok {
		return ErrNotFound
	}
	if r.nameTaken(d.Name, d.ID) {
		return ErrDuplicateName
	}
	r.rows[d.ID] = *d
	return nil
}

func (r *fakeRepo) Delete(_ context.Context, id uint) error {
	if _, ok := r.rows[id]; !ok {
		return ErrNotFound
	}
	delete(r.rows, id)
	return nil
}

type fakeConnector struct {
	tested      []targetdb.ConnConfig
	invalidated []targetdb.DatabaseID
	testErr     error
}

func (c *fakeConnector) Test(_ context.Context, cfg targetdb.ConnConfig) error {
	c.tested = append(c.tested, cfg)
	return c.testErr
}

func (c *fakeConnector) Invalidate(id targetdb.DatabaseID) {
	c.invalidated = append(c.invalidated, id)
}

func newTestService(t *testing.T) (*Service, *fakeRepo, *fakeConnector) {
	t.Helper()
	repo := newFakeRepo()
	conn := &fakeConnector{}
	svc, err := NewService(repo, newTestCipher(t, 1), conn)
	require.NoError(t, err)
	return svc, repo, conn
}

func validInput() Input {
	return Input{
		Name:     "shop",
		Host:     "db-a",
		Port:     5432,
		Username: "target",
		Password: "target_password",
		DBName:   "shop",
		SSLMode:  "disable",
	}
}

func TestNewServiceRejectsNilDependencies(t *testing.T) {
	repo, cipher, conn := newFakeRepo(), newTestCipher(t, 1), &fakeConnector{}

	_, err := NewService(nil, cipher, conn)
	require.Error(t, err)
	_, err = NewService(repo, nil, conn)
	require.Error(t, err)
	_, err = NewService(repo, cipher, nil)
	require.Error(t, err)
}

func TestCreateEncryptsPassword(t *testing.T) {
	svc, repo, _ := newTestService(t)

	d, err := svc.Create(context.Background(), validInput())
	require.NoError(t, err)
	require.NotZero(t, d.ID)

	stored := repo.rows[d.ID]
	require.NotEqual(t, []byte("target_password"), stored.EncryptedPassword)
	password, err := svc.cipher.Decrypt(stored.EncryptedPassword)
	require.NoError(t, err)
	require.Equal(t, "target_password", password)
}

func TestCreateNormalizesInput(t *testing.T) {
	svc, _, _ := newTestService(t)
	in := validInput()
	in.Name = "  shop  "
	in.Host = " db-a "
	in.SSLMode = ""

	d, err := svc.Create(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, "shop", d.Name)
	require.Equal(t, "db-a", d.Host)
	require.Equal(t, "prefer", d.SSLMode)
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	tests := map[string]func(*Input){
		"empty name":     func(in *Input) { in.Name = "  " },
		"empty password": func(in *Input) { in.Password = "" },
		"empty host":     func(in *Input) { in.Host = "" },
		"zero port":      func(in *Input) { in.Port = 0 },
		"bad sslmode":    func(in *Input) { in.SSLMode = "sometimes" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			svc, repo, _ := newTestService(t)
			in := validInput()
			mutate(&in)

			_, err := svc.Create(context.Background(), in)
			require.ErrorIs(t, err, ErrInvalidInput)
			require.Empty(t, repo.rows)
		})
	}
}

func TestCreateDuplicateName(t *testing.T) {
	svc, _, _ := newTestService(t)
	_, err := svc.Create(context.Background(), validInput())
	require.NoError(t, err)

	_, err = svc.Create(context.Background(), validInput())
	require.ErrorIs(t, err, ErrDuplicateName)
}

func TestUpdateWithoutPasswordKeepsStoredPassword(t *testing.T) {
	svc, repo, conn := newTestService(t)
	ctx := context.Background()
	d, err := svc.Create(ctx, validInput())
	require.NoError(t, err)
	before := repo.rows[d.ID].EncryptedPassword

	in := validInput()
	in.Host = "db-b"
	in.Password = ""
	updated, err := svc.Update(ctx, d.ID, in)
	require.NoError(t, err)

	require.Equal(t, "db-b", updated.Host)
	require.Equal(t, before, repo.rows[d.ID].EncryptedPassword)
	require.Equal(t, []targetdb.DatabaseID{d.ID}, conn.invalidated)
}

func TestUpdateWithPasswordReencrypts(t *testing.T) {
	svc, repo, conn := newTestService(t)
	ctx := context.Background()
	d, err := svc.Create(ctx, validInput())
	require.NoError(t, err)
	before := repo.rows[d.ID].EncryptedPassword

	in := validInput()
	in.Password = "new_password"
	_, err = svc.Update(ctx, d.ID, in)
	require.NoError(t, err)

	after := repo.rows[d.ID].EncryptedPassword
	require.NotEqual(t, before, after)
	password, err := svc.cipher.Decrypt(after)
	require.NoError(t, err)
	require.Equal(t, "new_password", password)
	require.Equal(t, []targetdb.DatabaseID{d.ID}, conn.invalidated)
}

func TestUpdateUnknownDatabase(t *testing.T) {
	svc, _, conn := newTestService(t)

	_, err := svc.Update(context.Background(), 42, validInput())
	require.ErrorIs(t, err, ErrNotFound)
	require.Empty(t, conn.invalidated)
}

func TestUpdateInvalidInputDoesNotInvalidate(t *testing.T) {
	svc, _, conn := newTestService(t)
	ctx := context.Background()
	d, err := svc.Create(ctx, validInput())
	require.NoError(t, err)

	in := validInput()
	in.DBName = ""
	_, err = svc.Update(ctx, d.ID, in)
	require.ErrorIs(t, err, ErrInvalidInput)
	require.Empty(t, conn.invalidated)
}

func TestDeleteInvalidatesPool(t *testing.T) {
	svc, repo, conn := newTestService(t)
	ctx := context.Background()
	d, err := svc.Create(ctx, validInput())
	require.NoError(t, err)

	require.NoError(t, svc.Delete(ctx, d.ID))
	require.Empty(t, repo.rows)
	require.Equal(t, []targetdb.DatabaseID{d.ID}, conn.invalidated)
}

func TestDeleteUnknownDatabase(t *testing.T) {
	svc, _, conn := newTestService(t)

	err := svc.Delete(context.Background(), 42)
	require.ErrorIs(t, err, ErrNotFound)
	require.Empty(t, conn.invalidated)
}

func TestTestConnectionPassesConfig(t *testing.T) {
	svc, repo, conn := newTestService(t)
	in := validInput()
	in.SSLMode = ""

	require.NoError(t, svc.TestConnection(context.Background(), in))
	require.Equal(t, []targetdb.ConnConfig{{
		Host:     "db-a",
		Port:     5432,
		User:     "target",
		Password: "target_password",
		DBName:   "shop",
		SSLMode:  "prefer",
	}}, conn.tested)
	require.Empty(t, repo.rows, "testing a connection must not save it")
}

func TestTestConnectionReturnsConnectorError(t *testing.T) {
	svc, _, conn := newTestService(t)
	conn.testErr = targetdb.ErrAuthFailed

	err := svc.TestConnection(context.Background(), validInput())
	require.ErrorIs(t, err, targetdb.ErrAuthFailed)
}

func TestTestConnectionRejectsInvalidInput(t *testing.T) {
	svc, _, conn := newTestService(t)
	in := validInput()
	in.Host = ""

	err := svc.TestConnection(context.Background(), in)
	require.ErrorIs(t, err, ErrInvalidInput)
	require.Empty(t, conn.tested)
}

func TestTestStoredUsesDecryptedPassword(t *testing.T) {
	svc, _, conn := newTestService(t)
	ctx := context.Background()
	d, err := svc.Create(ctx, validInput())
	require.NoError(t, err)

	require.NoError(t, svc.TestStored(ctx, d.ID))
	require.Len(t, conn.tested, 1)
	require.Equal(t, "target_password", conn.tested[0].Password)
}

func TestTestStoredUnknownDatabase(t *testing.T) {
	svc, _, _ := newTestService(t)

	err := svc.TestStored(context.Background(), 42)
	require.ErrorIs(t, err, ErrNotFound)
}
