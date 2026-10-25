package access

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"tubes-wbd/backend/auth"
)

type GroupService struct {
	users    auth.UserRepository
	groups   GroupRepository
	rotation auth.SessionRotationRequester
}

func NewGroupService(
	users auth.UserRepository,
	groups GroupRepository,
	rotation ...auth.SessionRotationRequester,
) (*GroupService, error) {
	if users == nil {
		return nil, errors.New("user repository is nil")
	}
	if groups == nil {
		return nil, errors.New("group repository is nil")
	}

	service := &GroupService{users: users, groups: groups}
	if len(rotation) > 0 {
		service.rotation = rotation[0]
	}
	return service, nil
}

func (s *GroupService) CreateGroup(
	ctx context.Context,
	name string,
) (*Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidGroupName
	}

	existing, err := s.groups.FindGroupByName(ctx, name)
	if err == nil && existing != nil {
		return nil, ErrGroupAlreadyExists
	}
	if err != nil && !errors.Is(err, ErrGroupNotFound) {
		return nil, fmt.Errorf("checking existing group: %w", err)
	}

	group := &Group{Name: name}
	if err := s.groups.CreateGroup(ctx, group); err != nil {
		return nil, fmt.Errorf("creating group: %w", err)
	}

	return group, nil
}

func (s *GroupService) ListGroups(ctx context.Context) ([]Group, error) {
	groups, err := s.groups.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing groups: %w", err)
	}

	return groups, nil
}

func (s *GroupService) ListMembers(
	ctx context.Context,
	groupID uint,
) ([]auth.User, error) {
	if _, err := s.groups.FindGroupByID(ctx, groupID); err != nil {
		return nil, err
	}

	members, err := s.groups.ListMembers(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("listing group members: %w", err)
	}

	return members, nil
}

func (s *GroupService) AddMember(
	ctx context.Context,
	groupID uint,
	userID uint,
) error {
	if _, err := s.groups.FindGroupByID(ctx, groupID); err != nil {
		return err
	}
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("finding user for group membership: %w", err)
	}
	if user.IsAdmin {
		return ErrAdminMembershipNotAllowed
	}

	existing, err := s.groups.FindUserGroup(ctx, userID, groupID)
	if err == nil && existing != nil {
		return ErrMembershipExists
	}
	if err != nil && !errors.Is(err, ErrMembershipNotFound) {
		return fmt.Errorf("checking group membership: %w", err)
	}

	if err := s.groups.CreateUserGroup(ctx, &UserGroup{
		UserID:  userID,
		GroupID: groupID,
	}); err != nil {
		return fmt.Errorf("creating group membership: %w", err)
	}

	permissions, err := s.groups.ListDatabasePermissions(ctx, groupID)
	if err != nil {
		return fmt.Errorf("checking group database permissions: %w", err)
	}
	if len(permissions) > 0 && s.rotation != nil {
		if err := s.rotation.RequireRotationByUser(ctx, userID); err != nil {
			return fmt.Errorf("requiring session rotation after group membership change: %w", err)
		}
	}

	return nil
}

func (s *GroupService) RemoveMember(
	ctx context.Context,
	groupID uint,
	userID uint,
) error {
	if err := s.groups.DeleteUserGroup(ctx, userID, groupID); err != nil {
		return err
	}

	return nil
}
