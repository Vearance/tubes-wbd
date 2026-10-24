//go:build integration

package migration

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"tubes-wbd/backend/access"
	"tubes-wbd/backend/database/platformdb"
	"tubes-wbd/backend/database/targetdb"
	"tubes-wbd/backend/manageddb"
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

	platformCfg, _ := newTarget(t, "")
	platform, err := platformdb.Open(ctx, platformdb.Config{DSN: dsn(platformCfg)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = platformdb.Close(platform) })
	require.NoError(t, manageddb.AutoMigrate(platform))
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

func TestIntegrationDraftLifecycle(t *testing.T) {
	env := newDraftEnv(t, "CREATE TABLE customers (id int PRIMARY KEY)")
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()

	d, err := svc.CreateDraft(ctx, 42, env.dbID, Migration{
		Name: "add phone",
		SQL:  "ALTER TABLE customers ADD COLUMN phone_number text",
	})
	require.NoError(t, err)
	require.Equal(t, DraftStatusDraft, d.Status)
	require.False(t, columnExists(t, env.target, "customers", "phone_number"), "saving a draft must not run it")

	run, err := svc.ExecuteDraft(ctx, 42, d.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusSuccess, run.Status)
	require.NotNil(t, run.Version)
	require.Equal(t, int64(1), *run.Version)
	require.Equal(t, uint(42), run.ExecutedBy)
	require.False(t, run.FinishedAt.Before(run.StartedAt))
	require.True(t, columnExists(t, env.target, "customers", "phone_number"))
	require.Equal(t, []int64{1}, historyVersions(t, env.target))

	stored, err := svc.GetDraft(ctx, 1, d.ID)
	require.NoError(t, err)
	require.Equal(t, DraftStatusApplied, stored.Status)
	require.NotNil(t, stored.AppliedVersion)
	require.Equal(t, int64(1), *stored.AppliedVersion)

	runs, err := svc.ListRuns(ctx, 1, d.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, run.ID, runs[0].ID)

	_, err = svc.UpdateDraft(ctx, 42, d.ID, Migration{Name: "changed", SQL: "SELECT 1"})
	require.ErrorIs(t, err, ErrDraftNotEditable, "an applied draft is history and must not change")
	_, err = svc.ExecuteDraft(ctx, 42, d.ID)
	require.ErrorIs(t, err, ErrDraftNotExecutable, "an applied draft must not run twice")
	require.Equal(t, []int64{1}, historyVersions(t, env.target))
}

func TestIntegrationFailedDraftCanBeFixedAndRetried(t *testing.T) {
	env := newDraftEnv(t, "CREATE TABLE customers (id int PRIMARY KEY)")
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()

	brokenSQL := "ALTER TABLE customer ADD COLUMN email text"
	d, err := svc.CreateDraft(ctx, 1, env.dbID, Migration{Name: "add email", SQL: brokenSQL})
	require.NoError(t, err)

	failed, err := svc.ExecuteDraft(ctx, 1, d.ID)
	require.ErrorIs(t, err, ErrExecutionFailed)
	require.NotNil(t, failed, "the failed run must be returned with the error")
	require.Equal(t, RunStatusFailed, failed.Status)
	require.Equal(t, "42P01", failed.ErrorCode)
	require.Contains(t, failed.ErrorMessage, `"customer"`)
	require.Nil(t, failed.Version)
	require.Nil(t, historyVersions(t, env.target))

	stored, err := svc.GetDraft(ctx, 1, d.ID)
	require.NoError(t, err)
	require.Equal(t, DraftStatusFailed, stored.Status)

	fixedSQL := "ALTER TABLE customers ADD COLUMN email text"
	reopened, err := svc.UpdateDraft(ctx, 1, d.ID, Migration{Name: "add email", SQL: fixedSQL})
	require.NoError(t, err)
	require.Equal(t, DraftStatusDraft, reopened.Status)

	succeeded, err := svc.ExecuteDraft(ctx, 1, d.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), *succeeded.Version, "the failed attempt must not use up a version")
	require.True(t, columnExists(t, env.target, "customers", "email"))

	runs, err := svc.ListRuns(ctx, 1, d.ID)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, RunStatusFailed, runs[0].Status)
	require.Equal(t, brokenSQL, runs[0].SQL, "a run keeps the SQL it actually executed")
	require.Equal(t, RunStatusSuccess, runs[1].Status)
	require.Equal(t, fixedSQL, runs[1].SQL)
}

