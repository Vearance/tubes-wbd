package auth

import "time"

type AuthenticatedSession struct {
	UserID    uint
	SessionID uint
}

type LoginResult struct {
	Token     string
	CSRFToken string
	User      *User
	ExpiresAt time.Time
}

type SessionValidation struct {
	AuthenticatedSession AuthenticatedSession
	Rotation             *SessionRotation
}

type SessionRotation struct {
	Token     string
	ExpiresAt time.Time
}

type SessionLookup struct {
	Session         *Session
	MatchedPrevious bool
}
