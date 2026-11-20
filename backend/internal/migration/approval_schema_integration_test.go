//go:build integration

package migration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"tubes-wbd/internal/access"
)

func requirePgError(t *testing.T, err error, code, constraint string) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "expected a PostgreSQL error, got %v", err)
	require.Equal(t, code, pgErr.Code)
	require.Equal(t, constraint, pgErr.ConstraintName)
}

type approvalFixture struct {
	env       *draftEnv
	group     access.Group
	execution *Execution
}

func newApprovalFixture(t *testing.T) approvalFixture {
	t.Helper()
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()
	env.addApproverGroup(t, "Approvers", "rudolf", access.PermissionViewEdit)

	draft, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{Name: "approval schema", SQL: "SELECT 1"})
	require.NoError(t, err)
	execution, err := svc.SubmitDraftForReview(ctx, 42, draft.ID)
	require.NoError(t, err)

	group := access.Group{Name: "Student Council"}
	require.NoError(t, env.platform.Create(&group).Error)
	return approvalFixture{env: env, group: group, execution: execution}
}

func (f approvalFixture) approval() *ExecutionApproval {
	return &ExecutionApproval{ExecutionID: f.execution.ID, GroupID: f.group.ID, GroupName: f.group.Name}
}

func TestIntegrationExecutionApprovalDefaultsToPending(t *testing.T) {
	f := newApprovalFixture(t)
	approval := f.approval()
	require.NoError(t, f.env.platform.Create(approval).Error)

	var stored ExecutionApproval
	require.NoError(t, f.env.platform.First(&stored, approval.ID).Error)
	require.Equal(t, ApprovalDecisionPending, stored.Decision)
	require.Nil(t, stored.DecidedBy)
	require.Nil(t, stored.DecidedAt)
	require.Equal(t, f.group.Name, stored.GroupName)
}

func TestIntegrationExecutionApprovalIsUniquePerGroup(t *testing.T) {
	f := newApprovalFixture(t)
	require.NoError(t, f.env.platform.Create(f.approval()).Error)

	err := f.env.platform.Create(f.approval()).Error
	requirePgError(t, err, "23505", "ux_execution_approval_group")
}

func TestIntegrationExecutionApprovalDecisionNeedsDecider(t *testing.T) {
	f := newApprovalFixture(t)
	decider := uint(7)
	now := time.Now()

	undecided := f.approval()
	undecided.Decision = ApprovalDecisionApproved
	err := f.env.platform.Create(undecided).Error
	requirePgError(t, err, "23514", "chk_execution_approval_decided")

	pendingWithDecider := f.approval()
	pendingWithDecider.DecidedBy = &decider
	pendingWithDecider.DecidedAt = &now
	err = f.env.platform.Create(pendingWithDecider).Error
	requirePgError(t, err, "23514", "chk_execution_approval_decided")

	decided := f.approval()
	decided.Decision = ApprovalDecisionRejected
	decided.DecidedBy = &decider
	decided.DecidedAt = &now
	decided.Note = "breaks the reporting service"
	require.NoError(t, f.env.platform.Create(decided).Error)
}

func TestIntegrationExecutionApprovalRejectsUnknownDecision(t *testing.T) {
	f := newApprovalFixture(t)
	decider := uint(7)
	now := time.Now()
	approval := f.approval()
	approval.Decision = "maybe"
	// Satisfy the decided check so only the decision check can fail.
	approval.DecidedBy = &decider
	approval.DecidedAt = &now

	err := f.env.platform.Create(approval).Error
	requirePgError(t, err, "23514", "chk_execution_approval_decision")
}

func TestIntegrationExecutionApprovalKeepsGroupFromBeingDeleted(t *testing.T) {
	f := newApprovalFixture(t)
	require.NoError(t, f.env.platform.Create(f.approval()).Error)

	err := f.env.platform.Delete(&access.Group{}, f.group.ID).Error
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "expected a PostgreSQL error, got %v", err)
	require.Equal(t, "23503", pgErr.Code)
}

func TestIntegrationExecutionApprovalRequiresExistingExecution(t *testing.T) {
	f := newApprovalFixture(t)
	approval := f.approval()
	approval.ExecutionID = f.execution.ID + 1000

	err := f.env.platform.Create(approval).Error
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "expected a PostgreSQL error, got %v", err)
	require.Equal(t, "23503", pgErr.Code)
}
