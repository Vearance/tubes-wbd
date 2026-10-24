package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"tubes-wbd/backend/auth"
	"tubes-wbd/backend/manageddb"
)

const sqlstateForeignKeyViolation = "23503"

type GormRepository struct {
	db *gorm.DB
}

var _ Repository = (*GormRepository)(nil)
var _ GroupRepository = (*GormRepository)(nil)

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) CreateGroup(ctx context.Context, group *Group) error {
	group.Name = strings.TrimSpace(group.Name)
	if err := r.db.WithContext(ctx).Create(group).Error; err != nil {
		return fmt.Errorf("creating group: %w", err)
	}

	return nil
}

func (r *GormRepository) ListGroups(ctx context.Context) ([]Group, error) {
	var groups []Group
	if err := r.db.WithContext(ctx).
		Order("id ASC").
		Find(&groups).
		Error; err != nil {
		return nil, fmt.Errorf("listing groups: %w", err)
	}

	return groups, nil
}

func (r *GormRepository) FindGroupByID(
	ctx context.Context,
	groupID uint,
) (*Group, error) {
	var group Group
	result := r.db.WithContext(ctx).
		Where("id = ?", groupID).
		Limit(1).
		Find(&group)
	if result.Error != nil {
		return nil, fmt.Errorf("querying group by id: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrGroupNotFound
	}

	return &group, nil
}

func (r *GormRepository) FindGroupByName(
	ctx context.Context,
	name string,
) (*Group, error) {
	var group Group
	result := r.db.WithContext(ctx).
		Where("name = ?", strings.TrimSpace(name)).
		Limit(1).
		Find(&group)
	if result.Error != nil {
		return nil, fmt.Errorf("querying group by name: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrGroupNotFound
	}

	return &group, nil
}

func (r *GormRepository) CreateUserGroup(
	ctx context.Context,
	membership *UserGroup,
) error {
	if err := r.db.WithContext(ctx).Create(membership).Error; err != nil {
		return fmt.Errorf("creating group membership: %w", err)
	}

	return nil
}

func (r *GormRepository) FindUserGroup(
	ctx context.Context,
	userID uint,
	groupID uint,
) (*UserGroup, error) {
	var membership UserGroup
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND group_id = ?", userID, groupID).
		Limit(1).
		Find(&membership)
	if result.Error != nil {
		return nil, fmt.Errorf("querying group membership: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrMembershipNotFound
	}

	return &membership, nil
}

func (r *GormRepository) ListMembers(
	ctx context.Context,
	groupID uint,
) ([]auth.User, error) {
	var users []auth.User
	if err := r.db.WithContext(ctx).
		Model(&auth.User{}).
		Joins("JOIN user_groups ON user_groups.user_id = users.id").
		Where("user_groups.group_id = ?", groupID).
		Order("users.id ASC").
		Find(&users).
		Error; err != nil {
		return nil, fmt.Errorf("listing group members: %w", err)
	}

	return users, nil
}

func (r *GormRepository) DeleteUserGroup(
	ctx context.Context,
	userID uint,
	groupID uint,
) error {
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND group_id = ?", userID, groupID).
		Delete(&UserGroup{})
	if result.Error != nil {
		return fmt.Errorf("deleting group membership: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrMembershipNotFound
	}

	return nil
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

func (r *GormRepository) UpsertDatabasePermission(
	ctx context.Context,
	permission *GroupDatabasePermission,
) error {
	permission.UpdatedAt = time.Now()
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "group_id"}, {Name: "managed_database_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"permission", "updated_at"}),
		}).
		Create(permission).
		Error; err != nil {
		if isForeignKeyViolation(err) {
			return manageddb.ErrNotFound
		}
		return fmt.Errorf("saving group database permission: %w", err)
	}

	return nil
}

func (r *GormRepository) DeleteDatabasePermission(
	ctx context.Context,
	groupID uint,
	managedDatabaseID uint,
) error {
	result := r.db.WithContext(ctx).
		Where("group_id = ? AND managed_database_id = ?", groupID, managedDatabaseID).
		Delete(&GroupDatabasePermission{})
	if result.Error != nil {
		return fmt.Errorf("deleting group database permission: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrPermissionNotFound
	}

	return nil
}

func (r *GormRepository) ListDatabasePermissions(
	ctx context.Context,
	groupID uint,
) ([]GroupDatabasePermission, error) {
	permissions := []GroupDatabasePermission{}
	if err := r.db.WithContext(ctx).
		Where("group_id = ?", groupID).
		Order("managed_database_id ASC").
		Find(&permissions).
		Error; err != nil {
		return nil, fmt.Errorf("listing group database permissions: %w", err)
	}

	return permissions, nil
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == sqlstateForeignKeyViolation
}
