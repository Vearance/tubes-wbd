package migration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	errTargetCommitOutcomeUnknown  = errors.New("migration: target commit outcome unknown")
	errBatchReconciliationRequired = errors.New("migration: batch checkpoint reconciliation required")
)

type targetCommitError struct {
	cause error
}

func (e targetCommitError) Error() string {
	return fmt.Sprintf("committing target transaction: %v", e.cause)
}

func (e targetCommitError) Unwrap() error { return e.cause }

func (e targetCommitError) Is(target error) bool {
	return target == errTargetCommitOutcomeUnknown
}

const ensureBatchSchemaSQL = `
CREATE SCHEMA IF NOT EXISTS _dbm;
CREATE TABLE IF NOT EXISTS _dbm.migration_batch_progress (
	execution_id bigint PRIMARY KEY,
	last_cursor bigint NOT NULL DEFAULT 0,
	batch_count integer NOT NULL DEFAULT 0,
	rows_processed bigint NOT NULL DEFAULT 0,
	scope_complete boolean NOT NULL DEFAULT false,
	completed boolean NOT NULL DEFAULT false
);
ALTER TABLE _dbm.migration_batch_progress
	ADD COLUMN IF NOT EXISTS scope_complete boolean NOT NULL DEFAULT false;
CREATE TABLE IF NOT EXISTS _dbm.migration_batch_commits (
	execution_id bigint NOT NULL,
	batch_number integer NOT NULL,
	keys bigint[] NOT NULL,
	rows_processed bigint NOT NULL,
	committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
	PRIMARY KEY (execution_id, batch_number)
	);
CREATE TABLE IF NOT EXISTS _dbm.migration_batch_scope (
	execution_id bigint NOT NULL,
	key bigint NOT NULL,
	PRIMARY KEY (execution_id, key)
);`

type batchProgress struct {
	LastCursor    int64
	BatchCount    int
	RowsProcessed int64
	Completed     bool
}

