package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadServerConfig(t *testing.T) {
	t.Setenv("TARGET_DB_ENCRYPTION_KEY", "ZGV2LW9ubHkta2V5LWRvLW5vdC11c2UtaW4tcHJvZCE=")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("APP_DATABASE_URL", "postgres://user:password@localhost:5432/platform")
	t.Setenv("APP_ADMIN_USERNAME", "admin")
	t.Setenv("APP_ADMIN_PASSWORD", "secret")
	t.Setenv("APP_COOKIE_SECURE", "true")
	t.Setenv("APP_MAX_OPEN_CONNS", "20")
	t.Setenv("APP_MAX_IDLE_CONNS", "7")
	t.Setenv("APP_CONN_MAX_LIFETIME", "45m")
	t.Setenv("APP_CONN_MAX_IDLE_TIME", "12m")
	t.Setenv("APP_PING_TIMEOUT", "8s")
	t.Setenv("APP_IDLE_TIMEOUT", "30m")
	t.Setenv("APP_ABSOLUTE_LIFETIME", "8h")
	t.Setenv("APP_ROTATION_INTERVAL", "15m")
	t.Setenv("APP_ROTATION_GRACE_PERIOD", "30s")
	t.Setenv("APP_VIEWER_DEFAULT_PAGE_SIZE", "25")
	t.Setenv("APP_VIEWER_MAX_PAGE_SIZE", "75")
	t.Setenv("APP_SHUTDOWN_TIMEOUT", "15s")

	cfg, err := loadServerConfig()
	require.NoError(t, err)
	require.Equal(t, "9090", cfg.Port)
	require.Equal(t, "postgres://user:password@localhost:5432/platform", cfg.PlatformDB.DSN)
	require.Equal(t, "admin", cfg.AdminUsername)
	require.Equal(t, "secret", cfg.AdminPassword)
	require.True(t, cfg.CookieSecure)
	require.Equal(t, 20, cfg.PlatformDB.MaxOpenConns)
	require.Equal(t, 7, cfg.PlatformDB.MaxIdleConns)
	require.Equal(t, 45*time.Minute, cfg.PlatformDB.ConnMaxLifetime)
	require.Equal(t, 12*time.Minute, cfg.PlatformDB.ConnMaxIdleTime)
	require.Equal(t, 8*time.Second, cfg.PlatformDB.PingTimeout)
	require.Equal(t, 30*time.Minute, cfg.Auth.IdleTimeout)
	require.Equal(t, 8*time.Hour, cfg.Auth.AbsoluteLifetime)
	require.Equal(t, 15*time.Minute, cfg.Auth.RotationInterval)
	require.Equal(t, 30*time.Second, cfg.Auth.RotationGracePeriod)
	require.Equal(t, 25, cfg.Viewer.DefaultPageSize)
	require.Equal(t, 75, cfg.Viewer.MaxPageSize)
	require.Equal(t, 15*time.Second, cfg.ShutdownTimeout)
}

func TestLoadServerConfigRejectsInvalidSessionDuration(t *testing.T) {
	t.Setenv("TARGET_DB_ENCRYPTION_KEY", "ZGV2LW9ubHkta2V5LWRvLW5vdC11c2UtaW4tcHJvZCE=")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("APP_DATABASE_URL", "postgres://user:password@localhost:5432/platform")
	t.Setenv("APP_ADMIN_USERNAME", "admin")
	t.Setenv("APP_ADMIN_PASSWORD", "secret")
	t.Setenv("APP_IDLE_TIMEOUT", "soon")

	_, err := loadServerConfig()
	require.ErrorContains(t, err, "APP_IDLE_TIMEOUT")
}

func TestLoadServerConfigRejectsInvalidViewerPageSize(t *testing.T) {
	t.Setenv("TARGET_DB_ENCRYPTION_KEY", "ZGV2LW9ubHkta2V5LWRvLW5vdC11c2UtaW4tcHJvZCE=")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("APP_DATABASE_URL", "postgres://user:password@localhost:5432/platform")
	t.Setenv("APP_ADMIN_USERNAME", "admin")
	t.Setenv("APP_ADMIN_PASSWORD", "secret")
	t.Setenv("APP_VIEWER_DEFAULT_PAGE_SIZE", "0")

	_, err := loadServerConfig()
	require.ErrorContains(t, err, "APP_VIEWER_DEFAULT_PAGE_SIZE")
}

func TestLoadServerConfigRejectsInvalidConnectionLimit(t *testing.T) {
	t.Setenv("TARGET_DB_ENCRYPTION_KEY", "ZGV2LW9ubHkta2V5LWRvLW5vdC11c2UtaW4tcHJvZCE=")
	t.Setenv("APP_PORT", "9090")
	t.Setenv("APP_DATABASE_URL", "postgres://user:password@localhost:5432/platform")
	t.Setenv("APP_ADMIN_USERNAME", "admin")
	t.Setenv("APP_ADMIN_PASSWORD", "secret")
	t.Setenv("APP_MAX_OPEN_CONNS", "many")

	_, err := loadServerConfig()
	require.ErrorContains(t, err, "APP_MAX_OPEN_CONNS")
}
