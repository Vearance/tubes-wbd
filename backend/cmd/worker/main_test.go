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

	cfg, err := loadConfig()

	require.NoError(t, err)
	require.Equal(t, "postgres://user:password@localhost:5432/platform", cfg.platformDB.DSN)
	require.Equal(t, "amqp://guest:guest@localhost:5672/", cfg.rabbitURL)
	require.Equal(t, "migration.test", cfg.queue)
	require.NotEmpty(t, cfg.targetKey)
}
