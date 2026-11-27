package migration

import (
	"context"
	"log/slog"

	"tubes-wbd/internal/telemetry"
)

func (s *MigrationService) CreateDraft(ctx context.Context, userID, dbID uint, m Migration) (*Draft, error) {
	if err := s.authz.RequireEdit(ctx, userID, dbID); err != nil {
		return nil, err
	}
	m, err := validate(m)
	if err != nil {
		return nil, err
	}

	m, err = validateDefinition(m)
	if err != nil {
		return nil, err
	}
	d := &Draft{
		ManagedDatabaseID:  dbID,
		Name:               m.Name,
		SQL:                m.SQL,
		DownSQL:            m.DownSQL,
		Batch:              m.Batch,
		Irreversible:       m.Irreversible,
		IrreversibleReason: m.IrreversibleReason,
		Status:             DraftStatusDraft,
		CreatedBy:          userID,
		UpdatedBy:          userID,
	}
	actor := telemetry.ActorUser(userID)
	dbLabel := telemetry.TargetDatabaseLabel(dbID)
	telemetry.SetRequestDatabase(ctx, dbLabel)
	logCtx := telemetry.WithActor(telemetry.WithDatabase(ctx, dbLabel), actor)

	if err := s.repo.CreateDraft(logCtx, d); err != nil {
		return nil, err
	}
	slog.InfoContext(logCtx, "migration draft created",
		"event", "migration.draft.created",
		"draft_id", d.ID,
		"database", dbLabel,
		"draft_status", string(d.Status),
		"status", "ok",
	)
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
	m, err = validateDefinition(m)
	if err != nil {
		return nil, err
	}

	d.Name, d.SQL, d.DownSQL, d.Batch = m.Name, m.SQL, m.DownSQL, m.Batch
	d.Irreversible, d.IrreversibleReason = m.Irreversible, m.IrreversibleReason
	d.UpdatedBy = userID

	actor := telemetry.ActorUser(userID)
	dbLabel := telemetry.TargetDatabaseLabel(d.ManagedDatabaseID)
	telemetry.SetRequestDatabase(ctx, dbLabel)
	logCtx := telemetry.WithActor(telemetry.WithDatabase(ctx, dbLabel), actor)

	if err := s.repo.UpdateDraft(logCtx, d); err != nil {
		return nil, err
	}
	slog.InfoContext(logCtx, "migration draft updated",
		"event", "migration.draft.updated",
		"draft_id", d.ID,
		"database", dbLabel,
		"draft_status", string(d.Status),
		"status", "ok",
	)
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
