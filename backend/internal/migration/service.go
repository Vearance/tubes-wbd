package migration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"tubes-wbd/internal/database/targetdb"
	"tubes-wbd/internal/telemetry"
)

const (
	MaxSQLBytes     = 1 << 20
	DefaultPageSize = 50
	MaxPageSize     = 100
	maxNameLen      = 200
	maxPage         = math.MaxInt32
	rollbackTimeout = 5 * time.Second
)

type Authorizer interface {
	RequireView(ctx context.Context, userID, managedDatabaseID uint) error
	RequireEdit(ctx context.Context, userID, managedDatabaseID uint) error
}

type PoolProvider interface {
	Pool(ctx context.Context, id targetdb.DatabaseID) (*pgxpool.Pool, error)
}

type TargetConfigSnapshotter interface {
	EncryptedSnapshot(ctx context.Context, id targetdb.DatabaseID) ([]byte, error)
	ResolveSnapshot([]byte) (targetdb.ConnConfig, error)
}

type explicitConfigPoolProvider interface {
	PoolWithConfig(ctx context.Context, config targetdb.ConnConfig) (*pgxpool.Pool, error)
}

type Options struct {
	StatementTimeout time.Duration
	LockTimeout      time.Duration
}

func DefaultOptions() Options {
	return Options{
		StatementTimeout: 5 * time.Minute,
		LockTimeout:      10 * time.Second,
	}
}

func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.StatementTimeout <= 0 {
		o.StatementTimeout = d.StatementTimeout
	}
	if o.LockTimeout <= 0 {
		o.LockTimeout = d.LockTimeout
	}
	return o
}

type MigrationService struct {
	authz     Authorizer
	pools     PoolProvider
	repo      Repository
	opts      Options
	snapshots TargetConfigSnapshotter
}

func NewMigrationService(authz Authorizer, pools PoolProvider, repo Repository, opts Options, snapshots ...TargetConfigSnapshotter) (*MigrationService, error) {
	if authz == nil {
		return nil, errors.New("migration authorizer is nil")
	}
	if pools == nil {
		return nil, errors.New("target database pool provider is nil")
	}
	if repo == nil {
		return nil, errors.New("migration repository is nil")
	}

	if len(snapshots) > 1 {
		return nil, errors.New("migration config snapshotter may be provided only once")
	}
	var snapshotter TargetConfigSnapshotter
	if len(snapshots) == 1 {
		snapshotter = snapshots[0]
	}
	return &MigrationService{
		authz: authz, pools: pools, repo: repo, opts: opts.withDefaults(), snapshots: snapshotter,
	}, nil
}

func (s *MigrationService) runExecution(ctx context.Context, execution *Execution) (*HistoryEntry, error) {
	pool, closePool, err := s.executionPool(ctx, execution)
	if err != nil {
		return nil, err
	}
	if closePool {
		defer pool.Close()
	}
	if execution.Batch != nil {
		return s.runBatchExecution(ctx, execution, pool)
	}
	if err := ensureHistorySchema(ctx, pool, false); err != nil {
		return nil, err
	}
	if execution.ID > 0 {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("migration: begin execution reconciliation: %w", err)
		}
		exists, err := historyExists(ctx, tx)
		if err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		if exists {
			entry, lookupErr := getExecutionHistoryByID(ctx, tx, execution.ID)
			if lookupErr == nil {
				_ = tx.Rollback(ctx)
				return entry, nil
			}
			if !errors.Is(lookupErr, ErrVersionNotFound) {
				_ = tx.Rollback(ctx)
				return nil, lookupErr
			}
		}
		_ = tx.Rollback(ctx)
	}
	return applyExecution(ctx, pool, execution, s.opts)
}

