package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"tubes-wbd/backend/auth"
	httpapi "tubes-wbd/backend/http"
	"tubes-wbd/backend/http/middleware"
)

type apiServer struct {
	authService   *auth.AuthService
	cookieOptions middleware.CookieOptions
}

func (apiServer) GetHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, httpapi.HealthResponse{Status: "ok"})
}

func (apiServer) GetAdminPing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, httpapi.HealthResponse{Status: "ok"})
}

func (s apiServer) PostAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil {
		writeAPIError(w, http.StatusInternalServerError, "authentication service unavailable")
		return
	}

	var request httpapi.LoginRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := ensureEndOfJSON(decoder); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := s.authService.Login(r.Context(), request.Username, request.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials),
			errors.Is(err, auth.ErrUserDisabled):
			writeAPIError(w, http.StatusUnauthorized, "invalid credentials")
		default:
			log.Printf("login failed: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	middleware.SetSessionCookie(
		w,
		result.Token,
		result.ExpiresAt,
		s.cookieOptions,
	)
	writeJSON(w, http.StatusOK, httpapi.LoginResponse{
		CsrfToken: result.CSRFToken,
		ExpiresAt: result.ExpiresAt,
		User: httpapi.AuthUser{
			Id:       int64(result.User.ID),
			Username: result.User.Username,
			IsAdmin:  result.User.IsAdmin,
		},
	})
}

func (s apiServer) PostAuthLogout(
	w http.ResponseWriter,
	r *http.Request,
	_ httpapi.PostAuthLogoutParams,
) {
	if s.authService == nil {
		writeAPIError(w, http.StatusInternalServerError, "authentication service unavailable")
		return
	}

	cookie, err := r.Cookie(middleware.SessionCookieName)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if err := s.authService.Logout(r.Context(), cookie.Value); err != nil {
		log.Printf("logout failed: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	middleware.ClearSessionCookie(w, s.cookieOptions)
	w.WriteHeader(http.StatusNoContent)
}

func ensureEndOfJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("request body contains multiple json values")
		}
		return err
	}

	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("writing json response: %v", err)
	}
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, httpapi.ErrorResponse{Message: message})
}
