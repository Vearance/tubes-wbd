package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"tubes-wbd/backend/auth"
	httpapi "tubes-wbd/backend/http"
	"tubes-wbd/backend/http/middleware"
)

type handlerHasher struct{}

func (handlerHasher) Hash(password string) (string, error) {
	return "hash:" + password, nil
}

func (handlerHasher) Compare(hash string, password string) error {
	if hash != "hash:"+password {
		return errors.New("password mismatch")
	}
	return nil
}

type handlerUsers struct {
	user *auth.User
}

func (r *handlerUsers) FindByUsername(context.Context, string) (*auth.User, error) {
	if r.user == nil {
		return nil, auth.ErrUserNotFound
	}
	clone := *r.user
	return &clone, nil
}

func (r *handlerUsers) FindByID(context.Context, uint) (*auth.User, error) {
	if r.user == nil {
		return nil, auth.ErrUserNotFound
	}
	clone := *r.user
	return &clone, nil
}

func (r *handlerUsers) FindAdmin(context.Context) (*auth.User, error) {
	if r.user == nil || !r.user.IsAdmin {
		return nil, auth.ErrUserNotFound
	}
	clone := *r.user
	return &clone, nil
}

func (r *handlerUsers) Create(context.Context, *auth.User) error {
	return nil
}

func (r *handlerUsers) Update(context.Context, *auth.User) error {
	return nil
}

type handlerAdminAuthorizer struct {
	isAdmin bool
}

func (a handlerAdminAuthorizer) IsAdmin(context.Context, uint) (bool, error) {
	return a.isAdmin, nil
}

type handlerSessions struct {
	nextID  uint
	session *auth.Session
}

func (r *handlerSessions) Create(_ context.Context, session *auth.Session) error {
	r.nextID++
	session.ID = r.nextID
	clone := *session
	r.session = &clone
	return nil
}

func (r *handlerSessions) FindByID(_ context.Context, sessionID uint) (*auth.Session, error) {
	if r.session == nil || r.session.ID != sessionID {
		return nil, auth.ErrSessionNotFound
	}
	clone := *r.session
	return &clone, nil
}

