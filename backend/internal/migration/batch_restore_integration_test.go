//go:build integration

package migration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"tubes-wbd/internal/access"
)

const lossyBatchTargetSQL = `
CREATE TABLE batch_items (
	id bigint PRIMARY KEY,
	email text NOT NULL,
	email_length integer GENERATED ALWAYS AS (length(email)) STORED
);
INSERT INTO batch_items (id, email) VALUES (1, 'Alice@Example.com'), (2, 'BOB@example.com'), (3, 'carol@example.com');
`

func batchEmails(t *testing.T, env *draftEnv) map[int64]string {
	t.Helper()
	rows, err := env.target.Query(context.Background(), "SELECT id, email FROM batch_items ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()
	emails := map[int64]string{}
	for rows.Next() {
		var id int64
		var email string
		require.NoError(t, rows.Scan(&id, &email))
		emails[id] = email
	}
	require.NoError(t, rows.Err())
	return emails
}

func TestIntegrationBatchRollbackRestoresValuesLostByDownSQL(t *testing.T) {
	env := newDraftEnv(t, lossyBatchTargetSQL)
	leaderID := env.addApproverGroup(t, "Restore Approvers", "restore-leader", access.PermissionViewEdit)
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()
	original := batchEmails(t, env)

	draft, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name:    "lowercase emails",
		SQL:     "UPDATE batch_items SET email = lower(email) WHERE id = ANY($1::bigint[])",
		DownSQL: "UPDATE batch_items SET email = email WHERE id = ANY($1::bigint[])",
		Batch:   &BatchDefinition{Schema: "public", Table: "batch_items", KeyColumn: "id", BatchSize: 2},
	})
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, runDraftThroughWorker(t, env, svc, leaderID, 42, draft.ID).Status)
	require.Equal(t, "alice@example.com", batchEmails(t, env)[1])

	rollback, err := svc.SubmitRollbackForReview(ctx, 42, env.dbID)
	require.NoError(t, err)
	approveAndQueue(t, svc, leaderID, rollback.ID)
	worker, err := NewExecutionWorker(svc, env.repo)
	require.NoError(t, err)
	require.NoError(t, worker.RunExecution(ctx, rollback.ID))

	rolledBack, err := svc.Execution(ctx, 42, rollback.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, rolledBack.Status)
	require.Equal(t, original, batchEmails(t, env), "rollback must restore the recorded pre-batch values")
}

func TestIntegrationCompensationRefusesBatchWithoutRestoreData(t *testing.T) {
	env := newDraftEnv(t, lossyBatchTargetSQL+`
CREATE SEQUENCE fail_once;
CREATE FUNCTION fail_third_row_once() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF NEW.id = 3 AND nextval('fail_once') = 1 THEN
		RAISE EXCEPTION 'fail second batch once';
	END IF;
	RETURN NEW;
END;
$$;
CREATE TRIGGER fail_third_row_once BEFORE UPDATE ON batch_items
FOR EACH ROW EXECUTE FUNCTION fail_third_row_once();
`)
	leaderID := env.addApproverGroup(t, "Restore Approvers", "legacy-leader", access.PermissionViewEdit)
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()

	draft, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name:    "lowercase emails",
		SQL:     "UPDATE batch_items SET email = lower(email) WHERE id = ANY($1::bigint[])",
		DownSQL: "UPDATE batch_items SET email = email WHERE id = ANY($1::bigint[])",
		Batch:   &BatchDefinition{Schema: "public", Table: "batch_items", KeyColumn: "id", BatchSize: 2},
	})
	require.NoError(t, err)
	partial := runDraftThroughWorker(t, env, svc, leaderID, 42, draft.ID)
	require.Equal(t, ExecutionStatusPartial, partial.Status)

	_, err = env.target.Exec(ctx, `
ALTER TABLE _dbm.migration_batch_commits DISABLE TRIGGER migration_batch_commits_append_only;
UPDATE _dbm.migration_batch_commits SET restore_rows = NULL;
ALTER TABLE _dbm.migration_batch_commits ENABLE ALWAYS TRIGGER migration_batch_commits_append_only;`)
	require.NoError(t, err, "simulate a batch committed before restore data was recorded")
	lowered := batchEmails(t, env)

	compensation, err := svc.SubmitCompensationForReview(ctx, 42, partial.ID)
	require.NoError(t, err)
	approveAndQueue(t, svc, leaderID, compensation.ID)
	worker, err := NewExecutionWorker(svc, env.repo)
	require.NoError(t, err)
	require.NoError(t, worker.RunExecution(ctx, compensation.ID))

	result, err := svc.Execution(ctx, 42, compensation.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusPartial, result.Status, "a failed compensation pauses instead of completing")
	require.Contains(t, result.ErrorMessage, "without pre-batch row values")
	require.Equal(t, lowered, batchEmails(t, env))
	target, err := svc.Execution(ctx, 42, partial.ID)
	require.NoError(t, err)
	require.NotEqual(t, ExecutionStatusCompensated, target.Status)
}
