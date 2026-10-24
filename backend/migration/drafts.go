package migration

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (s *MigrationService) CreateDraft(ctx context.Context, userID, dbID uint, m Migration) (*Draft, error) {
	if err := s.authz.RequireEdit(ctx, userID, dbID); err != nil {
		return nil, err
	}
	m, err := validate(m)
	if err != nil {
		return nil, err
	}

	d := &Draft{
		ManagedDatabaseID: dbID,
		Name:              m.Name,
		SQL:               m.SQL,
		Status:            DraftStatusDraft,
		CreatedBy:         userID,
		UpdatedBy:         userID,
	}
	if err := s.repo.CreateDraft(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *MigrationService) UpdateDraft(ctx context.Context, userID, draftID uint, m Migration) (*Draft, error) {
	d, err := s.repo.FindDraft(ctx, draftID)
	if err != nil {
		return nil, err
	}
	if err := s.authz.RequireEdit(ctx, userID, d.ManagedDatabaseID); err != nil {
		return nil, err
	}
	if !d.Status.open() {
		return nil, ErrDraftNotEditable
	}
	m, err = validate(m)
	if err != nil {
		return nil, err
	}

	d.Name, d.SQL, d.UpdatedBy = m.Name, m.SQL, userID
	if err := s.repo.UpdateDraft(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *MigrationService) GetDraft(ctx context.Context, userID, draftID uint) (*Draft, error) {
	d, err := s.repo.FindDraft(ctx, draftID)
	if err != nil {
		return nil, err
	}
	if err := s.authz.RequireView(ctx, userID, d.ManagedDatabaseID); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *MigrationService) ListDrafts(ctx context.Context, userID, dbID uint) ([]Draft, error) {
	if err := s.authz.RequireView(ctx, userID, dbID); err != nil {
		return nil, err
	}
	return s.repo.ListDrafts(ctx, dbID)
}

func (s *MigrationService) ListRuns(ctx context.Context, userID, draftID uint) ([]Run, error) {
	d, err := s.repo.FindDraft(ctx, draftID)
	if err != nil {
		return nil, err
	}
	if err := s.authz.RequireView(ctx, userID, d.ManagedDatabaseID); err != nil {
		return nil, err
	}
	return s.repo.ListRuns(ctx, draftID)
}

func (s *MigrationService) ExecuteDraft(ctx context.Context, userID, draftID uint) (*Run, error) {
	d, err := s.repo.FindDraft(ctx, draftID)
	if err != nil {
		return nil, err
	}
	if err := s.authz.RequireEdit(ctx, userID, d.ManagedDatabaseID); err != nil {
		return nil, err
	}
	if !d.Status.open() {
		return nil, ErrDraftNotExecutable
	}
	m, err := validate(Migration{Name: d.Name, SQL: d.SQL})
	if err != nil {
		return nil, err
	}

	ctx = context.WithoutCancel(ctx)
	if err := s.repo.ClaimDraft(ctx, d.ID, d.Status); err != nil {
		return nil, err
	}

	run := &Run{
		DraftID:           d.ID,
		ManagedDatabaseID: d.ManagedDatabaseID,
		SQL:               m.SQL,
		ExecutedBy:        userID,
		StartedAt:         time.Now(),
	}
	entry, execErr := s.run(ctx, userID, d.ManagedDatabaseID, m)
	run.FinishedAt = time.Now()

	var execFailure *ExecutionError
	switch {
	case execErr == nil:
		run.Status = RunStatusSuccess
		run.Version = &entry.Version
		err = s.repo.RecordRun(ctx, run, DraftStatusApplied, run.Version)
	case errors.As(execErr, &execFailure):
		run.Status = RunStatusFailed
		run.ErrorCode, run.ErrorMessage = execFailure.Code, execFailure.Message
		err = s.repo.RecordRun(ctx, run, DraftStatusFailed, nil)
	case errors.Is(execErr, ErrTransactionControl):
		run.Status = RunStatusFailed
		run.ErrorMessage = execErr.Error()
		err = s.repo.RecordRun(ctx, run, DraftStatusFailed, nil)
	default:
		if err := s.repo.ReleaseDraft(ctx, d.ID, d.Status); err != nil {
			return nil, errors.Join(execErr, err)
		}
		return nil, execErr
	}

	if err != nil {
		return nil, errors.Join(execErr, fmt.Errorf("migration: record run of draft %d: %w", d.ID, err))
	}
	return run, execErr
}