func (s *MigrationService) runBatchExecution(ctx context.Context, execution *Execution, pool *pgxpool.Pool) (*HistoryEntry, error) {
	if err := ensureHistorySchema(ctx, pool, true); err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, ensureBatchSchemaSQL); err != nil {
		return nil, fmt.Errorf("ensuring batch checkpoint schema: %w", err)
	}

	startedAt := time.Now()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", lockName); err != nil {
		return nil, fmt.Errorf("acquiring batch migration lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock(hashtext($1))", lockName)
	}()
	if _, err := conn.Exec(ctx, fmt.Sprintf("SET statement_timeout = %d; SET lock_timeout = %d",
		s.opts.StatementTimeout.Milliseconds(), s.opts.LockTimeout.Milliseconds())); err != nil {
		return nil, fmt.Errorf("setting batch migration timeouts: %w", err)
	}
	defer func() {
		resetCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_, _ = conn.Exec(resetCtx, "RESET statement_timeout; RESET lock_timeout")
	}()

	var prior *HistoryEntry
	if err := withTargetTransaction(ctx, conn, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT to_regclass('_dbm.migration_history') IS NOT NULL").Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return nil
		}
		entry, err := getExecutionHistoryByID(ctx, tx, execution.ID)
		if errors.Is(err, ErrVersionNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		prior = entry
		return nil
	}); err != nil {
		return nil, err
	}
	if prior != nil {
		return prior, nil
	}

	if err := withTargetTransaction(ctx, conn, func(tx pgx.Tx) error {
		return validateExecutionPosition(ctx, tx, execution)
	}); err != nil {
		return nil, err
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO _dbm.migration_batch_progress (execution_id) VALUES ($1) ON CONFLICT (execution_id) DO NOTHING`,
		int64(execution.ID)); err != nil {
		return nil, fmt.Errorf("initializing batch checkpoint: %w", err)
	}
	scopeExecutionID := execution.ID
	switch execution.Type {
	case ExecutionTypeUp:
		if err := withTargetTransaction(ctx, conn, func(tx pgx.Tx) error {
			return snapshotBatchScope(ctx, tx, execution)
		}); err != nil {
			return nil, recordBatchFailure(ctx, pool, execution, err, startedAt)
		}
	case ExecutionTypeForward:
		if execution.TargetExecutionID == nil {
			return nil, ErrInvalidForward
		}
		scopeExecutionID = *execution.TargetExecutionID
		var scopeComplete bool
		if err := conn.QueryRow(ctx,
			`SELECT scope_complete FROM _dbm.migration_batch_progress WHERE execution_id = $1`,
			int64(scopeExecutionID)).Scan(&scopeComplete); err != nil {
			return nil, recordBatchFailure(ctx, pool, execution, fmt.Errorf("reading original batch scope: %w", err), startedAt)
		}
		if !scopeComplete {
			return nil, recordBatchFailure(ctx, pool, execution, ErrInvalidForward, startedAt)
		}
	}

	for {
		progress, err := readBatchProgress(ctx, conn, execution.ID)
		if err != nil {
			return nil, recordBatchFailure(ctx, pool, execution, err, startedAt)
		}
		applyBatchProgress(execution, progress)
		if err := s.updateExecutionProgress(ctx, execution); err != nil {
			return nil, fmt.Errorf("reconciling batch progress with Platform DB: %w", err)
		}
		if progress.Completed {
			break
		}

		nextProgress := progress
		err = withTargetTransaction(ctx, conn, func(tx pgx.Tx) error {
			var keys []int64
			nextCursor := progress.LastCursor
			if execution.Type == ExecutionTypeRollback {
				if execution.TargetExecutionID == nil {
					return ErrInvalidRollback
				}
				beforeBatch := progress.LastCursor
				if beforeBatch == 0 {
					beforeBatch = math.MaxInt32
				}
				var originalBatch int
				err := tx.QueryRow(ctx,
					`SELECT batch_number, keys FROM _dbm.migration_batch_commits
					 WHERE execution_id = $1 AND batch_number < $2
					 ORDER BY batch_number DESC LIMIT 1`, int64(*execution.TargetExecutionID), beforeBatch,
				).Scan(&originalBatch, &keys)
				if errors.Is(err, pgx.ErrNoRows) {
					keys = nil
				} else if err != nil {
					return fmt.Errorf("selecting batch keys for rollback: %w", err)
				} else {
					nextCursor = int64(originalBatch)
				}
			} else {
				slog.Info("selecting keys from frozen migration batch scope",
					"execution_id", execution.ID,
					"database_id", execution.ManagedDatabaseID,
					"execution_type", execution.Type,
					"attempt", execution.Attempt,
					"batch_number", progress.BatchCount+1,
					"schema", execution.Batch.Schema,
					"table", execution.Batch.Table,
					"key_column", execution.Batch.KeyColumn,
				)
				rows, err := tx.Query(ctx,
					`SELECT key FROM _dbm.migration_batch_scope
					 WHERE execution_id = $1 AND key > $2 ORDER BY key LIMIT $3`,
					int64(scopeExecutionID), progress.LastCursor, execution.Batch.BatchSize,
				)
				if err != nil {
					return fmt.Errorf("selecting batch cursor: %w", err)
				}
				keys, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (int64, error) {
					var key int64
					return key, row.Scan(&key)
				})
				if err != nil {
					return fmt.Errorf("reading batch cursor: %w", err)
				}
				if len(keys) > 0 {
					previous := progress.LastCursor
					for _, key := range keys {
						if key <= previous {
							return fmt.Errorf("frozen batch keys must be strictly increasing positive bigints")
						}
						previous = key
					}
					nextCursor = previous
				}
			}
			if len(keys) > execution.Batch.BatchSize {
				return fmt.Errorf("batch cursor returned %d keys, exceeding configured batch size %d", len(keys), execution.Batch.BatchSize)
			}
			if len(keys) == 0 {
				if _, err := tx.Exec(ctx, `UPDATE _dbm.migration_batch_progress SET completed = true WHERE execution_id = $1`, int64(execution.ID)); err != nil {
					return fmt.Errorf("marking batch execution complete: %w", err)
				}
				nextProgress.Completed = true
				return nil
			}
			sqlKind := "batch_up"
			if execution.Type == ExecutionTypeRollback {
				sqlKind = "batch_down"
			}
			slog.Info("migration SQL attempt started",
				"execution_id", execution.ID,
				"database_id", execution.ManagedDatabaseID,
				"execution_type", execution.Type,
				"attempt", execution.Attempt,
				"batch_number", progress.BatchCount+1,
				"key_count", len(keys),
				"statement_type", sqlKind,
				"sql", safeSQLForLog(execution.SQL),
			)
			command, err := tx.Exec(ctx, execution.SQL, keys)
			if err != nil {
				return fmt.Errorf("executing batch SQL: %w", err)
			}
			batchNumber := progress.BatchCount + 1
			rowsAffected := command.RowsAffected()
			if _, err := tx.Exec(ctx,
				`INSERT INTO _dbm.migration_batch_commits (execution_id, batch_number, keys, rows_processed)
				 VALUES ($1, $2, $3, $4)`, int64(execution.ID), batchNumber, keys, rowsAffected); err != nil {
				return fmt.Errorf("recording committed batch keys: %w", err)
			}
			if _, err := tx.Exec(ctx,
				`UPDATE _dbm.migration_batch_progress
				 SET last_cursor = $2, batch_count = $3, rows_processed = rows_processed + $4
				 WHERE execution_id = $1`, int64(execution.ID), nextCursor, batchNumber, rowsAffected); err != nil {
				return fmt.Errorf("recording committed batch progress: %w", err)
			}
			nextProgress.LastCursor = nextCursor
			nextProgress.BatchCount = batchNumber
			nextProgress.RowsProcessed += rowsAffected
			return nil
		})
		if err != nil {
			if errors.Is(err, errTargetCommitOutcomeUnknown) {
				actualProgress, committed, reconcileErr := s.reconcileBatchCommit(ctx, conn, execution, progress)
				if reconcileErr != nil {
					return nil, reconcileErr
				}
				if committed {
					progress = actualProgress
					continue
				}
			}
			return nil, recordBatchFailure(ctx, pool, execution, err, startedAt)
		}
		progress = nextProgress
		applyBatchProgress(execution, progress)
		if err := s.updateExecutionProgress(ctx, execution); err != nil {
			return nil, batchReconciliationError("persisting batch progress to Platform DB", err)
		}
	}

	entry, err := finishBatchHistory(ctx, conn, execution, time.Since(startedAt))
	if err != nil {
		return nil, recordBatchFailure(ctx, pool, execution, err, startedAt)
	}
	return entry, nil
}

func withTargetTransaction(ctx context.Context, conn *pgxpool.Conn, fn func(pgx.Tx) error) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return targetCommitError{cause: err}
	}
	return nil
}

func readBatchProgress(ctx context.Context, conn *pgxpool.Conn, executionID uint) (batchProgress, error) {
	var progress batchProgress
	err := conn.QueryRow(ctx,
		`SELECT last_cursor, batch_count, rows_processed, completed
		 FROM _dbm.migration_batch_progress WHERE execution_id = $1`, int64(executionID)).Scan(
		&progress.LastCursor, &progress.BatchCount, &progress.RowsProcessed, &progress.Completed,
	)
	return progress, err
}

func readBatchProgressTx(ctx context.Context, tx pgx.Tx, executionID uint) (batchProgress, error) {
	var progress batchProgress
	err := tx.QueryRow(ctx,
		`SELECT last_cursor, batch_count, rows_processed, completed
		 FROM _dbm.migration_batch_progress WHERE execution_id = $1`, int64(executionID)).Scan(
		&progress.LastCursor, &progress.BatchCount, &progress.RowsProcessed, &progress.Completed,
	)
	return progress, err
}

func (s *MigrationService) reconcileBatchCommit(
	ctx context.Context,
	conn *pgxpool.Conn,
	execution *Execution,
	previous batchProgress,
) (batchProgress, bool, error) {
	actual, err := readBatchProgress(ctx, conn, execution.ID)
	if err != nil {
		return batchProgress{}, false, batchReconciliationError("reading target checkpoint after commit error", err)
	}

	committed := actual.BatchCount == previous.BatchCount+1 ||
		actual.BatchCount == previous.BatchCount && !previous.Completed && actual.Completed
	unchanged := actual == previous
	if !committed && !unchanged {
		return batchProgress{}, false, batchReconciliationError(
			fmt.Sprintf("target checkpoint changed unexpectedly from batch %d to %d", previous.BatchCount, actual.BatchCount),
			errTargetCommitOutcomeUnknown,
		)
	}

	applyBatchProgress(execution, actual)
	if err := s.updateExecutionProgress(ctx, execution); err != nil {
		return batchProgress{}, false, batchReconciliationError("syncing target checkpoint to Platform DB", err)
	}
	return actual, committed, nil
}

func batchReconciliationError(message string, err error) error {
	return fmt.Errorf("%w: %s: %w", errBatchReconciliationRequired, message, err)
}

func applyBatchProgress(execution *Execution, progress batchProgress) {
	execution.BatchCount = progress.BatchCount
	execution.RowsProcessed = progress.RowsProcessed
	cursor := progress.LastCursor
	execution.LastCursor = &cursor
	execution.BatchDone = progress.Completed
}

func (s *MigrationService) updateExecutionProgress(ctx context.Context, execution *Execution) error {
	repo, ok := s.repo.(interface {
		UpdateExecutionProgress(context.Context, uint, int, int64, *int64, bool) error
	})
	if !ok {
		return errors.New("migration: repository does not support batch progress")
	}
	return repo.UpdateExecutionProgress(ctx, execution.ID, execution.BatchCount, execution.RowsProcessed, execution.LastCursor, execution.BatchDone)
}

func recordBatchFailure(ctx context.Context, pool *pgxpool.Pool, execution *Execution, err error, startedAt time.Time) error {
	if recordErr := recordFailedHistoryAttempt(ctx, pool, execution, err, time.Since(startedAt)); recordErr != nil {
		return errors.Join(err, recordErr)
	}
	return err
}

func finishBatchHistory(ctx context.Context, conn *pgxpool.Conn, execution *Execution, duration time.Duration) (*HistoryEntry, error) {
	var entry *HistoryEntry
	err := withTargetTransaction(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "LOCK TABLE _dbm.migration_history IN EXCLUSIVE MODE"); err != nil {
			return err
		}
		if err := validateExecutionPosition(ctx, tx, execution); err != nil {
			return err
		}
		var version int64
		if err := tx.QueryRow(ctx, "SELECT coalesce(max(version), 0) + 1 FROM _dbm.migration_history").Scan(&version); err != nil {
			return err
		}
		if err := updateExecutionPosition(ctx, tx, execution); err != nil {
			return err
		}
		var executionID any
		if execution.ID > 0 {
			executionID = int64(execution.ID)
		}
		var targetID any
		if execution.TargetExecutionID != nil {
			targetID = int64(*execution.TargetExecutionID)
		}
		entry = &HistoryEntry{
			Version: version, ExecutionID: execution.ID, AttemptNumber: historyAttemptNumber(execution),
			Status: RunStatusSuccess, Type: execution.Type, TargetExecutionID: execution.TargetExecutionID,
			Active: execution.Type != ExecutionTypeRollback, Name: execution.Name, SQL: execution.SQL,
			DownSQL: execution.DownSQL, IrreversibleReason: execution.IrreversibleReason,
			ExecutedBy: execution.ExecutedBy, Duration: duration,
		}
		return tx.QueryRow(ctx,
			`INSERT INTO _dbm.migration_history
				(version, execution_id, attempt_number, status, execution_type, target_execution_id,
				 active, name, sql, down_sql, irreversible_reason, executed_by, duration_ms)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			 RETURNING executed_at`,
			version, executionID, historyAttemptNumber(execution), RunStatusSuccess, execution.Type, targetID,
			entry.Active, execution.Name, execution.SQL, execution.DownSQL, execution.IrreversibleReason,
			int64(execution.ExecutedBy), duration.Milliseconds()).Scan(&entry.ExecutedAt)
	})
	if err != nil {
		return nil, fmt.Errorf("recording completed batch migration history: %w", err)
	}
	return entry, nil
}