func (r *handlerSessions) FindByTokenHash(
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

func (r *handlerSessions) Touch(
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

func (r *handlerSessions) Update(_ context.Context, session *auth.Session) error {
	clone := *session
	r.session = &clone
	return nil
}

func (r *handlerSessions) Revoke(
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

func (r *handlerSessions) RevokeAllByUser(
	_ context.Context,
	userID uint,
	revokedAt time.Time,
) error {
	if r.session != nil && r.session.UserID == userID {
		r.session.RevokedAt = &revokedAt
	}
	return nil
}

func TestAuthHTTPLoginAndLogout(t *testing.T) {
	service, sessions := newHandlerAuthService(t)
	api := apiServer{
		authService: service,
		cookieOptions: middleware.CookieOptions{
			Secure: true,
		},
	}
	router := httpapi.NewRouter(api, httpapi.RouterConfig{
		Auth:          api.authService,
		CookieOptions: api.cookieOptions,
	})

	loginRequest := httptest.NewRequest(
		http.MethodPost,
		"/auth/login",
		strings.NewReader(`{"username":"alice","password":"secret"}`),
	)
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()

	router.ServeHTTP(loginResponse, loginRequest)

	require.Equal(t, http.StatusOK, loginResponse.Code)
	loginCookie := findCookie(loginResponse.Result().Cookies(), middleware.SessionCookieName)
	require.NotNil(t, loginCookie)
	require.True(t, loginCookie.HttpOnly)
	require.True(t, loginCookie.Secure)

	loginResponseBody := loginResponse.Body.String()
	var loginBody httpapi.LoginResponse
	require.NoError(t, json.Unmarshal([]byte(loginResponseBody), &loginBody))
	require.NotEmpty(t, loginBody.CsrfToken)
	require.Equal(t, "alice", loginBody.User.Username)
	require.NotContains(t, loginResponseBody, loginCookie.Value)

	logoutRequest := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logoutRequest.AddCookie(loginCookie)
	logoutRequest.Header.Set("X-CSRF-Token", loginBody.CsrfToken)
	logoutResponse := httptest.NewRecorder()

	router.ServeHTTP(logoutResponse, logoutRequest)

	require.Equal(t, http.StatusNoContent, logoutResponse.Code)
	require.NotNil(t, sessions.session.RevokedAt)
	clearedCookie := findCookie(logoutResponse.Result().Cookies(), middleware.SessionCookieName)
	require.NotNil(t, clearedCookie)
	require.Equal(t, -1, clearedCookie.MaxAge)
}

func TestAdminPingRequiresAdmin(t *testing.T) {
	service, _ := newHandlerAuthService(t)
	result, err := service.Login(context.Background(), "alice", "secret")
	require.NoError(t, err)

	api := apiServer{authService: service}
	router := httpapi.NewRouter(api, httpapi.RouterConfig{
		Auth:            service,
		AdminAuthorizer: handlerAdminAuthorizer{isAdmin: false},
	})
	request := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
	request.AddCookie(&http.Cookie{
		Name:  middleware.SessionCookieName,
		Value: result.Token,
	})
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
}

func TestAuthHTTPLoginRejectsInvalidCredentials(t *testing.T) {
	service, _ := newHandlerAuthService(t)
	api := apiServer{authService: service}

	request := httptest.NewRequest(
		http.MethodPost,
		"/auth/login",
		strings.NewReader(`{"username":"alice","password":"wrong"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.PostAuthLogin(response, request)

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), "invalid credentials")
	require.NotContains(t, response.Body.String(), "password mismatch")
}

func TestAuthHTTPLoginRequiresJSONContentType(t *testing.T) {
	service, sessions := newHandlerAuthService(t)
	api := apiServer{authService: service}

	for _, contentType := range []string{
		"",
		"text/plain",
		"application/x-www-form-urlencoded",
		"multipart/form-data; boundary=x",
	} {
		t.Run(contentType, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				"/auth/login",
				strings.NewReader(`{"username":"alice","password":"secret"}`),
			)
			if contentType != "" {
				request.Header.Set("Content-Type", contentType)
			}
			response := httptest.NewRecorder()

			api.PostAuthLogin(response, request)

			require.Equal(t, http.StatusUnsupportedMediaType, response.Code)
			require.Nil(t, findCookie(response.Result().Cookies(), middleware.SessionCookieName))
			require.Nil(t, sessions.session)
		})
	}
}

func TestAuthHTTPLoginAcceptsJSONContentTypeWithCharset(t *testing.T) {
	service, _ := newHandlerAuthService(t)
	api := apiServer{authService: service}

	request := httptest.NewRequest(
		http.MethodPost,
		"/auth/login",
		strings.NewReader(`{"username":"alice","password":"secret"}`),
	)
	request.Header.Set("Content-Type", "Application/JSON; charset=utf-8")
	response := httptest.NewRecorder()

	api.PostAuthLogin(response, request)

	require.Equal(t, http.StatusOK, response.Code)
}

func TestAuthHTTPLogoutRejectsInvalidCSRF(t *testing.T) {
	service, _ := newHandlerAuthService(t)
	api := apiServer{authService: service}
	router := httpapi.NewRouter(api, httpapi.RouterConfig{Auth: service})

	result, err := service.Login(context.Background(), "alice", "secret")
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.AddCookie(&http.Cookie{
		Name:  middleware.SessionCookieName,
		Value: result.Token,
	})
	request.Header.Set("X-CSRF-Token", "wrong")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
}

func newHandlerAuthService(t *testing.T) (*auth.AuthService, *handlerSessions) {
	t.Helper()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	users := &handlerUsers{user: &auth.User{
		ID:           1,
		Username:     "alice",
		PasswordHash: "hash:secret",
	}}
	sessions := &handlerSessions{}
	service, err := auth.NewAuthService(
		users,
		sessions,
		handlerHasher{},
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

	return service, sessions
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}

	return nil
}
