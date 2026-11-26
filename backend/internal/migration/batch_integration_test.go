//go:build integration

package migration

import (
	"context"
	"testing"

	"tubes-wbd/internal/access"

	"github.com/stretchr/testify/require"
)

func TestIntegrationCompensationResumesAfterDownBatchFailure(t *testing.T) {
	env := newDraftEnv(t, `
CREATE TABLE batch_items (id bigint PRIMARY KEY, value text NOT NULL);
INSERT INTO batch_items (id, value)
SELECT id, 'pending' FROM generate_series(1, 4) AS id;
CREATE SEQUENCE up_fail_once;
CREATE FUNCTION fail_up_second_batch_once() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF NEW.value = 'done' AND NEW.id = 3 AND nextval('up_fail_once') = 1 THEN
		RAISE EXCEPTION 'fail first attempt on second up batch';
	END IF;
	RETURN NEW;
END;
$$;
CREATE TRIGGER fail_up_second_batch_once BEFORE UPDATE ON batch_items
FOR EACH ROW EXECUTE FUNCTION fail_up_second_batch_once();
CREATE SEQUENCE down_fail_once;
CREATE FUNCTION fail_first_down_batch_once() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF NEW.value = 'pending' AND nextval('down_fail_once') = 1 THEN
		RAISE EXCEPTION 'fail first compensation batch';
	END IF;
	RETURN NEW;
END;
$$;
CREATE TRIGGER fail_first_down_batch_once BEFORE UPDATE ON batch_items
FOR EACH ROW EXECUTE FUNCTION fail_first_down_batch_once();
`)
	leaderID := env.addApproverGroup(t, "Batch Approvers", "batch-leader", access.PermissionViewEdit)
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()
	draft, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name:    "backfill batch values",
		SQL:     "UPDATE batch_items SET value = 'done' WHERE id = ANY($1::bigint[])",
		DownSQL: "UPDATE batch_items SET value = 'pending' WHERE id = ANY($1::bigint[])",
		Batch:   &BatchDefinition{Schema: "public", Table: "batch_items", KeyColumn: "id", BatchSize: 2},
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

	compensation, err := svc.SubmitCompensationForReview(ctx, 42, partial.ID)
	require.NoError(t, err)
	require.True(t, compensation.Compensation)
	approveAndQueue(t, svc, leaderID, compensation.ID)
	require.NoError(t, worker.RunExecution(ctx, compensation.ID))
	paused, err := svc.Execution(ctx, 42, compensation.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusPartial, paused.Status)
	var pendingRows int
	require.NoError(t, env.target.QueryRow(ctx, `SELECT count(*) FROM batch_items WHERE value = 'pending'`).Scan(&pendingRows))
	require.Equal(t, 2, pendingRows, "failed compensation batch must leave its transaction unapplied")

	_, err = svc.EnqueueExecution(ctx, 42, compensation.ID)
	require.NoError(t, err, "partial compensation resumes through its original approval")
	require.NoError(t, worker.RunExecution(ctx, compensation.ID))
	completed, err := svc.Execution(ctx, 42, compensation.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, completed.Status)
	compensated, err := svc.Execution(ctx, 42, partial.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusCompensated, compensated.Status)
	require.NoError(t, env.target.QueryRow(ctx, `SELECT count(*) FROM batch_items WHERE value = 'pending'`).Scan(&pendingRows))
	require.Equal(t, 4, pendingRows)

	hidden, err := svc.HistoryFiltered(ctx, 42, env.dbID, 1, 20, true)
	require.NoError(t, err)
	var compensationVisible bool
	for _, entry := range hidden.Entries {
		if entry.ExecutionID != nil && *entry.ExecutionID == compensation.ID && entry.Status == RunStatusSuccess {
			compensationVisible = true
		}
	}
	require.True(t, compensationVisible, "hide rollback must not hide a compensation whose partial target stays visible")
	require.Nil(t, hidden.Forward, "a compensated partial migration cannot be forwarded")
	_, err = svc.SubmitForwardForReview(ctx, 42, env.dbID)
	require.ErrorIs(t, err, ErrInvalidForward)
}

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
			Schema: "public", Table: "batch_items", KeyColumn: "id", BatchSize: 2,
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
	_, err = env.target.Exec(ctx, `INSERT INTO batch_items (id, value) VALUES (6, 'pending')`)
	require.NoError(t, err)

	_, err = svc.EnqueueExecution(ctx, 42, execution.ID)
	require.NoError(t, err, "a partial batch execution should resume through its existing approval")
	require.NoError(t, worker.RunExecution(ctx, execution.ID))
	var completedRows int
	require.NoError(t, env.target.QueryRow(ctx, `SELECT count(*) FROM batch_items WHERE value = 'done'`).Scan(&completedRows))
	require.Equal(t, 5, completedRows, "a row inserted after scope snapshot must not join the execution")
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
	require.Equal(t, 6, pendingRows, "rollback covers only the original frozen key set")
	rolledBack, err := svc.Execution(ctx, 42, rollback.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, rolledBack.Status)
}
