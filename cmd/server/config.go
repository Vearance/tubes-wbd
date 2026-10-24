package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"tubes-wbd/backend/auth"
	"tubes-wbd/backend/database/platformdb"
	"tubes-wbd/backend/manageddb"
)

const defaultShutdownTimeout = 10 * time.Second

type serverConfig struct {
	Port            string
	PlatformDB      platformdb.Config
	Auth            auth.ServiceConfig
	AdminUsername   string
	AdminPassword   string
	CookieSecure    bool
	ShutdownTimeout time.Duration
	TargetDBKey     []byte
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

	adminUsername := os.Getenv("APP_ADMIN_USERNAME")
	if adminUsername == "" {
		return serverConfig{}, fmt.Errorf("APP_ADMIN_USERNAME environment variable is required")
	}

	adminPassword := os.Getenv("APP_ADMIN_PASSWORD")
	if adminPassword == "" {
		return serverConfig{}, fmt.Errorf("APP_ADMIN_PASSWORD environment variable is required")
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

	idleTimeout, err := durationEnv("APP_IDLE_TIMEOUT")
	if err != nil {
		return serverConfig{}, err
	}

	absoluteLifetime, err := durationEnv("APP_ABSOLUTE_LIFETIME")
	if err != nil {
		return serverConfig{}, err
	}

	rotationInterval, err := durationEnv("APP_ROTATION_INTERVAL")
	if err != nil {
		return serverConfig{}, err
	}

	rotationGracePeriod, err := durationEnv("APP_ROTATION_GRACE_PERIOD")
	if err != nil {
		return serverConfig{}, err
	}

	cookieSecure, err := boolEnv("APP_COOKIE_SECURE")
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

	encodedKey := os.Getenv("TARGET_DB_ENCRYPTION_KEY")
	if strings.TrimSpace(encodedKey) == "" {
		return serverConfig{}, fmt.Errorf("TARGET_DB_ENCRYPTION_KEY environment variable is required")
	}
	targetDBKey, err := manageddb.ParseKey(encodedKey)
	if err != nil {
		return serverConfig{}, fmt.Errorf("TARGET_DB_ENCRYPTION_KEY: %w", err)
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
		Auth: auth.ServiceConfig{
			IdleTimeout:         idleTimeout,
			AbsoluteLifetime:    absoluteLifetime,
			RotationInterval:    rotationInterval,
			RotationGracePeriod: rotationGracePeriod,
		},
		AdminUsername:   adminUsername,
		AdminPassword:   adminPassword,
		CookieSecure:    cookieSecure,
		ShutdownTimeout: shutdownTimeout,
		TargetDBKey:     targetDBKey,
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

func boolEnv(name string) (bool, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return false, nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", name, err)
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
