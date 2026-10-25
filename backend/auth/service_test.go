package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakePasswordHasher struct{}

func (fakePasswordHasher) Hash(password string) (string, error) {
	return "hash:" + password, nil
}

func (fakePasswordHasher) Compare(hash string, password string) error {
	if hash != "hash:"+password {
		return errors.New("password mismatch")
	}

	return nil
}

type fakeUserRepository struct {
	users       map[uint]*User
	byUsername  map[string]uint
	nextID      uint
	createError error
	updateError error
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{
		users:      map[uint]*User{},
		byUsername: map[string]uint{},
		nextID:     1,
	}
}

func (r *fakeUserRepository) FindByUsername(
	ctx context.Context,
	username string,
) (*User, error) {
	userID, ok := r.byUsername[username]
	if !ok {
		return nil, ErrUserNotFound
	}

	return cloneUser(r.users[userID]), nil
}

func (r *fakeUserRepository) FindByID(
	ctx context.Context,
	userID uint,
) (*User, error) {
	user, ok := r.users[userID]
	if !ok {
		return nil, ErrUserNotFound
	}

	return cloneUser(user), nil
}

func (r *fakeUserRepository) FindAdmin(
	ctx context.Context,
) (*User, error) {
	for _, user := range r.users {
		if user.IsAdmin {
			return cloneUser(user), nil
		}
	}

	return nil, ErrUserNotFound
}

func (r *fakeUserRepository) Create(
	ctx context.Context,
	user *User,
) error {
	if r.createError != nil {
		return r.createError
	}
	if user.ID == 0 {
		user.ID = r.nextID
		r.nextID++
	}

	r.users[user.ID] = cloneUser(user)
	r.byUsername[user.Username] = user.ID
	return nil
}

func (r *fakeUserRepository) Update(
	ctx context.Context,
	user *User,
) error {
	if r.updateError != nil {
		return r.updateError
	}
	if _, ok := r.users[user.ID]; !ok {
		return ErrUserNotFound
	}

	r.users[user.ID] = cloneUser(user)
	r.byUsername[user.Username] = user.ID
	return nil
}

type fakeSessionRepository struct {
	sessions    map[uint]*Session
	nextID      uint
	createError error
	touchError  error
	updateError error
	revokeError error
	touchCalls  int
}

func newFakeSessionRepository() *fakeSessionRepository {
	return &fakeSessionRepository{
		sessions: map[uint]*Session{},
		nextID:   1,
	}
}

func (r *fakeSessionRepository) Create(
	ctx context.Context,
	session *Session,
) error {
	if r.createError != nil {
		return r.createError
	}
	if session.ID == 0 {
		session.ID = r.nextID
		r.nextID++
	}

	r.sessions[session.ID] = cloneSession(session)
	return nil
}

func (r *fakeSessionRepository) FindByID(
	ctx context.Context,
	sessionID uint,
) (*Session, error) {
	session, ok := r.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}

	return cloneSession(session), nil
}

func (r *fakeSessionRepository) FindByTokenHash(
	ctx context.Context,
	tokenHash string,
) (*SessionLookup, error) {
	for _, session := range r.sessions {
		if session.TokenHash == tokenHash {
			return &SessionLookup{Session: cloneSession(session)}, nil
		}
		if session.PreviousTokenHash != nil && *session.PreviousTokenHash == tokenHash {
			return &SessionLookup{
				Session:         cloneSession(session),
				MatchedPrevious: true,
			}, nil
		}
	}

	return nil, ErrSessionNotFound
}

func (r *fakeSessionRepository) Touch(
	ctx context.Context,
	sessionID uint,
	lastActivityAt time.Time,
) error {
	if r.touchError != nil {
		return r.touchError
	}

	session, ok := r.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	r.touchCalls++
	session.LastActivityAt = lastActivityAt
	return nil
}

func (r *fakeSessionRepository) Update(
	ctx context.Context,
	session *Session,
) error {
	if r.updateError != nil {
		return r.updateError
	}
	if _, ok := r.sessions[session.ID]; !ok {
		return ErrSessionNotFound
	}

	r.sessions[session.ID] = cloneSession(session)
	return nil
}

func (r *fakeSessionRepository) Revoke(
	ctx context.Context,
	sessionID uint,
	revokedAt time.Time,
) error {
	if r.revokeError != nil {
		return r.revokeError
	}

	session, ok := r.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	session.RevokedAt = &revokedAt
	return nil
}

