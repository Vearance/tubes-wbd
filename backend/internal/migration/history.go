package migration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func historyExists(ctx context.Context, tx pgx.Tx) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, "SELECT to_regclass('_dbm.migration_history') IS NOT NULL").Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("migration: check history table: %w", err)
	}
	return exists, nil
}

func listHistory(ctx context.Context, tx pgx.Tx, page, pageSize int, hideRollback bool) (*HistoryPage, error) {
	result := &HistoryPage{Entries: []HistorySummary{}, Page: page, PageSize: pageSize}

	exists, err := historyExists(ctx, tx)
	if err != nil || !exists {
		return result, err
	}

	offset := int64(page-1) * int64(pageSize)
	rows, err := tx.Query(ctx,
		`SELECT h.version, h.execution_id, h.attempt_number, h.status, h.error_code, h.error_message,
			h.execution_type, h.target_execution_id, h.active, h.name, h.executed_by, h.executed_at, h.duration_ms
		FROM _dbm.migration_history h
		WHERE NOT $1::boolean OR (
			NOT (
				h.status = 'success'
				AND h.execution_type = 'rollback'
				AND EXISTS (
					SELECT 1 FROM _dbm.migration_history target
					WHERE target.status = 'success'
						AND target.execution_id = h.target_execution_id
				)
			)
			AND NOT (
				h.status = 'success'
				AND EXISTS (
					SELECT 1 FROM _dbm.migration_history rollback
					WHERE rollback.status = 'success'
						AND rollback.execution_type = 'rollback'
						AND rollback.target_execution_id = h.execution_id
				)
			)
		)
		ORDER BY h.version DESC
		LIMIT $2 OFFSET $3`, hideRollback, pageSize+1, offset)
	if err != nil {
		return nil, fmt.Errorf("migration: list history: %w", err)
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (HistorySummary, error) {
		var (
			e               HistorySummary
			executionID     pgtype.Int8
			targetID        pgtype.Int8
			executionType   string
			executionStatus string
			executedBy      int64
			durationMS      int64
		)
		if err := row.Scan(&e.Version, &executionID, &e.AttemptNumber, &executionStatus, &e.ErrorCode, &e.ErrorMessage,
			&executionType, &targetID, &e.Active, &e.Name, &executedBy, &e.ExecutedAt, &durationMS); err != nil {
			return HistorySummary{}, err
		}
		if executionID.Valid {
			id := uint(executionID.Int64)
			e.ExecutionID = &id
		}
		if targetID.Valid {
			id := uint(targetID.Int64)
			e.TargetExecutionID = &id
		}
		e.Type = ExecutionType(executionType)
		e.Status = RunStatus(executionStatus)
		e.ExecutedBy = uint(executedBy)
		e.Duration = time.Duration(durationMS) * time.Millisecond
		return e, nil
	})
	if err != nil {
		return nil, fmt.Errorf("migration: list history: %w", err)
	}

	if len(entries) > pageSize {
		result.HasNext = true
		entries = entries[:pageSize]
	}
	result.Entries = append(result.Entries, entries...)
	return result, nil
}

func getExecutionHistoryByID(ctx context.Context, tx pgx.Tx, executionID uint) (*HistoryEntry, error) {
	exists, err := historyExists(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrVersionNotFound
	}
	var (
		entry           HistoryEntry
		storedExecution pgtype.Int8
		targetExecution pgtype.Int8
		executionType   string
		executionStatus string
		executedBy      int64
		durationMS      int64
	)
	err = tx.QueryRow(ctx,
		`SELECT version, execution_id, attempt_number, status, error_code, error_message,
					execution_type, target_execution_id, active, name, sql, down_sql,
					irreversible_reason, executed_by, executed_at, duration_ms
				 FROM _dbm.migration_history WHERE execution_id = $1 AND status = 'success'
				 ORDER BY version DESC LIMIT 1`, int64(executionID)).Scan(
		&entry.Version, &storedExecution, &entry.AttemptNumber, &executionStatus, &entry.ErrorCode, &entry.ErrorMessage,
		&executionType, &targetExecution, &entry.Active, &entry.Name, &entry.SQL, &entry.DownSQL,
		&entry.IrreversibleReason, &executedBy, &entry.ExecutedAt, &durationMS,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("migration: get execution history: %w", err)
	}
	if storedExecution.Valid {
		entry.ExecutionID = uint(storedExecution.Int64)
	}
	if targetExecution.Valid {
		targetID := uint(targetExecution.Int64)
		entry.TargetExecutionID = &targetID
	}
	entry.Type = ExecutionType(executionType)
	entry.Status = RunStatus(executionStatus)
	entry.ExecutedBy = uint(executedBy)
	entry.Duration = time.Duration(durationMS) * time.Millisecond
	return &entry, nil
}

