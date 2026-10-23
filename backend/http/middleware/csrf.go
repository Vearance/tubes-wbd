package middleware

import (
	"errors"
	"mime"
	"net/http"

	"tubes-wbd/backend/auth"
)

func CSRF(service *auth.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			if service == nil {
				writeError(w, http.StatusInternalServerError, "authentication service unavailable")
				return
			}

			session, ok := AuthenticatedSessionFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}

			csrfToken, err := csrfTokenFromRequest(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}

			if err := service.ValidateCSRFToken(
				r.Context(),
				session.SessionID,
				csrfToken,
			); err != nil {
				switch {
				case errors.Is(err, auth.ErrInvalidCSRFToken):
					writeError(w, http.StatusForbidden, "invalid csrf token")
				case errors.Is(err, auth.ErrUnauthenticated),
					errors.Is(err, auth.ErrSessionExpired),
					errors.Is(err, auth.ErrSessionRevoked):
					writeError(w, http.StatusUnauthorized, "authentication required")
				default:
					writeError(w, http.StatusInternalServerError, "internal server error")
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func csrfTokenFromRequest(r *http.Request) (string, error) {
	if token := r.Header.Get("X-CSRF-Token"); token != "" {
		return token, nil
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		return "", nil
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", err
	}
	if mediaType != "application/x-www-form-urlencoded" &&
		mediaType != "multipart/form-data" {
		return "", nil
	}
	if err := r.ParseForm(); err != nil {
		return "", err
	}

	return r.FormValue("csrf_token"), nil
}