func TestService_Login(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	users := newFakeUserRepository()
	users.users[1] = &User{
		ID:           1,
		Username:     "alice",
		PasswordHash: "hash:secret",
	}
	users.byUsername["alice"] = 1
	sessions := newFakeSessionRepository()
	service := newTestService(t, users, sessions, &now)

	result, err := service.Login(context.Background(), " alice ", "secret")
	require.NoError(t, err)
	require.NotEmpty(t, result.Token)
	require.NotEmpty(t, result.CSRFToken)
	require.NotEqual(t, result.Token, result.CSRFToken)
	require.Equal(t, uint(1), result.User.ID)
	require.Equal(t, now.Add(time.Hour), result.ExpiresAt)

	session, err := sessions.FindByID(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, hashToken(result.Token), session.TokenHash)
	require.Equal(t, hashToken(result.CSRFToken), session.CSRFTokenHash)
	require.Equal(t, now, session.LastActivityAt)
	require.Equal(t, now, session.LastRotatedAt)
}

func TestService_LoginRejectsInvalidCredentialsAndDisabledUser(t *testing.T) {
	tests := []struct {
		name       string
		password   string
		disabledAt *time.Time
		wantError  error
	}{
		{
			name:      "wrong password",
			password:  "wrong",
			wantError: ErrInvalidCredentials,
		},
		{
			name:       "disabled user",
			password:   "secret",
			disabledAt: timePtr(time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)),
			wantError:  ErrUserDisabled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			users := newFakeUserRepository()
			users.users[1] = &User{
				ID:           1,
				Username:     "alice",
				PasswordHash: "hash:secret",
				DisabledAt:   tt.disabledAt,
			}
			users.byUsername["alice"] = 1
			sessions := newFakeSessionRepository()
			service := newTestService(t, users, sessions, &now)

			_, err := service.Login(context.Background(), "alice", tt.password)
			require.ErrorIs(t, err, tt.wantError)
			require.Empty(t, sessions.sessions)
		})
	}
}

func TestService_LoginRejectsUnknownUser(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	users := newFakeUserRepository()
	sessions := newFakeSessionRepository()
	service := newTestService(t, users, sessions, &now)

	_, err := service.Login(context.Background(), "unknown", "secret")
	require.ErrorIs(t, err, ErrInvalidCredentials)
	require.Empty(t, sessions.sessions)
}

func TestService_ValidateSessionTouchesActivity(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	users, sessions, service := newServiceWithLogin(t, &now)
	result, err := service.Login(context.Background(), "alice", "secret")
	require.NoError(t, err)

	now = now.Add(5 * time.Minute)
	validation, err := service.ValidateSession(context.Background(), result.Token)
	require.NoError(t, err)
	require.Equal(t, AuthenticatedSession{UserID: 1, SessionID: 1}, validation.AuthenticatedSession)
	require.Nil(t, validation.Rotation)
	require.Equal(t, 1, sessions.touchCalls)

	session, err := sessions.FindByID(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, now, session.LastActivityAt)
	_ = users
}

func TestService_ValidateSessionRejectsIdleAndAbsoluteExpiration(t *testing.T) {
	tests := []struct {
		name       string
		advance    time.Duration
		wantReason string
	}{
		{
			name:       "idle timeout",
			advance:    31 * time.Minute,
			wantReason: "idle timeout",
		},
		{
			name:       "absolute expiration",
			advance:    time.Hour,
			wantReason: "session expired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
			_, _, service := newServiceWithLogin(t, &now)
			result, err := service.Login(context.Background(), "alice", "secret")
			require.NoError(t, err)

			now = now.Add(tt.advance)
			_, err = service.ValidateSession(context.Background(), result.Token)
			require.ErrorIs(t, err, ErrSessionExpired)
			require.Contains(t, err.Error(), tt.wantReason)
		})
	}
}

func TestService_RotatesSessionWhenPrivilegeRequiresIt(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	_, sessions, service := newServiceWithLogin(t, &now)
	result, err := service.Login(context.Background(), "alice", "secret")
	require.NoError(t, err)

	sessions.sessions[1].RotationRequired = true
	validation, err := service.ValidateSession(context.Background(), result.Token)

	require.NoError(t, err)
	require.NotNil(t, validation.Rotation)
	require.NotEqual(t, result.Token, validation.Rotation.Token)
	session, err := sessions.FindByID(context.Background(), 1)
	require.NoError(t, err)
	require.False(t, session.RotationRequired)
	require.Equal(t, hashToken(result.Token), *session.PreviousTokenHash)
}

