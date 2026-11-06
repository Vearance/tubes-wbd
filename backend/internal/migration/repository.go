package migration

import "context"

type Repository interface {
	CreateDraft(ctx context.Context, d *Draft) error
	FindDraft(ctx context.Context, id uint) (*Draft, error)
	ListDrafts(ctx context.Context, managedDatabaseID uint) ([]Draft, error)
	UpdateDraft(ctx context.Context, d *Draft) error
	ClaimDraft(ctx context.Context, id uint, from DraftStatus) error
	ReleaseDraft(ctx context.Context, id uint, to DraftStatus) error
	RecordRun(ctx context.Context, run *Run, status DraftStatus, appliedVersion *int64) error
	ListRuns(ctx context.Context, draftID uint) ([]Run, error)
}
