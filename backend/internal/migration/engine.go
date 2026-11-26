package migration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	execution_id        bigint,
	attempt_number      integer NOT NULL DEFAULT 1,
	status              text NOT NULL DEFAULT 'success',
	error_code          text NOT NULL DEFAULT '',
	error_message       text NOT NULL DEFAULT '',
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
);
ALTER TABLE _dbm.migration_history ADD COLUMN IF NOT EXISTS attempt_number integer NOT NULL DEFAULT 1;
ALTER TABLE _dbm.migration_history ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'success';
ALTER TABLE _dbm.migration_history ADD COLUMN IF NOT EXISTS error_code text NOT NULL DEFAULT '';
ALTER TABLE _dbm.migration_history ADD COLUMN IF NOT EXISTS error_message text NOT NULL DEFAULT '';
ALTER TABLE _dbm.migration_history DROP CONSTRAINT IF EXISTS migration_history_execution_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS migration_history_execution_attempt_key
	ON _dbm.migration_history (execution_id, attempt_number)
	WHERE execution_id IS NOT NULL;`

func ensureHistorySchema(ctx context.Context, pool *pgxpool.Pool, create bool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning migration history schema check: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", "dbm.migration.history_schema"); err != nil {
		return fmt.Errorf("locking migration history schema: %w", err)
	}

	var (
		exists          bool
		hasNewFields    bool
		hasLegacyUnique bool
	)
	if err := tx.QueryRow(ctx, `SELECT
		to_regclass('_dbm.migration_history') IS NOT NULL,
		(SELECT count(*) = 4 FROM information_schema.columns
		 WHERE table_schema = '_dbm' AND table_name = 'migration_history'
		 AND column_name IN ('attempt_number', 'status', 'error_code', 'error_message')),
		EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conrelid = to_regclass('_dbm.migration_history')
			AND conname = 'migration_history_execution_id_key'
		)`).Scan(&exists, &hasNewFields, &hasLegacyUnique); err != nil {
		return fmt.Errorf("checking migration history schema: %w", err)
	}
	if !exists && !create || exists && hasNewFields && !hasLegacyUnique {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, ensureHistorySQL); err != nil {
		return fmt.Errorf("ensuring migration history schema: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing migration history schema: %w", err)
	}
	return nil
}

func applyExecution(ctx context.Context, pool *pgxpool.Pool, execution *Execution, opts Options) (*HistoryEntry, error) {
	attemptStartedAt := time.Now()
	if err := ensureHistorySchema(ctx, pool, true); err != nil {
		return nil, executionError(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		attemptErr := fmt.Errorf("migration: begin transaction: %w", err)
		if recordErr := recordFailedHistoryAttempt(ctx, pool, execution, attemptErr, time.Since(attemptStartedAt)); recordErr != nil {
			return nil, errors.Join(attemptErr, recordErr)
		}
		return nil, attemptErr
	}
	fail := func(attemptErr error) (*HistoryEntry, error) {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
		if recordErr := recordFailedHistoryAttempt(ctx, pool, execution, attemptErr, time.Since(attemptStartedAt)); recordErr != nil {
			return nil, errors.Join(attemptErr, recordErr)
		}
		return nil, attemptErr
	}

	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtext($1))", lockName).Scan(&locked); err != nil {
		return fail(fmt.Errorf("migration: acquire lock: %w", err))
	}
	if !locked {
		return fail(ErrMigrationInProgress)
	}

	timeouts := fmt.Sprintf("SET LOCAL statement_timeout = %d; SET LOCAL lock_timeout = %d",
		opts.StatementTimeout.Milliseconds(), opts.LockTimeout.Milliseconds())
	if _, err := tx.Exec(ctx, timeouts); err != nil {
		return fail(fmt.Errorf("migration: set timeouts: %w", err))
	}
	if _, err := tx.Exec(ctx, "LOCK TABLE _dbm.migration_history IN EXCLUSIVE MODE"); err != nil {
		return fail(executionError(err))
	}
	if err := validateExecutionPosition(ctx, tx, execution); err != nil {
		return fail(err)
	}

	var version int64
	if err := tx.QueryRow(ctx,
		"SELECT coalesce(max(version), 0) + 1 FROM _dbm.migration_history").Scan(&version); err != nil {
		return fail(executionError(err))
	}

	start := time.Now()
	slog.Info("migration SQL attempt started",
		"execution_id", execution.ID,
		"database_id", execution.ManagedDatabaseID,
		"execution_type", execution.Type,
		"attempt", historyAttemptNumber(execution),
		"statement_type", "migration",
		"sql", safeSQLForLog(execution.SQL),
	)
	command, err := tx.Exec(ctx, execution.SQL)
	if err != nil {
		slog.Error("migration SQL attempt failed",
			"execution_id", execution.ID,
			"database_id", execution.ManagedDatabaseID,
			"execution_type", execution.Type,
			"attempt", historyAttemptNumber(execution),
			"duration", time.Since(start),
			"error", err,
		)
		return fail(executionError(err))
	}
	slog.Info("migration SQL attempt finished",
		"execution_id", execution.ID,
		"database_id", execution.ManagedDatabaseID,
		"execution_type", execution.Type,
		"attempt", historyAttemptNumber(execution),
		"duration", time.Since(start),
		"rows_affected", command.RowsAffected(),
	)
	if tx.Conn().PgConn().TxStatus() != 'T' {
		return fail(ErrTransactionControl)
	}

	if err := updateExecutionPosition(ctx, tx, execution); err != nil {
		return fail(executionError(err))
	}

	duration := time.Since(attemptStartedAt)
	attemptNumber := historyAttemptNumber(execution)
	entry := &HistoryEntry{
		Version:       version,
		AttemptNumber: attemptNumber,
		Status:        RunStatusSuccess,
		Name:          execution.Name,
		SQL:           execution.SQL,
		ExecutedBy:    execution.ExecutedBy,
		Duration:      duration,
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
			(version, execution_id, attempt_number, status, execution_type, target_execution_id,
			 active, name, sql, down_sql, irreversible_reason, executed_by, duration_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING executed_at`,
		version, executionID, attemptNumber, RunStatusSuccess, execution.Type, targetExecutionID,
		execution.Type != ExecutionTypeRollback, execution.Name, execution.SQL, execution.DownSQL,
		execution.IrreversibleReason, int64(execution.ExecutedBy), duration.Milliseconds()).Scan(&entry.ExecutedAt); err != nil {
		return fail(executionError(err))
	}

	if err := tx.Commit(ctx); err != nil {
		attemptErr := executionError(err)
		if recordErr := recordFailedHistoryAttempt(ctx, pool, execution, attemptErr, time.Since(attemptStartedAt)); recordErr != nil {
			return nil, errors.Join(attemptErr, recordErr)
		}
		return nil, attemptErr
	}
	entry.ExecutionID = execution.ID
	entry.Type = execution.Type
	entry.IrreversibleReason = execution.IrreversibleReason
	entry.TargetExecutionID = execution.TargetExecutionID
	entry.Active = execution.Type != ExecutionTypeRollback
	return entry, nil
}

