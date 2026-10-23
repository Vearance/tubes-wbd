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
	httpapi "tubes-wbd/backend/http"
	"tubes-wbd/backend/http/middleware"
	"tubes-wbd/backend/manageddb"
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

	cookieOptions := middleware.CookieOptions{Secure: cfg.CookieSecure}
	api := apiServer{
		authService:   authService,
		cookieOptions: cookieOptions,
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
