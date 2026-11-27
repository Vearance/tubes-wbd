//go:build integration

package migration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"tubes-wbd/internal/access"
	"tubes-wbd/internal/database/platformdb"
	"tubes-wbd/internal/database/targetdb"
	"tubes-wbd/internal/manageddb"
	"tubes-wbd/internal/users"
)

type draftEnv struct {
	platform *gorm.DB
	repo     *GormRepository
	target   *pgxpool.Pool
	targets  staticSource
	dbID     uint
}

func newDraftEnv(t *testing.T, targetSetup string) *draftEnv {
	t.Helper()
	ctx := context.Background()

	platform, err := platformdb.Open(ctx, platformdb.Config{DSN: dsn(newPlatformDB(t))})
	require.NoError(t, err)
	t.Cleanup(func() { _ = platformdb.Close(platform) })
	require.NoError(t, users.AutoMigrate(platform))
	require.NoError(t, manageddb.AutoMigrate(platform))
	require.NoError(t, access.AutoMigrate(platform))
	require.NoError(t, AutoMigrate(platform))

	targetCfg, targetPool := newTarget(t, targetSetup)
	managed := manageddb.ManagedDatabase{
		Name:              targetCfg.DBName,
		Host:              targetCfg.Host,
		Port:              targetCfg.Port,
		Username:          targetCfg.User,
		EncryptedPassword: []byte("not used by these tests"),
		DBName:            targetCfg.DBName,
		SSLMode:           targetCfg.SSLMode,
	}
	require.NoError(t, platform.Create(&managed).Error)

	return &draftEnv{
		platform: platform,
		repo:     NewGormRepository(platform),
		target:   targetPool,
		targets:  staticSource{managed.ID: targetCfg},
		dbID:     managed.ID,
	}
}

func (e *draftEnv) service(t *testing.T, authz Authorizer) *MigrationService {
	t.Helper()
	manager := targetdb.NewManager(e.targets, targetdb.Options{})
	t.Cleanup(manager.Close)

	svc, err := NewMigrationService(authz, manager, e.repo, Options{})
	require.NoError(t, err)
	return svc
}

func runDraftThroughWorker(t *testing.T, env *draftEnv, svc *MigrationService, leaderID, submitterID, draftID uint) *Execution {
	t.Helper()
	ctx := context.Background()
	submitted, err := svc.SubmitDraftForReview(ctx, submitterID, draftID)
	require.NoError(t, err)
	approveAndQueue(t, svc, leaderID, submitted.ID)
	worker, err := NewExecutionWorker(svc, env.repo)
	require.NoError(t, err)
	require.NoError(t, worker.RunExecution(ctx, submitted.ID))
	execution, err := svc.Execution(ctx, submitterID, submitted.ID)
	require.NoError(t, err)
	return execution
}

func TestIntegrationDraftLifecycle(t *testing.T) {
	env := newDraftEnv(t, "CREATE TABLE customers (id int PRIMARY KEY)")
	svc := env.service(t, stubAuthorizer{})
	leaderID := env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit)
	ctx := context.Background()

	d, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name: "add phone",
		SQL:  "ALTER TABLE customers ADD COLUMN phone_number text",
	})
	require.NoError(t, err)
	require.Equal(t, DraftStatusDraft, d.Status)
	require.False(t, columnExists(t, env.target, "customers", "phone_number"), "saving a draft must not run it")

	execution := runDraftThroughWorker(t, env, svc, leaderID, 42, d.ID)
	require.Equal(t, ExecutionStatusSuccess, execution.Status)
	require.NotNil(t, execution.Version)
	require.Equal(t, int64(1), *execution.Version)
	require.Equal(t, uint(42), execution.ExecutedBy)
	require.True(t, columnExists(t, env.target, "customers", "phone_number"))
	require.Equal(t, []int64{1}, historyVersions(t, env.target))

	stored, err := svc.GetDraft(ctx, 1, d.ID)
	require.NoError(t, err)
	require.Equal(t, DraftStatusApplied, stored.Status)
	require.NotNil(t, stored.AppliedVersion)
	require.Equal(t, int64(1), *stored.AppliedVersion)

	_, err = svc.UpdateDraft(ctx, 42, d.ID, Migration{Name: "changed", SQL: "SELECT 1"})
	require.ErrorIs(t, err, ErrDraftNotEditable, "an applied draft is history and must not change")
	_, err = svc.SubmitDraftForReview(ctx, 42, d.ID)
	require.ErrorIs(t, err, ErrDraftNotExecutable, "an applied draft must not run twice")
	require.Equal(t, []int64{1}, historyVersions(t, env.target))
}

