package middleware

import (
	"errors"
	"log"
	"net/http"
	"time"

	"tubes-wbd/backend/auth"
)

const SessionCookieName = "labpro_sucks_lol"

type CookieOptions struct {
	Secure   bool
	SameSite http.SameSite
	Path     string
}

func (o CookieOptions) withDefaults() CookieOptions {
	if o.SameSite == http.SameSiteDefaultMode {
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
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}

			validation, err := service.ValidateSession(r.Context(), cookie.Value)
			if err != nil {
				writeAuthError(w, err)
				return
			}

			if validation.Rotation != nil && validation.Rotation.Token != "" {
				SetSessionCookie(
					w,
					validation.Rotation.Token,
					validation.Rotation.ExpiresAt,
					cookieOptions,
				)
			}

			ctx := withAuthenticatedSession(
				r.Context(),
				validation.AuthenticatedSession,
			)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthenticated),
		errors.Is(err, auth.ErrSessionExpired),
		errors.Is(err, auth.ErrSessionRevoked),
		errors.Is(err, auth.ErrStaleSessionToken),
		errors.Is(err, auth.ErrUserDisabled):
		writeError(w, http.StatusUnauthorized, "authentication required")
	default:
		log.Printf("session middleware: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
