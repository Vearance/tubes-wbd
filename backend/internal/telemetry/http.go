package telemetry

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
		r.ResponseWriter.WriteHeader(code)
	}
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// HTTPMiddleware creates a server span per request, binds a correlation ID and
// RequestMeta (actor + database), records RED metrics using bounded chi route patterns,
// and emits trace-correlated structured JSON access logs.
func HTTPMiddleware(serviceName string) func(http.Handler) http.Handler {
	tracer := Tracer("tubes-wbd/http")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			parentCtx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			spanCtx, span := tracer.Start(parentCtx, r.Method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.request.method", r.Method),
					attribute.String("url.scheme", schemeOf(r)),
				),
			)
			defer span.End()

			corrID := sanitizeCorrelationID(r.Header.Get("X-Correlation-ID"))
			if corrID == "" {
				corrID = sanitizeCorrelationID(r.Header.Get("X-Request-ID"))
			}
			if corrID == "" {
				if sc := span.SpanContext(); sc.IsValid() {
					corrID = sc.TraceID().String()
				} else {
					corrID = NewCorrelationID()
				}
			}

			reqMeta := &RequestMeta{actor: "anonymous"}
			spanCtx = WithCorrelationID(spanCtx, corrID)
			spanCtx = WithRequestMeta(spanCtx, reqMeta)
			span.SetAttributes(attribute.String("http.correlation_id", corrID))
			w.Header().Set("X-Correlation-ID", corrID)

			m := Metrics()
			m.AddHTTPInFlight(spanCtx, r.Method, 1)
			defer m.AddHTTPInFlight(spanCtx, r.Method, -1)

			rec := &responseRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r.WithContext(spanCtx))

			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}

			route := "unmatched"
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				if pattern := rctx.RoutePattern(); pattern != "" {
					route = pattern
				}
			}

			duration := time.Since(start)
			durationMs := RoundMillis(duration)
			span.SetName(fmt.Sprintf("%s %s", r.Method, route))
			span.SetAttributes(
				attribute.String("http.route", route),
				attribute.Int("http.response.status_code", status),
				attribute.Int("http.response.body.size", rec.bytes),
			)
			if status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(status))
			}

			m.RecordHTTPRequest(spanCtx, r.Method, route, status, duration)

			actor := ActorFromContext(spanCtx)
			if actor == "" {
				actor = "anonymous"
			}
			outcomeStatus := "ok"
			if status >= http.StatusInternalServerError {
				outcomeStatus = "error"
			} else if status >= http.StatusBadRequest {
				outcomeStatus = "rejected"
			}

			logAttrs := []any{
				"event", "http.request",
				"actor", actor,
				"method", r.Method,
				"route", route,
				"http_status", status,
				"status", outcomeStatus,
				"duration_ms", durationMs,
				"bytes", rec.bytes,
			}
			if db := DatabaseFromContext(spanCtx); db != "" {
				logAttrs = append(logAttrs, "database", db)
			}

			if status >= http.StatusInternalServerError {
				slog.ErrorContext(spanCtx, "http request failed", logAttrs...)
			} else if status >= http.StatusBadRequest {
				slog.WarnContext(spanCtx, "http request rejected", logAttrs...)
			} else {
				slog.InfoContext(spanCtx, "http request completed", logAttrs...)
			}
		})
	}
}

func sanitizeCorrelationID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > 128 {
		return ""
	}
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == ':':
		default:
			return ""
		}
	}
	return trimmed
}

func schemeOf(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
