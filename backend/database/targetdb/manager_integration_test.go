//go:build integration

package targetdb

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	testUser     = "target"
	testPassword = "target_password"
)

var targetA, targetB ConnConfig

func TestMain(m *testing.M) {
	os.Exit(runWithTargets(m))
}

func runWithTargets(m *testing.M) int {
	ctx := context.Background()

	ctrA, cfgA, err := startPostgres(ctx, "target_a")
	defer testcontainers.TerminateContainer(ctrA)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start target A:", err)
		return 1
	}
	ctrB, cfgB, err := startPostgres(ctx, "target_b")
	defer testcontainers.TerminateContainer(ctrB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start target B:", err)
		return 1
	}

	targetA, targetB = cfgA, cfgB
	return m.Run()
}

func startPostgres(ctx context.Context, dbName string) (*tcpostgres.PostgresContainer, ConnConfig, error) {
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase(dbName),
		tcpostgres.WithUsername(testUser),
		tcpostgres.WithPassword(testPassword),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return ctr, ConnConfig{}, err
	}

	host, err := ctr.Host(ctx)
	if err != nil {
		return ctr, ConnConfig{}, err
	}
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return ctr, ConnConfig{}, err
	}

	return ctr, ConnConfig{
		Host:     host,
		Port:     port.Num(),
		User:     testUser,
		Password: testPassword,
		DBName:   dbName,
		SSLMode:  "disable",
	}, nil
}

func newTestManager(t *testing.T, opts Options) (*Manager, *fakeSource) {
	t.Helper()
	src := newFakeSource(map[DatabaseID]ConnConfig{1: targetA, 2: targetB})
	m := NewManager(src, opts)
	t.Cleanup(m.Close)
	return m, src
}

func TestIntegrationTestConnection(t *testing.T) {
	m, _ := newTestManager(t, Options{})

	require.NoError(t, m.Test(context.Background(), targetA))
}

func TestIntegrationWrongPassword(t *testing.T) {
	m, _ := newTestManager(t, Options{})
	cfg := targetA
	cfg.Password = "wrong-password"

	err := m.Test(context.Background(), cfg)
	require.ErrorIs(t, err, ErrAuthFailed)
	require.NotContains(t, err.Error(), cfg.Password)
}

func TestIntegrationUnreachable(t *testing.T) {
	m, _ := newTestManager(t, Options{ConnectTimeout: 2 * time.Second})
	cfg := targetA
	cfg.Host = "127.0.0.1"
	cfg.Port = 1 // nothing listens here

	err := m.Test(context.Background(), cfg)
	require.ErrorIs(t, err, ErrUnreachable)
}

func TestIntegrationPoolIsCached(t *testing.T) {
	m, src := newTestManager(t, Options{})
	ctx := context.Background()

	first, err := m.Pool(ctx, 1)
	require.NoError(t, err)
	second, err := m.Pool(ctx, 1)
	require.NoError(t, err)

	require.Same(t, first, second)
	require.Equal(t, 1, src.callCount())
}

func TestIntegrationTargetsAreIsolated(t *testing.T) {
	m, _ := newTestManager(t, Options{})
	ctx := context.Background()

	poolA, err := m.Pool(ctx, 1)
	require.NoError(t, err)
	poolB, err := m.Pool(ctx, 2)
	require.NoError(t, err)
	require.NotSame(t, poolA, poolB)

	_, err = poolA.Exec(ctx, "CREATE TABLE isolation_check (id int)")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = poolA.Exec(context.Background(), "DROP TABLE IF EXISTS isolation_check")
	})

	requireTableExists(t, poolA, "isolation_check", true)
	requireTableExists(t, poolB, "isolation_check", false)
}

func TestIntegrationConcurrentPoolCalls(t *testing.T) {
	m, src := newTestManager(t, Options{})
	ctx := context.Background()

	const callers = 20
	pools := make([]*pgxpool.Pool, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			pools[i], errs[i] = m.Pool(ctx, 1)
		})
	}
	wg.Wait()

	for i := range callers {
		require.NoError(t, errs[i])
		require.Same(t, pools[0], pools[i])
	}
	require.Equal(t, 1, src.callCount(), "concurrent callers must share one connection attempt")
}

func TestIntegrationInvalidateReconnects(t *testing.T) {
	m, src := newTestManager(t, Options{})
	ctx := context.Background()

	old, err := m.Pool(ctx, 1)
	require.NoError(t, err)

	src.set(1, targetB)
	m.Invalidate(1)

	fresh, err := m.Pool(ctx, 1)
	require.NoError(t, err)
	require.NotSame(t, old, fresh)
	require.Error(t, old.Ping(ctx), "invalidated pool must be closed")

	var dbName string
	require.NoError(t, fresh.QueryRow(ctx, "SELECT current_database()").Scan(&dbName))
	require.Equal(t, targetB.DBName, dbName)
}

func TestIntegrationSessionSettings(t *testing.T) {
	m, _ := newTestManager(t, Options{StatementTimeout: 1500 * time.Millisecond})
	ctx := context.Background()

	pool, err := m.Pool(ctx, 1)
	require.NoError(t, err)

	var timeout, appName string
	require.NoError(t, pool.QueryRow(ctx, "SHOW statement_timeout").Scan(&timeout))
	require.NoError(t, pool.QueryRow(ctx, "SHOW application_name").Scan(&appName))
	require.Equal(t, "1500ms", timeout)
	require.Equal(t, applicationName, appName)
}

func requireTableExists(t *testing.T, pool *pgxpool.Pool, table string, want bool) {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(),
		"SELECT to_regclass($1) IS NOT NULL", "public."+table).Scan(&exists)
	require.NoError(t, err)
	require.Equal(t, want, exists)
}