package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

type errorResponse struct {
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeErrorContext(context.Background(), w, status, message)
}

func writeErrorContext(ctx context.Context, w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(errorResponse{Message: message}); err != nil {
		slog.ErrorContext(ctx, "writing middleware error response",
			"event", "http.response.write_error",
			"status", "error",
			"error", err,
		)
	}
}
