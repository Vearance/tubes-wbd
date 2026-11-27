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
	"sync"
	"syscall"
	"time"

	"tubes-wbd/internal/access"
	"tubes-wbd/internal/auth"
	"tubes-wbd/internal/database/platformdb"
	"tubes-wbd/internal/database/targetdb"
	httpapi "tubes-wbd/internal/http"
	"tubes-wbd/internal/http/middleware"
	"tubes-wbd/internal/manageddb"
	"tubes-wbd/internal/migration"
	"tubes-wbd/internal/telemetry"
	"tubes-wbd/internal/users"
	"tubes-wbd/internal/viewer"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadServerConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	tel, err := telemetry.Init(ctx, cfg.Telemetry)
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

	db, err := platformdb.Open(ctx, cfg.PlatformDB)
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

	// migrate schema
	if err := users.AutoMigrate(db); err != nil {
		return fmt.Errorf("migrating user schema: %w", err)
	}
	if err := auth.AutoMigrate(db); err != nil {
		return fmt.Errorf("migrating authentication schema: %w", err)
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
	sessionRepository := auth.NewGormSessionRepository(db)
	passwordHasher := auth.NewBcryptHasher(0)
	if err := users.BootstrapAdmin(
		ctx,
		userRepository,
		passwordHasher,
		cfg.AdminUsername,
		cfg.AdminPassword,
	); err != nil {
		return fmt.Errorf("bootstrapping platform admin: %w", err)
	}

	// authN service
	authService, err := auth.NewAuthService(
		userRepository,
		sessionRepository,
		passwordHasher,
		cfg.Auth,
	)
	if err != nil {
		return fmt.Errorf("creating authentication service: %w", err)
	}

	// authZ service
	accessRepository := access.NewGormRepository(db)
	authorizationService, err := access.NewAuthorizationService(
		userRepository,
		accessRepository,
	)
	if err != nil {
		return fmt.Errorf("creating authorization service: %w", err)
	}

	userManagementService, err := users.NewService(userRepository, sessionRepository, passwordHasher)
	if err != nil {
		return fmt.Errorf("creating user management service: %w", err)
	}
	groupService, err := access.NewGroupService(userRepository, accessRepository, sessionRepository)
	if err != nil {
		return fmt.Errorf("creating group service: %w", err)
	}

	// Target databases: connection configs live in the Platform DB with
	// encrypted passwords; the manager opens one pool per database on demand.
	cipher, err := manageddb.NewCipher(cfg.TargetDBKey)
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
	// Deferred after the platform DB close, so it runs first; and run returns
	// only after server.Shutdown, so no request still holds a connection.
	defer targetManager.Close()

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("getting platform sql.DB for pool metrics: %w", err)
	}
	if err := tel.RegisterDBPoolStats(sqlDB, func() telemetry.TargetPoolStats {
		s := targetManager.Stats()
		return telemetry.TargetPoolStats{
			ActiveConns:  s.ActiveConns,
			IdleConns:    s.IdleConns,
			WaitCount:    s.WaitCount,
			WaitDuration: s.WaitDuration,
		}
	}); err != nil {
		return fmt.Errorf("registering database pool metrics: %w", err)
	}

	const statusCacheTTL = 5 * time.Second
	var (
		statusMu     sync.Mutex
		cachedAt     time.Time
		cachedCounts map[string]int64
	)
	if err := tel.RegisterMigrationStatusCollector(func(collectCtx context.Context) (map[string]int64, error) {
		statusMu.Lock()
		defer statusMu.Unlock()

		if cachedCounts != nil && time.Since(cachedAt) < statusCacheTTL {
			copyCounts := make(map[string]int64, len(cachedCounts))
			for k, v := range cachedCounts {
				copyCounts[k] = v
			}
			return copyCounts, nil
		}

		queryCtx, cancel := context.WithTimeout(telemetry.WithSkipDBTelemetry(collectCtx), 2*time.Second)
		defer cancel()

		type statusRow struct {
			Status string
			Count  int64
		}
		var rows []statusRow
		if err := db.WithContext(queryCtx).
			Table("migration_executions").
			Select("status, COUNT(*) AS count").
			Group("status").
			Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("querying migration execution status counts: %w", err)
		}
		counts := make(map[string]int64, len(rows))
		for _, row := range rows {
			counts[row.Status] = row.Count
		}

		cachedCounts = counts
		cachedAt = time.Now()
		copyCounts := make(map[string]int64, len(counts))
		for k, v := range counts {
			copyCounts[k] = v
		}
		return copyCounts, nil
	}); err != nil {
		return fmt.Errorf("registering migration status metrics: %w", err)
	}

	managedDatabaseService, err := manageddb.NewManagedDBService(managedRepository, cipher, targetManager)
	if err != nil {
		return fmt.Errorf("creating managed database service: %w", err)
	}
	viewerService, err := viewer.NewViewerService(authorizationService, targetManager, cfg.Viewer)
	if err != nil {
		return fmt.Errorf("creating viewer service: %w", err)
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

	// Register HTTP routes and inject authentication dependencies.
	cookieOptions := middleware.CookieOptions{Secure: cfg.CookieSecure}
	api := apiServer{
		authService:      authService,
		userManagement:   userManagementService,
		groupService:     groupService,
		authorization:    authorizationService,
		managedDatabases: managedDatabaseService,
		viewer:           viewerService,
		migrations:       migrationService,
		cookieOptions:    cookieOptions,
	}
	mux := httpapi.NewRouter(api, httpapi.RouterConfig{
		Auth:            authService,
		AdminAuthorizer: authorizationService,
		CookieOptions:   cookieOptions,
		Middlewares: []func(http.Handler) http.Handler{
			telemetry.HTTPMiddleware(cfg.Telemetry.ServiceName),
		},
	})
	mux.Handle("/metrics", tel.MetricsHandler())
	// The React frontend runs separately and calls these JSON API routes.

	// Placeholder for '/'.
	mux.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := io.WriteString(w, "Hello, world!\n"); err != nil {
			slog.ErrorContext(r.Context(), "writing root response", "error", err)
		}
	})

	// Configure the HTTP server.
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: mux,
	}

	// Start serving in the background so signals can be handled.
	serverErrors := make(chan error, 1)
	go func() {
		if err := migration.RunOutboxRelay(ctx, db, cfg.RabbitMQURL, cfg.RabbitMQQueue); err != nil && !errors.Is(err, context.Canceled) {
			serverErrors <- fmt.Errorf("running migration outbox relay: %w", err)
		}
	}()
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	slog.InfoContext(ctx, "server listening", "port", cfg.Port, "service", cfg.Telemetry.ServiceName)

	// Wait for a server error or a shutdown signal.
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serving HTTP: %w", err)
		}
	case <-ctx.Done():
		slog.InfoContext(ctx, "shutdown signal received")
	}

	// Stop accepting requests and wait for active requests to finish.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down HTTP server: %w", err)
	}

	return nil
}
