package migration

import (
	"context"
	"errors"
	"fmt"
	"time"
	"github.com/jackc/pgx/v5"
)

func historyExists(ctx context.Context, tx pgx.Tx) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, "SELECT to_regclass('_dbm.migration_history') IS NOT NULL").Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("migration: check history table: %w", err)
	}
	return exists, nil
}

func listHistory(ctx context.Context, tx pgx.Tx, page, pageSize int) (*HistoryPage, error) {
	result := &HistoryPage{Entries: []HistorySummary{}, Page: page, PageSize: pageSize}

	exists, err := historyExists(ctx, tx)
	if err != nil || !exists {
		return result, err
	}

	offset := int64(page-1) * int64(pageSize)
	rows, err := tx.Query(ctx,
		`SELECT version, name, executed_by, executed_at, duration_ms
		FROM _dbm.migration_history
		ORDER BY version DESC
		LIMIT $1 OFFSET $2`, pageSize+1, offset)
	if err != nil {
		return nil, fmt.Errorf("migration: list history: %w", err)
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (HistorySummary, error) {
		var (
			e          HistorySummary
			executedBy int64
			durationMS int64
		)
		if err := row.Scan(&e.Version, &e.Name, &executedBy, &e.ExecutedAt, &durationMS); err != nil {
			return HistorySummary{}, err
		}
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

func getHistoryEntry(ctx context.Context, tx pgx.Tx, version int64) (*HistoryEntry, error) {
	exists, err := historyExists(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrVersionNotFound
	}

	var (
		e          HistoryEntry
		executedBy int64
		durationMS int64
	)
	err = tx.QueryRow(ctx,
		`SELECT version, name, sql, executed_by, executed_at, duration_ms
		FROM _dbm.migration_history
		WHERE version = $1`, version).Scan(&e.Version, &e.Name, &e.SQL, &executedBy, &e.ExecutedAt, &durationMS)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("migration: get history entry: %w", err)
	}

	e.ExecutedBy = uint(executedBy)
	e.Duration = time.Duration(durationMS) * time.Millisecond
	return &e, nil
}