func (s *MigrationService) executionPool(ctx context.Context, execution *Execution) (*pgxpool.Pool, bool, error) {
	if len(execution.EncryptedTargetConfig) == 0 {
		pool, err := s.pools.Pool(ctx, execution.ManagedDatabaseID)
		return pool, false, err
	}
	if s.snapshots == nil {
		return nil, false, errors.New("migration: execution has an encrypted target config snapshot but no snapshot resolver is configured")
	}
	opener, ok := s.pools.(explicitConfigPoolProvider)
	if !ok {
		return nil, false, errors.New("migration: target pool provider cannot open a snapshotted config")
	}
	config, err := s.snapshots.ResolveSnapshot(execution.EncryptedTargetConfig)
	if err != nil {
		return nil, false, err
	}
	pool, err := opener.PoolWithConfig(ctx, config)
	return pool, true, err
}

func (s *MigrationService) targetConfigSnapshot(ctx context.Context, databaseID uint) ([]byte, error) {
	if s.snapshots == nil {
		return nil, nil
	}
	return s.snapshots.EncryptedSnapshot(ctx, databaseID)
}

func (s *MigrationService) History(ctx context.Context, userID, dbID uint, page, pageSize int) (*HistoryPage, error) {
	return s.HistoryFiltered(ctx, userID, dbID, page, pageSize, false)
}

func (s *MigrationService) HistoryFiltered(ctx context.Context, userID, dbID uint, page, pageSize int, hideRollback bool) (*HistoryPage, error) {
	actor := telemetry.ActorUser(userID)
	dbLabel := telemetry.TargetDatabaseLabel(dbID)
	telemetry.SetRequestDatabase(ctx, dbLabel)
	ctx = telemetry.WithActor(telemetry.WithDatabase(ctx, dbLabel), actor)

	if err := s.authz.RequireView(ctx, userID, dbID); err != nil {
		return nil, err
	}
	page, pageSize, err := normalizePage(page, pageSize)
	if err != nil {
		return nil, err
	}

	var result *HistoryPage
	err = s.readOnly(ctx, dbID, func(tx pgx.Tx) (err error) {
		result, err = listHistory(ctx, tx, page, pageSize, hideRollback)
		if err != nil {
			return err
		}
		rollback, err := forwardCandidate(ctx, tx)
		if errors.Is(err, ErrInvalidForward) {
			return nil
		}
		if err != nil {
			return err
		}
		if rollback.TargetExecutionID == nil {
			return nil
		}
		target, err := getExecutionHistoryByID(ctx, tx, *rollback.TargetExecutionID)
		if err != nil {
			return err
		}
		result.Forward = &ForwardCandidate{
			RollbackExecutionID: rollback.ExecutionID,
			TargetExecutionID:   target.ExecutionID,
			Name:                target.Name,
		}
		return nil
	})
	return result, err
}

func (s *MigrationService) HistoryEntry(ctx context.Context, userID, dbID uint, version int64) (*HistoryEntry, error) {
	actor := telemetry.ActorUser(userID)
	dbLabel := telemetry.TargetDatabaseLabel(dbID)
	telemetry.SetRequestDatabase(ctx, dbLabel)
	ctx = telemetry.WithActor(telemetry.WithDatabase(ctx, dbLabel), actor)

	if err := s.authz.RequireView(ctx, userID, dbID); err != nil {
		return nil, err
	}
	if version <= 0 {
		return nil, fmt.Errorf("%w: version must be positive", ErrInvalidInput)
	}

	var entry *HistoryEntry
	err := s.readOnly(ctx, dbID, func(tx pgx.Tx) (err error) {
		entry, err = getHistoryEntry(ctx, tx, version)
		return err
	})
	return entry, err
}