func TestIntegrationDraftRunsOnlyOnceUnderConcurrency(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()

	d, err := svc.CreateDraft(ctx, 1, env.dbID, Migration{
		Name: "slow",
		SQL:  "SELECT pg_sleep(0.5); CREATE TABLE once (id int)",
	})
	require.NoError(t, err)

	const callers = 5
	runs := make([]*Run, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			runs[i], errs[i] = svc.ExecuteDraft(ctx, 1, d.ID)
		})
	}
	wg.Wait()

	succeeded := 0
	for i := range callers {
		if errs[i] == nil {
			succeeded++
			require.Equal(t, RunStatusSuccess, runs[i].Status)
			continue
		}
		require.ErrorIs(t, errs[i], ErrDraftNotExecutable)
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, []int64{1}, historyVersions(t, env.target))

	recorded, err := svc.ListRuns(ctx, 1, d.ID)
	require.NoError(t, err)
	require.Len(t, recorded, 1)
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
	_, err = viewer.ListRuns(ctx, 5, d.ID)
	require.NoError(t, err)

	_, err = viewer.CreateDraft(ctx, 5, env.dbID, Migration{Name: "x", SQL: "SELECT 1"})
	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	_, err = viewer.UpdateDraft(ctx, 5, d.ID, Migration{Name: "x", SQL: "DROP TABLE t"})
	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	_, err = viewer.ExecuteDraft(ctx, 5, d.ID)
	require.ErrorIs(t, err, access.ErrInsufficientPermission)

	require.False(t, tableExists(t, env.target, "t"))
	unchanged, err := editor.GetDraft(ctx, 1, d.ID)
	require.NoError(t, err)
	require.Equal(t, DraftStatusDraft, unchanged.Status)
	require.Equal(t, "CREATE TABLE t (id int)", unchanged.SQL)
}

func TestIntegrationUnreachableTargetLeavesDraftUntouched(t *testing.T) {
	env := newDraftEnv(t, "")
	ctx := context.Background()

	svc := env.service(t, stubAuthorizer{})
	d, err := svc.CreateDraft(ctx, 1, env.dbID, Migration{Name: "create t", SQL: "CREATE TABLE t (id int)"})
	require.NoError(t, err)

	env.targets = staticSource{}
	disconnected := env.service(t, stubAuthorizer{})
	_, err = disconnected.ExecuteDraft(ctx, 1, d.ID)
	require.ErrorIs(t, err, targetdb.ErrUnknownDatabase)

	stored, err := svc.GetDraft(ctx, 1, d.ID)
	require.NoError(t, err)
	require.Equal(t, DraftStatusDraft, stored.Status, "the draft must not be stuck in running")
	runs, err := svc.ListRuns(ctx, 1, d.ID)
	require.NoError(t, err)
	require.Empty(t, runs)
}

func TestIntegrationDraftForUnknownDatabase(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})

	_, err := svc.CreateDraft(context.Background(), 1, env.dbID+100, Migration{Name: "x", SQL: "SELECT 1"})

	require.ErrorIs(t, err, manageddb.ErrNotFound)
}

func TestIntegrationDraftsAndRunsCascadeWithManagedDatabase(t *testing.T) {
	env := newDraftEnv(t, "")
	svc := env.service(t, stubAuthorizer{})
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		d, err := svc.CreateDraft(ctx, 1, env.dbID, Migration{
			Name: fmt.Sprintf("create t%d", i),
			SQL:  fmt.Sprintf("CREATE TABLE t%d (id int)", i),
		})
		require.NoError(t, err)
		_, err = svc.ExecuteDraft(ctx, 1, d.ID)
		require.NoError(t, err)
	}

	require.NoError(t, env.platform.Delete(&manageddb.ManagedDatabase{}, env.dbID).Error)

	var drafts, runs int64
	require.NoError(t, env.platform.Model(&Draft{}).Count(&drafts).Error)
	require.NoError(t, env.platform.Model(&Run{}).Count(&runs).Error)
	require.Zero(t, drafts)
	require.Zero(t, runs)
}
