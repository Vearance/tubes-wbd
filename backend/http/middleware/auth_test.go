package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"tubes-wbd/backend/auth"
)

type fakeHasher struct{}

func (fakeHasher) Hash(password string) (string, error) {
	return "hash:" + password, nil
}

func (fakeHasher) Compare(hash string, password string) error {
	if hash != "hash:"+password {
		return errors.New("password mismatch")
	}
	return nil
}

type fakeUsers struct {
	user *auth.User
}

func (r *fakeUsers) FindByUsername(context.Context, string) (*auth.User, error) {
	if r.user == nil {
		return nil, auth.ErrUserNotFound
	}
	clone := *r.user
	return &clone, nil
}

func (r *fakeUsers) FindByID(context.Context, uint) (*auth.User, error) {
	if r.user == nil {
		return nil, auth.ErrUserNotFound
	}
	clone := *r.user
	return &clone, nil
}

func (r *fakeUsers) FindAdmin(context.Context) (*auth.User, error) {
	if r.user == nil || !r.user.IsAdmin {
		return nil, auth.ErrUserNotFound
	}
	clone := *r.user
	return &clone, nil
}

func (r *fakeUsers) Create(context.Context, *auth.User) error {
	return nil
}

func (r *fakeUsers) Update(context.Context, *auth.User) error {
	return nil
}

type fakeSessions struct {
	nextID  uint
	session *auth.Session
}

func (r *fakeSessions) Create(_ context.Context, session *auth.Session) error {
	r.nextID++
	session.ID = r.nextID
	clone := *session
	r.session = &clone
	return nil
}

func (r *fakeSessions) FindByID(_ context.Context, sessionID uint) (*auth.Session, error) {
	if r.session == nil || r.session.ID != sessionID {
		return nil, auth.ErrSessionNotFound
	}
	clone := *r.session
	return &clone, nil
}

func (r *fakeSessions) FindByTokenHash(
	_ context.Context,
	tokenHash string,
) (*auth.SessionLookup, error) {
	if r.session == nil {
		return nil, auth.ErrSessionNotFound
	}
	if r.session.TokenHash == tokenHash {
		clone := *r.session
		return &auth.SessionLookup{Session: &clone}, nil
	}
	if r.session.PreviousTokenHash != nil && *r.session.PreviousTokenHash == tokenHash {
		clone := *r.session
		return &auth.SessionLookup{Session: &clone, MatchedPrevious: true}, nil
	}
	return nil, auth.ErrSessionNotFound
}

func (r *fakeSessions) Touch(
	_ context.Context,
	sessionID uint,
	lastActivityAt time.Time,
) error {
	if r.session == nil || r.session.ID != sessionID {
		return auth.ErrSessionNotFound
	}
	r.session.LastActivityAt = lastActivityAt
	return nil
}

func (r *fakeSessions) Update(_ context.Context, session *auth.Session) error {
	clone := *session
	r.session = &clone
	return nil
}

func (r *fakeSessions) Revoke(
	_ context.Context,
	sessionID uint,
	revokedAt time.Time,
) error {
	if r.session == nil || r.session.ID != sessionID {
		return auth.ErrSessionNotFound
	}
	r.session.RevokedAt = &revokedAt
	return nil
}

type fakeAdminAuthorizer struct {
	isAdmin bool
	err     error
}

func (a fakeAdminAuthorizer) IsAdmin(context.Context, uint) (bool, error) {
	return a.isAdmin, a.err
}

func TestSessionMiddlewareAuthenticatesAndStoresSession(t *testing.T) {
	service, result := newAuthService(t, false)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := AuthenticatedSessionFromContext(r.Context())
		require.True(t, ok)
		require.Equal(t, auth.AuthenticatedSession{UserID: 1, SessionID: 1}, session)
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: result.Token})
	res := httptest.NewRecorder()

	Session(service, CookieOptions{})(next).ServeHTTP(res, req)

	require.Equal(t, http.StatusNoContent, res.Code)
}

func TestSessionMiddlewareRejectsMissingCookie(t *testing.T) {
	service, _ := newAuthService(t, false)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	res := httptest.NewRecorder()

	Session(service, CookieOptions{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	})).ServeHTTP(res, req)

	require.Equal(t, http.StatusUnauthorized, res.Code)
}

func TestCSRFMiddlewareValidatesHeaderToken(t *testing.T) {
	service, result := newAuthService(t, false)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := Session(service, CookieOptions{})(CSRF(service)(next))

	req := httptest.NewRequest(http.MethodPost, "/mutation", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: result.Token})
	req.Header.Set("X-CSRF-Token", result.CSRFToken)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	require.Equal(t, http.StatusNoContent, res.Code)
}

func TestCSRFMiddlewareRejectsMissingToken(t *testing.T) {
	service, result := newAuthService(t, false)
	handler := Session(service, CookieOptions{})(CSRF(service)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	})))

	req := httptest.NewRequest(http.MethodPost, "/mutation", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: result.Token})
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	require.Equal(t, http.StatusForbidden, res.Code)
}

func TestRequireAdmin(t *testing.T) {
	tests := []struct {
		name       string
		isAdmin    bool
		wantStatus int
	}{
		{name: "admin", isAdmin: true, wantStatus: http.StatusNoContent},
		{name: "regular user", isAdmin: false, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, result := newAuthService(t, tt.isAdmin)
			handler := Session(service, CookieOptions{})(RequireAdmin(fakeAdminAuthorizer{
				isAdmin: tt.isAdmin,
			})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})))

			req := httptest.NewRequest(http.MethodGet, "/admin", nil)
			req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: result.Token})
			res := httptest.NewRecorder()

			handler.ServeHTTP(res, req)

			require.Equal(t, tt.wantStatus, res.Code)
		})
	}
}

func newAuthService(t *testing.T, isAdmin bool) (*auth.AuthService, auth.LoginResult) {
	t.Helper()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	users := &fakeUsers{user: &auth.User{
		ID:           1,
		Username:     "alice",
		PasswordHash: "hash:secret",
		IsAdmin:      isAdmin,
	}}
	sessions := &fakeSessions{}
	service, err := auth.NewAuthService(
		users,
		sessions,
		fakeHasher{},
		auth.ServiceConfig{
			IdleTimeout:         time.Hour,
			AbsoluteLifetime:    8 * time.Hour,
			RotationInterval:    time.Hour,
			RotationGracePeriod: time.Minute,
			Now: func() time.Time {
				return now
			},
		},
	)
	require.NoError(t, err)

	result, err := service.Login(context.Background(), "alice", "secret")
	require.NoError(t, err)

	return service, result
}
