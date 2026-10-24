package migration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"tubes-wbd/backend/access"
	"tubes-wbd/backend/database/targetdb"
)

func createTestDraft(t *testing.T, svc *MigrationService, dbID uint) *Draft {
	t.Helper()
	d, err := svc.CreateDraft(context.Background(), 1, dbID, validMigration())
	require.NoError(t, err)
	return d
}

func setStatus(repo *fakeRepo, id uint, status DraftStatus) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	d := repo.drafts[id]
	d.Status = status
	repo.drafts[id] = d
}

func TestCreateDraft(t *testing.T) {
	svc, authz, pools, repo := newTestServiceWithRepo(t)

	d, err := svc.CreateDraft(context.Background(), 7, 3, Migration{Name: "  add phone ", SQL: "ALTER TABLE t ADD c int"})

	require.NoError(t, err)
	require.Equal(t, "edit", authz.method)
	require.Equal(t, uint(3), authz.dbID)
	require.Equal(t, "add phone", d.Name)
	require.Equal(t, DraftStatusDraft, d.Status)
	require.Equal(t, uint(7), d.CreatedBy)
	require.Equal(t, uint(7), d.UpdatedBy)
	require.Equal(t, uint(3), d.ManagedDatabaseID)
	require.Zero(t, pools.calls, "saving a draft must not touch the target")

	stored, err := repo.FindDraft(context.Background(), d.ID)
	require.NoError(t, err)
	require.Equal(t, *d, *stored)
}

func TestCreateDraftRequiresEdit(t *testing.T) {
	svc, authz, _, repo := newTestServiceWithRepo(t)
	authz.err = access.ErrInsufficientPermission

	_, err := svc.CreateDraft(context.Background(), 1, 1, validMigration())

	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	require.Empty(t, repo.drafts)
}

func TestCreateDraftValidatesSQL(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)

	_, err := svc.CreateDraft(context.Background(), 1, 1, Migration{Name: "x", SQL: "COMMIT"})
	require.ErrorIs(t, err, ErrTransactionControl)
	_, err = svc.CreateDraft(context.Background(), 1, 1, Migration{Name: "", SQL: "SELECT 1"})
	require.ErrorIs(t, err, ErrInvalidInput)
	require.Empty(t, repo.drafts)
}

func TestUpdateDraft(t *testing.T) {
	svc, authz, _, repo := newTestServiceWithRepo(t)
	d := createTestDraft(t, svc, 3)

	updated, err := svc.UpdateDraft(context.Background(), 9, d.ID, Migration{Name: "renamed", SQL: "SELECT 2"})

	require.NoError(t, err)
	require.Equal(t, "edit", authz.method)
	require.Equal(t, uint(3), authz.dbID, "permission is checked on the draft's own database")
	require.Equal(t, "renamed", updated.Name)
	require.Equal(t, uint(9), updated.UpdatedBy)
	stored, _ := repo.FindDraft(context.Background(), d.ID)
	require.Equal(t, "SELECT 2", stored.SQL)
}

func TestUpdateFailedDraftReopensIt(t *testing.T) {
	svc, _, _, repo := newTestServiceWithRepo(t)
	d := createTestDraft(t, svc, 1)
	setStatus(repo, d.ID, DraftStatusFailed)

	updated, err := svc.UpdateDraft(context.Background(), 1, d.ID, Migration{Name: "fixed", SQL: "SELECT 1"})

	require.NoError(t, err)
	require.Equal(t, DraftStatusDraft, updated.Status)
	require.Equal(t, DraftStatusDraft, repo.status(d.ID))
}

func TestUpdateDraftRejected(t *testing.T) {
	ctx := context.Background()

	t.Run("not found", func(t *testing.T) {
		svc, _, _, _ := newTestServiceWithRepo(t)
		_, err := svc.UpdateDraft(ctx, 1, 99, validMigration())
		require.ErrorIs(t, err, ErrDraftNotFound)
	})

	t.Run("without edit permission", func(t *testing.T) {
		svc, authz, _, repo := newTestServiceWithRepo(t)
		d := createTestDraft(t, svc, 1)
		authz.err = access.ErrInsufficientPermission

		_, err := svc.UpdateDraft(ctx, 1, d.ID, Migration{Name: "changed", SQL: "SELECT 2"})
		require.ErrorIs(t, err, access.ErrInsufficientPermission)
		stored, _ := repo.FindDraft(ctx, d.ID)
		require.Equal(t, validMigration().SQL, stored.SQL)
	})

	for _, status := range []DraftStatus{DraftStatusApplied, DraftStatusRunning} {
		t.Run(string(status), func(t *testing.T) {
			svc, _, _, repo := newTestServiceWithRepo(t)
			d := createTestDraft(t, svc, 1)
			setStatus(repo, d.ID, status)

			_, err := svc.UpdateDraft(ctx, 1, d.ID, Migration{Name: "changed", SQL: "SELECT 2"})
			require.ErrorIs(t, err, ErrDraftNotEditable)
		})
	}
}

