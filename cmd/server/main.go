package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"tubes-wbd/backend/access"
	"tubes-wbd/backend/auth"
	"tubes-wbd/backend/database/platformdb"
	"tubes-wbd/backend/database/targetdb"
	httpapi "tubes-wbd/backend/http"
	"tubes-wbd/backend/http/middleware"
	"tubes-wbd/backend/manageddb"
	"tubes-wbd/backend/migration"
	"tubes-wbd/backend/viewer"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
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

	db, err := platformdb.Open(ctx, cfg.PlatformDB)
	if err != nil {
		return fmt.Errorf("opening platform database: %w", err)
	}
	defer func() {
		if err := platformdb.Close(db); err != nil {
			log.Printf("closing platform database: %v", err)
		}
	}()

	// migrate schema
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

	userRepository := auth.NewGormUserRepository(db)
	sessionRepository := auth.NewGormSessionRepository(db)
	passwordHasher := auth.NewBcryptHasher(0)
	if err := auth.BootstrapAdmin(
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

	userManagementService, err := auth.NewUserManagementService(userRepository, passwordHasher)
	if err != nil {
		return fmt.Errorf("creating user management service: %w", err)
	}
	groupService, err := access.NewGroupService(userRepository, accessRepository)
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
	targetManager := targetdb.NewManager(configSource, targetdb.Options{})
	// Deferred after the platform DB close, so it runs first; and run returns
	// only after server.Shutdown, so no request still holds a connection.
	defer targetManager.Close()

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
	})
	// placeholder for '/'
	mux.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := io.WriteString(w, "Hello, world!\n"); err != nil {
			log.Printf("writing response: %v", err)
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
		serverErrors <- server.ListenAndServe()
	}()

	log.Printf("server listening on :%s", cfg.Port)

	// Wait for a server error or a shutdown signal.
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serving HTTP: %w", err)
		}
	case <-ctx.Done():
		log.Printf("shutdown signal received")
	}

	// Stop accepting requests and wait for active requests to finish.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down HTTP server: %w", err)
	}

	return nil
}
