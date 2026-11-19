package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"tubes-wbd/internal/database/platformdb"
	"tubes-wbd/internal/manageddb"
)

const defaultExecutionQueue = "migration.executions"

type config struct {
	platformDB platformdb.Config
	rabbitURL  string
	queue      string
	targetKey  []byte
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

	return config{
		platformDB: platformdb.Config{DSN: dsn},
		rabbitURL:  rabbitURL,
		queue:      queue,
		targetKey:  targetKey,
	}, nil
}
