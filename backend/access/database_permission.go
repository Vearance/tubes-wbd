package access

import (
	"context"
	"fmt"
)

func (s *GroupService) SetDatabasePermission(
	ctx context.Context,
	groupID uint,
	managedDatabaseID uint,
	level PermissionLevel,
) (*GroupDatabasePermission, error) {
	if level != PermissionViewOnly && level != PermissionViewEdit {
		return nil, fmt.Errorf("%w: %d", ErrInvalidPermission, level)
	}
	if _, err := s.groups.FindGroupByID(ctx, groupID); err != nil {
		return nil, err
	}

	permission := &GroupDatabasePermission{
		GroupID:           groupID,
		ManagedDatabaseID: managedDatabaseID,
		Permission:        level,
	}
	if err := s.groups.UpsertDatabasePermission(ctx, permission); err != nil {
		return nil, err
	}

	return permission, nil
}

func (s *GroupService) RemoveDatabasePermission(
	ctx context.Context,
	groupID uint,
	managedDatabaseID uint,
) error {
	return s.groups.DeleteDatabasePermission(ctx, groupID, managedDatabaseID)
}

func (s *GroupService) ListDatabasePermissions(
	ctx context.Context,
	groupID uint,
) ([]GroupDatabasePermission, error) {
	if _, err := s.groups.FindGroupByID(ctx, groupID); err != nil {
		return nil, err
	}

	permissions, err := s.groups.ListDatabasePermissions(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("listing group database permissions: %w", err)
	}

	return permissions, nil
}