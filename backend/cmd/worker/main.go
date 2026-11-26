package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tubes-wbd/internal/access"
	"tubes-wbd/internal/database/platformdb"
	"tubes-wbd/internal/database/targetdb"
	"tubes-wbd/internal/manageddb"
	"tubes-wbd/internal/migration"
	"tubes-wbd/internal/telemetry"
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

	tel, err := telemetry.Init(ctx, cfg.telemetry)
	if err != nil {
		return fmt.Errorf("initializing telemetry: %w", err)
	}
	slog.SetDefault(slog.New(tel.LogHandler()))
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tel.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutting down telemetry", "error", err)
		}
	}()

	db, err := platformdb.Open(ctx, cfg.platformDB)
	if err != nil {
		return fmt.Errorf("opening platform database: %w", err)
	}
	defer func() {
		if err := platformdb.Close(db); err != nil {
			slog.Error("closing platform database", "error", err)
		}
	}()
	if err := telemetry.RegisterGORMPlugin(db); err != nil {
		return fmt.Errorf("registering database telemetry plugin: %w", err)
	}

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
	targetManager := targetdb.NewManager(configSource, targetdb.Options{
		QueryTracer: telemetry.NewPGXTracer,
	})
	defer targetManager.Close()

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("getting platform sql.DB for pool metrics: %w", err)
	}
	if err := tel.RegisterDBPoolStats(sqlDB, func() telemetry.TargetPoolStats {
		s := targetManager.Stats()
		return telemetry.TargetPoolStats{
			ActivePools:  s.ActivePools,
			ActiveConns:  s.ActiveConns,
			IdleConns:    s.IdleConns,
			TotalConns:   s.TotalConns,
			MaxConns:     s.MaxConns,
			WaitCount:    s.WaitCount,
			WaitDuration: s.WaitDuration,
		}
	}); err != nil {
		return fmt.Errorf("registering database pool metrics: %w", err)
	}

	migrationService, err := migration.NewMigrationService(
		authorizationService,
		targetManager,
		migration.NewGormRepository(db),
		migration.Options{},
		configSource,
	)
	if err != nil {
		return fmt.Errorf("creating migration service: %w", err)
	}
	worker, err := migration.NewExecutionWorker(migrationService, migration.NewGormRepository(db))
	if err != nil {
		return fmt.Errorf("creating migration worker: %w", err)
	}

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", tel.MetricsHandler())
	metricsMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})
	metricsServer := &http.Server{
		Addr:    cfg.metricsAddr,
		Handler: metricsMux,
	}
	metricsErrs := make(chan error, 1)
	go func() {
		metricsErrs <- metricsServer.ListenAndServe()
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = metricsServer.Shutdown(shutdownCtx)
	}()

	slog.InfoContext(ctx, "migration worker consuming RabbitMQ queue",
		"queue", cfg.queue,
		"metrics_addr", cfg.metricsAddr,
	)

	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()

	workerErrs := make(chan error, 1)
	go func() {
		workerErrs <- worker.ConsumeWithReconnect(workerCtx, cfg.rabbitURL, cfg.queue)
	}()

	select {
	case err := <-workerErrs:
		if errors.Is(err, context.Canceled) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("consuming migration executions: %w", err)
		}
		return nil
	case err := <-metricsErrs:
		workerCancel()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serving worker metrics: %w", err)
		}
		return nil
	}
}
