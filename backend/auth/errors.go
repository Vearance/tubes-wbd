package auth

import "errors"

var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrUserNotFound         = errors.New("user not found")
	ErrUserDisabled         = errors.New("user disabled")
	ErrInvalidCSRFToken     = errors.New("invalid csrf token")
	ErrUnauthenticated      = errors.New("unauthenticated")
	ErrSessionNotFound      = errors.New("session not found")
	ErrSessionExpired       = errors.New("session expired")
	ErrSessionRevoked       = errors.New("session revoked")
	ErrStaleSessionToken    = errors.New("stale session token")
	ErrInvalidServiceConfig = errors.New("invalid auth service config")
)
