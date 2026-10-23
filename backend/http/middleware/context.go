package middleware

import (
	"context"

	"tubes-wbd/backend/auth"
)

type contextKey struct{}

func withAuthenticatedSession(
	ctx context.Context,
	session auth.AuthenticatedSession,
) context.Context {
	return context.WithValue(ctx, contextKey{}, session)
}

func AuthenticatedSessionFromContext(
	ctx context.Context,
) (auth.AuthenticatedSession, bool) {
	session, ok := ctx.Value(contextKey{}).(auth.AuthenticatedSession)
	return session, ok
}
