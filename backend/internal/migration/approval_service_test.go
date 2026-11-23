package migration

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"tubes-wbd/internal/access"
)

type perDatabaseAuthorizer struct {
	viewDenied map[uint]bool
	editDenied map[uint]bool
}

func (a perDatabaseAuthorizer) RequireView(_ context.Context, _ uint, databaseID uint) error {
	if a.viewDenied[databaseID] {
		return access.ErrInsufficientPermission
	}
	return nil
}

func (a perDatabaseAuthorizer) RequireEdit(_ context.Context, _ uint, databaseID uint) error {
	if a.editDenied[databaseID] {
		return access.ErrInsufficientPermission
	}
	return nil
}

const (
	submitter = uint(1)
	council   = uint(10)
	spica     = uint(20)
	outsider  = uint(99)
)

func submitForReview(t *testing.T, svc *MigrationService, databaseID uint) *Execution {
	t.Helper()
	draft := createTestDraft(t, svc, databaseID)
	execution, err := svc.SubmitDraftForReview(context.Background(), submitter, draft.ID)
	require.NoError(t, err)
	return execution
}

func TestSubmitRequiresGroupsWithLeaders(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	ctx := context.Background()

	draft := createTestDraft(t, svc, 9)
	_, err := svc.SubmitDraftForReview(ctx, submitter, draft.ID)
	require.ErrorIs(t, err, ErrNoApprovers)

	repo.addGroup(1, "Student Council", new(council), 9)
	repo.addGroup(2, "Team Spica", nil, 9)
	_, err = svc.SubmitDraftForReview(ctx, submitter, draft.ID)
	require.ErrorIs(t, err, ErrGroupLeaderMissing)
	require.ErrorContains(t, err, "Team Spica")
	require.Equal(t, DraftStatusDraft, repo.status(draft.ID))
	require.Empty(t, repo.executions)
}

func TestSubmitSnapshotsOnePendingApprovalPerGroup(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(council), 9)
	repo.addGroup(2, "Team Spica", new(spica), 9)
	repo.addGroup(3, "Other Database", new(outsider), 8)

	execution := submitForReview(t, svc, 9)

	summary, err := svc.ExecutionApprovals(context.Background(), council, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusPendingReview, summary.ExecutionStatus)
	require.True(t, summary.CanDecide)
	require.Len(t, summary.Approvals, 2)
	for _, approval := range summary.Approvals {
		require.Equal(t, ApprovalDecisionPending, approval.Decision)
	}
}

func TestEveryGroupMustApprove(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(council), 9)
	repo.addGroup(2, "Team Spica", new(spica), 9)
	execution := submitForReview(t, svc, 9)
	ctx := context.Background()

	summary, err := svc.ApproveExecution(ctx, council, execution.ID, "looks fine")
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusPendingReview, summary.ExecutionStatus)
	require.False(t, summary.CanDecide)
	require.Equal(t, DraftStatusPendingReview, repo.status(execution.DraftID))

	_, err = svc.EnqueueExecution(ctx, submitter, execution.ID)
	require.ErrorIs(t, err, ErrExecutionNotApproved)

	summary, err = svc.ApproveExecution(ctx, spica, execution.ID, "")
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusApproved, summary.ExecutionStatus)
	require.Equal(t, DraftStatusApproved, repo.status(execution.DraftID))
	for _, approval := range summary.Approvals {
		require.Equal(t, ApprovalDecisionApproved, approval.Decision)
		require.NotNil(t, approval.DecidedAt)
	}
	require.Equal(t, "looks fine", summary.Approvals[0].Note)

	queued, err := svc.EnqueueExecution(ctx, submitter, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusQueued, queued.Status)
}

func TestLeaderOfSeveralGroupsApprovesForAllOfThem(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(council), 9)
	repo.addGroup(2, "Team Spica", new(council), 9)
	execution := submitForReview(t, svc, 9)

	summary, err := svc.ApproveExecution(context.Background(), council, execution.ID, "")

	require.NoError(t, err)
	require.Equal(t, ExecutionStatusApproved, summary.ExecutionStatus)
	for _, approval := range summary.Approvals {
		require.Equal(t, council, *approval.DecidedBy)
	}
}

func TestSubmitterWhoLeadsTheGroupCanApprove(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(submitter), 9)
	execution := submitForReview(t, svc, 9)

	summary, err := svc.ApproveExecution(context.Background(), submitter, execution.ID, "")

	require.NoError(t, err)
	require.Equal(t, ExecutionStatusApproved, summary.ExecutionStatus)
}

func TestOnlyPendingGroupLeadersCanDecide(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(council), 9)
	repo.addGroup(2, "Team Spica", new(spica), 9)
	execution := submitForReview(t, svc, 9)
	ctx := context.Background()

	_, err := svc.ApproveExecution(ctx, outsider, execution.ID, "")
	require.ErrorIs(t, err, ErrNotApprover)
	_, err = svc.RejectExecution(ctx, outsider, execution.ID, "no")
	require.ErrorIs(t, err, ErrNotApprover)

	_, err = svc.ApproveExecution(ctx, council, execution.ID, "")
	require.NoError(t, err)
	_, err = svc.ApproveExecution(ctx, council, execution.ID, "")
	require.ErrorIs(t, err, ErrNotApprover)

	_, err = svc.ApproveExecution(ctx, spica, execution.ID, "")
	require.NoError(t, err)
	_, err = svc.ApproveExecution(ctx, spica, execution.ID, "")
	require.ErrorIs(t, err, ErrExecutionNotPendingReview)
}