func TestService_RotatesSessionAndHonorsGracePeriod(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	_, sessions, service := newServiceWithLogin(t, &now)
	result, err := service.Login(context.Background(), "alice", "secret")
	require.NoError(t, err)
	originalExpiry := result.ExpiresAt

	now = now.Add(16 * time.Minute)
	validation, err := service.ValidateSession(context.Background(), result.Token)
	require.NoError(t, err)
	require.NotNil(t, validation.Rotation)
	require.NotEqual(t, result.Token, validation.Rotation.Token)
	require.Equal(t, originalExpiry, validation.Rotation.ExpiresAt)

	session, err := sessions.FindByID(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, hashToken(result.Token), *session.PreviousTokenHash)
	require.Equal(t, now.Add(30*time.Second), *session.PreviousTokenExpiresAt)
	require.Equal(t, originalExpiry, session.ExpiresAt)

	now = now.Add(10 * time.Second)
	graceValidation, err := service.ValidateSession(context.Background(), result.Token)
	require.NoError(t, err)
	require.Nil(t, graceValidation.Rotation)

	staleNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	_, _, staleService := newServiceWithLogin(t, &staleNow)
	staleResult, err := staleService.Login(context.Background(), "alice", "secret")
	require.NoError(t, err)

	staleNow = staleNow.Add(16 * time.Minute)
	_, err = staleService.ValidateSession(context.Background(), staleResult.Token)
	require.NoError(t, err)

	staleNow = staleNow.Add(31 * time.Second)
	_, err = staleService.ValidateSession(context.Background(), staleResult.Token)
	require.ErrorIs(t, err, ErrStaleSessionToken)
}

func TestService_LogoutRevokesSession(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	_, sessions, service := newServiceWithLogin(t, &now)
	result, err := service.Login(context.Background(), "alice", "secret")
	require.NoError(t, err)

	require.NoError(t, service.Logout(context.Background(), result.Token))

	session, err := sessions.FindByID(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, session.RevokedAt)

	_, err = service.ValidateSession(context.Background(), result.Token)
	require.ErrorIs(t, err, ErrSessionRevoked)
}

func TestService_ValidateCSRFToken(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	_, _, service := newServiceWithLogin(t, &now)
	result, err := service.Login(context.Background(), "alice", "secret")
	require.NoError(t, err)

	require.NoError(t, service.ValidateCSRFToken(context.Background(), 1, result.CSRFToken))
	require.ErrorIs(
		t,
		service.ValidateCSRFToken(context.Background(), 1, "wrong-token"),
		ErrInvalidCSRFToken,
	)
}

func newTestService(
	t *testing.T,
	users *fakeUserRepository,
	sessions *fakeSessionRepository,
	now *time.Time,
) *AuthService {
	t.Helper()

	service, err := NewAuthService(
		users,
		sessions,
		fakePasswordHasher{},
		ServiceConfig{
			IdleTimeout:         30 * time.Minute,
			AbsoluteLifetime:    time.Hour,
			RotationInterval:    15 * time.Minute,
			RotationGracePeriod: 30 * time.Second,
			TokenBytes:          32,
			Now: func() time.Time {
				return *now
			},
		},
	)
	require.NoError(t, err)

	return service
}

func newServiceWithLogin(
	t *testing.T,
	now *time.Time,
) (*fakeUserRepository, *fakeSessionRepository, *AuthService) {
	t.Helper()

	users := newFakeUserRepository()
	users.users[1] = &User{
		ID:           1,
		Username:     "alice",
		PasswordHash: "hash:secret",
	}
	users.byUsername["alice"] = 1
	sessions := newFakeSessionRepository()
	service := newTestService(t, users, sessions, now)

	return users, sessions, service
}

func cloneUser(user *User) *User {
	if user == nil {
		return nil
	}

	clone := *user
	if user.DisabledAt != nil {
		disabledAt := *user.DisabledAt
		clone.DisabledAt = &disabledAt
	}

	return &clone
}

func cloneSession(session *Session) *Session {
	if session == nil {
		return nil
	}

	clone := *session
	if session.PreviousTokenHash != nil {
		previousTokenHash := *session.PreviousTokenHash
		clone.PreviousTokenHash = &previousTokenHash
	}
	if session.PreviousTokenExpiresAt != nil {
		previousTokenExpiresAt := *session.PreviousTokenExpiresAt
		clone.PreviousTokenExpiresAt = &previousTokenExpiresAt
	}
	if session.RevokedAt != nil {
		revokedAt := *session.RevokedAt
		clone.RevokedAt = &revokedAt
	}

	return &clone
}

func timePtr(value time.Time) *time.Time {
	return &value
}
