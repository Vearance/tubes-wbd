package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"

	"tubes-wbd/internal/database/platformdb"
	"tubes-wbd/internal/manageddb"
	"tubes-wbd/internal/telemetry"
)

const defaultExecutionQueue = "migration.executions"

type config struct {
	platformDB  platformdb.Config
	rabbitURL   string
	queue       string
	targetKey   []byte
	telemetry   telemetry.Config
	metricsAddr string
}

func loadConfig() (config, error) {
	if err := godotenv.Load(); err != nil {
		if !os.IsNotExist(err) {
			return config{}, fmt.Errorf("loading .env: %w", err)
		}
		if err := godotenv.Load("../.env"); err != nil && !os.IsNotExist(err) {
			return config{}, fmt.Errorf("loading ../.env: %w", err)
		}
	}

	dsn := strings.TrimSpace(os.Getenv("APP_DATABASE_URL"))
	if dsn == "" {
		return config{}, errors.New("APP_DATABASE_URL environment variable is required")
	}
	targetDBEncodedKey := strings.TrimSpace(os.Getenv("TARGET_DB_ENCRYPTION_KEY"))
	if targetDBEncodedKey == "" {
		return config{}, errors.New("TARGET_DB_ENCRYPTION_KEY environment variable is required")
	}
	targetKey, err := manageddb.ParseKey(targetDBEncodedKey)
	if err != nil {
		return config{}, fmt.Errorf("TARGET_DB_ENCRYPTION_KEY: %w", err)
	}

	rabbitURL := strings.TrimSpace(os.Getenv("APP_RABBITMQ_URL"))
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@localhost:5672/"
	}
	queue := strings.TrimSpace(os.Getenv("APP_RABBITMQ_QUEUE"))
	if queue == "" {
		queue = defaultExecutionQueue
	}

	telemetryCfg, err := telemetry.LoadConfig("bscheme-worker")
	if err != nil {
		return config{}, fmt.Errorf("loading telemetry config: %w", err)
	}

	maxOpenConns, err := optionalNonNegativeIntEnv("APP_MAX_OPEN_CONNS")
	if err != nil {
		return config{}, err
	}
	maxIdleConns, err := optionalNonNegativeIntEnv("APP_MAX_IDLE_CONNS")
	if err != nil {
		return config{}, err
	}

	return config{
		platformDB: platformdb.Config{
			DSN:          dsn,
			MaxOpenConns: maxOpenConns,
			MaxIdleConns: maxIdleConns,
		},
		rabbitURL:   rabbitURL,
		queue:       queue,
		targetKey:   targetKey,
		telemetry:   telemetryCfg,
		metricsAddr: telemetryCfg.MetricsAddr,
	}, nil
}

func optionalNonNegativeIntEnv(name string) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
}
