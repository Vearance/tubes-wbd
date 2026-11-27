//go:build integration

package migration

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"tubes-wbd/internal/access"
)

func successfulHistoryVersions(t *testing.T, pool *pgxpool.Pool) []int64 {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		"SELECT version FROM _dbm.migration_history WHERE status = 'success' ORDER BY version")
	require.NoError(t, err)
	defer rows.Close()
	var versions []int64
	for rows.Next() {
		var v int64
		require.NoError(t, rows.Scan(&v))
		versions = append(versions, v)
	}
	require.NoError(t, rows.Err())
	return versions
}

func TestIntegrationMigrationCannotModifyInternalSchema(t *testing.T) {
	tampering := map[string]string{
		"truncate history":      `DO $$ BEGIN EXECUTE 'TRUNCATE _' || 'dbm.migration_history'; END $$`,
		"delete history":        `DO $$ BEGIN EXECUTE 'DELETE FROM _' || 'dbm.migration_history'; END $$`,
		"deactivate history":    `DO $$ BEGIN EXECUTE 'UPDATE _' || 'dbm.migration_history SET active = false'; END $$`,
		"drop history table":    `DO $$ BEGIN EXECUTE 'DROP TABLE _' || 'dbm.migration_history'; END $$`,
		"add column to history": `DO $$ BEGIN EXECUTE 'ALTER TABLE _' || 'dbm.migration_history ADD COLUMN x int'; END $$`,
		"insert fake history":   `DO $$ BEGIN EXECUTE 'INSERT INTO _' || 'dbm.migration_history (version, name, sql, executed_by, duration_ms) VALUES (99, ''x'', ''x'', 1, 0)'; END $$`,
		"disable guard trigger": `DO $$ BEGIN EXECUTE 'ALTER TABLE _' || 'dbm.migration_history DISABLE TRIGGER migration_history_append_only'; END $$`,
		"replace guard function": `DO $$ BEGIN EXECUTE 'CREATE OR REPLACE FUNCTION _' || 'dbm.reject_internal_rewrite() RETURNS trigger ` +
			`LANGUAGE plpgsql AS $f$ BEGIN RETURN NEW; END $f$'; END $$`,
		"truncate batch scope": `CREATE TABLE IF NOT EXISTS t (id int); DO $$ BEGIN ` +
			`EXECUTE 'CREATE TABLE IF NOT EXISTS _' || 'dbm.migration_batch_scope (execution_id bigint, key bigint)'; ` +
			`EXECUTE 'TRUNCATE _' || 'dbm.migration_batch_scope'; END $$`,
		"shadowed fingerprint functions": `CREATE SCHEMA evil;
CREATE FUNCTION evil.md5(text) RETURNS text LANGUAGE sql AS $f$ SELECT 'same' $f$;
SET LOCAL search_path = evil, pg_catalog;
DO $$ BEGIN EXECUTE 'UPDATE _' || 'dbm.migration_history SET active = false'; END $$`,
	}
	for name, sql := range tampering {
		t.Run(name, func(t *testing.T) {
			cfg, pool := newTarget(t, "")
			svc := newIntegrationService(t, staticSource{1: cfg}, Options{})
			ctx := context.Background()
			_, err := applyForTest(ctx, svc, 1, 1, Migration{Name: "A", SQL: "CREATE TABLE a (id int)"})
			require.NoError(t, err)
			_, err = applyForTest(ctx, svc, 1, 1, Migration{Name: "B", SQL: "CREATE TABLE b (id int)"})
			require.NoError(t, err)

			_, err = applyForTest(ctx, svc, 1, 1, Migration{Name: "tamper", SQL: sql})

			require.ErrorIs(t, err, ErrInternalStateModified)
			require.Equal(t, []int64{1, 2}, successfulHistoryVersions(t, pool))
			require.Equal(t, []int64{1, 2, 3}, historyVersions(t, pool), "the rejected attempt is still recorded as failed")
		})
	}
}

