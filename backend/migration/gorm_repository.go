package migration

import (
	"context"
	"errors"
	"fmt"
	"time"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"tubes-wbd/backend/manageddb"
)

const sqlstateForeignKeyViolation = "23503"

type GormRepository struct {
	db *gorm.DB
}

var _ Repository = (*GormRepository)(nil)

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) CreateDraft(ctx context.Context, d *Draft) error {
	if err := r.db.WithContext(ctx).Create(d).Error; err != nil {
		if isForeignKeyViolation(err) {
			return manageddb.ErrNotFound
		}
		return fmt.Errorf("creating migration draft: %w", err)
	}
	return nil
}

func (r *GormRepository) FindDraft(ctx context.Context, id uint) (*Draft, error) {
	var d Draft
	result := r.db.WithContext(ctx).Where("id = ?", id).Limit(1).Find(&d)
	if result.Error != nil {
		return nil, fmt.Errorf("querying migration draft by id: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrDraftNotFound
	}
	return &d, nil
}

func (r *GormRepository) ListDrafts(ctx context.Context, managedDatabaseID uint) ([]Draft, error) {
	list := []Draft{}
	err := r.db.WithContext(ctx).
		Where("managed_database_id = ?", managedDatabaseID).
		Order("id DESC").
		Find(&list).Error
	if err != nil {
		return nil, fmt.Errorf("listing migration drafts: %w", err)
	}
	return list, nil
}

func (r *GormRepository) UpdateDraft(ctx context.Context, d *Draft) error {
	d.Status = DraftStatusDraft
	d.UpdatedAt = time.Now()

	result := r.db.WithContext(ctx).
		Model(&Draft{}).
		Where("id = ? AND status IN ?", d.ID, []DraftStatus{DraftStatusDraft, DraftStatusFailed}).
		Select("name", "sql", "status", "updated_by", "updated_at").
		Updates(d)
	if result.Error != nil {
		return fmt.Errorf("updating migration draft: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		if _, err := r.FindDraft(ctx, d.ID); err != nil {
			return err
		}
		return ErrDraftNotEditable
	}
	return nil
}

func (r *GormRepository) ClaimDraft(ctx context.Context, id uint, from DraftStatus) error {
	result := r.db.WithContext(ctx).
		Model(&Draft{}).
		Where("id = ? AND status = ?", id, from).
		Updates(map[string]any{"status": DraftStatusRunning, "updated_at": time.Now()})
	if result.Error != nil {
		return fmt.Errorf("claiming migration draft: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		if _, err := r.FindDraft(ctx, id); err != nil {
			return err
		}
		return ErrDraftNotExecutable
	}
	return nil
}

func (r *GormRepository) ReleaseDraft(ctx context.Context, id uint, to DraftStatus) error {
	err := r.db.WithContext(ctx).
		Model(&Draft{}).
		Where("id = ? AND status = ?", id, DraftStatusRunning).
		Updates(map[string]any{"status": to, "updated_at": time.Now()}).Error
	if err != nil {
		return fmt.Errorf("releasing migration draft: %w", err)
	}
	return nil
}

func (r *GormRepository) RecordRun(ctx context.Context, run *Run, status DraftStatus, appliedVersion *int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(run).Error; err != nil {
			return fmt.Errorf("creating migration run: %w", err)
		}
		err := tx.Model(&Draft{}).
			Where("id = ? AND status = ?", run.DraftID, DraftStatusRunning).
			Updates(map[string]any{
				"status":          status,
				"applied_version": appliedVersion,
				"updated_at":      time.Now(),
			}).Error
		if err != nil {
			return fmt.Errorf("finishing migration draft: %w", err)
		}
		return nil
	})
}

func (r *GormRepository) ListRuns(ctx context.Context, draftID uint) ([]Run, error) {
	list := []Run{}
	if err := r.db.WithContext(ctx).Where("draft_id = ?", draftID).Order("id").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("listing migration runs: %w", err)
	}
	return list, nil
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == sqlstateForeignKeyViolation
}