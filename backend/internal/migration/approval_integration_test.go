//go:build integration

package migration

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"tubes-wbd/internal/access"
	"tubes-wbd/internal/users"
)

func (e *draftEnv) addApproverGroup(t *testing.T, groupName, leaderName string, permission access.PermissionLevel) uint {
	t.Helper()
	leader := users.User{Username: leaderName, PasswordHash: "x"}
	require.NoError(t, e.platform.Create(&leader).Error)
	group := access.Group{Name: groupName}
	require.NoError(t, e.platform.Create(&group).Error)
	e.grantGroup(t, group.ID, permission)
	require.NoError(t, e.platform.Create(&access.UserGroup{UserID: leader.ID, GroupID: group.ID}).Error)
	require.NoError(t, e.platform.Model(&group).Update("leader_user_id", leader.ID).Error)
	return leader.ID
}

func (e *draftEnv) grantGroup(t *testing.T, groupID uint, permission access.PermissionLevel) {
	t.Helper()
	require.NoError(t, e.platform.Create(&access.GroupDatabasePermission{
		GroupID: groupID, ManagedDatabaseID: e.dbID, Permission: permission,
	}).Error)
}

func approveAndQueue(t *testing.T, svc *MigrationService, leaderID, executionID uint) {
	t.Helper()
	summary, err := svc.ApproveExecution(context.Background(), leaderID, executionID, "")
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusApproved, summary.ExecutionStatus)
	_, err = svc.EnqueueExecution(context.Background(), leaderID, executionID)
	require.NoError(t, err)
}

func submitIntegrationDraft(t *testing.T, env *draftEnv, svc *MigrationService) *Execution {
	t.Helper()
	draft, err := svc.CreateDraft(context.Background(), 42, env.dbID, Migration{Name: "add index", SQL: "SELECT 1"})
	require.NoError(t, err)
	execution, err := svc.SubmitDraftForReview(context.Background(), 42, draft.ID)
	require.NoError(t, err)
	return execution
}

func countRows(t *testing.T, env *draftEnv, model any, query string, args ...any) int64 {
	t.Helper()
	var count int64
	require.NoError(t, env.platform.Model(model).Where(query, args...).Count(&count).Error)
	return count
}

func TestIntegrationApprovalFromEveryGroupQueuesExecution(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()
	rudolf := env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit)
	goldship := env.addApproverGroup(t, "Team Spica", "goldship", access.PermissionViewOnly)
	execution := submitIntegrationDraft(t, env, svc)

	summary, err := svc.ExecutionApprovals(ctx, goldship, execution.ID)
	require.NoError(t, err)
	require.True(t, summary.CanDecide)
	require.Len(t, summary.Approvals, 2)
	require.Equal(t, "Student Council", summary.Approvals[0].GroupName)
	require.Equal(t, "rudolf", *summary.Approvals[0].LeaderUsername)
	require.Equal(t, "goldship", *summary.Approvals[1].LeaderUsername)

	summary, err = svc.ApproveExecution(ctx, rudolf, execution.ID, "safe for the council app")
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusPendingReview, summary.ExecutionStatus)
	require.Equal(t, "rudolf", *summary.Approvals[0].DecidedByUsername)
	require.Nil(t, summary.Approvals[1].DecidedBy)

	pending, err := svc.PendingApprovals(ctx, goldship)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, execution.ID, pending[0].ID)
	pending, err = svc.PendingApprovals(ctx, rudolf)
	require.NoError(t, err)
	require.Empty(t, pending)

	_, err = svc.EnqueueExecution(ctx, rudolf, execution.ID)
	require.ErrorIs(t, err, ErrExecutionNotApproved)

	summary, err = svc.ApproveExecution(ctx, goldship, execution.ID, "")
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusApproved, summary.ExecutionStatus)

	queued, err := svc.EnqueueExecution(ctx, rudolf, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusQueued, queued.Status)
	require.EqualValues(t, 1, countRows(t, env, &ExecutionOutbox{}, "execution_id = ?", execution.ID))
	draft, err := env.repo.FindDraft(ctx, execution.DraftID)
	require.NoError(t, err)
	require.Equal(t, DraftStatusQueued, draft.Status)
}