func latestActiveExecutionHistory(ctx context.Context, tx pgx.Tx) (*HistoryEntry, error) {
	exists, err := historyExists(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrVersionNotFound
	}
	var executionID pgtype.Int8
	err = tx.QueryRow(ctx,
		"SELECT execution_id FROM _dbm.migration_history WHERE active = true AND status = 'success' ORDER BY version DESC LIMIT 1").Scan(&executionID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !executionID.Valid {
		return nil, ErrVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("migration: find active execution history: %w", err)
	}
	return getExecutionHistoryByID(ctx, tx, uint(executionID.Int64))
}

func forwardCandidate(ctx context.Context, tx pgx.Tx) (*HistoryEntry, error) {
	exists, err := historyExists(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrInvalidForward
	}
	var executionID pgtype.Int8
	err = tx.QueryRow(ctx,
		`SELECT latest.execution_id
		FROM (
			SELECT execution_id, execution_type, target_execution_id
			FROM _dbm.migration_history
			WHERE status = 'success'
			ORDER BY version DESC
			LIMIT 1
		) latest
		WHERE latest.execution_type = 'rollback'
			AND EXISTS (
				SELECT 1 FROM _dbm.migration_history target
				WHERE target.status = 'success'
					AND target.execution_id = latest.target_execution_id
			)`).Scan(&executionID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !executionID.Valid {
		return nil, ErrInvalidForward
	}
	if err != nil {
		return nil, fmt.Errorf("migration: find forward candidate: %w", err)
	}
	return getExecutionHistoryByID(ctx, tx, uint(executionID.Int64))
}

func getHistoryEntry(ctx context.Context, tx pgx.Tx, version int64) (*HistoryEntry, error) {
	exists, err := historyExists(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrVersionNotFound
	}

	var (
		e               HistoryEntry
		executionID     pgtype.Int8
		targetExecution pgtype.Int8
		executionType   string
		executionStatus string
		executedBy      int64
		durationMS      int64
	)
	err = tx.QueryRow(ctx,
		`SELECT version, execution_id, attempt_number, status, error_code, error_message,
			execution_type, target_execution_id, active, name, sql, down_sql,
			irreversible_reason, executed_by, executed_at, duration_ms
		FROM _dbm.migration_history
		WHERE version = $1`, version).Scan(
		&e.Version, &executionID, &e.AttemptNumber, &executionStatus, &e.ErrorCode, &e.ErrorMessage,
		&executionType, &targetExecution, &e.Active, &e.Name, &e.SQL, &e.DownSQL, &e.IrreversibleReason,
		&executedBy, &e.ExecutedAt, &durationMS,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("migration: get history entry: %w", err)
	}

	if executionID.Valid {
		e.ExecutionID = uint(executionID.Int64)
	}
	if targetExecution.Valid {
		targetID := uint(targetExecution.Int64)
		e.TargetExecutionID = &targetID
	}
	e.Type = ExecutionType(executionType)
	e.Status = RunStatus(executionStatus)
	e.ExecutedBy = uint(executedBy)
	e.Duration = time.Duration(durationMS) * time.Millisecond
	return &e, nil
}
