package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"
)

// default fallback
const (
	defaultIdleTimeout         = 30 * time.Minute
	defaultAbsoluteLifetime    = 8 * time.Hour
	defaultRotationInterval    = 15 * time.Minute
	defaultRotationGracePeriod = 30 * time.Second
	defaultTokenBytes          = 32
)

type ServiceConfig struct {
	IdleTimeout         time.Duration
	AbsoluteLifetime    time.Duration
	RotationInterval    time.Duration
	RotationGracePeriod time.Duration
	TokenBytes          int
	Now                 func() time.Time
}

type AuthService struct {
	users    UserRepository
	sessions SessionRepository
	hasher   PasswordHasher
	config   ServiceConfig
}

func NewAuthService(
	users UserRepository,
	sessions SessionRepository,
	hasher PasswordHasher,
	config ServiceConfig,
) (*AuthService, error) {
	if users == nil {
		return nil, errors.New("user repository is nil")
	}
	if sessions == nil {
		return nil, errors.New("session repository is nil")
	}
	if hasher == nil {
		return nil, errors.New("password hasher is nil")
	}

	config = config.withDefaults()
	if err := config.validate(); err != nil {
		return nil, err
	}

	return &AuthService{
		users:    users,
		sessions: sessions,
		hasher:   hasher,
		config:   config,
	}, nil
}

func (c ServiceConfig) withDefaults() ServiceConfig {
	if c.IdleTimeout == 0 {
		c.IdleTimeout = defaultIdleTimeout
	}
	if c.AbsoluteLifetime == 0 {
		c.AbsoluteLifetime = defaultAbsoluteLifetime
	}
	if c.RotationInterval == 0 {
		c.RotationInterval = defaultRotationInterval
	}
	if c.RotationGracePeriod == 0 {
		c.RotationGracePeriod = defaultRotationGracePeriod
	}
	if c.TokenBytes == 0 {
		c.TokenBytes = defaultTokenBytes
	}
	if c.Now == nil {
		c.Now = time.Now
	}

	return c
}

func (c ServiceConfig) validate() error {
	if c.IdleTimeout <= 0 {
		return fmt.Errorf("%w: idle timeout must be greater than zero", ErrInvalidServiceConfig)
	}
	if c.AbsoluteLifetime <= 0 {
		return fmt.Errorf("%w: absolute lifetime must be greater than zero", ErrInvalidServiceConfig)
	}
	if c.RotationInterval <= 0 {
		return fmt.Errorf("%w: rotation interval must be greater than zero", ErrInvalidServiceConfig)
	}
	if c.RotationGracePeriod <= 0 {
		return fmt.Errorf("%w: rotation grace period must be greater than zero", ErrInvalidServiceConfig)
	}
	if c.TokenBytes <= 0 {
		return fmt.Errorf("%w: token bytes must be greater than zero", ErrInvalidServiceConfig)
	}

	return nil
}

func (s *AuthService) Login(
	ctx context.Context,
	username string,
	password string,
) (LoginResult, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return LoginResult{}, ErrInvalidCredentials
	}

	user, err := s.users.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, fmt.Errorf("finding user for login: %w", err)
	}
	if user == nil {
		return LoginResult{}, ErrInvalidCredentials
	}

	if err := s.hasher.Compare(user.PasswordHash, password); err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	if user.DisabledAt != nil {
		return LoginResult{}, ErrUserDisabled
	}

	sessionToken, err := GenerateToken(s.config.TokenBytes)
	if err != nil {
		return LoginResult{}, fmt.Errorf("generating session token: %w", err)
	}

	csrfToken, err := GenerateToken(s.config.TokenBytes)
	if err != nil {
		return LoginResult{}, fmt.Errorf("generating csrf token: %w", err)
	}

	now := s.now()
	session := &Session{
		UserID:         user.ID,
		TokenHash:      hashToken(sessionToken),
		CreatedAt:      now,
		LastActivityAt: now,
		LastRotatedAt:  now,
		ExpiresAt:      now.Add(s.config.AbsoluteLifetime),
		CSRFTokenHash:  hashToken(csrfToken),
	}
	if err := s.sessions.Create(ctx, session); err != nil {
		return LoginResult{}, fmt.Errorf("creating login session: %w", err)
	}

	return LoginResult{
		Token:     sessionToken,
		CSRFToken: csrfToken,
		User:      user,
		ExpiresAt: session.ExpiresAt,
	}, nil
}

