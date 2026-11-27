//go:build integration

package migration

import (
	"context"
	"testing"
	"time"

	"tubes-wbd/internal/access"
	"tubes-wbd/internal/database/targetdb"

	"github.com/stretchr/testify/require"
)

func TestIntegrationBatchMigrationIsNotBoundByPoolStatementTimeout(t *testing.T) {
	env := newDraftEnv(t, `
CREATE TABLE batch_items (id bigint PRIMARY KEY, value integer NOT NULL);
INSERT INTO batch_items (id, value) SELECT id, 0 FROM generate_series(1, 4) AS id;
`)
	leaderID := env.addApproverGroup(t, "Batch Timeout Approvers", "timeout-leader", access.PermissionViewEdit)
	manager := targetdb.NewManager(env.targets, targetdb.Options{StatementTimeout: 200 * time.Millisecond})
	t.Cleanup(manager.Close)
	svc, err := NewMigrationService(stubAuthorizer{}, manager, env.repo, Options{StatementTimeout: 10 * time.Second})
	require.NoError(t, err)

	draft, err := svc.CreateDraft(context.Background(), 42, env.dbID, Migration{
		Name:    "batch slower than the pool limit",
		SQL:     "UPDATE batch_items SET value = value + 1 WHERE id = ANY($1::bigint[]) AND pg_sleep(0.1) IS NOT NULL",
		DownSQL: "UPDATE batch_items SET value = value - 1 WHERE id = ANY($1::bigint[])",
		Batch: &BatchDefinition{
			Schema: "public", Table: "batch_items", KeyColumn: "id",
			ScopeSQL: "pg_sleep(0.1) IS NOT NULL", BatchSize: 4,
		},
	})
	require.NoError(t, err)

	execution := runDraftThroughWorker(t, env, svc, leaderID, 42, draft.ID)
	require.Equal(t, ExecutionStatusSuccess, execution.Status, execution.ErrorMessage)
	require.EqualValues(t, 4, execution.RowsProcessed)

	var total int
	require.NoError(t, env.target.QueryRow(context.Background(), "SELECT sum(value) FROM batch_items").Scan(&total))
	require.Equal(t, 4, total)
}

func TestIntegrationStaleBatchRecoveryReconcilesTargetCheckpoint(t *testing.T) {
	env := newDraftEnv(t, `
CREATE TABLE batch_items (id bigint PRIMARY KEY, value integer NOT NULL);
INSERT INTO batch_items (id, value) SELECT id, 0 FROM generate_series(1, 4) AS id;
`)
	leaderID := env.addApproverGroup(t, "Batch Approvers", "recovery-leader", access.PermissionViewEdit)
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()
	draft, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name:    "increment batch values",
		SQL:     "UPDATE batch_items SET value = value + 1 WHERE id = ANY($1::bigint[])",
		DownSQL: "UPDATE batch_items SET value = value - 1 WHERE id = ANY($1::bigint[])",
		Batch:   &BatchDefinition{Schema: "public", Table: "batch_items", KeyColumn: "id", BatchSize: 2},
	})
	require.NoError(t, err)
	execution, err := svc.SubmitDraftForReview(ctx, 42, draft.ID)
	require.NoError(t, err)
	approveAndQueue(t, svc, leaderID, execution.ID)
	claimed, err := env.repo.ClaimExecution(ctx, execution.ID)
	require.NoError(t, err)
	require.NotNil(t, claimed)

	require.NoError(t, ensureHistorySchema(ctx, env.target, true))
	_, err = env.target.Exec(ctx, ensureBatchSchemaSQL)
	require.NoError(t, err)
	tx, err := env.target.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx,
		`INSERT INTO _dbm.migration_batch_progress
			(execution_id, last_cursor, batch_count, rows_processed, scope_complete)
		 VALUES ($1, 2, 1, 2, true)`, int64(execution.ID))
	require.NoError(t, err)
	_, err = tx.Exec(ctx,
		`INSERT INTO _dbm.migration_batch_scope (execution_id, key)
		 SELECT $1, scope.key FROM unnest($2::bigint[]) AS scope(key)`, int64(execution.ID), []int64{1, 2, 3, 4})
	require.NoError(t, err)
	_, err = tx.Exec(ctx,
		`UPDATE batch_items SET value = value + 1 WHERE id = ANY($1::bigint[])`, []int64{1, 2})
	require.NoError(t, err)
	_, err = tx.Exec(ctx,
		`INSERT INTO _dbm.migration_batch_commits (execution_id, batch_number, keys, rows_processed)
		 VALUES ($1, 1, $2, 2)`, int64(execution.ID), []int64{1, 2})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx), "target batch committed before Platform DB progress was updated")

	require.NoError(t, env.platform.Model(&Execution{}).
		Where("id = ?", execution.ID).
		Update("started_at", time.Now().Add(-time.Hour)).Error)
	worker, err := NewExecutionWorker(svc, env.repo)
	require.NoError(t, err)
	require.NoError(t, worker.RecoverStale(ctx))

	queued, err := svc.Execution(ctx, 42, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusQueued, queued.Status)
	require.Equal(t, 1, queued.BatchCount)
	require.EqualValues(t, 2, queued.RowsProcessed)
	require.NotNil(t, queued.ActiveDatabaseID, "recovery must retain the per-database lock")
	require.Equal(t, env.dbID, *queued.ActiveDatabaseID)

	require.NoError(t, worker.RunExecution(ctx, execution.ID))
	completed, err := svc.Execution(ctx, 42, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, completed.Status)
	require.Equal(t, 2, completed.BatchCount)
	require.EqualValues(t, 4, completed.RowsProcessed)
	var minValue, maxValue int
	require.NoError(t, env.target.QueryRow(ctx, "SELECT min(value), max(value) FROM batch_items").Scan(&minValue, &maxValue))
	require.Equal(t, 1, minValue, "the committed first batch must not be executed twice")
	require.Equal(t, 1, maxValue)
}

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
