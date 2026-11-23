//go:build integration

package migration

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"tubes-wbd/internal/access"
	"tubes-wbd/internal/manageddb"
)

func TestIntegrationTargetCannotBeDeletedWithExecutionHistory(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit)
	draft, err := svc.CreateDraft(context.Background(), 42, env.dbID, Migration{
		Name: "retained history",
		SQL:  "SELECT 1",
	})
	require.NoError(t, err)
	_, err = svc.SubmitDraftForReview(context.Background(), 42, draft.ID)
	require.NoError(t, err)

	err = manageddb.NewGormRepository(env.platform).Delete(context.Background(), env.dbID)
	require.ErrorIs(t, err, manageddb.ErrHasExecutions)
}

func TestIntegrationMigrationHistoryIncludesFailedAttempts(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()

	failed := &Execution{
		ID:                501,
		ManagedDatabaseID: env.dbID,
		Type:              ExecutionTypeUp,
		Name:              "failed migration",
		SQL:               "SELECT * FROM table_that_does_not_exist",
		ExecutedBy:        42,
		Attempt:           1,
	}
	_, err := svc.runExecution(ctx, failed)
	require.Error(t, err)
	failed.Attempt = 2
	_, err = svc.runExecution(ctx, failed)
	require.Error(t, err)

	success := &Execution{
		ID:                502,
		ManagedDatabaseID: env.dbID,
		Type:              ExecutionTypeUp,
		Name:              "successful migration",
		SQL:               "CREATE TABLE history_success (id bigint PRIMARY KEY)",
		DownSQL:           "DROP TABLE history_success",
		ExecutedBy:        42,
		Attempt:           1,
	}
	_, err = svc.runExecution(ctx, success)
	require.NoError(t, err)

	failedAfterSuccess := &Execution{
		ID:                503,
		ManagedDatabaseID: env.dbID,
		Type:              ExecutionTypeUp,
		Name:              "failed migration after success",
		SQL:               "SELECT * FROM another_missing_table",
		ExecutedBy:        42,
		Attempt:           1,
	}
	_, err = svc.runExecution(ctx, failedAfterSuccess)
	require.Error(t, err)

	history, err := svc.History(ctx, 42, env.dbID, 1, 20)
	require.NoError(t, err)
	require.Len(t, history.Entries, 4)
	require.Equal(t, RunStatusFailed, history.Entries[0].Status)
	require.Equal(t, "42P01", history.Entries[0].ErrorCode)
	require.Equal(t, RunStatusSuccess, history.Entries[1].Status)
	require.True(t, history.Entries[1].Active)
	require.Equal(t, RunStatusFailed, history.Entries[2].Status)
	require.Equal(t, RunStatusFailed, history.Entries[3].Status)
	require.Equal(t, 1, history.Entries[3].AttemptNumber)
	require.Equal(t, 2, history.Entries[2].AttemptNumber)

	var latestActive *HistoryEntry
	require.NoError(t, svc.readOnly(ctx, env.dbID, func(tx pgx.Tx) error {
		var err error
		latestActive, err = latestActiveExecutionHistory(ctx, tx)
		return err
	}))
	require.Equal(t, uint(502), latestActive.ExecutionID, "failed history entries must not alter active migration state")
}

func TestIntegrationAsyncUpRollbackAndForward(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()
	leader := env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit)

	draft, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name:    "create projects",
		SQL:     "CREATE TABLE projects (id bigint PRIMARY KEY)",
		DownSQL: "DROP TABLE projects",
	})
	require.NoError(t, err)

	up, err := svc.SubmitDraftForReview(ctx, 42, draft.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusPendingReview, up.Status)
	require.False(t, tableExists(t, env.target, "projects"))
	approveAndQueue(t, svc, leader, up.ID)

	worker, err := NewExecutionWorker(svc, env.repo)
	require.NoError(t, err)
	err = worker.RunExecution(ctx, up.ID)
	require.NoError(t, err)
	require.True(t, tableExists(t, env.target, "projects"))
	storedUp, err := env.repo.GetExecution(ctx, up.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, storedUp.Status)

	rollback, err := svc.SubmitRollbackForReview(ctx, 42, env.dbID)
	require.NoError(t, err)
	require.Equal(t, ExecutionTypeRollback, rollback.Type)
	require.Equal(t, up.ID, *rollback.TargetExecutionID)
	require.Equal(t, "DROP TABLE projects", rollback.SQL)
	approveAndQueue(t, svc, leader, rollback.ID)
	err = worker.RunExecution(ctx, rollback.ID)
	require.NoError(t, err)
	require.False(t, tableExists(t, env.target, "projects"))

	forward, err := svc.SubmitForwardForReview(ctx, 42, env.dbID)
	require.NoError(t, err)
	require.Equal(t, ExecutionTypeForward, forward.Type)
	require.Equal(t, up.ID, *forward.TargetExecutionID)
	require.Equal(t, "CREATE TABLE projects (id bigint PRIMARY KEY)", forward.SQL)
	approveAndQueue(t, svc, leader, forward.ID)
	err = worker.RunExecution(ctx, forward.ID)
	require.NoError(t, err)
	require.True(t, tableExists(t, env.target, "projects"))

	history, err := svc.History(ctx, 42, env.dbID, 1, 20)
	require.NoError(t, err)
	require.Len(t, history.Entries, 3)
	require.Equal(t, ExecutionTypeForward, history.Entries[0].Type)
	require.True(t, history.Entries[0].Active)
	require.Equal(t, ExecutionTypeRollback, history.Entries[1].Type)
	require.False(t, history.Entries[1].Active)
	require.Equal(t, ExecutionTypeUp, history.Entries[2].Type)
	require.False(t, history.Entries[2].Active)
}