func (s *AuthService) ValidateSession(
	ctx context.Context,
	token string,
) (SessionValidation, error) {
	if token == "" {
		return SessionValidation{}, ErrUnauthenticated
	}

	lookup, err := s.sessions.FindByTokenHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return SessionValidation{}, ErrUnauthenticated
		}
		return SessionValidation{}, fmt.Errorf("finding session: %w", err)
	}
	if lookup == nil || lookup.Session == nil {
		return SessionValidation{}, ErrUnauthenticated
	}

	session := lookup.Session
	if session.RevokedAt != nil {
		return SessionValidation{}, ErrSessionRevoked
	}

	now := s.now()
	if !now.Before(session.ExpiresAt) {
		return SessionValidation{}, ErrSessionExpired
	}

	lastActivityAt := session.LastActivityAt
	if lastActivityAt.IsZero() {
		lastActivityAt = session.CreatedAt
	}
	if lastActivityAt.IsZero() || now.Sub(lastActivityAt) >= s.config.IdleTimeout {
		return SessionValidation{}, fmt.Errorf("%w: idle timeout", ErrSessionExpired)
	}

	user, err := s.users.FindByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return SessionValidation{}, ErrUnauthenticated
		}
		return SessionValidation{}, fmt.Errorf("finding session user: %w", err)
	}
	if user == nil {
		return SessionValidation{}, ErrUnauthenticated
	}
	if user.DisabledAt != nil {
		return SessionValidation{}, ErrUserDisabled
	}

	if lookup.MatchedPrevious {
		if session.PreviousTokenExpiresAt == nil || !now.Before(*session.PreviousTokenExpiresAt) {
			return SessionValidation{}, ErrStaleSessionToken
		}

		if err := s.sessions.Touch(ctx, session.ID, now); err != nil {
			return SessionValidation{}, fmt.Errorf("touching session activity: %w", err)
		}

		return SessionValidation{
			AuthenticatedSession: AuthenticatedSession{
				UserID:    session.UserID,
				SessionID: session.ID,
			},
		}, nil
	}

	shouldRotate := s.rotationDue(session, now)

	validation := SessionValidation{
		AuthenticatedSession: AuthenticatedSession{
			UserID:    session.UserID,
			SessionID: session.ID,
		},
	}

	if shouldRotate {
		rotation, err := s.rotateSession(ctx, session, now)
		if err != nil {
			return SessionValidation{}, err
		}
		validation.Rotation = &rotation
		return validation, nil
	}

	if err := s.sessions.Touch(ctx, session.ID, now); err != nil {
		return SessionValidation{}, fmt.Errorf("touching session activity: %w", err)
	}

	return validation, nil
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}

	lookup, err := s.sessions.FindByTokenHash(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil
		}
		return fmt.Errorf("finding session for logout: %w", err)
	}
	if lookup == nil || lookup.Session == nil {
		return nil
	}

	if err := s.sessions.Revoke(ctx, lookup.Session.ID, s.now()); err != nil {
		return fmt.Errorf("revoking session: %w", err)
	}

	return nil
}

func (s *AuthService) ValidateCSRFToken(
	ctx context.Context,
	sessionID uint,
	csrfToken string,
) error {
	if csrfToken == "" {
		return ErrInvalidCSRFToken
	}

	session, err := s.sessions.FindByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return ErrUnauthenticated
		}
		return fmt.Errorf("finding session for csrf validation: %w", err)
	}
	if session == nil {
		return ErrUnauthenticated
	}
	if session.RevokedAt != nil {
		return ErrSessionRevoked
	}
	if !s.now().Before(session.ExpiresAt) {
		return ErrSessionExpired
	}

	if subtle.ConstantTimeCompare(
		[]byte(session.CSRFTokenHash),
		[]byte(hashToken(csrfToken)),
	) != 1 {
		return ErrInvalidCSRFToken
	}

	return nil
}

func (s *AuthService) rotateSession(
	ctx context.Context,
	session *Session,
	now time.Time,
) (SessionRotation, error) {
	newToken, err := GenerateToken(s.config.TokenBytes)
	if err != nil {
		return SessionRotation{}, fmt.Errorf("generating rotated session token: %w", err)
	}

	previousTokenHash := session.TokenHash
	previousTokenExpiresAt := now.Add(s.config.RotationGracePeriod)
	session.PreviousTokenHash = &previousTokenHash
	session.PreviousTokenExpiresAt = &previousTokenExpiresAt
	session.TokenHash = hashToken(newToken)
	session.LastActivityAt = now
	session.LastRotatedAt = now

	if err := s.sessions.Update(ctx, session); err != nil {
		return SessionRotation{}, fmt.Errorf("saving rotated session: %w", err)
	}

	return SessionRotation{
		Token:     newToken,
		ExpiresAt: session.ExpiresAt,
	}, nil
}

func (s *AuthService) rotationDue(session *Session, now time.Time) bool {
	lastRotatedAt := session.LastRotatedAt
	if lastRotatedAt.IsZero() {
		lastRotatedAt = session.CreatedAt
	}
	if lastRotatedAt.IsZero() {
		return true
	}

	return now.Sub(lastRotatedAt) >= s.config.RotationInterval
}

func (s *AuthService) now() time.Time {
	return s.config.Now().UTC()
}
