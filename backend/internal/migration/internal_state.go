package migration

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const internalStateSQLState = "DBM01"

var internalSchemaReference = regexp.MustCompile(`(?i)\b_dbm\b`)

const internalGuardFunctionBody = `
BEGIN
	IF TG_OP = 'UPDATE' AND TG_TABLE_NAME = 'migration_history'
		AND to_jsonb(OLD) -> 'active' = 'true'::jsonb
		AND to_jsonb(NEW) -> 'active' = 'false'::jsonb
		AND (to_jsonb(NEW) - 'active') = (to_jsonb(OLD) - 'active') THEN
		RETURN NEW;
	END IF;
	RAISE EXCEPTION 'migration: _dbm.% is append-only; % is not allowed', TG_TABLE_NAME, TG_OP
		USING ERRCODE = 'DBM01';
END
`

const ensureInternalGuardFunctionSQL = `
CREATE OR REPLACE FUNCTION _dbm.reject_internal_rewrite() RETURNS trigger
LANGUAGE plpgsql AS $guard$` + internalGuardFunctionBody + `$guard$;`

func ensureInternalGuardsSQL(tables ...string) string {
	values := make([]string, 0, len(tables)*2)
	for _, table := range tables {
		values = append(values,
			fmt.Sprintf("('%s', '%s_append_only', 'BEFORE UPDATE OR DELETE', 'ROW')", table, table),
			fmt.Sprintf("('%s', '%s_no_truncate', 'BEFORE TRUNCATE', 'STATEMENT')", table, table),
		)
	}
	return `
DO $guards$
DECLARE
	guard record;
	state "char";
BEGIN
	FOR guard IN SELECT * FROM (VALUES ` + strings.Join(values, ", ") + `) AS g(tbl, name, events, level)
	LOOP
		SELECT t.tgenabled INTO state FROM pg_trigger AS t
		WHERE t.tgrelid = format('_dbm.%I', guard.tbl)::regclass AND t.tgname = guard.name;
		IF NOT FOUND THEN
			EXECUTE format('CREATE TRIGGER %I %s ON _dbm.%I FOR EACH %s EXECUTE FUNCTION _dbm.reject_internal_rewrite()',
				guard.name, guard.events, guard.tbl, guard.level);
			state := 'O';
		END IF;
		IF state <> 'A' THEN
			EXECUTE format('ALTER TABLE _dbm.%I ENABLE ALWAYS TRIGGER %I', guard.tbl, guard.name);
		END IF;
	END LOOP;
END
$guards$;`
}

const internalCatalogFingerprintSQL = `
WITH ns AS (SELECT oid, nspowner, nspacl FROM pg_namespace WHERE nspname = '_dbm')
SELECT
	coalesce((SELECT string_agg(item, E'\n' ORDER BY item) FROM (
		SELECT concat_ws(':', 'schema', ns.nspowner, ns.nspacl) AS item FROM ns
		UNION ALL
		SELECT concat_ws(':', 'relation', c.relname, c.oid, c.relfilenode, c.relkind, c.relowner, c.relacl,
			c.relrowsecurity, c.relforcerowsecurity,
			pg_stat_get_xact_tuples_inserted(c.oid), pg_stat_get_xact_tuples_updated(c.oid),
			pg_stat_get_xact_tuples_deleted(c.oid),
			(SELECT string_agg(concat_ws(' ', a.attname, format_type(a.atttypid, a.atttypmod), a.attnotnull,
					a.attgenerated, a.attidentity, pg_get_expr(d.adbin, d.adrelid)), ',' ORDER BY a.attnum)
			 FROM pg_attribute AS a
			 LEFT JOIN pg_attrdef AS d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
			 WHERE a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped))
		FROM pg_class AS c JOIN ns ON c.relnamespace = ns.oid
		UNION ALL
		SELECT concat_ws(':', 'trigger', c.relname, t.tgname, t.tgenabled, pg_get_triggerdef(t.oid))
		FROM pg_trigger AS t JOIN pg_class AS c ON c.oid = t.tgrelid JOIN ns ON c.relnamespace = ns.oid
		WHERE NOT t.tgisinternal
		UNION ALL
		SELECT concat_ws(':', 'rule', c.relname, r.rulename, r.ev_enabled, pg_get_ruledef(r.oid))
		FROM pg_rewrite AS r JOIN pg_class AS c ON c.oid = r.ev_class JOIN ns ON c.relnamespace = ns.oid
		UNION ALL
		SELECT concat_ws(':', 'policy', c.relname, p.polname, p.polcmd, p.polpermissive, p.polroles,
			pg_get_expr(p.polqual, p.polrelid), pg_get_expr(p.polwithcheck, p.polrelid))
		FROM pg_policy AS p JOIN pg_class AS c ON c.oid = p.polrelid JOIN ns ON c.relnamespace = ns.oid
		UNION ALL
		SELECT concat_ws(':', 'constraint', k.conname, pg_get_constraintdef(k.oid))
		FROM pg_constraint AS k JOIN ns ON k.connamespace = ns.oid
		UNION ALL
		SELECT concat_ws(':', 'function', p.oid::regprocedure, p.proowner, p.proacl, p.prosecdef, p.proconfig, md5(p.prosrc))
		FROM pg_proc AS p JOIN ns ON p.pronamespace = ns.oid
	) AS items), ''),
	to_regclass('_dbm.migration_history') IS NOT NULL,
	to_regclass('_dbm.migration_batch_progress') IS NOT NULL`

