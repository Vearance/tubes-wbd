package migration

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var (
	batchIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]*$`)
	batchParameterPattern  = regexp.MustCompile(`\$[0-9]+`)
)

func validateBatchDefinition(batch BatchDefinition) error {
	identifiers := []struct {
		label string
		value string
	}{
		{label: "schema", value: batch.Schema},
		{label: "table", value: batch.Table},
		{label: "key_column", value: batch.KeyColumn},
	}
	for _, identifier := range identifiers {
		if !validBatchIdentifier(identifier.value) {
			return fmt.Errorf("%w: batch %s must be a simple PostgreSQL identifier", ErrInvalidInput, identifier.label)
		}
	}

	if len(batch.ScopeSQL) > MaxSQLBytes || !utf8.ValidString(batch.ScopeSQL) || strings.ContainsRune(batch.ScopeSQL, 0) {
		return fmt.Errorf("%w: batch scope predicate is invalid", ErrInvalidInput)
	}
	if scope := strings.TrimSpace(batch.ScopeSQL); scope != "" {
		if batchParameterPattern.MatchString(scope) {
			return fmt.Errorf("%w: batch scope predicate cannot contain positional parameters", ErrInvalidInput)
		}
		if len(statements("SELECT 1 WHERE ("+scope+")")) != 1 {
			return fmt.Errorf("%w: batch scope must be a single WHERE predicate", ErrInvalidInput)
		}
	}
	if batch.BatchSize < 1 || batch.BatchSize > 10000 {
		return fmt.Errorf("%w: batch size must be between 1 and 10000", ErrInvalidInput)
	}
	return nil
}

func validBatchIdentifier(identifier string) bool {
	return len(identifier) > 0 && len(identifier) <= 63 && batchIdentifierPattern.MatchString(identifier)
}

func quoteBatchIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func (batch BatchDefinition) qualifiedTable() string {
	return quoteBatchIdentifier(batch.Schema) + "." + quoteBatchIdentifier(batch.Table)
}

func (batch BatchDefinition) quotedKeyColumn() string {
	return quoteBatchIdentifier(batch.KeyColumn)
}

func (batch BatchDefinition) scopePredicate() string {
	if strings.TrimSpace(batch.ScopeSQL) == "" {
		return "TRUE"
	}
	return batch.ScopeSQL
}

func snapshotBatchScope(ctx context.Context, tx pgx.Tx, execution *Execution) error {
	batch := execution.Batch
	var complete bool
	if err := tx.QueryRow(ctx,
		`SELECT scope_complete FROM _dbm.migration_batch_progress WHERE execution_id = $1`,
		int64(execution.ID),
	).Scan(&complete); err != nil {
		return fmt.Errorf("checking batch scope snapshot: %w", err)
	}
	if complete {
		return nil
	}

	table := batch.qualifiedTable()
	key := batch.quotedKeyColumn()
	regclass := quoteBatchIdentifier(batch.Schema) + "." + quoteBatchIdentifier(batch.Table)
	var keyIsUniqueBigint bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_attribute a
			WHERE a.attrelid = to_regclass($1)
			  AND a.attname = $2
			  AND a.attnum > 0
			  AND NOT a.attisdropped
			  AND a.attnotnull
			  AND a.atttypid = 'bigint'::regtype
			  AND EXISTS (
				SELECT 1
				FROM pg_index i
				WHERE i.indrelid = a.attrelid
				  AND i.indisunique
				  AND i.indisvalid
				  AND i.indisready
				  AND i.indpred IS NULL
				  AND i.indnkeyatts = 1
				  AND i.indkey[0] = a.attnum
			  )
		)`, regclass, batch.KeyColumn).Scan(&keyIsUniqueBigint); err != nil {
		return fmt.Errorf("validating batch key constraint: %w", err)
	}
	if !keyIsUniqueBigint {
		return fmt.Errorf("batch key %s.%s.%s must be a NOT NULL bigint with a single-column unique or primary-key constraint", batch.Schema, batch.Table, batch.KeyColumn)
	}

	var hasInvalidKey bool
	if err := tx.QueryRow(ctx,
		`WITH scope_check AS MATERIALIZED (
			SELECT EXISTS (
				SELECT 1 FROM `+table+`
				WHERE (`+batch.scopePredicate()+`) AND (`+key+` <= 0)
			) AS has_invalid_key
		), snapshot AS (
			INSERT INTO _dbm.migration_batch_scope (execution_id, key)
			SELECT $1, `+key+`
			FROM `+table+` CROSS JOIN scope_check
			WHERE NOT scope_check.has_invalid_key AND (`+batch.scopePredicate()+`)
			ON CONFLICT (execution_id, key) DO NOTHING
			RETURNING 1
		)
		SELECT has_invalid_key FROM scope_check`,
		int64(execution.ID)).Scan(&hasInvalidKey); err != nil {
		return fmt.Errorf("validating and snapshotting batch keys: %w", err)
	}
	if hasInvalidKey {
		return fmt.Errorf("batch scope contains a non-positive key; batch keys must be positive bigints")
	}
	if _, err := tx.Exec(ctx,
		`UPDATE _dbm.migration_batch_progress SET scope_complete = true WHERE execution_id = $1`,
		int64(execution.ID)); err != nil {
		return fmt.Errorf("marking batch scope snapshot complete: %w", err)
	}
	return nil
}
