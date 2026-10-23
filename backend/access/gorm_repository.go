package access

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

type GormRepository struct {
	db *gorm.DB
}

var _ Repository = (*GormRepository)(nil)

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) FindGroupIDsByUserID(
	ctx context.Context,
	userID uint,
) ([]uint, error) {
	var groupIDs []uint
	if err := r.db.WithContext(ctx).
		Model(&UserGroup{}).
		Where("user_id = ?", userID).
		Pluck("group_id", &groupIDs).
		Error; err != nil {
		return nil, fmt.Errorf("querying user groups: %w", err)
	}

	return groupIDs, nil
}

func (r *GormRepository) FindPermissionsByGroupIDs(
	ctx context.Context,
	groupIDs []uint,
	managedDatabaseID uint,
) ([]GroupDatabasePermission, error) {
	if len(groupIDs) == 0 {
		return []GroupDatabasePermission{}, nil
	}

	var permissions []GroupDatabasePermission
	if err := r.db.WithContext(ctx).
		Where("group_id IN ?", groupIDs).
		Where("managed_database_id = ?", managedDatabaseID).
		Find(&permissions).
		Error; err != nil {
		return nil, fmt.Errorf("querying group database permissions: %w", err)
	}

	return permissions, nil
}
