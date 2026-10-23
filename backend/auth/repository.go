package auth

import (
	"context"
	"time"
)

type UserRepository interface {
	FindByUsername(ctx context.Context, username string) (*User, error)
	FindByID(ctx context.Context, userID uint) (*User, error)
	FindAdmin(ctx context.Context) (*User, error)
	Create(ctx context.Context, user *User) error
	Update(ctx context.Context, user *User) error
}

type UserManagementRepository interface {
	UserRepository
	List(ctx context.Context) ([]User, error)
}

type SessionRepository interface {
	Create(ctx context.Context, session *Session) error
	FindByID(ctx context.Context, sessionID uint) (*Session, error)
	FindByTokenHash(ctx context.Context, tokenHash string) (*SessionLookup, error)
	Touch(ctx context.Context, sessionID uint, lastActivityAt time.Time) error
	Update(ctx context.Context, session *Session) error
	Revoke(ctx context.Context, sessionID uint, revokedAt time.Time) error
}
