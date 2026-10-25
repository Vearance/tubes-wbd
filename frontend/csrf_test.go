package frontend

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"tubes-wbd/backend/auth"
	"tubes-wbd/backend/http/middleware"
)

type loginTestUsers struct {
	user *auth.User
}

func (r *loginTestUsers) FindByUsername(_ context.Context, username string) (*auth.User, error) {
	if r.user == nil || r.user.Username != username {
		return nil, nil
	}
	user := *r.user
	return &user, nil
}

func (r *loginTestUsers) FindByID(context.Context, uint) (*auth.User, error) { return nil, nil }
func (r *loginTestUsers) FindAdmin(context.Context) (*auth.User, error)      { return nil, nil }
func (r *loginTestUsers) Create(context.Context, *auth.User) error           { return nil }
func (r *loginTestUsers) Update(context.Context, *auth.User) error           { return nil }

type loginTestSessions struct {
	created int
}

func (r *loginTestSessions) Create(_ context.Context, session *auth.Session) error {
	r.created++
	session.ID = uint(r.created)
	return nil
}

func (r *loginTestSessions) FindByID(context.Context, uint) (*auth.Session, error) {
	return nil, nil
}

func (r *loginTestSessions) FindByTokenHash(context.Context, string) (*auth.SessionLookup, error) {
	return nil, auth.ErrSessionNotFound
}

func (r *loginTestSessions) Touch(context.Context, uint, time.Time) error  { return nil }
func (r *loginTestSessions) Update(context.Context, *auth.Session) error   { return nil }
func (r *loginTestSessions) Revoke(context.Context, uint, time.Time) error { return nil }

type loginTestHasher struct{}

func (loginTestHasher) Hash(password string) (string, error) {
	return "hash:" + password, nil
}

func (loginTestHasher) Compare(hash string, password string) error {
	if hash != "hash:"+password {
		return errors.New("password mismatch")
	}
	return nil
}

func newLoginTestHandler(t *testing.T) (*Handler, *loginTestSessions) {
	t.Helper()

	sessions := &loginTestSessions{}
	service, err := auth.NewAuthService(
		&loginTestUsers{user: &auth.User{ID: 1, Username: "alice", PasswordHash: "hash:secret"}},
		sessions,
		loginTestHasher{},
		auth.ServiceConfig{},
	)
	require.NoError(t, err)

	templates, err := parseTemplates()
	require.NoError(t, err)

	return &Handler{deps: Dependencies{Auth: service}, templates: templates}, sessions
}

func newLoginPostRequest(form url.Values, loginCSRFCookie string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/app/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if loginCSRFCookie != "" {
		request.AddCookie(&http.Cookie{Name: LoginCSRFCookieName, Value: loginCSRFCookie})
	}
	return request
}

func TestLoginCSRFRequiresMatchingPreAuthCookie(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequest(http.MethodPost, "/app/login", nil)
	request.AddCookie(&http.Cookie{
		Name:  LoginCSRFCookieName,
		Value: "login-token",
	})

	require.True(t, handler.validateLoginCSRF(request, "login-token"))
	require.False(t, handler.validateLoginCSRF(request, "wrong-token"))
	require.False(t, handler.validateLoginCSRF(request, ""))
}

func TestSetLoginCSRFCookieUsesSecurityAttributes(t *testing.T) {
	handler := &Handler{deps: Dependencies{
		CookieOptions: middleware.CookieOptions{Secure: true},
	}}
	response := httptest.NewRecorder()

	handler.setLoginCSRFCookie(response, "login-token")

	cookies := response.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, LoginCSRFCookieName, cookies[0].Name)
	require.Equal(t, "login-token", cookies[0].Value)
	require.True(t, cookies[0].HttpOnly)
	require.True(t, cookies[0].Secure)
	require.Contains(t, strings.ToLower(response.Header().Get("Set-Cookie")), "samesite=lax")
}

func TestLoginPostRejectsMissingOrMismatchedCSRF(t *testing.T) {
	tests := []struct {
		name      string
		cookie    string
		formToken string
	}{
		{name: "no cookie and no form token"},
		{name: "cookie without form token", cookie: "login-token"},
		{name: "form token without cookie", formToken: "login-token"},
		{name: "form token does not match cookie", cookie: "login-token", formToken: "attacker-token"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, sessions := newLoginTestHandler(t)
			form := url.Values{"username": {"alice"}, "password": {"secret"}}
			if test.formToken != "" {
				form.Set("csrf_token", test.formToken)
			}
			response := httptest.NewRecorder()

			handler.loginPost(response, newLoginPostRequest(form, test.cookie))

			require.Equal(t, http.StatusForbidden, response.Code)
			require.Contains(t, response.Body.String(), "Invalid CSRF token")
			require.Zero(t, sessions.created, "login must not create a session")
			require.Nil(t, findTestCookie(response.Result().Cookies(), middleware.SessionCookieName))
		})
	}
}

func TestLoginGetThenPostWithMatchingCSRFSignsIn(t *testing.T) {
	handler, sessions := newLoginTestHandler(t)

	getResponse := httptest.NewRecorder()
	handler.loginGet(getResponse, httptest.NewRequest(http.MethodGet, "/app/login", nil))

	require.Equal(t, http.StatusOK, getResponse.Code)
	loginCookie := findTestCookie(getResponse.Result().Cookies(), LoginCSRFCookieName)
	require.NotNil(t, loginCookie)
	require.NotEmpty(t, loginCookie.Value)
	require.Contains(t, getResponse.Body.String(), `name="csrf_token" value="`+loginCookie.Value+`"`)

	form := url.Values{
		"username":   {"alice"},
		"password":   {"secret"},
		"csrf_token": {loginCookie.Value},
	}
	postResponse := httptest.NewRecorder()

	handler.loginPost(postResponse, newLoginPostRequest(form, loginCookie.Value))

	require.Equal(t, http.StatusSeeOther, postResponse.Code)
	require.Equal(t, BasePath+"/databases", postResponse.Header().Get("Location"))
	require.Equal(t, 1, sessions.created)
	cookies := postResponse.Result().Cookies()
	require.NotNil(t, findTestCookie(cookies, middleware.SessionCookieName))
	require.NotNil(t, findTestCookie(cookies, CSRFCookieName))
	clearedLoginCookie := findTestCookie(cookies, LoginCSRFCookieName)
	require.NotNil(t, clearedLoginCookie)
	require.Equal(t, -1, clearedLoginCookie.MaxAge)
}

func TestLoginPostChecksCSRFBeforeCredentials(t *testing.T) {
	handler, _ := newLoginTestHandler(t)
	form := url.Values{"username": {"alice"}, "password": {"wrong"}, "csrf_token": {"attacker-token"}}
	response := httptest.NewRecorder()

	handler.loginPost(response, newLoginPostRequest(form, "login-token"))

	require.Equal(t, http.StatusForbidden, response.Code)
	require.NotContains(t, response.Body.String(), "Invalid username or password")
}

func findTestCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
