//go:build integration

package migration

import (
	"context"
	"testing"

	"tubes-wbd/internal/access"

	"github.com/stretchr/testify/require"
)

func TestIntegrationBatchMigrationAndReverseRollback(t *testing.T) {
	env := newDraftEnv(t, `
CREATE TABLE batch_items (id bigint PRIMARY KEY, value text NOT NULL);
INSERT INTO batch_items (id, value)
SELECT id, 'pending' FROM generate_series(1, 5) AS id;
CREATE SEQUENCE batch_fail_once;
CREATE FUNCTION fail_second_batch_once() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF NEW.id = 3 AND nextval('batch_fail_once') = 1 THEN
		RAISE EXCEPTION 'fail first attempt on second batch';
	END IF;
	RETURN NEW;
END;
$$;
CREATE TRIGGER fail_second_batch_once BEFORE UPDATE ON batch_items
FOR EACH ROW EXECUTE FUNCTION fail_second_batch_once();
`)
	leaderID := env.addApproverGroup(t, "Batch Approvers", "batch-leader", access.PermissionViewEdit)
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()

	draft, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name:    "backfill batch values",
		SQL:     "UPDATE batch_items SET value = 'done' WHERE id = ANY($1::bigint[])",
		DownSQL: "UPDATE batch_items SET value = 'pending' WHERE id = ANY($1::bigint[])",
		Batch: &BatchDefinition{
			CursorSQL: "SELECT id FROM batch_items WHERE id > $1 ORDER BY id LIMIT $2",
			BatchSize: 2,
		},
	})
	require.NoError(t, err)
	execution, err := svc.SubmitDraftForReview(ctx, 42, draft.ID)
	require.NoError(t, err)
	approveAndQueue(t, svc, leaderID, execution.ID)

	worker, err := NewExecutionWorker(svc, env.repo)
	require.NoError(t, err)
	require.NoError(t, worker.RunExecution(ctx, execution.ID))

	partial, err := svc.Execution(ctx, 42, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusPartial, partial.Status)
	require.Equal(t, 1, partial.BatchCount)
	var doneRows int
	require.NoError(t, env.target.QueryRow(ctx, `SELECT count(*) FROM batch_items WHERE value = 'done'`).Scan(&doneRows))
	require.Equal(t, 2, doneRows)

	_, err = svc.EnqueueExecution(ctx, 42, execution.ID)
	require.NoError(t, err, "a partial batch execution should resume through its existing approval")
	require.NoError(t, worker.RunExecution(ctx, execution.ID))
	var completedRows int
	require.NoError(t, env.target.QueryRow(ctx, `SELECT count(*) FROM batch_items WHERE value = 'done'`).Scan(&completedRows))
	require.Equal(t, 5, completedRows)
	completed, err := svc.Execution(ctx, 42, execution.ID)
	require.NoError(t, err)
	require.Equal(t, 3, completed.BatchCount)
	require.EqualValues(t, 5, completed.RowsProcessed)
	require.True(t, completed.BatchDone)
	require.Equal(t, ExecutionStatusSuccess, completed.Status)

	rollback, err := svc.SubmitRollbackForReview(ctx, 42, env.dbID)
	require.NoError(t, err)
	approveAndQueue(t, svc, leaderID, rollback.ID)
	require.NoError(t, worker.RunExecution(ctx, rollback.ID))

	var pendingRows int
	require.NoError(t, env.target.QueryRow(ctx, `SELECT count(*) FROM batch_items WHERE value = 'pending'`).Scan(&pendingRows))
	require.Equal(t, 5, pendingRows)
	rolledBack, err := svc.Execution(ctx, 42, rollback.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, rolledBack.Status)
}
