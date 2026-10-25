//go:build integration

package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"gorm.io/gorm"

	"tubes-wbd/backend/auth"
	"tubes-wbd/backend/database/platformdb"
)

func openPlatformDB(t *testing.T) *gorm.DB {
	t.Helper()
	ctx := context.Background()

	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("platform"),
		tcpostgres.WithUsername("platform"),
		tcpostgres.WithPassword("platform_password"),
		tcpostgres.BasicWaitStrategies(),
	)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	require.NoError(t, err)

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := platformdb.Open(ctx, platformdb.Config{DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { _ = platformdb.Close(db) })

	require.NoError(t, auth.AutoMigrate(db))
	return db
}

func TestIntegrationRevokeAllByUserRevokesOnlyActiveSessionsOfThatUser(t *testing.T) {
	db := openPlatformDB(t)
	ctx := context.Background()
	users := auth.NewGormUserRepository(db)
	sessions := auth.NewGormSessionRepository(db)

	alice := &auth.User{Username: "alice", PasswordHash: "x"}
	bob := &auth.User{Username: "bob", PasswordHash: "x"}
	require.NoError(t, users.Create(ctx, alice))
	require.NoError(t, users.Create(ctx, bob))

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	loggedOutAt := now.Add(-time.Hour)
	newSession := func(userID uint, tokenHash string) *auth.Session {
		t.Helper()
		session := &auth.Session{
			UserID:         userID,
			TokenHash:      tokenHash,
			CreatedAt:      now.Add(-2 * time.Hour),
			LastActivityAt: now.Add(-2 * time.Hour),
			LastRotatedAt:  now.Add(-2 * time.Hour),
			ExpiresAt:      now.Add(6 * time.Hour),
			CSRFTokenHash:  "csrf-" + tokenHash,
		}
		require.NoError(t, sessions.Create(ctx, session))
		return session
	}
	aliceLaptop := newSession(alice.ID, "alice-laptop")
	alicePhone := newSession(alice.ID, "alice-phone")
	aliceOld := newSession(alice.ID, "alice-old")
	bobLaptop := newSession(bob.ID, "bob-laptop")
	require.NoError(t, sessions.Revoke(ctx, aliceOld.ID, loggedOutAt))

	require.NoError(t, sessions.RevokeAllByUser(ctx, alice.ID, now))

	revokedAt := func(sessionID uint) *time.Time {
		t.Helper()
		session, err := sessions.FindByID(ctx, sessionID)
		require.NoError(t, err)
		return session.RevokedAt
	}
	require.NotNil(t, revokedAt(aliceLaptop.ID))
	require.True(t, revokedAt(aliceLaptop.ID).Equal(now))
	require.NotNil(t, revokedAt(alicePhone.ID))
	require.True(t, revokedAt(alicePhone.ID).Equal(now))
	require.True(t, revokedAt(aliceOld.ID).Equal(loggedOutAt), "earlier logout time must be kept")
	require.Nil(t, revokedAt(bobLaptop.ID), "other users' sessions must stay active")
}
