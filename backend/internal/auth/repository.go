package auth

import (
	"context"
	"time"

	"tubes-wbd/internal/users"
)

type UserRepository = users.IdentityRepository

type SessionRepository interface {
	Create(ctx context.Context, session *Session) error
	FindByID(ctx context.Context, sessionID uint) (*Session, error)
	FindByTokenHash(ctx context.Context, tokenHash string) (*SessionLookup, error)
	Touch(ctx context.Context, sessionID uint, lastActivityAt time.Time) error
	Update(ctx context.Context, session *Session) error
	Revoke(ctx context.Context, sessionID uint, revokedAt time.Time) error
}

type SessionRevoker interface {
	RevokeAllByUser(ctx context.Context, userID uint, revokedAt time.Time) error
}

type SessionRotationRequester interface {
	RequireRotationByUser(ctx context.Context, userID uint) error
}
