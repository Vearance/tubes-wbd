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

const lowercaseEmailsSQL = "UPDATE batch_items SET email = lower(email) WHERE id = ANY($1::bigint[])"

func TestIntegrationBatchRollbackRunsOnlyTheApprovedDownSQL(t *testing.T) {
	env := newDraftEnv(t, lossyBatchTargetSQL)
	leaderID := env.addApproverGroup(t, "Restore Approvers", "restore-leader", access.PermissionViewEdit)
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()

	draft, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name:    "lowercase emails",
		SQL:     lowercaseEmailsSQL,
		DownSQL: "UPDATE batch_items SET email = email WHERE id = ANY($1::bigint[])",
		Batch:   &BatchDefinition{Schema: "public", Table: "batch_items", KeyColumn: "id", BatchSize: 2},
	})
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, runDraftThroughWorker(t, env, svc, leaderID, 42, draft.ID).Status)
	lowered := batchEmails(t, env)
	require.Equal(t, "alice@example.com", lowered[1])

	rollback, err := svc.SubmitRollbackForReview(ctx, 42, env.dbID)
	require.NoError(t, err)
	approveAndQueue(t, svc, leaderID, rollback.ID)
	worker, err := NewExecutionWorker(svc, env.repo)
	require.NoError(t, err)
	require.NoError(t, worker.RunExecution(ctx, rollback.ID))

	rolledBack, err := svc.Execution(ctx, 42, rollback.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, rolledBack.Status)
	require.Equal(t, lowered, batchEmails(t, env), "rollback runs the approved Down SQL only and writes nothing else")
}

func newPartialLowercaseExecution(t *testing.T, downSQL string) (*draftEnv, *MigrationService, uint, *Execution) {
	t.Helper()
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
	leaderID := env.addApproverGroup(t, "Restore Approvers", "partial-leader", access.PermissionViewEdit)
	svc := env.service(t, stubAuthorizer{})
	draft, err := svc.CreateDraft(context.Background(), 42, env.dbID, Migration{
		Name:    "lowercase emails",
		SQL:     lowercaseEmailsSQL,
		DownSQL: downSQL,
		Batch:   &BatchDefinition{Schema: "public", Table: "batch_items", KeyColumn: "id", BatchSize: 2},
	})
	require.NoError(t, err)
	partial := runDraftThroughWorker(t, env, svc, leaderID, 42, draft.ID)
	require.Equal(t, ExecutionStatusPartial, partial.Status)
	return env, svc, leaderID, partial
}

func runCompensation(t *testing.T, env *draftEnv, svc *MigrationService, leaderID uint, partial *Execution) *Execution {
	t.Helper()
	ctx := context.Background()
	compensation, err := svc.SubmitCompensationForReview(ctx, 42, partial.ID)
	require.NoError(t, err)
	approveAndQueue(t, svc, leaderID, compensation.ID)
	worker, err := NewExecutionWorker(svc, env.repo)
	require.NoError(t, err)
	require.NoError(t, worker.RunExecution(ctx, compensation.ID))
	result, err := svc.Execution(ctx, 42, compensation.ID)
	require.NoError(t, err)
	return result
}

func requireCompensationRejected(t *testing.T, env *draftEnv, svc *MigrationService, partial, result *Execution, message string, emails map[int64]string) {
	t.Helper()
	require.Equal(t, ExecutionStatusPartial, result.Status, "a rejected compensation stays partially reverted")
	require.Contains(t, result.ErrorMessage, message)
	require.Contains(t, result.ErrorMessage, "corrective migration")
	require.Equal(t, emails, batchEmails(t, env), "the rejected compensation batch must be rolled back")
	target, err := svc.Execution(context.Background(), 42, partial.ID)
	require.NoError(t, err)
	require.NotEqual(t, ExecutionStatusCompensated, target.Status)
}

func TestIntegrationCompensationVerifiesDownSQLRestoredData(t *testing.T) {
	env, svc, leaderID, partial := newPartialLowercaseExecution(t,
		"UPDATE batch_items SET email = CASE id WHEN 1 THEN 'Alice@Example.com' WHEN 2 THEN 'BOB@example.com' ELSE email END WHERE id = ANY($1::bigint[])")
	original := map[int64]string{1: "Alice@Example.com", 2: "BOB@example.com", 3: "carol@example.com"}

	result := runCompensation(t, env, svc, leaderID, partial)

	require.Equal(t, ExecutionStatusSuccess, result.Status)
	require.Equal(t, original, batchEmails(t, env))
	target, err := svc.Execution(context.Background(), 42, partial.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusCompensated, target.Status)
}

func TestIntegrationCompensationRejectsDownSQLThatDoesNotRestoreData(t *testing.T) {
	env, svc, leaderID, partial := newPartialLowercaseExecution(t, "UPDATE batch_items SET email = email WHERE id = ANY($1::bigint[])")
	lowered := batchEmails(t, env)

	result := runCompensation(t, env, svc, leaderID, partial)

	requireCompensationRejected(t, env, svc, partial, result, "did not restore the recorded pre-batch values", lowered)
}

func TestIntegrationCompensationRefusesBatchWithoutRestoreData(t *testing.T) {
	env, svc, leaderID, partial := newPartialLowercaseExecution(t, "UPDATE batch_items SET email = email WHERE id = ANY($1::bigint[])")
	_, err := env.target.Exec(context.Background(), `
ALTER TABLE _dbm.migration_batch_commits DISABLE TRIGGER migration_batch_commits_append_only;
UPDATE _dbm.migration_batch_commits SET restore_rows = NULL;
ALTER TABLE _dbm.migration_batch_commits ENABLE ALWAYS TRIGGER migration_batch_commits_append_only;`)
	require.NoError(t, err, "simulate a batch committed before restore data was recorded")
	lowered := batchEmails(t, env)

	result := runCompensation(t, env, svc, leaderID, partial)

	requireCompensationRejected(t, env, svc, partial, result, "without pre-batch row values", lowered)
}
