package migration

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"tubes-wbd/internal/database/targetdb"
)

func TestRetryableExecutionErrorRecognizesTransientPostgresErrors(t *testing.T) {
	for _, code := range []string{"40001", "40P01"} {
		require.True(t, retryableExecutionError(&pgconn.PgError{Code: code}), "SQLSTATE %s", code)
	}
	require.False(t, retryableExecutionError(&pgconn.PgError{Code: "23505"}))
}

type fixedConfigSnapshots struct {
	encrypted []byte
	config    targetdb.ConnConfig
}

func (s fixedConfigSnapshots) EncryptedSnapshot(context.Context, targetdb.DatabaseID) ([]byte, error) {
	return append([]byte(nil), s.encrypted...), nil
}

func (s fixedConfigSnapshots) ResolveSnapshot([]byte) (targetdb.ConnConfig, error) {
	return s.config, nil
}

func TestSubmitDraftForReviewSnapshotsAndLocksDraft(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	svc.snapshots = fixedConfigSnapshots{encrypted: []byte("ciphertext")}
	repo.addGroup(1, "Student Council", new(uint(1)), 9)
	draft := createTestDraft(t, svc, 9)

	execution, err := svc.SubmitDraftForReview(context.Background(), 1, draft.ID)

	require.NoError(t, err)
	require.Equal(t, ExecutionTypeUp, execution.Type)
	require.Equal(t, ExecutionStatusPendingReview, execution.Status)
	require.Equal(t, draft.SQL, execution.SQL)
	require.Equal(t, []byte("ciphertext"), execution.EncryptedTargetConfig)
	require.Equal(t, DraftStatusPendingReview, repo.status(draft.ID))
	_, err = svc.UpdateDraft(context.Background(), 1, draft.ID, Migration{Name: "changed", SQL: "SELECT 1"})
	require.ErrorIs(t, err, ErrDraftNotEditable)
}

func TestExecutionMustBeApprovedBeforeEnqueue(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(uint(1)), 9)
	draft := createTestDraft(t, svc, 9)
	execution, err := svc.SubmitDraftForReview(context.Background(), 1, draft.ID)
	require.NoError(t, err)

	_, err = svc.EnqueueExecution(context.Background(), 1, execution.ID)
	require.ErrorIs(t, err, ErrExecutionNotApproved)

	_, err = svc.ApproveExecution(context.Background(), 1, execution.ID, "")
	require.NoError(t, err)
	queued, err := svc.EnqueueExecution(context.Background(), 1, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusQueued, queued.Status)
	require.Equal(t, DraftStatusQueued, repo.status(draft.ID))
}

func TestSubmitCompensationForPartialBatchAndResumeAfterFailure(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(uint(1)), 9)
	ctx := context.Background()
	batch := &BatchDefinition{Schema: "public", Table: "items", KeyColumn: "id", BatchSize: 10}
	draft, err := svc.CreateDraft(ctx, 1, 9, Migration{
		Name: "backfill items", SQL: "UPDATE items SET value = 'done' WHERE id = ANY($1::bigint[])",
		DownSQL: "UPDATE items SET value = 'pending' WHERE id = ANY($1::bigint[])", Batch: batch,
	})
	require.NoError(t, err)
	activeDatabaseID := uint(9)
	partial := &Execution{
		ManagedDatabaseID: 9, DraftID: draft.ID, Type: ExecutionTypeUp,
		Status: ExecutionStatusPartial, Name: draft.Name, SQL: draft.SQL, DownSQL: draft.DownSQL,
		Batch: batch, BatchCount: 1, ActiveDatabaseID: &activeDatabaseID, ExecutedBy: 1,
	}
	require.NoError(t, repo.CreateExecution(ctx, partial))

	compensation, err := svc.SubmitCompensationForReview(ctx, 1, partial.ID)
	require.NoError(t, err)
	require.True(t, compensation.Compensation)
	require.Equal(t, ExecutionTypeRollback, compensation.Type)
	require.Equal(t, &partial.ID, compensation.TargetExecutionID)
	require.Equal(t, ExecutionStatusCompensationPending, repo.executions[partial.ID].Status)

	_, err = svc.ApproveExecution(ctx, 1, compensation.ID, "")
	require.NoError(t, err)
	_, err = svc.EnqueueExecution(ctx, 1, compensation.ID)
	require.NoError(t, err)
	claimed, err := repo.ClaimExecution(ctx, compensation.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusRunning, claimed.Status)
	require.Equal(t, ExecutionStatusCompensating, repo.executions[partial.ID].Status)

	require.NoError(t, repo.FinishExecution(ctx, compensation.ID, ExecutionStatusPartial, nil, "down_failed", "retry later"))
	paused, err := svc.Execution(ctx, 1, compensation.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusPartial, paused.Status)
	require.NotNil(t, paused.ActiveDatabaseID)

	_, err = svc.EnqueueExecution(ctx, 1, compensation.ID)
	require.NoError(t, err)
	claimed, err = repo.ClaimExecution(ctx, compensation.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusRunning, claimed.Status)
	require.NoError(t, repo.FinishExecution(ctx, compensation.ID, ExecutionStatusSuccess, new(int64(2)), "", ""))

	completed, err := svc.Execution(ctx, 1, compensation.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, completed.Status)
	require.Equal(t, ExecutionStatusCompensated, repo.executions[partial.ID].Status)
	require.Nil(t, repo.executions[partial.ID].ActiveDatabaseID)
}

func TestSubmitDraftForReviewDoesNotQueueOrExecute(t *testing.T) {
	svc, _, pools, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(uint(1)), 9)
	draft := createTestDraft(t, svc, 9)

	execution, err := svc.SubmitDraftForReview(context.Background(), 1, draft.ID)

	require.NoError(t, err)
	require.Zero(t, pools.calls, "submission must not connect to or execute against the target database")
	require.Equal(t, ExecutionStatusPendingReview, execution.Status)
	require.Empty(t, repo.executions[execution.ID].Version)
}
