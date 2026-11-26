package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigNeedsOnlyWorkerSettings(t *testing.T) {
	t.Setenv("APP_DATABASE_URL", "postgres://user:password@localhost:5432/platform")
	t.Setenv("TARGET_DB_ENCRYPTION_KEY", "ZGV2LW9ubHkta2V5LWRvLW5vdC11c2UtaW4tcHJvZCE=")
	t.Setenv("APP_RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	t.Setenv("APP_RABBITMQ_QUEUE", "migration.test")
	t.Setenv("APP_PORT", "")
	t.Setenv("APP_ADMIN_USERNAME", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("APP_METRICS_ADDR", ":9191")
	t.Setenv("APP_MAX_OPEN_CONNS", "12")
	t.Setenv("APP_MAX_IDLE_CONNS", "4")

	cfg, err := loadConfig()

	require.NoError(t, err)
	require.Equal(t, "postgres://user:password@localhost:5432/platform", cfg.platformDB.DSN)
	require.Equal(t, 12, cfg.platformDB.MaxOpenConns)
	require.Equal(t, 4, cfg.platformDB.MaxIdleConns)
	require.Equal(t, "amqp://guest:guest@localhost:5672/", cfg.rabbitURL)
	require.Equal(t, "migration.test", cfg.queue)
	require.NotEmpty(t, cfg.targetKey)
	require.Equal(t, "bscheme-worker", cfg.telemetry.ServiceName)
	require.Equal(t, ":9191", cfg.metricsAddr)
}
