package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"tubes-wbd/internal/auth"
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
					writeErrorContext(r.Context(), w, http.StatusUnauthorized, "authentication required")
					return
				}
				slog.ErrorContext(r.Context(), "admin authorization check failed",
					"event", "authz.admin.error",
					"database", "platform",
					"status", "error",
					"error", err,
				)
				writeErrorContext(r.Context(), w, http.StatusInternalServerError, "internal server error")
				return
			}
			if !isAdmin {
				slog.WarnContext(r.Context(), "admin authorization denied",
					"event", "authz.admin.denied",
					"database", "platform",
					"status", "rejected",
				)
				writeErrorContext(r.Context(), w, http.StatusForbidden, "admin access required")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
