package migration

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func captureBatchRows(ctx context.Context, tx pgx.Tx, batch *BatchDefinition, keys []int64) (string, error) {
	var rows string
	err := withCatalogSearchPath(ctx, tx, func() error {
		return tx.QueryRow(ctx,
			`SELECT coalesce(jsonb_object_agg(t.`+batch.quotedKeyColumn()+`::text, to_jsonb(t)), '{}'::jsonb)::text
			 FROM `+batch.qualifiedTable()+` AS t
			 WHERE t.`+batch.quotedKeyColumn()+` = ANY($1::bigint[])`, keys).Scan(&rows)
	})
	if err != nil {
		return "", fmt.Errorf("capturing pre-batch row values: %w", err)
	}
	return rows, nil
}

func changedBatchRows(ctx context.Context, tx pgx.Tx, batch *BatchDefinition, keys []int64, before string) (string, error) {
	var restore string
	err := withCatalogSearchPath(ctx, tx, func() error {
		return tx.QueryRow(ctx,
			`WITH before AS (
				SELECT b.key, b.value AS row FROM jsonb_each($2::jsonb) AS b
			), after AS (
				SELECT t.`+batch.quotedKeyColumn()+`::text AS key, to_jsonb(t) AS row
				FROM `+batch.qualifiedTable()+` AS t
				WHERE t.`+batch.quotedKeyColumn()+` = ANY($1::bigint[])
			)
			SELECT coalesce(jsonb_object_agg(before.key, CASE
				WHEN after.row IS NULL THEN before.row
				ELSE (SELECT coalesce(jsonb_object_agg(c.key, c.value), '{}'::jsonb)
				      FROM jsonb_each(before.row) AS c
				      WHERE after.row -> c.key IS DISTINCT FROM c.value)
			END), '{}'::jsonb)::text
			FROM before LEFT JOIN after USING (key)
			WHERE after.row IS DISTINCT FROM before.row`, keys, before).Scan(&restore)
	})
	if err != nil {
		return "", fmt.Errorf("recording pre-batch values of changed rows: %w", err)
	}
	return restore, nil
}

func restoreBatchRows(ctx context.Context, tx pgx.Tx, batch *BatchDefinition, batchNumber int, restore string) error {
	return withCatalogSearchPath(ctx, tx, func() error {
		table := batch.qualifiedTable()
		key := batch.quotedKeyColumn()
		var missing []int64
		if err := tx.QueryRow(ctx,
			`SELECT coalesce(array_agg(r.key::bigint ORDER BY r.key::bigint), '{}')
			 FROM jsonb_each($1::jsonb) AS r
			 WHERE NOT EXISTS (SELECT 1 FROM `+table+` AS t WHERE t.`+key+` = r.key::bigint)`, restore).Scan(&missing); err != nil {
			return fmt.Errorf("checking rows to restore for batch %d: %w", batchNumber, err)
		}
		if len(missing) > 0 {
			return fmt.Errorf("%w: batch %d cannot be restored because rows %v no longer exist", ErrInvalidCompensation, batchNumber, missing)
		}

		var columns []string
		if err := tx.QueryRow(ctx,
			`SELECT coalesce(array_agg(a.attname::text ORDER BY a.attnum), '{}')
			 FROM pg_attribute AS a
			 WHERE a.attrelid = to_regclass($1) AND a.attnum > 0 AND NOT a.attisdropped
			   AND a.attgenerated = '' AND a.attidentity <> 'a'
			   AND a.attname::text IN (SELECT DISTINCT jsonb_object_keys(r.value) FROM jsonb_each($2::jsonb) AS r)`,
			table, restore).Scan(&columns); err != nil {
			return fmt.Errorf("listing columns to restore for batch %d: %w", batchNumber, err)
		}
		if len(columns) == 0 {
			return nil
		}
		assignments := make([]string, len(columns))
		for i, column := range columns {
			quoted := quoteBatchIdentifier(column)
			assignments[i] = quoted + " = (jsonb_populate_record(t, r.value))." + quoted
		}
		if _, err := tx.Exec(ctx,
			`UPDATE `+table+` AS t SET `+strings.Join(assignments, ", ")+`
			 FROM jsonb_each($1::jsonb) AS r
			 WHERE t.`+key+` = r.key::bigint
			   AND EXISTS (SELECT 1 FROM jsonb_each(r.value) AS c WHERE to_jsonb(t) -> c.key IS DISTINCT FROM c.value)`,
			restore); err != nil {
			return fmt.Errorf("restoring pre-batch values for batch %d: %w", batchNumber, err)
		}
		return nil
	})
}
