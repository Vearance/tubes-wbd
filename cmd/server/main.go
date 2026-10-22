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

	"github.com/go-chi/chi/v5"

	"tubes-wbd/backend/auth"
	"tubes-wbd/backend/database/platformdb"
	httpapi "tubes-wbd/backend/http"
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

	mux := chi.NewRouter()
	// placeholder for '/'
	mux.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := io.WriteString(w, "Hello, world!\n"); err != nil {
			log.Printf("writing response: %v", err)
		}
	})
	httpapi.HandlerFromMux(apiServer{}, mux)

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: mux,
	}

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	log.Printf("server listening on :%s", cfg.Port)

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serving HTTP: %w", err)
		}
	case <-ctx.Done():
		log.Printf("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down HTTP server: %w", err)
	}

	return nil
}
