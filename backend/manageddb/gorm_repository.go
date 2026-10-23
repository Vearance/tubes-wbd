package manageddb

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const sqlstateUniqueViolation = "23505"

type GormRepository struct {
	db *gorm.DB
}

var _ Repository = (*GormRepository)(nil)

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) Create(ctx context.Context, d *ManagedDatabase) error {
	if err := r.db.WithContext(ctx).Create(d).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicateName
		}
		return fmt.Errorf("creating managed database: %w", err)
	}

	return nil
}

func (r *GormRepository) FindByID(ctx context.Context, id uint) (*ManagedDatabase, error) {
	var d ManagedDatabase

	result := r.db.WithContext(ctx).
		Where("id = ?", id).
		Limit(1).
		Find(&d)
	if result.Error != nil {
		return nil, fmt.Errorf("querying managed database by id: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}

	return &d, nil
}

func (r *GormRepository) List(ctx context.Context) ([]ManagedDatabase, error) {
	list := []ManagedDatabase{}
	if err := r.db.WithContext(ctx).Order("id").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("listing managed databases: %w", err)
	}

	return list, nil
}

func (r *GormRepository) Update(ctx context.Context, d *ManagedDatabase) error {
	result := r.db.WithContext(ctx).
		Model(&ManagedDatabase{}).
		Where("id = ?", d.ID).
		Select(
			"name",
			"host",
			"port",
			"username",
			"encrypted_password",
			"db_name",
			"ssl_mode",
			"updated_at",
		).
		Updates(d)
	if result.Error != nil {
		if isUniqueViolation(result.Error) {
			return ErrDuplicateName
		}
		return fmt.Errorf("updating managed database: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *GormRepository) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Delete(&ManagedDatabase{}, id)
	if result.Error != nil {
		return fmt.Errorf("deleting managed database: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == sqlstateUniqueViolation
}