package access

import "errors"

var (
	ErrInvalidGroupName          = errors.New("invalid group name")
	ErrGroupAlreadyExists        = errors.New("group already exists")
	ErrGroupNotFound             = errors.New("group not found")
	ErrMembershipExists          = errors.New("membership already exists")
	ErrMembershipNotFound        = errors.New("membership not found")
	ErrAdminMembershipNotAllowed = errors.New("platform admin cannot be a group member")
	ErrPermissionNotFound        = errors.New("database permission not found")
	ErrLeaderNotMember           = errors.New("group leader must be a member of the group")
	ErrLeaderDisabled            = errors.New("a disabled user cannot be a group leader")
	ErrMemberIsLeader            = errors.New("the group leader cannot be removed from the group; assign another leader or clear the leader first")
)
