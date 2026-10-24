package viewer

import (
	"context"
	"testing"
	"tubes-wbd/backend/access"
	"tubes-wbd/backend/database/targetdb"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
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
	userID uint
	dbID   uint
}

func (a *fakeAuthorizer) RequireView(_ context.Context, userID, managedDatabaseID uint) error {
	a.calls++
	a.userID, a.dbID = userID, managedDatabaseID
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

func newTestViewerService(t *testing.T) (*ViewerService, *fakeAuthorizer, *fakePools) {
	t.Helper()
	authz, pools := &fakeAuthorizer{}, &fakePools{}
	svc, err := NewViewerService(authz, pools, testViewerConfig())
	require.NoError(t, err)
	return svc, authz, pools
}

// operations calls every public method with valid arguments.
var operations = map[string]func(*ViewerService) error{
	"ListSchemas": func(s *ViewerService) error {
		_, err := s.ListSchemas(context.Background(), 7, 3)
		return err
	},
	"ListTables": func(s *ViewerService) error {
		_, err := s.ListTables(context.Background(), 7, 3, "public")
		return err
	},
	"DescribeTable": func(s *ViewerService) error {
		_, err := s.DescribeTable(context.Background(), 7, 3, "public", "users")
		return err
	},
	"TableData": func(s *ViewerService) error {
		_, err := s.TableData(context.Background(), 7, 3, "public", "users", 1, 10)
		return err
	},
}

func TestNewViewerServiceRejectsNilDependencies(t *testing.T) {
	_, err := NewViewerService(nil, &fakePools{}, testViewerConfig())
	require.Error(t, err)
	_, err = NewViewerService(&fakeAuthorizer{}, nil, testViewerConfig())
	require.Error(t, err)
}

func TestPermissionIsCheckedBeforeConnecting(t *testing.T) {
	for name, call := range operations {
		t.Run(name, func(t *testing.T) {
			svc, authz, pools := newTestViewerService(t)
			authz.err = access.ErrInsufficientPermission

			require.ErrorIs(t, call(svc), access.ErrInsufficientPermission)
			require.Equal(t, 1, authz.calls)
			require.Equal(t, uint(7), authz.userID)
			require.Equal(t, uint(3), authz.dbID)
			require.Zero(t, pools.calls, "target connection must not be opened without permission")
		})
	}
}

func TestPermissionIsCheckedBeforeValidatingInput(t *testing.T) {
	svc, authz, pools := newTestViewerService(t)
	authz.err = access.ErrInsufficientPermission

	_, err := svc.TableData(context.Background(), 1, 1, "", "", -1, -1)

	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	require.Zero(t, pools.calls)
}

func TestInvalidInputDoesNotConnect(t *testing.T) {
	ctx := context.Background()
	tests := map[string]func(*ViewerService) error{
		"list tables, empty schema": func(s *ViewerService) error {
			_, err := s.ListTables(ctx, 1, 1, "")
			return err
		},
		"describe, empty schema": func(s *ViewerService) error {
			_, err := s.DescribeTable(ctx, 1, 1, "", "users")
			return err
		},
		"describe, empty table": func(s *ViewerService) error {
			_, err := s.DescribeTable(ctx, 1, 1, "public", "")
			return err
		},
		"data, empty table": func(s *ViewerService) error {
			_, err := s.TableData(ctx, 1, 1, "public", "", 1, 10)
			return err
		},
		"data, NUL in table": func(s *ViewerService) error {
			_, err := s.TableData(ctx, 1, 1, "public", "us\x00ers", 1, 10)
			return err
		},
		"data, invalid UTF-8 in schema": func(s *ViewerService) error {
			_, err := s.TableData(ctx, 1, 1, "pub\xfflic", "users", 1, 10)
			return err
		},
		"data, negative page": func(s *ViewerService) error {
			_, err := s.TableData(ctx, 1, 1, "public", "users", -1, 10)
			return err
		},
		"data, negative page size": func(s *ViewerService) error {
			_, err := s.TableData(ctx, 1, 1, "public", "users", 1, -10)
			return err
		},
		"data, page too large": func(s *ViewerService) error {
			_, err := s.TableData(ctx, 1, 1, "public", "users", maxPage+1, 10)
			return err
		},
	}

	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			svc, _, pools := newTestViewerService(t)

			require.ErrorIs(t, call(svc), ErrInvalidInput)
			require.Zero(t, pools.calls)
		})
	}
}

func TestPoolErrorsArePassedThrough(t *testing.T) {
	for _, poolErr := range []error{targetdb.ErrUnknownDatabase, targetdb.ErrUnreachable, targetdb.ErrAuthFailed} {
		for name, call := range operations {
			t.Run(name+"/"+poolErr.Error(), func(t *testing.T) {
				svc, _, pools := newTestViewerService(t)
				pools.err = poolErr

				require.ErrorIs(t, call(svc), poolErr)
				require.Equal(t, 1, pools.calls)
			})
		}
	}
}

func testViewerConfig() Config {
	return Config{DefaultPageSize: 50, MaxPageSize: 100}
}

func TestNormalizePage(t *testing.T) {
	tests := []struct {
		name        string
		page, size  int
		wantPage    int
		wantSize    int
		wantInvalid bool
	}{
		{name: "zero values use defaults", wantPage: 1, wantSize: testViewerConfig().DefaultPageSize},
		{name: "explicit values", page: 3, size: 20, wantPage: 3, wantSize: 20},
		{name: "size is capped", page: 1, size: testViewerConfig().MaxPageSize + 1, wantPage: 1, wantSize: testViewerConfig().MaxPageSize},
		{name: "largest page", page: maxPage, size: 1, wantPage: maxPage, wantSize: 1},
		{name: "negative page", page: -1, size: 10, wantInvalid: true},
		{name: "negative size", page: 1, size: -1, wantInvalid: true},
		{name: "page too large", page: maxPage + 1, size: 10, wantInvalid: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, size, err := normalizePage(tt.page, tt.size, testViewerConfig())

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
