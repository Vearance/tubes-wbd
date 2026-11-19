package migration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubmitDraftForReviewSnapshotsAndLocksDraft(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	draft := createTestDraft(t, svc, 9)

	execution, err := svc.SubmitDraftForReview(context.Background(), 1, draft.ID)

	require.NoError(t, err)
	require.Equal(t, ExecutionTypeUp, execution.Type)
	require.Equal(t, ExecutionStatusPendingReview, execution.Status)
	require.Equal(t, draft.SQL, execution.SQL)
	require.Equal(t, DraftStatusPendingReview, repo.status(draft.ID))
	_, err = svc.UpdateDraft(context.Background(), 1, draft.ID, Migration{Name: "changed", SQL: "SELECT 1"})
	require.ErrorIs(t, err, ErrDraftNotEditable)
}

func TestExecutionMustBeApprovedBeforeEnqueue(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	draft := createTestDraft(t, svc, 9)
	execution, err := svc.SubmitDraftForReview(context.Background(), 1, draft.ID)
	require.NoError(t, err)

	err = svc.EnqueueApprovedExecution(context.Background(), execution.ID)
	require.ErrorIs(t, err, ErrExecutionNotApproved)

	require.NoError(t, svc.ApproveExecution(context.Background(), execution.ID))
	require.NoError(t, svc.EnqueueApprovedExecution(context.Background(), execution.ID))
	stored, err := repo.GetExecution(context.Background(), execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusQueued, stored.Status)
	require.Equal(t, DraftStatusQueued, repo.status(draft.ID))
}

func TestSubmitDraftForReviewDoesNotQueueOrExecute(t *testing.T) {
	svc, _, pools, repo := newTestServiceWithRepo(t)
	draft := createTestDraft(t, svc, 9)

	execution, err := svc.SubmitDraftForReview(context.Background(), 1, draft.ID)

	require.NoError(t, err)
	require.Zero(t, pools.calls, "submission must not connect to or execute against the target database")
	require.Equal(t, ExecutionStatusPendingReview, execution.Status)
	require.Empty(t, repo.executions[execution.ID].Version)
}
