package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	httpapi "tubes-wbd/backend/http"

	"github.com/go-chi/chi/v5"
)

type apiServer struct{}

func (apiServer) GetHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(httpapi.HealthResponse{Status: "ok"}); err != nil {
		log.Printf("writing health response: %v", err)
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := chi.NewRouter()
	mux.Get("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := io.WriteString(w, "Hello, world!\n"); err != nil {
			log.Printf("writing response: %v", err)
		}
	})
	httpapi.HandlerFromMux(apiServer{}, mux)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	log.Printf("server listening on :%s", port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