func TestReadingDraftsRequiresView(t *testing.T) {
	ctx := context.Background()
	reads := map[string]func(*MigrationService, uint) error{
		"GetDraft": func(s *MigrationService, id uint) error {
			_, err := s.GetDraft(ctx, 7, id)
			return err
		},
		"ListDrafts": func(s *MigrationService, _ uint) error {
			_, err := s.ListDrafts(ctx, 7, 3)
			return err
		},
		"ListRuns": func(s *MigrationService, id uint) error {
			_, err := s.ListRuns(ctx, 7, id)
			return err
		},
	}

	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			svc, authz, _, _ := newTestServiceWithRepo(t)
			d := createTestDraft(t, svc, 3)

			require.NoError(t, read(svc, d.ID))
			require.Equal(t, "view", authz.method, "View Only must be enough to read drafts")
			require.Equal(t, uint(3), authz.dbID)

			authz.err = access.ErrInsufficientPermission
			require.ErrorIs(t, read(svc, d.ID), access.ErrInsufficientPermission)
		})
	}
}

func TestListDraftsOnlyReturnsThatDatabase(t *testing.T) {
	svc, _, _, _ := newTestServiceWithRepo(t)
	first := createTestDraft(t, svc, 1)
	createTestDraft(t, svc, 2)
	second := createTestDraft(t, svc, 1)

	list, err := svc.ListDrafts(context.Background(), 1, 1)

	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, second.ID, list[0].ID, "newest first")
	require.Equal(t, first.ID, list[1].ID)
}

func TestExecuteDraftRejected(t *testing.T) {
	ctx := context.Background()

	t.Run("not found", func(t *testing.T) {
		svc, _, pools, _ := newTestServiceWithRepo(t)
		_, err := svc.ExecuteDraft(ctx, 1, 99)
		require.ErrorIs(t, err, ErrDraftNotFound)
		require.Zero(t, pools.calls)
	})

	t.Run("without edit permission", func(t *testing.T) {
		svc, authz, pools, repo := newTestServiceWithRepo(t)
		d := createTestDraft(t, svc, 3)
		authz.err = access.ErrInsufficientPermission

		_, err := svc.ExecuteDraft(ctx, 1, d.ID)

		require.ErrorIs(t, err, access.ErrInsufficientPermission)
		require.Equal(t, "edit", authz.method)
		require.Equal(t, uint(3), authz.dbID)
		require.Zero(t, pools.calls, "target connection must not be opened without permission")
		require.Equal(t, DraftStatusDraft, repo.status(d.ID))
	})

	for _, status := range []DraftStatus{DraftStatusApplied, DraftStatusRunning} {
		t.Run(string(status), func(t *testing.T) {
			svc, _, pools, repo := newTestServiceWithRepo(t)
			d := createTestDraft(t, svc, 1)
			setStatus(repo, d.ID, status)

			_, err := svc.ExecuteDraft(ctx, 1, d.ID)

			require.ErrorIs(t, err, ErrDraftNotExecutable)
			require.Zero(t, pools.calls)
			require.Equal(t, status, repo.status(d.ID))
		})
	}
}

func TestExecuteDraftThatCannotStartKeepsItsStatus(t *testing.T) {
	for _, status := range []DraftStatus{DraftStatusDraft, DraftStatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			svc, _, pools, repo := newTestServiceWithRepo(t)
			d := createTestDraft(t, svc, 1)
			setStatus(repo, d.ID, status)
			pools.err = targetdb.ErrUnreachable

			run, err := svc.ExecuteDraft(context.Background(), 1, d.ID)

			require.ErrorIs(t, err, targetdb.ErrUnreachable)
			require.Nil(t, run)
			require.Equal(t, status, repo.status(d.ID), "a migration that never started must not change the draft")
			require.Empty(t, repo.runs, "a migration that never started must not be recorded")
		})
	}
}
