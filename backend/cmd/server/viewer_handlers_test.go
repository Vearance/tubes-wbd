package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	httpapi "tubes-wbd/internal/http"
	"tubes-wbd/internal/http/middleware"
)

func TestExecuteDatabaseQueryRouteRegisteredAndProtected(t *testing.T) {
	service, _ := newHandlerAuthService(t)
	api := apiServer{authService: service}
	router := httpapi.NewRouter(api, httpapi.RouterConfig{Auth: service})

	t.Run("rejects unauthenticated request", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/databases/1/query", strings.NewReader(`{"sql":"SELECT 1"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", "dummy")
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusUnauthorized, response.Code)
	})

	t.Run("rejects request without csrf token", func(t *testing.T) {
		loginResult, err := service.Login(context.Background(), "alice", "secret")
		require.NoError(t, err)

		request := httptest.NewRequest(http.MethodPost, "/databases/1/query", strings.NewReader(`{"sql":"SELECT 1"}`))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{
			Name:  middleware.SessionCookieName,
			Value: loginResult.Token,
		})
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusBadRequest, response.Code) // openAPI validator requires X-CSRF-Token header
	})

	t.Run("rejects request with invalid csrf token", func(t *testing.T) {
		loginResult, err := service.Login(context.Background(), "alice", "secret")
		require.NoError(t, err)

		request := httptest.NewRequest(http.MethodPost, "/databases/1/query", strings.NewReader(`{"sql":"SELECT 1"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", "wrong-csrf")
		request.AddCookie(&http.Cookie{
			Name:  middleware.SessionCookieName,
			Value: loginResult.Token,
		})
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusForbidden, response.Code)
	})
}