func TestReplacedLeaderLosesTheDecision(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(council), 9)
	execution := submitForReview(t, svc, 9)
	repo.setLeader(1, new(spica))
	ctx := context.Background()

	_, err := svc.ApproveExecution(ctx, council, execution.ID, "")
	require.ErrorIs(t, err, ErrNotApprover)

	summary, err := svc.ApproveExecution(ctx, spica, execution.ID, "")
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusApproved, summary.ExecutionStatus)
}

func TestRejectReopensDraftAndBlocksQueue(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(council), 9)
	repo.addGroup(2, "Team Spica", new(spica), 9)
	execution := submitForReview(t, svc, 9)
	ctx := context.Background()

	_, err := svc.RejectExecution(ctx, council, execution.ID, "   ")
	require.ErrorIs(t, err, ErrInvalidInput)

	summary, err := svc.RejectExecution(ctx, council, execution.ID, "breaks the reporting job")
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusRejected, summary.ExecutionStatus)
	require.False(t, summary.CanDecide)
	require.Equal(t, DraftStatusDraft, repo.status(execution.DraftID))

	_, err = svc.ApproveExecution(ctx, spica, execution.ID, "")
	require.ErrorIs(t, err, ErrExecutionNotPendingReview)
	_, err = svc.EnqueueExecution(ctx, submitter, execution.ID)
	require.ErrorIs(t, err, ErrExecutionNotApproved)

	_, err = svc.UpdateDraft(ctx, submitter, execution.DraftID, Migration{Name: "fixed", SQL: "SELECT 1"})
	require.NoError(t, err)
	resubmitted, err := svc.SubmitDraftForReview(ctx, submitter, execution.DraftID)
	require.NoError(t, err)
	require.NotEqual(t, execution.ID, resubmitted.ID)
}

func TestDecisionNoteIsLimited(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(council), 9)
	execution := submitForReview(t, svc, 9)

	_, err := svc.ApproveExecution(context.Background(), council, execution.ID, strings.Repeat("x", maxApprovalNoteLen+1))

	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestDecisionRequiresViewPermission(t *testing.T) {
	svc, authz, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(council), 9)
	execution := submitForReview(t, svc, 9)
	authz.err = access.ErrInsufficientPermission

	_, err := svc.ApproveExecution(context.Background(), council, execution.ID, "")
	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	_, err = svc.ExecutionApprovals(context.Background(), council, execution.ID)
	require.ErrorIs(t, err, access.ErrInsufficientPermission)

	authz.err = nil
	summary, err := svc.ExecutionApprovals(context.Background(), council, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ApprovalDecisionPending, summary.Approvals[0].Decision)
}

func TestEnqueueRequiresEditPermission(t *testing.T) {
	svc, authz, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(council), 9)
	execution := submitForReview(t, svc, 9)
	_, err := svc.ApproveExecution(context.Background(), council, execution.ID, "")
	require.NoError(t, err)
	authz.err = access.ErrInsufficientPermission

	_, err = svc.EnqueueExecution(context.Background(), council, execution.ID)

	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	require.Equal(t, "edit", authz.method)
	require.Equal(t, ExecutionStatusApproved, repo.executions[execution.ID].Status)
}

func TestEnqueueAsksNewGroupsToApprove(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	repo.addGroup(1, "Student Council", new(council), 9)
	execution := submitForReview(t, svc, 9)
	ctx := context.Background()
	_, err := svc.ApproveExecution(ctx, council, execution.ID, "")
	require.NoError(t, err)

	repo.addGroup(2, "Team Spica", new(spica), 9)
	_, err = svc.EnqueueExecution(ctx, submitter, execution.ID)
	require.ErrorIs(t, err, ErrApprovalsOutdated)

	summary, err := svc.ExecutionApprovals(ctx, spica, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusPendingReview, summary.ExecutionStatus)
	require.True(t, summary.CanDecide)
	require.Len(t, summary.Approvals, 2)
	require.Equal(t, DraftStatusPendingReview, repo.status(execution.DraftID))

	_, err = svc.ApproveExecution(ctx, spica, execution.ID, "")
	require.NoError(t, err)
	queued, err := svc.EnqueueExecution(ctx, submitter, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusQueued, queued.Status)
}

func TestPendingApprovalsListsOnlyTheLeadersOpenReviews(t *testing.T) {
	repo := newFakeRepo()
	authz := perDatabaseAuthorizer{viewDenied: map[uint]bool{}}
	svc, err := NewMigrationService(authz, &fakePools{}, repo, Options{})
	require.NoError(t, err)
	repo.addGroup(1, "Student Council", new(council), 9, 8, 7)
	repo.addGroup(2, "Team Spica", new(spica), 9)
	ctx := context.Background()

	waiting := submitForReview(t, svc, 9)
	approved := submitForReview(t, svc, 8)
	hidden := submitForReview(t, svc, 7)
	_, err = svc.ApproveExecution(ctx, council, approved.ID, "")
	require.NoError(t, err)
	authz.viewDenied[7] = true

	pending, err := svc.PendingApprovals(ctx, council)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, waiting.ID, pending[0].ID)
	require.NotEqual(t, hidden.ID, pending[0].ID)

	pending, err = svc.PendingApprovals(ctx, outsider)
	require.NoError(t, err)
	require.Empty(t, pending)
}
