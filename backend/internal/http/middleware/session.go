package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"tubes-wbd/internal/auth"
	"tubes-wbd/internal/telemetry"
)

const SessionCookieName = "labpro_sucks"

type CookieOptions struct {
	Secure   bool
	SameSite http.SameSite
	Path     string
}

func (o CookieOptions) withDefaults() CookieOptions {
	if o.SameSite == 0 || o.SameSite == http.SameSiteDefaultMode {
		o.SameSite = http.SameSiteLaxMode
	}
	if o.Path == "" {
		o.Path = "/"
	}

	return o
}

func SetSessionCookie(
	w http.ResponseWriter,
	token string,
	expiresAt time.Time,
	options CookieOptions,
) {
	options = options.withDefaults()
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     options.Path,
		Expires:  expiresAt.UTC(),
		HttpOnly: true,
		Secure:   options.Secure,
		SameSite: options.SameSite,
	})
}

func ClearSessionCookie(w http.ResponseWriter, options CookieOptions) {
	options = options.withDefaults()
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     options.Path,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   options.Secure,
		SameSite: options.SameSite,
	})
}

func Session(
	service *auth.AuthService,
	cookieOptions CookieOptions,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if service == nil {
				writeError(w, http.StatusInternalServerError, "authentication service unavailable")
				return
			}

			cookie, err := r.Cookie(SessionCookieName)
			if err != nil {
				slog.WarnContext(r.Context(), "session rejected",
					"event", "auth.session.rejected",
					"database", "platform",
					"status", "rejected",
					"reason", "missing_cookie",
				)
				writeErrorContext(r.Context(), w, http.StatusUnauthorized, "authentication required")
				return
			}

			validation, err := service.ValidateSession(r.Context(), cookie.Value)
			if err != nil {
				writeAuthError(r.Context(), w, err)
				return
			}

			actor := telemetry.ActorUser(validation.AuthenticatedSession.UserID)
			telemetry.SetRequestActor(r.Context(), actor)
			ctx := telemetry.WithActor(
				withAuthenticatedSession(r.Context(), validation.AuthenticatedSession),
				actor,
			)

			if validation.Rotation != nil && validation.Rotation.Token != "" {
				SetSessionCookie(
					w,
					validation.Rotation.Token,
					validation.Rotation.ExpiresAt,
					cookieOptions,
				)
				slog.InfoContext(ctx, "session rotated",
					"event", "auth.session.rotated",
					"database", "platform",
					"status", "ok",
				)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeAuthError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthenticated),
		errors.Is(err, auth.ErrSessionExpired),
		errors.Is(err, auth.ErrSessionRevoked),
		errors.Is(err, auth.ErrStaleSessionToken),
		errors.Is(err, auth.ErrUserDisabled):
		slog.WarnContext(ctx, "session rejected",
			"event", "auth.session.rejected",
			"database", "platform",
			"status", "rejected",
			"reason", sessionRejectReason(err),
		)
		writeErrorContext(ctx, w, http.StatusUnauthorized, "authentication required")
	default:
		slog.ErrorContext(ctx, "session validation failed",
			"event", "auth.session.error",
			"database", "platform",
			"status", "error",
			"error", err,
		)
		writeErrorContext(ctx, w, http.StatusInternalServerError, "internal server error")
	}
}

func sessionRejectReason(err error) string {
	switch {
	case errors.Is(err, auth.ErrSessionExpired):
		return "expired"
	case errors.Is(err, auth.ErrSessionRevoked):
		return "revoked"
	case errors.Is(err, auth.ErrStaleSessionToken):
		return "stale"
	case errors.Is(err, auth.ErrUserDisabled):
		return "user_disabled"
	default:
		return "unauthenticated"
	}
}
