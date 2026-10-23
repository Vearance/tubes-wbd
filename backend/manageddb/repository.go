package manageddb

import "context"

type Repository interface {
	Create(ctx context.Context, d *ManagedDatabase) error
	FindByID(ctx context.Context, id uint) (*ManagedDatabase, error)
	List(ctx context.Context) ([]ManagedDatabase, error)
	Update(ctx context.Context, d *ManagedDatabase) error
	Delete(ctx context.Context, id uint) error
}