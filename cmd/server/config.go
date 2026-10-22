package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"tubes-wbd/backend/database/platformdb"
)

const defaultShutdownTimeout = 10 * time.Second

type serverConfig struct {
	Port            string
	PlatformDB      platformdb.Config
	ShutdownTimeout time.Duration
}

func loadServerConfig() (serverConfig, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return serverConfig{}, fmt.Errorf("loading .env: %w", err)
	}

	port := os.Getenv("APP_PORT")
	if strings.TrimSpace(port) == "" {
		return serverConfig{}, fmt.Errorf("APP_PORT environment variable is required")
	}

	dsn := os.Getenv("APP_DATABASE_URL")
	if strings.TrimSpace(dsn) == "" {
		return serverConfig{}, fmt.Errorf("APP_DATABASE_URL environment variable is required")
	}

	maxOpenConns, err := intEnv("APP_MAX_OPEN_CONNS")
	if err != nil {
		return serverConfig{}, err
	}

	maxIdleConns, err := intEnv("APP_MAX_IDLE_CONNS")
	if err != nil {
		return serverConfig{}, err
	}

	connMaxLifetime, err := durationEnv("APP_CONN_MAX_LIFETIME")
	if err != nil {
		return serverConfig{}, err
	}

	connMaxIdleTime, err := durationEnv("APP_CONN_MAX_IDLE_TIME")
	if err != nil {
		return serverConfig{}, err
	}

	pingTimeout, err := durationEnv("APP_PING_TIMEOUT")
	if err != nil {
		return serverConfig{}, err
	}

	shutdownTimeout, err := durationEnv("APP_SHUTDOWN_TIMEOUT")
	if err != nil {
		return serverConfig{}, err
	}
	if shutdownTimeout == 0 {
		shutdownTimeout = defaultShutdownTimeout
	}

	return serverConfig{
		Port: port,
		PlatformDB: platformdb.Config{
			DSN:             dsn,
			MaxOpenConns:    maxOpenConns,
			MaxIdleConns:    maxIdleConns,
			ConnMaxLifetime: connMaxLifetime,
			ConnMaxIdleTime: connMaxIdleTime,
			PingTimeout:     pingTimeout,
		},
		ShutdownTimeout: shutdownTimeout,
	}, nil
}

func intEnv(name string) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a non-negative integer: %w", name, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}

	return value, nil
}

func durationEnv(name string) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", name, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}

	return value, nil
}
