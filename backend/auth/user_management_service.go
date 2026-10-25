package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type UserManagementService struct {
	users    UserManagementRepository
	sessions SessionRevoker
	hasher   PasswordHasher
	now      func() time.Time
}

func NewUserManagementService(
	users UserManagementRepository,
	sessions SessionRevoker,
	hasher PasswordHasher,
) (*UserManagementService, error) {
	if users == nil {
		return nil, errors.New("user management repository is nil")
	}
	if sessions == nil {
		return nil, errors.New("session revoker is nil")
	}
	if hasher == nil {
		return nil, errors.New("password hasher is nil")
	}

	return &UserManagementService{
		users:    users,
		sessions: sessions,
		hasher:   hasher,
		now:      time.Now,
	}, nil
}

func (s *UserManagementService) CreateUser(
	ctx context.Context,
	username string,
	password string,
) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, ErrInvalidUserInput
	}

	existing, err := s.users.FindByUsername(ctx, username)
	if err == nil && existing != nil {
		return nil, ErrUserAlreadyExists
	}
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, fmt.Errorf("checking existing user: %w", err)
	}

	passwordHash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("hashing user password: %w", err)
	}

	user := &User{
		Username:     username,
		PasswordHash: passwordHash,
		IsAdmin:      false,
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	return user, nil
}

func (s *UserManagementService) ListUsers(ctx context.Context) ([]User, error) {
	users, err := s.users.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}

	return users, nil
}

func (s *UserManagementService) SetDisabled(
	ctx context.Context,
	userID uint,
	disabled bool,
) (*User, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("finding user to update: %w", err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	if user.IsAdmin && disabled {
		return nil, ErrCannotDisableAdmin
	}

	if disabled {
		now := s.now().UTC()
		user.DisabledAt = &now
	} else {
		user.DisabledAt = nil
	}
	if err := s.users.Update(ctx, user); err != nil {
		return nil, fmt.Errorf("updating user status: %w", err)
	}
	if disabled {
		if err := s.sessions.RevokeAllByUser(ctx, user.ID, s.now().UTC()); err != nil {
			return nil, fmt.Errorf("revoking user sessions: %w", err)
		}
	}

	return user, nil
}
