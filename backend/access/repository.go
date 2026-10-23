package access

import "context"

type Repository interface {
	FindGroupIDsByUserID(ctx context.Context, userID uint) ([]uint, error)
	FindPermissionsByGroupIDs(
		ctx context.Context,
		groupIDs []uint,
		managedDatabaseID uint,
	) ([]GroupDatabasePermission, error)
}
