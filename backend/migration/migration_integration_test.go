//go:build integration

package migration

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"tubes-wbd/backend/access"
	"tubes-wbd/backend/database/targetdb"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	testUser     = "target"
	testPassword = "target_password"
)

const (
	platformUser     = "platform"
	platformPassword = "platform_password"
)

var (
	server            targetdb.ConnConfig
	adminPool         *pgxpool.Pool
	platformServer    targetdb.ConnConfig
	platformAdminPool *pgxpool.Pool
	dbCounter         atomic.Int64
)

func TestMain(m *testing.M) {
	os.Exit(runWithServer(m))
}

func runWithServer(m *testing.M) int {
	ctx := context.Background()

	var stop func()
	var err error
	server, adminPool, stop, err = startServer(ctx, testUser, testPassword)
	defer stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "start target server:", err)
		return 1
	}
	platformServer, platformAdminPool, stop, err = startServer(ctx, platformUser, platformPassword)
	defer stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "start platform server:", err)
		return 1
	}

	return m.Run()
}

func startServer(ctx context.Context, user, password string) (targetdb.ConnConfig, *pgxpool.Pool, func(), error) {
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername(user),
		tcpostgres.WithPassword(password),
		tcpostgres.BasicWaitStrategies(),
	)
	stop := func() { _ = testcontainers.TerminateContainer(ctr) }
	if err != nil {
		return targetdb.ConnConfig{}, nil, stop, err
	}

	host, err := ctr.Host(ctx)
	if err != nil {
		return targetdb.ConnConfig{}, nil, stop, fmt.Errorf("server host: %w", err)
	}
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return targetdb.ConnConfig{}, nil, stop, fmt.Errorf("server port: %w", err)
	}
	cfg := targetdb.ConnConfig{
		Host:     host,
		Port:     port.Num(),
		User:     user,
		Password: password,
		DBName:   "postgres",
		SSLMode:  "disable",
	}

	pool, err := pgxpool.New(ctx, dsn(cfg))
	if err != nil {
		return targetdb.ConnConfig{}, nil, stop, fmt.Errorf("admin pool: %w", err)
	}
	return cfg, pool, func() {
		pool.Close()
		stop()
	}, nil
}

func dsn(c targetdb.ConnConfig) string {
	return fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
		c.User, c.Password, net.JoinHostPort(c.Host, strconv.Itoa(int(c.Port))), c.DBName)
}

func newTarget(t *testing.T, setupSQL string) (targetdb.ConnConfig, *pgxpool.Pool) {
	t.Helper()
	return newDatabaseOn(t, server, adminPool, "target", setupSQL)
}

func newPlatformDB(t *testing.T) targetdb.ConnConfig {
	t.Helper()
	cfg, _ := newDatabaseOn(t, platformServer, platformAdminPool, "platform", "")
	return cfg
}

func newDatabaseOn(t *testing.T, base targetdb.ConnConfig, admin *pgxpool.Pool, prefix, setupSQL string) (targetdb.ConnConfig, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	cfg := base
	cfg.DBName = fmt.Sprintf("%s_%d", prefix, dbCounter.Add(1))
	_, err := admin.Exec(ctx, "CREATE DATABASE "+cfg.DBName)
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, dsn(cfg))
	require.NoError(t, err)
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+cfg.DBName+" WITH (FORCE)")
	})

	if setupSQL != "" {
		_, err = pool.Exec(ctx, setupSQL)
		require.NoError(t, err)
	}
	return cfg, pool
}

type stubAuthorizer struct {
	viewErr error
	editErr error
}

func (a stubAuthorizer) RequireView(context.Context, uint, uint) error { return a.viewErr }

func (a stubAuthorizer) RequireEdit(context.Context, uint, uint) error { return a.editErr }

type staticSource map[targetdb.DatabaseID]targetdb.ConnConfig

func (s staticSource) ConnConfig(_ context.Context, id targetdb.DatabaseID) (targetdb.ConnConfig, error) {
	cfg, ok := s[id]
	if !ok {
		return targetdb.ConnConfig{}, targetdb.ErrUnknownDatabase
	}
	return cfg, nil
}

func newIntegrationService(t *testing.T, targets staticSource, opts Options) *MigrationService {
	t.Helper()
	return newIntegrationServiceWithManager(t, targets, targetdb.Options{}, opts)
}

