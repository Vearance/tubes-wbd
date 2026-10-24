package migration

import (
	"context"
	"errors"
	"fmt"
	"time"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const lockName = "dbm.migration"
const ensureHistorySQL = `
CREATE SCHEMA IF NOT EXISTS _dbm;
CREATE TABLE IF NOT EXISTS _dbm.migration_history (
	version     bigint PRIMARY KEY,
	name        text NOT NULL,
	sql         text NOT NULL,
	executed_by bigint NOT NULL,
	executed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
	duration_ms bigint NOT NULL
)`

func apply(ctx context.Context, pool *pgxpool.Pool, m Migration, userID uint, opts Options) (*HistoryEntry, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("migration: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtext($1))", lockName).Scan(&locked); err != nil {
		return nil, fmt.Errorf("migration: acquire lock: %w", err)
	}
	if !locked {
		return nil, ErrMigrationInProgress
	}

	timeouts := fmt.Sprintf("SET LOCAL statement_timeout = %d; SET LOCAL lock_timeout = %d",
		opts.StatementTimeout.Milliseconds(), opts.LockTimeout.Milliseconds())
	if _, err := tx.Exec(ctx, timeouts); err != nil {
		return nil, fmt.Errorf("migration: set timeouts: %w", err)
	}
	if _, err := tx.Exec(ctx, ensureHistorySQL); err != nil {
		return nil, executionError(err)
	}

	var version int64
	if err := tx.QueryRow(ctx,
		"SELECT coalesce(max(version), 0) + 1 FROM _dbm.migration_history").Scan(&version); err != nil {
		return nil, executionError(err)
	}

	start := time.Now()
	if _, err := tx.Exec(ctx, m.SQL); err != nil {
		return nil, executionError(err)
	}
	duration := time.Since(start)
	if tx.Conn().PgConn().TxStatus() != 'T' {
		return nil, ErrTransactionControl
	}

	entry := &HistoryEntry{
		Version:    version,
		Name:       m.Name,
		SQL:        m.SQL,
		ExecutedBy: userID,
		Duration:   duration,
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO _dbm.migration_history (version, name, sql, executed_by, duration_ms)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING executed_at`,
		version, m.Name, m.SQL, int64(userID), duration.Milliseconds()).Scan(&entry.ExecutedAt); err != nil {
		return nil, executionError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, executionError(err)
	}
	return entry, nil
}

func executionError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return &ExecutionError{
			Code:     pgErr.Code,
			Message:  pgErr.Message,
			Detail:   pgErr.Detail,
			Hint:     pgErr.Hint,
			Position: pgErr.Position,
		}
	}
	if errors.Is(err, pgx.ErrTxClosed) {
		return ErrTransactionControl
	}
	return fmt.Errorf("migration: execute: %w", err)
}