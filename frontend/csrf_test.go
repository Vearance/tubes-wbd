package frontend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"tubes-wbd/backend/http/middleware"
)

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