func TestIntegrationUnknownCommitOutcomeIsReconciledBeforeFailure(t *testing.T) {
	tests := []struct {
		name          string
		commit        bool
		wantStatus    ExecutionStatus
		wantTable     bool
		wantRunStatus RunStatus
	}{
		{
			name:          "commit succeeded but response was lost",
			commit:        true,
			wantStatus:    ExecutionStatusSuccess,
			wantTable:     true,
			wantRunStatus: RunStatusSuccess,
		},
		{
			name:          "transaction did not commit",
			wantStatus:    ExecutionStatusFailed,
			wantRunStatus: RunStatusFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newDraftEnv(t, "")
			svc := env.service(t, stubAuthorizer{})
			leaderID := env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit)
			ctx := context.Background()

			draft, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
				Name: "ambiguous commit",
				SQL:  "CREATE TABLE ambiguous_commit (id bigint PRIMARY KEY)",
			})
			require.NoError(t, err)
			execution, err := svc.SubmitDraftForReview(ctx, 42, draft.ID)
			require.NoError(t, err)
			approveAndQueue(t, svc, leaderID, execution.ID)

			svc.commitTargetTx = func(ctx context.Context, tx pgx.Tx) error {
				if tt.commit {
					require.NoError(t, tx.Commit(ctx))
				} else {
					require.NoError(t, tx.Rollback(ctx))
				}
				return errors.New("simulated lost COMMIT response")
			}
			worker, err := NewExecutionWorker(svc, env.repo)
			require.NoError(t, err)
			require.NoError(t, worker.RunExecution(ctx, execution.ID))

			finished, err := svc.Execution(ctx, 42, execution.ID)
			require.NoError(t, err)
			require.Equal(t, tt.wantStatus, finished.Status)
			require.Nil(t, finished.ActiveDatabaseID, "resolved execution must release its platform lock")
			require.Equal(t, tt.wantTable, tableExists(t, env.target, "ambiguous_commit"))
			if !tt.commit {
				require.NoError(t, recordFailedHistoryAttempt(ctx, env.target, finished, errors.New("duplicate recovery attempt"), 0))
			}

			history, err := svc.History(ctx, 42, env.dbID, 1, 10)
			require.NoError(t, err)
			require.Len(t, history.Entries, 1)
			require.Equal(t, tt.wantRunStatus, history.Entries[0].Status)
		})
	}
}

func TestIntegrationUnknownCommitRemainsRunningUntilReconciled(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	leaderID := env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit)
	ctx := context.Background()

	draft, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name: "unresolved commit",
		SQL:  "CREATE TABLE unresolved_commit (id bigint PRIMARY KEY)",
	})
	require.NoError(t, err)
	execution, err := svc.SubmitDraftForReview(ctx, 42, draft.ID)
	require.NoError(t, err)
	approveAndQueue(t, svc, leaderID, execution.ID)

	var lockConn *pgxpool.Conn
	t.Cleanup(func() {
		if lockConn != nil {
			_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock(hashtext($1))", lockName)
			lockConn.Release()
		}
	})
	svc.commitTargetTx = func(ctx context.Context, tx pgx.Tx) error {
		require.NoError(t, tx.Rollback(ctx))
		var err error
		lockConn, err = env.target.Acquire(ctx)
		require.NoError(t, err)
		_, err = lockConn.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", lockName)
		require.NoError(t, err)
		return errors.New("simulated unresolved COMMIT response")
	}
	worker, err := NewExecutionWorker(svc, env.repo)
	require.NoError(t, err)
	require.Error(t, worker.RunExecution(ctx, execution.ID))

	unresolved, err := env.repo.GetExecution(ctx, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusRunning, unresolved.Status)
	require.NotNil(t, unresolved.ActiveDatabaseID, "an unresolved target outcome must retain the platform lock")
	require.False(t, tableExists(t, env.target, "unresolved_commit"))
	unresolvedHistory, err := svc.History(ctx, 42, env.dbID, 1, 10)
	require.NoError(t, err)
	require.Empty(t, unresolvedHistory.Entries, "do not record failure before reconciliation establishes the outcome")

	_, err = lockConn.Exec(ctx, "SELECT pg_advisory_unlock(hashtext($1))", lockName)
	require.NoError(t, err)
	lockConn.Release()
	lockConn = nil

	startedAt := time.Now().Add(-time.Hour)
	unresolved.StartedAt = &startedAt
	require.NoError(t, worker.recoverStaleExecution(ctx, unresolved))
	resolved, err := env.repo.GetExecution(ctx, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusQueued, resolved.Status)
	require.Nil(t, resolved.ActiveDatabaseID)

	history, err := svc.History(ctx, 42, env.dbID, 1, 10)
	require.NoError(t, err)
	require.Len(t, history.Entries, 1)
	require.Equal(t, RunStatusFailed, history.Entries[0].Status)
}