const historyContentFingerprintSQL = `
SELECT md5(coalesce(string_agg(concat_ws(',',
	t.version, coalesce(t.migration_id::text, '-'), coalesce(t.execution_id::text, '-'), t.attempt_number, t.status, md5(t.error_code), md5(t.error_message),
	t.execution_type, coalesce(t.target_execution_id::text, '-'), t.active, md5(t.name), md5(t.sql), md5(t.down_sql),
	md5(t.irreversible_reason), t.executed_by, extract(epoch FROM t.executed_at), t.duration_ms
), E'\n' ORDER BY t.version, t.attempt_number), ''))
FROM _dbm.migration_history AS t`

const batchProgressContentFingerprintSQL = `
SELECT md5(coalesce(string_agg(concat_ws(',',
	t.execution_id, t.last_cursor, t.batch_count, t.rows_processed, t.scope_complete, t.completed
), E'\n' ORDER BY t.execution_id), ''))
FROM _dbm.migration_batch_progress AS t`

func referencesInternalSchema(sql string) bool {
	return internalSchemaReference.MatchString(blankSQL(sql, false))
}

func internalStateError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == internalStateSQLState {
		return fmt.Errorf("%w: %s", ErrInternalStateModified, pgErr.Message)
	}
	return err
}

func withCatalogSearchPath(ctx context.Context, tx pgx.Tx, fn func() error) error {
	var original string
	if err := tx.QueryRow(ctx,
		"SELECT pg_catalog.current_setting('search_path'), pg_catalog.set_config('search_path', 'pg_catalog, pg_temp', true)",
	).Scan(&original, new(string)); err != nil {
		return fmt.Errorf("pinning search_path: %w", err)
	}
	if err := fn(); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SELECT pg_catalog.set_config('search_path', $1, true)", original); err != nil {
		return fmt.Errorf("restoring search_path: %w", err)
	}
	return nil
}

func internalStateFingerprint(ctx context.Context, tx pgx.Tx) (string, error) {
	var fingerprint string
	err := withCatalogSearchPath(ctx, tx, func() error {
		var err error
		fingerprint, err = readInternalStateFingerprint(ctx, tx)
		return err
	})
	return fingerprint, err
}

func readInternalStateFingerprint(ctx context.Context, tx pgx.Tx) (string, error) {
	var (
		catalog     string
		hasHistory  bool
		hasProgress bool
	)
	if err := tx.QueryRow(ctx, internalCatalogFingerprintSQL).Scan(&catalog, &hasHistory, &hasProgress); err != nil {
		return "", fmt.Errorf("fingerprinting _dbm catalog: %w", err)
	}
	parts := []string{catalog}
	for _, content := range []struct {
		present bool
		name    string
		query   string
	}{
		{hasHistory, "migration_history", historyContentFingerprintSQL},
		{hasProgress, "migration_batch_progress", batchProgressContentFingerprintSQL},
	} {
		if !content.present {
			continue
		}
		var value string
		if err := tx.QueryRow(ctx, content.query).Scan(&value); err != nil {
			return "", fmt.Errorf("fingerprinting _dbm.%s: %w", content.name, err)
		}
		parts = append(parts, content.name+"="+value)
	}
	return strings.Join(parts, "|"), nil
}

func requireInternalStateUnchanged(ctx context.Context, tx pgx.Tx, before string) error {
	after, err := internalStateFingerprint(ctx, tx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInternalStateModified, err)
	}
	if after != before {
		return ErrInternalStateModified
	}
	return nil
}