func (s *MigrationService) readOnly(ctx context.Context, dbID uint, fn func(pgx.Tx) error) error {
	pool, err := s.pools.Pool(ctx, dbID)
	if err != nil {
		return err
	}
	if err := ensureHistorySchema(ctx, pool, false); err != nil {
		return err
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("migration: begin read-only transaction: %w", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	return fn(tx)
}

func normalizePage(page, pageSize int) (int, int, error) {
	switch {
	case page < 0 || pageSize < 0:
		return 0, 0, fmt.Errorf("%w: page and page size must not be negative", ErrInvalidInput)
	case page > maxPage:
		return 0, 0, fmt.Errorf("%w: page is too large", ErrInvalidInput)
	}

	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return page, pageSize, nil
}

func sameBatch(a, b *BatchDefinition) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func validateDefinition(m Migration) (Migration, error) {
	m, err := validate(m)
	if err != nil {
		return m, err
	}
	m.DownSQL = strings.TrimSpace(m.DownSQL)
	if m.Batch != nil {
		batch := *m.Batch
		batch.Schema = strings.TrimSpace(batch.Schema)
		batch.Table = strings.TrimSpace(batch.Table)
		batch.KeyColumn = strings.TrimSpace(batch.KeyColumn)
		batch.ScopeSQL = strings.TrimSpace(batch.ScopeSQL)
		m.Batch = &batch
		if err := validateBatchDefinition(batch); err != nil {
			return m, err
		}
		if !isSingleBatchUpdate(m.SQL) {
			return m, fmt.Errorf("%w: batch up SQL must be one UPDATE statement using $1 as the selected bigint key array; deletes and other changes that cannot be compensated per batch must run as a normal migration", ErrInvalidInput)
		}
	}
	switch {
	case m.Batch != nil && (m.Irreversible || m.DownSQL == ""):
		return m, fmt.Errorf("%w: batch migrations require Down SQL and cannot be marked irreversible", ErrInvalidInput)
	case m.Irreversible && m.DownSQL != "":
		return m, fmt.Errorf("%w: provide down SQL or mark the migration irreversible, not both", ErrInvalidInput)
	case m.Irreversible && strings.TrimSpace(m.IrreversibleReason) == "":
		return m, fmt.Errorf("%w: irreversible migrations require a reason", ErrInvalidInput)
	case !m.Irreversible && m.DownSQL == "":
		// Existing M1-style drafts do not contain down SQL. Treat them as
		// irreversible until the author supplies an explicit down migration.
		m.Irreversible = true
		m.IrreversibleReason = "No down SQL was provided."
	case m.DownSQL != "":
		down, err := validate(Migration{Name: m.Name, SQL: m.DownSQL})
		if err != nil {
			return m, fmt.Errorf("invalid down SQL: %w", err)
		}
		m.DownSQL = down.SQL
		if m.Batch != nil && !isSingleBatchUpdate(m.DownSQL) {
			return m, fmt.Errorf("%w: batch down SQL must be one UPDATE statement using $1 as the selected bigint key array", ErrInvalidInput)
		}
	}
	return m, nil
}

func validate(m Migration) (Migration, error) {
	m.Name = strings.TrimSpace(m.Name)
	switch {
	case m.Name == "":
		return m, fmt.Errorf("%w: name is required", ErrInvalidInput)
	case utf8.RuneCountInString(m.Name) > maxNameLen:
		return m, fmt.Errorf("%w: name is longer than %d characters", ErrInvalidInput, maxNameLen)
	case len(m.SQL) > MaxSQLBytes:
		return m, fmt.Errorf("%w: SQL is larger than %d bytes", ErrInvalidInput, MaxSQLBytes)
	case !utf8.ValidString(m.SQL) || !utf8.ValidString(m.Name) ||
		strings.ContainsRune(m.SQL, 0) || strings.ContainsRune(m.Name, 0):
		return m, fmt.Errorf("%w: contains invalid characters", ErrInvalidInput)
	}

	stmts := statements(m.SQL)
	if len(stmts) == 0 {
		return m, fmt.Errorf("%w: SQL has no statements", ErrInvalidInput)
	}
	if keyword, found := transactionControl(stmts); found {
		return m, fmt.Errorf("%w: %s", ErrTransactionControl, strings.ToUpper(keyword))
	}
	if referencesInternalSchema(m.SQL) {
		return m, fmt.Errorf("%w: SQL must not reference the platform-managed _dbm schema", ErrInvalidInput)
	}
	return m, nil
}

func isSingleBatchUpdate(sql string) bool {
	stmts := statements(sql)
	return len(stmts) == 1 && stmts[0][0] == "update" && strings.Contains(sql, "$1")
}