func historyAttemptNumber(execution *Execution) int {
	if execution.Attempt > 0 {
		return execution.Attempt
	}
	return 1
}

func recordFailedHistoryAttempt(ctx context.Context, pool *pgxpool.Pool, execution *Execution, attemptErr error, duration time.Duration) error {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	tx, err := pool.Begin(writeCtx)
	if err != nil {
		return fmt.Errorf("begin failed migration history transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(writeCtx) }()

	if _, err := tx.Exec(writeCtx, "LOCK TABLE _dbm.migration_history IN EXCLUSIVE MODE"); err != nil {
		return fmt.Errorf("locking failed migration history: %w", err)
	}
	if execution.ID > 0 {
		var successExists bool
		if err := tx.QueryRow(writeCtx,
			`SELECT EXISTS (
				SELECT 1 FROM _dbm.migration_history
				WHERE execution_id = $1 AND attempt_number = $2 AND status = $3
			)`, int64(execution.ID), historyAttemptNumber(execution), RunStatusSuccess).Scan(&successExists); err != nil {
			return fmt.Errorf("checking successful migration history before recording failure: %w", err)
		}
		if successExists {
			return nil
		}
	}

	var version int64
	if err := tx.QueryRow(writeCtx,
		"SELECT coalesce(max(version), 0) + 1 FROM _dbm.migration_history").Scan(&version); err != nil {
		return fmt.Errorf("allocating failed migration history version: %w", err)
	}
	var executionID any
	if execution.ID > 0 {
		executionID = int64(execution.ID)
	}
	var targetExecutionID any
	if execution.TargetExecutionID != nil {
		targetExecutionID = int64(*execution.TargetExecutionID)
	}
	code, message := migrationFailureDetails(attemptErr)
	if _, err := tx.Exec(writeCtx,
		`INSERT INTO _dbm.migration_history
			(version, execution_id, attempt_number, status, error_code, error_message,
			 execution_type, target_execution_id, active, name, sql, down_sql,
			 irreversible_reason, executed_by, duration_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, false, $9, $10, $11, $12, $13, $14)`,
		version, executionID, historyAttemptNumber(execution), RunStatusFailed, code, message,
		execution.Type, targetExecutionID, execution.Name, execution.SQL, execution.DownSQL,
		execution.IrreversibleReason, int64(execution.ExecutedBy), duration.Milliseconds()); err != nil {
		return fmt.Errorf("recording failed migration history: %w", err)
	}
	if err := tx.Commit(writeCtx); err != nil {
		return fmt.Errorf("committing failed migration history: %w", err)
	}
	return nil
}

func migrationFailureDetails(err error) (string, string) {
	var executionErr *ExecutionError
	if errors.As(err, &executionErr) {
		return executionErr.Code, executionErr.Message
	}
	switch {
	case errors.Is(err, ErrMigrationInProgress):
		return "migration_in_progress", err.Error()
	case errors.Is(err, ErrInvalidRollback):
		return "invalid_rollback", err.Error()
	case errors.Is(err, ErrInvalidForward):
		return "invalid_forward", err.Error()
	case errors.Is(err, ErrTransactionControl):
		return "transaction_control", err.Error()
	case errors.Is(err, ErrInvalidInput):
		return "invalid_input", err.Error()
	default:
		return "execution_error", err.Error()
	}
}

func validateExecutionPosition(ctx context.Context, tx pgx.Tx, execution *Execution) error {
	switch execution.Type {
	case ExecutionTypeUp:
		return nil
	case ExecutionTypeRollback:
		if execution.TargetExecutionID == nil {
			return ErrInvalidInput
		}
		if execution.Compensation {
			var scopeComplete, committedBatchExists bool
			if err := tx.QueryRow(ctx,
				`SELECT scope_complete FROM _dbm.migration_batch_progress WHERE execution_id = $1`,
				int64(*execution.TargetExecutionID)).Scan(&scopeComplete); err != nil {
				return executionError(err)
			}
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS (
					SELECT 1 FROM _dbm.migration_batch_commits
					WHERE execution_id = $1
				)`, int64(*execution.TargetExecutionID)).Scan(&committedBatchExists); err != nil {
				return executionError(err)
			}
			if !scopeComplete || !committedBatchExists {
				return ErrInvalidCompensation
			}
			break
		}
		var activeID int64
		var downSQL string
		if err := tx.QueryRow(ctx,
			`SELECT execution_id, down_sql FROM _dbm.migration_history
			 WHERE active = true AND status = 'success' ORDER BY version DESC LIMIT 1`).Scan(&activeID, &downSQL); err != nil {
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
			 JOIN _dbm.migration_history original
			   ON original.execution_id = h.target_execution_id AND original.status = 'success'
			 WHERE h.status = 'success'
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
		if execution.Compensation {
			return nil
		}
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
			"SELECT down_sql FROM _dbm.migration_history WHERE execution_id = $1 AND status = 'success' ORDER BY version DESC LIMIT 1",
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
