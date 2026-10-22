package main

import (
	"encoding/json"
	"log"
	"net/http"

	httpapi "tubes-wbd/backend/http"
)

type apiServer struct{}

func (apiServer) GetHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(httpapi.HealthResponse{Status: "ok"}); err != nil {
		log.Printf("writing health response: %v", err)
	}
}