func TestIntegrationFailedDraftCanBeFixedAndRetried(t *testing.T) {
	env := newDraftEnv(t, "CREATE TABLE customers (id int PRIMARY KEY)")
	svc := env.service(t, stubAuthorizer{})
	leaderID := env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit)
	ctx := context.Background()

	brokenSQL := "ALTER TABLE customer ADD COLUMN email text"
	d, err := svc.CreateDraft(ctx, 1, env.dbID, Migration{Name: "add email", SQL: brokenSQL})
	require.NoError(t, err)

	failed := runDraftThroughWorker(t, env, svc, leaderID, 1, d.ID)
	require.Equal(t, ExecutionStatusFailed, failed.Status)
	require.Equal(t, "42P01", failed.ErrorCode)
	require.Contains(t, failed.ErrorMessage, `"customer"`)
	require.Nil(t, failed.Version)
	require.Equal(t, []int64{1}, historyVersions(t, env.target), "the failed attempt must be recorded in target history")

	stored, err := svc.GetDraft(ctx, 1, d.ID)
	require.NoError(t, err)
	require.Equal(t, DraftStatusFailed, stored.Status)

	fixedSQL := "ALTER TABLE customers ADD COLUMN email text"
	reopened, err := svc.UpdateDraft(ctx, 1, d.ID, Migration{Name: "add email", SQL: fixedSQL})
	require.NoError(t, err)
	require.Equal(t, DraftStatusDraft, reopened.Status)

	succeeded := runDraftThroughWorker(t, env, svc, leaderID, 1, d.ID)
	require.Equal(t, ExecutionStatusSuccess, succeeded.Status)
	require.Equal(t, int64(2), *succeeded.Version, "the failed attempt must occupy a target history version")
	require.True(t, columnExists(t, env.target, "customers", "email"))

	executions, err := svc.Executions(ctx, 1, env.dbID)
	require.NoError(t, err)
	require.Len(t, executions, 2)
	require.Equal(t, ExecutionStatusSuccess, executions[0].Status)
	require.Equal(t, fixedSQL, executions[0].SQL)
	require.Equal(t, ExecutionStatusFailed, executions[1].Status)
	require.Equal(t, brokenSQL, executions[1].SQL, "an execution keeps the SQL it actually ran")
}

func TestIntegrationDraftRunsOnlyOnceUnderConcurrency(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	leaderID := env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit)
	ctx := context.Background()

	d, err := svc.CreateDraft(ctx, 1, env.dbID, Migration{
		Name: "slow",
		SQL:  "SELECT pg_sleep(0.5); CREATE TABLE once (id int)",
	})
	require.NoError(t, err)

	const callers = 5
	submitted := make([]*Execution, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			submitted[i], errs[i] = svc.SubmitDraftForReview(ctx, 1, d.ID)
		})
	}
	wg.Wait()

	var execution *Execution
	for i := range callers {
		if errs[i] == nil {
			require.Nil(t, execution, "only one submission may succeed")
			execution = submitted[i]
			continue
		}
		require.ErrorIs(t, errs[i], ErrDraftNotExecutable)
	}
	require.NotNil(t, execution)
	approveAndQueue(t, svc, leaderID, execution.ID)

	worker, err := NewExecutionWorker(svc, env.repo)
	require.NoError(t, err)
	for i := range callers {
		wg.Go(func() {
			errs[i] = worker.RunExecution(ctx, execution.ID)
		})
	}
	wg.Wait()
	for i := range callers {
		require.NoError(t, errs[i])
	}

	require.Equal(t, []int64{1}, historyVersions(t, env.target), "duplicate deliveries must not commit twice")
	finished, err := svc.Execution(ctx, 1, execution.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusSuccess, finished.Status)
	require.Equal(t, 1, finished.Attempt)
}

