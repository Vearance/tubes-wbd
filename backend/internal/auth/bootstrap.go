package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func BootstrapAdmin(
	ctx context.Context,
	users UserRepository,
	hasher PasswordHasher,
	username string,
	password string,
) error {
	if users == nil {
		return errors.New("user repository is nil")
	}
	if hasher == nil {
		return errors.New("password hasher is nil")
	}

	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("admin username is empty")
	}
	if password == "" {
		return errors.New("admin password is empty")
	}

	existingAdmin, err := users.FindAdmin(ctx)
	if err == nil {
		if existingAdmin == nil {
			return errors.New("admin repository returned a nil user")
		}
		return nil
	}
	if !errors.Is(err, ErrUserNotFound) {
		return fmt.Errorf("checking for existing admin: %w", err)
	}

	passwordHash, err := hasher.Hash(password)
	if err != nil {
		return fmt.Errorf("hashing admin password: %w", err)
	}

	admin := &User{
		Username:     username,
		PasswordHash: passwordHash,
		IsAdmin:      true,
	}
	if err := users.Create(ctx, admin); err != nil {
		return fmt.Errorf("creating initial admin: %w", err)
	}

	return nil
}
