package middleware

import (
	"context"
	"errors"
	"log"
	"net/http"

	"tubes-wbd/backend/auth"
)

type AdminAuthorizer interface {
	IsAdmin(ctx context.Context, userID uint) (bool, error)
}

func RequireAdmin(authorizer AdminAuthorizer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session, ok := AuthenticatedSessionFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if authorizer == nil {
				writeError(w, http.StatusInternalServerError, "authorization service unavailable")
				return
			}

			isAdmin, err := authorizer.IsAdmin(r.Context(), session.UserID)
			if err != nil {
				if errors.Is(err, auth.ErrUserNotFound) {
					writeError(w, http.StatusUnauthorized, "authentication required")
					return
				}
				log.Printf("admin middleware: %v", err)
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			if !isAdmin {
				writeError(w, http.StatusForbidden, "admin access required")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
