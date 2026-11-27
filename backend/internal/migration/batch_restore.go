package migration

import (
	"context"
	"fmt"

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

func verifyBatchCompensated(ctx context.Context, tx pgx.Tx, batch *BatchDefinition, batchNumber int, restore string) error {
	var unrestored []int64
	err := withCatalogSearchPath(ctx, tx, func() error {
		return tx.QueryRow(ctx,
			`SELECT coalesce(array_agg(r.key::bigint ORDER BY r.key::bigint), '{}')
			 FROM jsonb_each($1::jsonb) AS r
			 LEFT JOIN `+batch.qualifiedTable()+` AS t ON t.`+batch.quotedKeyColumn()+` = r.key::bigint
			 WHERE t.`+batch.quotedKeyColumn()+` IS NULL
			    OR EXISTS (SELECT 1 FROM jsonb_each(r.value) AS c WHERE to_jsonb(t) -> c.key IS DISTINCT FROM c.value)`,
			restore).Scan(&unrestored)
	})
	if err != nil {
		return fmt.Errorf("verifying compensation of batch %d: %w", batchNumber, err)
	}
	if len(unrestored) == 0 {
		return nil
	}
	shown := unrestored
	if len(shown) > 10 {
		shown = shown[:10]
	}
	return fmt.Errorf("%w: Down SQL did not restore the recorded pre-batch values of batch %d (%d rows, keys %v); compensation is unsafe, create a new corrective migration instead",
		ErrInvalidCompensation, batchNumber, len(unrestored), shown)
}
