package access

import (
	"context"

	"tubes-wbd/backend/auth"
)

type Repository interface {
	FindGroupIDsByUserID(ctx context.Context, userID uint) ([]uint, error)
	FindPermissionsByGroupIDs(
		ctx context.Context,
		groupIDs []uint,
		managedDatabaseID uint,
	) ([]GroupDatabasePermission, error)
}

type GroupRepository interface {
	CreateGroup(ctx context.Context, group *Group) error
	ListGroups(ctx context.Context) ([]Group, error)
	FindGroupByID(ctx context.Context, groupID uint) (*Group, error)
	FindGroupByName(ctx context.Context, name string) (*Group, error)
	CreateUserGroup(ctx context.Context, membership *UserGroup) error
	FindUserGroup(ctx context.Context, userID uint, groupID uint) (*UserGroup, error)
	ListMembers(ctx context.Context, groupID uint) ([]auth.User, error)
	DeleteUserGroup(ctx context.Context, userID uint, groupID uint) error
}
