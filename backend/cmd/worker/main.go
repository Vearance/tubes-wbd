package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"tubes-wbd/internal/access"
	"tubes-wbd/internal/database/platformdb"
	"tubes-wbd/internal/database/targetdb"
	"tubes-wbd/internal/manageddb"
	"tubes-wbd/internal/migration"
	"tubes-wbd/internal/users"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := platformdb.Open(ctx, cfg.platformDB)
	if err != nil {
		return fmt.Errorf("opening platform database: %w", err)
	}
	defer func() {
		if err := platformdb.Close(db); err != nil {
			slog.Error("closing platform database", "error", err)
		}
	}()

	if err := users.AutoMigrate(db); err != nil {
		return fmt.Errorf("migrating user schema: %w", err)
	}
	if err := manageddb.AutoMigrate(db); err != nil {
		return fmt.Errorf("migrating managed database schema: %w", err)
	}
	if err := access.AutoMigrate(db); err != nil {
		return fmt.Errorf("migrating access-control schema: %w", err)
	}
	if err := migration.AutoMigrate(db); err != nil {
		return fmt.Errorf("migrating migration schema: %w", err)
	}

	userRepository := users.NewGormRepository(db)
	authorizationService, err := access.NewAuthorizationService(
		userRepository,
		access.NewGormRepository(db),
	)
	if err != nil {
		return fmt.Errorf("creating authorization service: %w", err)
	}

	cipher, err := manageddb.NewCipher(cfg.targetKey)
	if err != nil {
		return fmt.Errorf("creating target database cipher: %w", err)
	}
	managedRepository := manageddb.NewGormRepository(db)
	configSource, err := manageddb.NewSource(managedRepository, cipher)
	if err != nil {
		return fmt.Errorf("creating target database config source: %w", err)
	}
	targetManager := targetdb.NewManager(configSource, targetdb.Options{})
	defer targetManager.Close()

	migrationService, err := migration.NewMigrationService(
		authorizationService,
		targetManager,
		migration.NewGormRepository(db),
		migration.Options{},
	)
	if err != nil {
		return fmt.Errorf("creating migration service: %w", err)
	}
	worker, err := migration.NewExecutionWorker(migrationService, migration.NewGormRepository(db))
	if err != nil {
		return fmt.Errorf("creating migration worker: %w", err)
	}

	slog.Info("migration worker consuming RabbitMQ queue", "queue", cfg.queue)
	err = worker.ConsumeWithReconnect(ctx, cfg.rabbitURL, cfg.queue)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("consuming migration executions: %w", err)
	}
	return nil
}