func newIntegrationServiceWithManager(t *testing.T, targets staticSource, managerOpts targetdb.Options, opts Options) *MigrationService {
	t.Helper()
	return newIntegrationServiceWithAuthorizer(t, targets, managerOpts, opts, stubAuthorizer{})
}

func newIntegrationServiceWithAuthorizer(t *testing.T, targets staticSource, managerOpts targetdb.Options, opts Options, authz Authorizer) *MigrationService {
	t.Helper()
	manager := targetdb.NewManager(targets, managerOpts)
	t.Cleanup(manager.Close)

	svc, err := NewMigrationService(authz, manager, newFakeRepo(), opts)
	require.NoError(t, err)
	return svc
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, pool.QueryRow(context.Background(),
		"SELECT to_regclass($1) IS NOT NULL", name).Scan(&exists))
	return exists
}

func columnExists(t *testing.T, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = $1 AND column_name = $2)`,
		table, column).Scan(&exists))
	return exists
}

func historyVersions(t *testing.T, pool *pgxpool.Pool) []int64 {
	t.Helper()
	if !tableExists(t, pool, "_dbm.migration_history") {
		return nil
	}
	rows, err := pool.Query(context.Background(), "SELECT version FROM _dbm.migration_history ORDER BY version")
	require.NoError(t, err)
	defer rows.Close()

	var versions []int64
	for rows.Next() {
		var v int64
		require.NoError(t, rows.Scan(&v))
		versions = append(versions, v)
	}
	require.NoError(t, rows.Err())
	return versions
}

func TestIntegrationExecuteCommitsAndRecordsHistory(t *testing.T) {
	cfg, pool := newTarget(t, "CREATE TABLE customers (id int PRIMARY KEY)")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})
	ctx := context.Background()

	before := time.Now()
	entry, err := svc.Execute(ctx, 42, 1, Migration{
		Name: "add phone",
		SQL:  "ALTER TABLE customers ADD COLUMN phone_number text",
	})
	require.NoError(t, err)

	require.True(t, columnExists(t, pool, "customers", "phone_number"))
	require.Equal(t, int64(1), entry.Version)
	require.Equal(t, "add phone", entry.Name)
	require.Equal(t, uint(42), entry.ExecutedBy)
	require.WithinDuration(t, before, entry.ExecutedAt, time.Minute)

	var name, sql string
	var executedBy int64
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT name, sql, executed_by FROM _dbm.migration_history WHERE version = 1").Scan(&name, &sql, &executedBy))
	require.Equal(t, "add phone", name)
	require.Equal(t, "ALTER TABLE customers ADD COLUMN phone_number text", sql)
	require.Equal(t, int64(42), executedBy)
}

func TestIntegrationVersionsAreLinear(t *testing.T) {
	cfg, pool := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		entry, err := svc.Execute(ctx, 1, 1, Migration{
			Name: fmt.Sprintf("create t%d", i),
			SQL:  fmt.Sprintf("CREATE TABLE t%d (id int)", i),
		})
		require.NoError(t, err)
		require.Equal(t, int64(i), entry.Version)
	}
	require.Equal(t, []int64{1, 2, 3}, historyVersions(t, pool))
}

func TestIntegrationMultiStatementMigrationIsAtomic(t *testing.T) {
	cfg, pool := newTarget(t, "CREATE TABLE customers (id int PRIMARY KEY)")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})
	ctx := context.Background()

	_, err := svc.Execute(ctx, 1, 1, Migration{Name: "first", SQL: "CREATE TABLE kept (id int)"})
	require.NoError(t, err)

	_, err = svc.Execute(ctx, 1, 1, Migration{
		Name: "half broken",
		SQL: `CREATE TABLE orders (id int);
			ALTER TABLE customers ADD COLUMN email text;
			ALTER TABLE does_not_exist ADD COLUMN x int;`,
	})

	var execErr *ExecutionError
	require.ErrorAs(t, err, &execErr)
	require.ErrorIs(t, err, ErrExecutionFailed)
	require.Equal(t, "42P01", execErr.Code)
	require.Contains(t, execErr.Message, "does_not_exist")

	require.False(t, tableExists(t, pool, "orders"), "earlier statements must be rolled back")
	require.False(t, columnExists(t, pool, "customers", "email"), "earlier statements must be rolled back")
	require.True(t, tableExists(t, pool, "kept"), "a previous migration must not be affected")
	require.Equal(t, []int64{1}, historyVersions(t, pool), "a failed migration must not be recorded")

	entry, err := svc.Execute(ctx, 1, 1, Migration{Name: "after failure", SQL: "CREATE TABLE next (id int)"})
	require.NoError(t, err)
	require.Equal(t, int64(2), entry.Version, "a failed migration must not use up a version")
}

func TestIntegrationFailedFirstMigrationLeavesNoHistoryTable(t *testing.T) {
	cfg, pool := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})

	_, err := svc.Execute(context.Background(), 1, 1, Migration{Name: "typo", SQL: "CREAT TABLE x (id int)"})

	var execErr *ExecutionError
	require.ErrorAs(t, err, &execErr)
	require.Equal(t, "42601", execErr.Code)
	require.Positive(t, execErr.Position)
	require.False(t, tableExists(t, pool, "_dbm.migration_history"))
}

func TestIntegrationTransactionControlIsRejectedBeforeRunning(t *testing.T) {
	cfg, pool := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})

	_, err := svc.Execute(context.Background(), 1, 1, Migration{
		Name: "sneaky",
		SQL:  "CREATE TABLE committed_early (id int); COMMIT; CREATE TABLE after (id int)",
	})

	require.ErrorIs(t, err, ErrTransactionControl)
	require.False(t, tableExists(t, pool, "committed_early"))
	require.Nil(t, historyVersions(t, pool))
}

func TestIntegrationConcurrentMigrationIsRejected(t *testing.T) {
	cfg, pool := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})
	ctx := context.Background()

	holder, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = holder.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", lockName)
	require.NoError(t, err)

	_, err = svc.Execute(ctx, 1, 1, Migration{Name: "blocked", SQL: "CREATE TABLE blocked (id int)"})
	require.ErrorIs(t, err, ErrMigrationInProgress)
	require.False(t, tableExists(t, pool, "blocked"))

	require.NoError(t, holder.Rollback(ctx))
	_, err = svc.Execute(ctx, 1, 1, Migration{Name: "unblocked", SQL: "CREATE TABLE unblocked (id int)"})
	require.NoError(t, err)
}

func TestIntegrationSimultaneousExecuteOnlyOneWins(t *testing.T) {
	cfg, pool := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})

	const callers = 4
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			_, errs[i] = svc.Execute(context.Background(), 1, 1, Migration{
				Name: fmt.Sprintf("slow %d", i),
				SQL:  fmt.Sprintf("SELECT pg_sleep(1); CREATE TABLE slow_%d (id int)", i),
			})
		})
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		require.ErrorIs(t, err, ErrMigrationInProgress)
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, []int64{1}, historyVersions(t, pool))
}

func TestIntegrationTargetsAreIsolated(t *testing.T) {
	cfgA, poolA := newTarget(t, "")
	cfgB, poolB := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfgA, 2: cfgB}, Options{})
	ctx := context.Background()

	_, err := svc.Execute(ctx, 1, 1, Migration{Name: "only A", SQL: "CREATE TABLE only_a (id int)"})
	require.NoError(t, err)

	require.True(t, tableExists(t, poolA, "only_a"))
	require.False(t, tableExists(t, poolB, "only_a"))
	require.Nil(t, historyVersions(t, poolB))

	holder, err := poolA.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(ctx) }()
	_, err = holder.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", lockName)
	require.NoError(t, err)

	entry, err := svc.Execute(ctx, 1, 2, Migration{Name: "B while A is locked", SQL: "CREATE TABLE only_b (id int)"})
	require.NoError(t, err, "a migration on one database must not block another")
	require.Equal(t, int64(1), entry.Version)
}

func TestIntegrationStatementTimeoutRollsBack(t *testing.T) {
	cfg, pool := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{StatementTimeout: 300 * time.Millisecond})

	_, err := svc.Execute(context.Background(), 1, 1, Migration{
		Name: "too slow",
		SQL:  "CREATE TABLE created_before_timeout (id int); SELECT pg_sleep(3)",
	})

	var execErr *ExecutionError
	require.ErrorAs(t, err, &execErr)
	require.Equal(t, "57014", execErr.Code)
	require.False(t, tableExists(t, pool, "created_before_timeout"))
}

func TestIntegrationMigrationIsNotBoundByPoolStatementTimeout(t *testing.T) {
	cfg, pool := newTarget(t, "")
	svc := newIntegrationServiceWithManager(t, staticSource{1: cfg},
		targetdb.Options{StatementTimeout: 200 * time.Millisecond},
		Options{StatementTimeout: 10 * time.Second})

	_, err := svc.Execute(context.Background(), 1, 1, Migration{
		Name: "slower than the pool limit",
		SQL:  "SELECT pg_sleep(1); CREATE TABLE survived (id int)",
	})

	require.NoError(t, err)
	require.True(t, tableExists(t, pool, "survived"))
}

func TestIntegrationLockTimeoutRollsBack(t *testing.T) {
	cfg, pool := newTarget(t, "CREATE TABLE busy (id int)")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{LockTimeout: 300 * time.Millisecond})
	ctx := context.Background()

	holder, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(ctx) }()
	_, err = holder.Exec(ctx, "LOCK TABLE busy IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)

	start := time.Now()
	_, err = svc.Execute(ctx, 1, 1, Migration{Name: "waits", SQL: "ALTER TABLE busy ADD COLUMN c int"})

	var execErr *ExecutionError
	require.ErrorAs(t, err, &execErr)
	require.Equal(t, "55P03", execErr.Code)
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestIntegrationClientDisconnectDoesNotAbortMigration(t *testing.T) {
	cfg, pool := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := svc.Execute(ctx, 1, 1, Migration{
		Name: "outlives the request",
		SQL:  "SELECT pg_sleep(0.5); CREATE TABLE finished (id int)",
	})

	require.NoError(t, err)
	require.True(t, tableExists(t, pool, "finished"))
}

func TestIntegrationUnknownDatabase(t *testing.T) {
	svc := newIntegrationService(t, staticSource{}, Options{})

	_, err := svc.Execute(context.Background(), 1, 99, Migration{Name: "x", SQL: "SELECT 1"})

	require.ErrorIs(t, err, targetdb.ErrUnknownDatabase)
}

func applyMigrations(t *testing.T, svc *MigrationService, dbID uint, count int) {
	t.Helper()
	for i := 1; i <= count; i++ {
		_, err := svc.Execute(context.Background(), uint(10+i), dbID, Migration{
			Name: fmt.Sprintf("migration %d", i),
			SQL:  fmt.Sprintf("CREATE TABLE h%d (id int)", i),
		})
		require.NoError(t, err)
	}
}

func summaryVersions(page *HistoryPage) []int64 {
	versions := make([]int64, len(page.Entries))
	for i, e := range page.Entries {
		versions[i] = e.Version
	}
	return versions
}

func TestIntegrationHistoryIsEmptyBeforeFirstMigration(t *testing.T) {
	cfg, _ := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})
	ctx := context.Background()

	page, err := svc.History(ctx, 1, 1, 1, 10)
	require.NoError(t, err)
	require.NotNil(t, page.Entries)
	require.Empty(t, page.Entries)
	require.False(t, page.HasNext)

	_, err = svc.HistoryEntry(ctx, 1, 1, 1)
	require.ErrorIs(t, err, ErrVersionNotFound)
}

func TestIntegrationHistoryListsNewestFirst(t *testing.T) {
	cfg, _ := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})
	applyMigrations(t, svc, 1, 3)

	page, err := svc.History(context.Background(), 1, 1, 0, 0)

	require.NoError(t, err)
	require.Equal(t, []int64{3, 2, 1}, summaryVersions(page))
	require.Equal(t, 1, page.Page)
	require.Equal(t, DefaultPageSize, page.PageSize)
	require.False(t, page.HasNext)

	newest := page.Entries[0]
	require.Equal(t, "migration 3", newest.Name)
	require.Equal(t, uint(13), newest.ExecutedBy)
	require.False(t, newest.ExecutedAt.IsZero())
	require.False(t, page.Entries[2].ExecutedAt.After(newest.ExecutedAt))
}

func TestIntegrationHistoryPagination(t *testing.T) {
	cfg, _ := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})
	applyMigrations(t, svc, 1, 5)
	ctx := context.Background()

	want := []struct {
		versions []int64
		hasNext  bool
	}{
		{[]int64{5, 4}, true},
		{[]int64{3, 2}, true},
		{[]int64{1}, false},
		{[]int64{}, false},
	}
	for i, w := range want {
		page, err := svc.History(ctx, 1, 1, i+1, 2)
		require.NoError(t, err)
		require.Equal(t, w.versions, summaryVersions(page), "page %d", i+1)
		require.Equal(t, w.hasNext, page.HasNext, "page %d", i+1)
	}

	exact, err := svc.History(ctx, 1, 1, 1, 5)
	require.NoError(t, err)
	require.Len(t, exact.Entries, 5)
	require.False(t, exact.HasNext, "a full final page must not report a next page")
}

func TestIntegrationHistoryEntryDetail(t *testing.T) {
	cfg, _ := newTarget(t, "CREATE TABLE customers (id int PRIMARY KEY)")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})
	ctx := context.Background()

	sql := "ALTER TABLE customers ADD COLUMN phone text;\nCREATE INDEX customers_phone_idx ON customers (phone);"
	applied, err := svc.Execute(ctx, 42, 1, Migration{Name: "add phone", SQL: sql})
	require.NoError(t, err)

	entry, err := svc.HistoryEntry(ctx, 1, 1, applied.Version)
	require.NoError(t, err)
	require.Equal(t, applied.Version, entry.Version)
	require.Equal(t, "add phone", entry.Name)
	require.Equal(t, sql, entry.SQL)
	require.Equal(t, uint(42), entry.ExecutedBy)
	require.WithinDuration(t, applied.ExecutedAt, entry.ExecutedAt, time.Millisecond)

	_, err = svc.HistoryEntry(ctx, 1, 1, applied.Version+1)
	require.ErrorIs(t, err, ErrVersionNotFound)
}

func TestIntegrationFailedMigrationIsNotInHistory(t *testing.T) {
	cfg, _ := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})
	ctx := context.Background()
	applyMigrations(t, svc, 1, 1)

	_, err := svc.Execute(ctx, 1, 1, Migration{Name: "broken", SQL: "ALTER TABLE nope ADD x int"})
	require.ErrorIs(t, err, ErrExecutionFailed)

	page, err := svc.History(ctx, 1, 1, 1, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{1}, summaryVersions(page))
}

func TestIntegrationViewOnlyCanReadHistoryButNotExecute(t *testing.T) {
	cfg, pool := newTarget(t, "")
	ctx := context.Background()

	editor := newIntegrationService(t, staticSource{1: cfg}, Options{})
	applyMigrations(t, editor, 1, 2)

	viewer := newIntegrationServiceWithAuthorizer(t, staticSource{1: cfg}, targetdb.Options{}, Options{},
		stubAuthorizer{editErr: access.ErrInsufficientPermission})

	page, err := viewer.History(ctx, 5, 1, 1, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{2, 1}, summaryVersions(page))

	entry, err := viewer.HistoryEntry(ctx, 5, 1, 1)
	require.NoError(t, err)
	require.Equal(t, "CREATE TABLE h1 (id int)", entry.SQL)

	_, err = viewer.Execute(ctx, 5, 1, Migration{Name: "not allowed", SQL: "CREATE TABLE denied (id int)"})
	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	require.False(t, tableExists(t, pool, "denied"))
	require.Equal(t, []int64{1, 2}, historyVersions(t, pool))
}

func TestIntegrationNoPermissionCannotReadHistory(t *testing.T) {
	cfg, _ := newTarget(t, "")
	svc := newIntegrationServiceWithAuthorizer(t, staticSource{1: cfg}, targetdb.Options{}, Options{},
		stubAuthorizer{viewErr: access.ErrInsufficientPermission, editErr: access.ErrInsufficientPermission})

	_, err := svc.History(context.Background(), 1, 1, 1, 10)
	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	_, err = svc.HistoryEntry(context.Background(), 1, 1, 1)
	require.ErrorIs(t, err, access.ErrInsufficientPermission)
}

func TestIntegrationHistoryIsPerDatabase(t *testing.T) {
	cfgA, _ := newTarget(t, "")
	cfgB, _ := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfgA, 2: cfgB}, Options{})
	applyMigrations(t, svc, 1, 3)
	applyMigrations(t, svc, 2, 1)

	pageA, err := svc.History(context.Background(), 1, 1, 1, 10)
	require.NoError(t, err)
	pageB, err := svc.History(context.Background(), 1, 2, 1, 10)
	require.NoError(t, err)

	require.Equal(t, []int64{3, 2, 1}, summaryVersions(pageA))
	require.Equal(t, []int64{1}, summaryVersions(pageB))
}
