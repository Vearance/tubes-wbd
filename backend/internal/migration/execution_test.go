package migration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubmitDraftForReviewSnapshotsAndLocksDraft(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(uint(1)), 9)
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