func TestIntegrationSubmitWithoutLeaderWritesNothing(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()
	env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit)
	leaderless := access.Group{Name: "Team Spica"}
	require.NoError(t, env.platform.Create(&leaderless).Error)
	env.grantGroup(t, leaderless.ID, access.PermissionViewOnly)

	draft, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{Name: "add index", SQL: "SELECT 1"})
	require.NoError(t, err)
	_, err = svc.SubmitDraftForReview(ctx, 42, draft.ID)
	require.ErrorIs(t, err, ErrGroupLeaderMissing)
	require.ErrorContains(t, err, "Team Spica")

	require.Zero(t, countRows(t, env, &Execution{}, "draft_id = ?", draft.ID))
	require.Zero(t, countRows(t, env, &ExecutionApproval{}, "1 = 1"))
	stored, err := env.repo.FindDraft(ctx, draft.ID)
	require.NoError(t, err)
	require.Equal(t, DraftStatusDraft, stored.Status)
}

func TestIntegrationRejectReopensDraft(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()
	rudolf := env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit)
	goldship := env.addApproverGroup(t, "Team Spica", "goldship", access.PermissionViewOnly)
	execution := submitIntegrationDraft(t, env, svc)

	summary, err := svc.RejectExecution(ctx, goldship, execution.ID, "locks the trainees table")
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusRejected, summary.ExecutionStatus)
	require.Equal(t, ApprovalDecisionRejected, summary.Approvals[1].Decision)
	require.Equal(t, "locks the trainees table", summary.Approvals[1].Note)

	_, err = svc.ApproveExecution(ctx, rudolf, execution.ID, "")
	require.ErrorIs(t, err, ErrExecutionNotPendingReview)
	_, err = svc.EnqueueExecution(ctx, rudolf, execution.ID)
	require.ErrorIs(t, err, ErrExecutionNotApproved)

	draft, err := env.repo.FindDraft(ctx, execution.DraftID)
	require.NoError(t, err)
	require.Equal(t, DraftStatusDraft, draft.Status)
}

func TestIntegrationEnqueueAsksGroupsAddedAfterSubmission(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()
	rudolf := env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit)
	execution := submitIntegrationDraft(t, env, svc)
	_, err := svc.ApproveExecution(ctx, rudolf, execution.ID, "")
	require.NoError(t, err)

	goldship := env.addApproverGroup(t, "Team Spica", "goldship", access.PermissionViewOnly)
	_, err = svc.EnqueueExecution(ctx, rudolf, execution.ID)
	require.ErrorIs(t, err, ErrApprovalsOutdated)

	stored, err := env.repo.GetExecution(ctx, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusPendingReview, stored.Status)
	require.Zero(t, countRows(t, env, &ExecutionOutbox{}, "execution_id = ?", execution.ID))

	pending, err := svc.PendingApprovals(ctx, goldship)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	approveAndQueue(t, svc, goldship, execution.ID)
}

func TestIntegrationConcurrentApprovalsApproveOnce(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	leaders := []uint{
		env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit),
		env.addApproverGroup(t, "Team Spica", "goldship", access.PermissionViewOnly),
		env.addApproverGroup(t, "Team Rigil", "maruzensky", access.PermissionViewOnly),
	}
	execution := submitIntegrationDraft(t, env, svc)

	var wg sync.WaitGroup
	errs := make([]error, len(leaders))
	for i, leader := range leaders {
		wg.Go(func() {
			_, errs[i] = svc.ApproveExecution(context.Background(), leader, execution.ID, "")
		})
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	stored, err := env.repo.GetExecution(context.Background(), execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusApproved, stored.Status)
	require.Zero(t, countRows(t, env, &ExecutionApproval{}, "execution_id = ? AND decision <> ?", execution.ID, ApprovalDecisionApproved))
}

func TestIntegrationRollbackNeedsItsOwnApproval(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()
	rudolf := env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit)
	draft, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name: "create teams", SQL: "CREATE TABLE teams (id bigint PRIMARY KEY)", DownSQL: "DROP TABLE teams",
	})
	require.NoError(t, err)
	up, err := svc.SubmitDraftForReview(ctx, 42, draft.ID)
	require.NoError(t, err)
	approveAndQueue(t, svc, rudolf, up.ID)
	worker, err := NewExecutionWorker(svc, env.repo)
	require.NoError(t, err)
	require.NoError(t, worker.RunExecution(ctx, up.ID))

	rollback, err := svc.SubmitRollbackForReview(ctx, 42, env.dbID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusPendingReview, rollback.Status)
	_, err = svc.EnqueueExecution(ctx, rudolf, rollback.ID)
	require.ErrorIs(t, err, ErrExecutionNotApproved)

	summary, err := svc.ExecutionApprovals(ctx, rudolf, rollback.ID)
	require.NoError(t, err)
	require.Len(t, summary.Approvals, 1)
	require.Equal(t, ApprovalDecisionPending, summary.Approvals[0].Decision)
}
