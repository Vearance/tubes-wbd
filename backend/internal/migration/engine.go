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
	version             bigint PRIMARY KEY,
	execution_id        bigint UNIQUE,
	execution_type      text NOT NULL DEFAULT 'up',
	target_execution_id bigint,
	active              boolean NOT NULL DEFAULT true,
	name                text NOT NULL,
	sql                 text NOT NULL,
	down_sql            text NOT NULL DEFAULT '',
	irreversible_reason text NOT NULL DEFAULT '',
	executed_by         bigint NOT NULL,
	executed_at         timestamptz NOT NULL DEFAULT clock_timestamp(),
	duration_ms         bigint NOT NULL
)`

func apply(ctx context.Context, pool *pgxpool.Pool, m Migration, userID uint, opts Options) (*HistoryEntry, error) {
	return applyExecution(ctx, pool, &Execution{
		Type:       ExecutionTypeUp,
		Name:       m.Name,
		SQL:        m.SQL,
		DownSQL:    m.DownSQL,
		ExecutedBy: userID,
	}, opts)
}

func applyExecution(ctx context.Context, pool *pgxpool.Pool, execution *Execution, opts Options) (*HistoryEntry, error) {
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

	if err := validateExecutionPosition(ctx, tx, execution); err != nil {
		return nil, err
	}

	var version int64
	if err := tx.QueryRow(ctx,
		"SELECT coalesce(max(version), 0) + 1 FROM _dbm.migration_history").Scan(&version); err != nil {
		return nil, executionError(err)
	}

	start := time.Now()
	if _, err := tx.Exec(ctx, execution.SQL); err != nil {
		return nil, executionError(err)
	}
	duration := time.Since(start)
	if tx.Conn().PgConn().TxStatus() != 'T' {
		return nil, ErrTransactionControl
	}

	if err := updateExecutionPosition(ctx, tx, execution); err != nil {
		return nil, executionError(err)
	}

	entry := &HistoryEntry{
		Version:    version,
		Name:       execution.Name,
		SQL:        execution.SQL,
		ExecutedBy: execution.ExecutedBy,
		Duration:   duration,
	}
	var executionID any
	if execution.ID > 0 {
		executionID = int64(execution.ID)
	}
	var targetExecutionID any
	if execution.TargetExecutionID != nil {
		targetExecutionID = int64(*execution.TargetExecutionID)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO _dbm.migration_history
			(version, execution_id, execution_type, target_execution_id, active, name, sql, down_sql, irreversible_reason, executed_by, duration_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING executed_at`,
		version, executionID, execution.Type, targetExecutionID, execution.Type != ExecutionTypeRollback,
		execution.Name, execution.SQL, execution.DownSQL, execution.IrreversibleReason,
		int64(execution.ExecutedBy), duration.Milliseconds()).Scan(&entry.ExecutedAt); err != nil {
		return nil, executionError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, executionError(err)
	}
	entry.ExecutionID = execution.ID
	entry.Type = execution.Type
	entry.IrreversibleReason = execution.IrreversibleReason
	entry.TargetExecutionID = execution.TargetExecutionID
	entry.Active = execution.Type != ExecutionTypeRollback
	return entry, nil
}

func validateExecutionPosition(ctx context.Context, tx pgx.Tx, execution *Execution) error {
	switch execution.Type {
	case ExecutionTypeUp:
		return nil
	case ExecutionTypeRollback:
		if execution.TargetExecutionID == nil {
			return ErrInvalidInput
		}
		var activeID int64
		var downSQL string
		if err := tx.QueryRow(ctx,
			`SELECT execution_id, down_sql FROM _dbm.migration_history
			 WHERE active = true ORDER BY version DESC LIMIT 1`).Scan(&activeID, &downSQL); err != nil {
			return executionError(err)
		}
		if uint(activeID) != *execution.TargetExecutionID || downSQL == "" || execution.SQL != downSQL {
			return ErrInvalidRollback
		}
	case ExecutionTypeForward:
		if execution.TargetExecutionID == nil {
			return ErrInvalidInput
		}
		var lastType string
		var lastTarget int64
		var originalSQL string
		err := tx.QueryRow(ctx,
			`SELECT h.execution_type, h.target_execution_id, original.sql
			 FROM _dbm.migration_history h
			 JOIN _dbm.migration_history original ON original.execution_id = h.target_execution_id
			 ORDER BY h.version DESC LIMIT 1`).Scan(&lastType, &lastTarget, &originalSQL)
		if err != nil {
			return executionError(err)
		}
		if ExecutionType(lastType) != ExecutionTypeRollback || uint(lastTarget) != *execution.TargetExecutionID || originalSQL != execution.SQL {
			return ErrInvalidForward
		}
	default:
		return fmt.Errorf("migration: unsupported execution type %q", execution.Type)
	}
	return nil
}

func updateExecutionPosition(ctx context.Context, tx pgx.Tx, execution *Execution) error {
	switch execution.Type {
	case ExecutionTypeUp:
		return nil
	case ExecutionTypeRollback:
		result, err := tx.Exec(ctx,
			"UPDATE _dbm.migration_history SET active = false WHERE execution_id = $1 AND active = true",
			int64(*execution.TargetExecutionID))
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return ErrInvalidRollback
		}
	case ExecutionTypeForward:
		var downSQL string
		if err := tx.QueryRow(ctx,
			"SELECT down_sql FROM _dbm.migration_history WHERE execution_id = $1 ORDER BY version DESC LIMIT 1",
			int64(*execution.TargetExecutionID)).Scan(&downSQL); err != nil {
			return err
		}
		execution.DownSQL = downSQL
	}
	return nil
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
