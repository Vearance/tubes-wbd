package migration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"tubes-wbd/backend/access"
	"tubes-wbd/backend/database/targetdb"
)

var (
	_ Authorizer   = (*access.AuthorizationService)(nil)
	_ PoolProvider = (*targetdb.Manager)(nil)
	_ Authorizer   = (*fakeAuthorizer)(nil)
	_ PoolProvider = (*fakePools)(nil)
)

type fakeAuthorizer struct {
	err    error
	calls  int
	method string
	userID uint
	dbID   uint
}

func (a *fakeAuthorizer) RequireView(_ context.Context, userID, managedDatabaseID uint) error {
	return a.record("view", userID, managedDatabaseID)
}

func (a *fakeAuthorizer) RequireEdit(_ context.Context, userID, managedDatabaseID uint) error {
	return a.record("edit", userID, managedDatabaseID)
}

func (a *fakeAuthorizer) record(method string, userID, dbID uint) error {
	a.calls++
	a.method, a.userID, a.dbID = method, userID, dbID
	return a.err
}

type fakePools struct {
	err   error
	calls int
}

func (p *fakePools) Pool(context.Context, targetdb.DatabaseID) (*pgxpool.Pool, error) {
	p.calls++
	return nil, p.err
}

func newTestService(t *testing.T) (*MigrationService, *fakeAuthorizer, *fakePools) {
	t.Helper()
	svc, authz, pools, _ := newTestServiceWithRepo(t)
	return svc, authz, pools
}

func newTestServiceWithRepo(t *testing.T) (*MigrationService, *fakeAuthorizer, *fakePools, *fakeRepo) {
	t.Helper()
	authz, pools, repo := &fakeAuthorizer{}, &fakePools{}, newFakeRepo()
	svc, err := NewMigrationService(authz, pools, repo, Options{})
	require.NoError(t, err)
	return svc, authz, pools, repo
}

func validMigration() Migration {
	return Migration{Name: "add phone", SQL: "ALTER TABLE customers ADD COLUMN phone text"}
}

func TestNewMigrationServiceRejectsNilDependencies(t *testing.T) {
	_, err := NewMigrationService(nil, &fakePools{}, newFakeRepo(), Options{})
	require.Error(t, err)
	_, err = NewMigrationService(&fakeAuthorizer{}, nil, newFakeRepo(), Options{})
	require.Error(t, err)
	_, err = NewMigrationService(&fakeAuthorizer{}, &fakePools{}, nil, Options{})
	require.Error(t, err)
}

func TestOptionsDefaults(t *testing.T) {
	require.Equal(t, DefaultOptions(), Options{}.withDefaults())

	custom := Options{StatementTimeout: time.Second, LockTimeout: 2 * time.Second}
	require.Equal(t, custom, custom.withDefaults())
}

func TestExecuteRequiresEditPermissionBeforeConnecting(t *testing.T) {
	svc, authz, pools := newTestService(t)
	authz.err = access.ErrInsufficientPermission

	_, err := svc.Execute(context.Background(), 7, 3, validMigration())

	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	require.Equal(t, 1, authz.calls)
	require.Equal(t, "edit", authz.method)
	require.Equal(t, uint(7), authz.userID)
	require.Equal(t, uint(3), authz.dbID)
	require.Zero(t, pools.calls, "target connection must not be opened without permission")
}

func TestExecutePermissionIsCheckedBeforeInput(t *testing.T) {
	svc, authz, pools := newTestService(t)
	authz.err = access.ErrInsufficientPermission

	_, err := svc.Execute(context.Background(), 1, 1, Migration{SQL: "COMMIT"})

	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	require.Zero(t, pools.calls)
}

func TestExecuteRejectsInvalidInputWithoutConnecting(t *testing.T) {
	tests := []struct {
		name string
		m    Migration
		want error
	}{
		{"empty name", Migration{Name: "  ", SQL: "SELECT 1"}, ErrInvalidInput},
		{"name too long", Migration{Name: strings.Repeat("x", maxNameLen+1), SQL: "SELECT 1"}, ErrInvalidInput},
		{"empty SQL", Migration{Name: "x", SQL: ""}, ErrInvalidInput},
		{"comment-only SQL", Migration{Name: "x", SQL: "-- nothing\n;"}, ErrInvalidInput},
		{"SQL too large", Migration{Name: "x", SQL: strings.Repeat("a", MaxSQLBytes+1)}, ErrInvalidInput},
		{"NUL in SQL", Migration{Name: "x", SQL: "SELECT 1\x00"}, ErrInvalidInput},
		{"invalid UTF-8", Migration{Name: "x", SQL: "SELECT '\xff'"}, ErrInvalidInput},
		{"commit", Migration{Name: "x", SQL: "ALTER TABLE t ADD c int; COMMIT"}, ErrTransactionControl},
		{"begin", Migration{Name: "x", SQL: "BEGIN; ALTER TABLE t ADD c int"}, ErrTransactionControl},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, pools := newTestService(t)

			_, err := svc.Execute(context.Background(), 1, 1, tt.m)

			require.ErrorIs(t, err, tt.want)
			require.Zero(t, pools.calls)
		})
	}
}