func TestIntegrationHistoryGuardBlocksDeferredTampering(t *testing.T) {
	cfg, pool := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})
	ctx := context.Background()
	_, err := applyForTest(ctx, svc, 1, 1, Migration{Name: "A", SQL: "CREATE TABLE a (id int)"})
	require.NoError(t, err)
	_, err = applyForTest(ctx, svc, 1, 1, Migration{Name: "plant trigger", SQL: `
CREATE FUNCTION wipe_history_later() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	EXECUTE 'DELETE FROM _' || 'dbm.migration_history';
	RETURN NEW;
END
$$;
CREATE TRIGGER wipe_history_later AFTER INSERT ON a FOR EACH ROW EXECUTE FUNCTION wipe_history_later();`})
	require.NoError(t, err, "the migration itself does not touch _dbm")

	for _, sql := range []string{
		"INSERT INTO a VALUES (1)",
		"TRUNCATE _dbm.migration_history",
		"SET session_replication_role = replica; DELETE FROM _dbm.migration_history",
	} {
		_, err = pool.Exec(ctx, sql)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr, sql)
		require.Equal(t, internalStateSQLState, pgErr.Code, sql)
	}
	require.Equal(t, []int64{1, 2}, successfulHistoryVersions(t, pool))
}

func TestIntegrationSessionSettingsDoNotLookLikeInternalChanges(t *testing.T) {
	cfg, pool := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})
	ctx := context.Background()
	_, err := applyForTest(ctx, svc, 1, 1, Migration{Name: "A", SQL: "CREATE TABLE a (id int)"})
	require.NoError(t, err)

	_, err = applyForTest(ctx, svc, 1, 1, Migration{
		Name: "session settings",
		SQL:  "SET LOCAL timezone = 'Pacific/Kiritimati'; SET LOCAL datestyle = 'SQL, DMY'; CREATE TABLE b (id int)",
	})

	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, successfulHistoryVersions(t, pool))
}

func TestIntegrationBatchMigrationCannotModifyInternalSchema(t *testing.T) {
	env := newDraftEnv(t, `
CREATE TABLE batch_items (id bigint PRIMARY KEY, value integer NOT NULL);
INSERT INTO batch_items (id, value) SELECT id, 0 FROM generate_series(1, 4) AS id;
CREATE FUNCTION wipe_platform_history() RETURNS boolean LANGUAGE plpgsql AS $$
BEGIN
	EXECUTE 'DELETE FROM _' || 'dbm.migration_history';
	RETURN true;
END;
$$;
`)
	leaderID := env.addApproverGroup(t, "Integrity Approvers", "integrity-leader", access.PermissionViewEdit)
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()

	first, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{Name: "create seen", SQL: "CREATE TABLE seen (id int)"})
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, runDraftThroughWorker(t, env, svc, leaderID, 42, first.ID).Status)
	before := successfulHistoryVersions(t, env.target)
	require.Len(t, before, 1)

	draft, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name:    "batch that wipes history",
		SQL:     "UPDATE batch_items SET value = value + 1 WHERE id = ANY($1::bigint[]) AND wipe_platform_history()",
		DownSQL: "UPDATE batch_items SET value = value - 1 WHERE id = ANY($1::bigint[])",
		Batch:   &BatchDefinition{Schema: "public", Table: "batch_items", KeyColumn: "id", BatchSize: 2},
	})
	require.NoError(t, err)

	execution := runDraftThroughWorker(t, env, svc, leaderID, 42, draft.ID)

	require.Equal(t, ExecutionStatusFailed, execution.Status)
	require.Contains(t, execution.ErrorMessage, "_dbm")
	require.Equal(t, before, successfulHistoryVersions(t, env.target))
	var total int
	require.NoError(t, env.target.QueryRow(ctx, "SELECT sum(value) FROM batch_items").Scan(&total))
	require.Zero(t, total)
}
