package migration

import "context"

type Repository interface {
	CreateDraft(ctx context.Context, d *Draft) error
	FindDraft(ctx context.Context, id uint) (*Draft, error)
	ListDrafts(ctx context.Context, managedDatabaseID uint) ([]Draft, error)
	UpdateDraft(ctx context.Context, d *Draft) error
}