func TestExecuteTrimsName(t *testing.T) {
	m, err := validate(Migration{Name: "  add phone  ", SQL: "SELECT 1"})
	require.NoError(t, err)
	require.Equal(t, "add phone", m.Name)
}

func TestExecutePassesPoolErrorsThrough(t *testing.T) {
	for _, poolErr := range []error{targetdb.ErrUnknownDatabase, targetdb.ErrUnreachable, targetdb.ErrAuthFailed} {
		t.Run(poolErr.Error(), func(t *testing.T) {
			svc, _, pools := newTestService(t)
			pools.err = poolErr

			_, err := svc.Execute(context.Background(), 1, 1, validMigration())

			require.ErrorIs(t, err, poolErr)
			require.Equal(t, 1, pools.calls)
		})
	}
}

func TestExecutionErrorMatchesSentinel(t *testing.T) {
	err := &ExecutionError{Code: "42P01", Message: `relation "nope" does not exist`}

	require.ErrorIs(t, err, ErrExecutionFailed)
	require.Contains(t, err.Error(), `relation "nope" does not exist`)
	require.Contains(t, err.Error(), "42P01")
}

var historyOperations = map[string]func(*MigrationService) error{
	"History": func(s *MigrationService) error {
		_, err := s.History(context.Background(), 7, 3, 1, 10)
		return err
	},
	"HistoryEntry": func(s *MigrationService) error {
		_, err := s.HistoryEntry(context.Background(), 7, 3, 1)
		return err
	},
}

func TestHistoryRequiresViewPermissionBeforeConnecting(t *testing.T) {
	for name, call := range historyOperations {
		t.Run(name, func(t *testing.T) {
			svc, authz, pools := newTestService(t)
			authz.err = access.ErrInsufficientPermission

			require.ErrorIs(t, call(svc), access.ErrInsufficientPermission)
			require.Equal(t, "view", authz.method, "View Only must be enough to read history")
			require.Equal(t, uint(7), authz.userID)
			require.Equal(t, uint(3), authz.dbID)
			require.Zero(t, pools.calls)
		})
	}
}

func TestHistoryRejectsInvalidInputWithoutConnecting(t *testing.T) {
	ctx := context.Background()
	tests := map[string]func(*MigrationService) error{
		"negative page": func(s *MigrationService) error {
			_, err := s.History(ctx, 1, 1, -1, 10)
			return err
		},
		"negative page size": func(s *MigrationService) error {
			_, err := s.History(ctx, 1, 1, 1, -1)
			return err
		},
		"page too large": func(s *MigrationService) error {
			_, err := s.History(ctx, 1, 1, maxPage+1, 10)
			return err
		},
		"zero version": func(s *MigrationService) error {
			_, err := s.HistoryEntry(ctx, 1, 1, 0)
			return err
		},
		"negative version": func(s *MigrationService) error {
			_, err := s.HistoryEntry(ctx, 1, 1, -5)
			return err
		},
	}

	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			svc, _, pools := newTestService(t)

			require.ErrorIs(t, call(svc), ErrInvalidInput)
			require.Zero(t, pools.calls)
		})
	}
}

func TestHistoryPassesPoolErrorsThrough(t *testing.T) {
	for name, call := range historyOperations {
		t.Run(name, func(t *testing.T) {
			svc, _, pools := newTestService(t)
			pools.err = targetdb.ErrUnreachable

			require.ErrorIs(t, call(svc), targetdb.ErrUnreachable)
			require.Equal(t, 1, pools.calls)
		})
	}
}

func TestNormalizePage(t *testing.T) {
	tests := []struct {
		name               string
		page, size         int
		wantPage, wantSize int
		wantInvalid        bool
	}{
		{name: "zero values use defaults", wantPage: 1, wantSize: DefaultPageSize},
		{name: "explicit values", page: 3, size: 20, wantPage: 3, wantSize: 20},
		{name: "size is capped", page: 1, size: MaxPageSize + 1, wantPage: 1, wantSize: MaxPageSize},
		{name: "negative page", page: -1, size: 10, wantInvalid: true},
		{name: "negative size", page: 1, size: -1, wantInvalid: true},
		{name: "page too large", page: maxPage + 1, size: 10, wantInvalid: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, size, err := normalizePage(tt.page, tt.size)
			if tt.wantInvalid {
				require.ErrorIs(t, err, ErrInvalidInput)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantPage, page)
			require.Equal(t, tt.wantSize, size)
		})
	}
}