func TestIntegrationViewOnlyCanReadDraftsButNotChangeThem(t *testing.T) {
	env := newDraftEnv(t, "")
	ctx := context.Background()

	editor := env.service(t, stubAuthorizer{})
	d, err := editor.CreateDraft(ctx, 1, env.dbID, Migration{Name: "create t", SQL: "CREATE TABLE t (id int)"})
	require.NoError(t, err)

	viewer := env.service(t, stubAuthorizer{editErr: access.ErrInsufficientPermission})

	got, err := viewer.GetDraft(ctx, 5, d.ID)
	require.NoError(t, err)
	require.Equal(t, "CREATE TABLE t (id int)", got.SQL)
	list, err := viewer.ListDrafts(ctx, 5, env.dbID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	_, err = viewer.CreateDraft(ctx, 5, env.dbID, Migration{Name: "x", SQL: "SELECT 1"})
	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	_, err = viewer.UpdateDraft(ctx, 5, d.ID, Migration{Name: "x", SQL: "DROP TABLE t"})
	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	_, err = viewer.SubmitDraftForReview(ctx, 5, d.ID)
	require.ErrorIs(t, err, access.ErrInsufficientPermission)

	require.False(t, tableExists(t, env.target, "t"))
	unchanged, err := editor.GetDraft(ctx, 1, d.ID)
	require.NoError(t, err)
	require.Equal(t, DraftStatusDraft, unchanged.Status)
	require.Equal(t, "CREATE TABLE t (id int)", unchanged.SQL)
}

func TestIntegrationUnreachableTargetFailsExecutionWithoutStrandingDraft(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	leaderID := env.addApproverGroup(t, "Student Council", "rudolf", access.PermissionViewEdit)
	ctx := context.Background()

	d, err := svc.CreateDraft(ctx, 1, env.dbID, Migration{Name: "create t", SQL: "CREATE TABLE t (id int)"})
	require.NoError(t, err)
	submitted, err := svc.SubmitDraftForReview(ctx, 1, d.ID)
	require.NoError(t, err)
	approveAndQueue(t, svc, leaderID, submitted.ID)

	env.targets = staticSource{}
	disconnected := env.service(t, stubAuthorizer{})
	worker, err := NewExecutionWorker(disconnected, env.repo)
	require.NoError(t, err)
	require.NoError(t, worker.RunExecution(ctx, submitted.ID))

	execution, err := svc.Execution(ctx, 1, submitted.ID)
	require.NoError(t, err)
	require.Equal(t, ExecutionStatusFailed, execution.Status)
	stored, err := svc.GetDraft(ctx, 1, d.ID)
	require.NoError(t, err)
	require.Equal(t, DraftStatusFailed, stored.Status, "the draft must not be stuck in running")
	require.False(t, tableExists(t, env.target, "t"))
}

func TestIntegrationDraftForUnknownDatabase(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})

	_, err := svc.CreateDraft(context.Background(), 1, env.dbID+100, Migration{Name: "x", SQL: "SELECT 1"})

	require.ErrorIs(t, err, manageddb.ErrNotFound)
}

func TestIntegrationDraftsCascadeWithManagedDatabase(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		_, err := svc.CreateDraft(ctx, 1, env.dbID, Migration{
			Name: fmt.Sprintf("create t%d", i),
			SQL:  fmt.Sprintf("CREATE TABLE t%d (id int)", i),
		})
		require.NoError(t, err)
	}

	require.NoError(t, env.platform.Delete(&manageddb.ManagedDatabase{}, env.dbID).Error)

	var drafts int64
	require.NoError(t, env.platform.Model(&Draft{}).Count(&drafts).Error)
	require.Zero(t, drafts)
}
