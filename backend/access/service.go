package access

import (
	"context"
	"errors"
	"fmt"

	"tubes-wbd/backend/auth"
)

var (
	ErrInsufficientPermission = errors.New("insufficient permission")
	ErrInvalidPermission      = errors.New("invalid permission")
)

type AuthorizationService struct {
	users  auth.UserRepository
	access Repository
}

func NewAuthorizationService(
	users auth.UserRepository,
	access Repository,
) (*AuthorizationService, error) {
	if users == nil {
		return nil, errors.New("user repository is nil")
	}
	if access == nil {
		return nil, errors.New("access repository is nil")
	}

	return &AuthorizationService{
		users:  users,
		access: access,
	}, nil
}

func (s *AuthorizationService) IsAdmin(ctx context.Context, userID uint) (bool, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			return false, auth.ErrUserNotFound
		}
		return false, fmt.Errorf("finding user for admin check: %w", err)
	}
	if user == nil {
		return false, auth.ErrUserNotFound
	}

	return user.IsAdmin, nil
}

func (s *AuthorizationService) EffectivePermission(
	ctx context.Context,
	userID uint,
	managedDatabaseID uint,
) (PermissionLevel, error) {
	groupIDs, err := s.access.FindGroupIDsByUserID(ctx, userID)
	if err != nil {
		return PermissionNone, fmt.Errorf("finding user groups: %w", err)
	}

	permissions, err := s.access.FindPermissionsByGroupIDs(ctx, groupIDs, managedDatabaseID)
	if err != nil {
		return PermissionNone, fmt.Errorf("finding database permissions: %w", err)
	}

	effective := PermissionNone
	for _, permission := range permissions {
		if !permission.Permission.Valid() {
			return PermissionNone, fmt.Errorf(
				"%w: %d",
				ErrInvalidPermission,
				permission.Permission,
			)
		}
		if permission.Permission > effective {
			effective = permission.Permission
		}
	}

	return effective, nil
}

func (s *AuthorizationService) RequireView(
	ctx context.Context,
	userID uint,
	managedDatabaseID uint,
) error {
	permission, err := s.EffectivePermission(ctx, userID, managedDatabaseID)
	if err != nil {
		return err
	}
	if permission < PermissionViewOnly {
		return ErrInsufficientPermission
	}

	return nil
}

func (s *AuthorizationService) RequireEdit(
	ctx context.Context,
	userID uint,
	managedDatabaseID uint,
) error {
	permission, err := s.EffectivePermission(ctx, userID, managedDatabaseID)
	if err != nil {
		return err
	}
	if permission < PermissionViewEdit {
		return ErrInsufficientPermission
	}

	return nil
}
