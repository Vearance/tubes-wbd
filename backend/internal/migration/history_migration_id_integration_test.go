//go:build integration

package migration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"tubes-wbd/internal/access"
)

func historyMigrationIDs(t *testing.T, svc *MigrationService, dbID uint) map[int64]*uint {
	t.Helper()
	page, err := svc.HistoryFiltered(context.Background(), 42, dbID, 1, 50, false)
	require.NoError(t, err)
	ids := make(map[int64]*uint, len(page.Entries))
	for _, entry := range page.Entries {
		ids[entry.Version] = entry.MigrationID
	}
	return ids
}

func TestIntegrationHistoryRecordsMigrationIDForEveryAttempt(t *testing.T) {
	env := newDraftEnv(t, `
CREATE TABLE customers (id bigint PRIMARY KEY, email text NOT NULL);
INSERT INTO customers (id, email) VALUES (1, 'a@example.com'), (2, 'b@example.com');
`)
	leaderID := env.addApproverGroup(t, "History Approvers", "history-leader", access.PermissionViewEdit)
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()

	failing, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{Name: "broken", SQL: "ALTER TABLE nope ADD COLUMN x int"})
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusFailed, runDraftThroughWorker(t, env, svc, leaderID, 42, failing.ID).Status)

	up, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name:    "add phone",
		SQL:     "ALTER TABLE customers ADD COLUMN phone text",
		DownSQL: "ALTER TABLE customers DROP COLUMN phone",
	})
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, runDraftThroughWorker(t, env, svc, leaderID, 42, up.ID).Status)

	rollback, err := svc.SubmitRollbackForReview(ctx, 42, env.dbID)
	require.NoError(t, err)
	approveAndQueue(t, svc, leaderID, rollback.ID)
	worker, err := NewExecutionWorker(svc, env.repo)
	require.NoError(t, err)
	require.NoError(t, worker.RunExecution(ctx, rollback.ID))

	batch, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name:    "lowercase emails",
		SQL:     "UPDATE customers SET email = upper(email) WHERE id = ANY($1::bigint[])",
		DownSQL: "UPDATE customers SET email = lower(email) WHERE id = ANY($1::bigint[])",
		Batch:   &BatchDefinition{Schema: "public", Table: "customers", KeyColumn: "id", BatchSize: 1},
	})
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, runDraftThroughWorker(t, env, svc, leaderID, 42, batch.ID).Status)

	ids := historyMigrationIDs(t, svc, env.dbID)
	require.Len(t, ids, 4)
	want := map[int64]uint{1: failing.ID, 2: up.ID, 3: up.ID, 4: batch.ID}
	for version, migrationID := range want {
		require.NotNil(t, ids[version], "version %d", version)
		require.Equal(t, migrationID, *ids[version], "version %d", version)
	}

	detail, err := svc.HistoryEntry(ctx, 42, env.dbID, 3)
	require.NoError(t, err)
	require.Equal(t, ExecutionTypeRollback, detail.Type)
	require.NotNil(t, detail.MigrationID)
	require.Equal(t, up.ID, *detail.MigrationID, "a rollback is recorded under the migration it reverts")
}

func TestIntegrationHistoryUpgradeKeepsLegacyRowsWithoutMigrationID(t *testing.T) {
	cfg, pool := newTarget(t, "")
	svc := newIntegrationService(t, staticSource{1: cfg}, Options{})
	ctx := context.Background()
	_, err := applyForTest(ctx, svc, 1, 1, Migration{Name: "A", SQL: "CREATE TABLE a (id int)"})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "ALTER TABLE _dbm.migration_history DROP COLUMN migration_id")
	require.NoError(t, err, "simulate history written before migration_id existed")

	_, err = applyForTest(ctx, svc, 1, 1, Migration{Name: "B", SQL: "CREATE TABLE b (id int)"})
	require.NoError(t, err)

	require.True(t, columnExists(t, pool, "migration_history", "migration_id"))
	entry, err := svc.HistoryEntry(ctx, 1, 1, 1)
	require.NoError(t, err)
	require.Nil(t, entry.MigrationID)
}
