package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// MigrationLogMeta carries execution, draft, attempt, and batch identifiers on context.
type MigrationLogMeta struct {
	ExecutionID   uint
	DraftID       uint
	ExecutionType string
	Attempt       int
	BatchNumber   int
}

// RequestMeta holds mutable per-request actor and database labels populated by inner layers.
type RequestMeta struct {
	mu       sync.RWMutex
	actor    string
	database string
}

func (m *RequestMeta) SetActor(actor string) {
	if m == nil || actor == "" {
		return
	}
	m.mu.Lock()
	m.actor = actor
	m.mu.Unlock()
}

func (m *RequestMeta) Actor() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.actor
}

func (m *RequestMeta) SetDatabase(db string) {
	if m == nil || db == "" {
		return
	}
	m.mu.Lock()
	m.database = db
	m.mu.Unlock()
}

func (m *RequestMeta) Database() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.database
}

type (
	actorCtxKey         struct{}
	databaseCtxKey      struct{}
	correlationCtxKey   struct{}
	migrationMetaCtxKey struct{}
	requestMetaCtxKey   struct{}
	skipDBTelemetryKey  struct{}
)

// ActorUser formats a platform user ID into a non-secret actor identifier.
func ActorUser(userID uint) string {
	if userID == 0 {
		return "anonymous"
	}
	return fmt.Sprintf("user:%d", userID)
}

// TargetDatabaseLabel formats a managed target database ID into a concise label.
func TargetDatabaseLabel(dbID uint) string {
	if dbID == 0 {
		return "target:test"
	}
	return fmt.Sprintf("target:%d", dbID)
}

func WithRequestMeta(ctx context.Context, meta *RequestMeta) context.Context {
	if meta == nil {
		return ctx
	}
	return context.WithValue(ctx, requestMetaCtxKey{}, meta)
}

func SetRequestActor(ctx context.Context, actor string) {
	if ctx != nil && actor != "" {
		if meta, ok := ctx.Value(requestMetaCtxKey{}).(*RequestMeta); ok {
			meta.SetActor(actor)
		}
	}
}

func SetRequestDatabase(ctx context.Context, db string) {
	if ctx != nil && db != "" {
		if meta, ok := ctx.Value(requestMetaCtxKey{}).(*RequestMeta); ok {
			meta.SetDatabase(db)
		}
	}
}

func WithActor(ctx context.Context, actor string) context.Context {
	if actor == "" {
		return ctx
	}
	return context.WithValue(ctx, actorCtxKey{}, actor)
}

func ActorFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if actor, ok := ctx.Value(actorCtxKey{}).(string); ok && actor != "" {
		return actor
	}
	if meta, ok := ctx.Value(requestMetaCtxKey{}).(*RequestMeta); ok {
		return meta.Actor()
	}
	return ""
}

func WithDatabase(ctx context.Context, db string) context.Context {
	if db == "" {
		return ctx
	}
	return context.WithValue(ctx, databaseCtxKey{}, db)
}

func DatabaseFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if db, ok := ctx.Value(databaseCtxKey{}).(string); ok && db != "" {
		return db
	}
	if meta, ok := ctx.Value(requestMetaCtxKey{}).(*RequestMeta); ok {
		return meta.Database()
	}
	return ""
}

func WithCorrelationID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, correlationCtxKey{}, id)
}

func CorrelationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(correlationCtxKey{}).(string)
	return id
}

func WithMigrationMeta(ctx context.Context, meta MigrationLogMeta) context.Context {
	return context.WithValue(ctx, migrationMetaCtxKey{}, meta)
}

func WithBatchNumber(ctx context.Context, batchNumber int) context.Context {
	meta := MigrationMetaFromContext(ctx)
	meta.BatchNumber = batchNumber
	return WithMigrationMeta(ctx, meta)
}

func MigrationMetaFromContext(ctx context.Context) MigrationLogMeta {
	if ctx == nil {
		return MigrationLogMeta{}
	}
	meta, _ := ctx.Value(migrationMetaCtxKey{}).(MigrationLogMeta)
	return meta
}

// WithSkipDBTelemetry suppresses routine SQL logs and spans on idle background poll contexts.
func WithSkipDBTelemetry(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipDBTelemetryKey{}, true)
}

func SkipDBTelemetry(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	skip, _ := ctx.Value(skipDBTelemetryKey{}).(bool)
	return skip
}

// NewCorrelationID generates a 128-bit random lowercase hex correlation identifier.
func NewCorrelationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%016x", time.Now().UnixNano())
}

const amqpCorrelationHeader = "x-correlation-id"

// InjectTraceContext serializes W3C trace context and correlation_id into JSON for the outbox table.
func InjectTraceContext(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if corrID := CorrelationIDFromContext(ctx); corrID != "" {
		carrier["correlation_id"] = corrID
	}
	if len(carrier) == 0 {
		return ""
	}
	raw, err := json.Marshal(carrier)
	if err != nil {
		return ""
	}
	return string(raw)
}

// ExtractTraceContext restores W3C trace context and correlation_id serialized by InjectTraceContext.
func ExtractTraceContext(ctx context.Context, raw string) context.Context {
	var carrier propagation.MapCarrier
	if raw == "" || json.Unmarshal([]byte(raw), &carrier) != nil || len(carrier) == 0 {
		return ctx
	}
	if corrID := carrier["correlation_id"]; corrID != "" {
		ctx = WithCorrelationID(ctx, corrID)
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// InjectAMQPHeaders injects W3C trace headers and x-correlation-id into an amqp.Table.
func InjectAMQPHeaders(ctx context.Context, headers amqp.Table) amqp.Table {
	if headers == nil {
		headers = amqp.Table{}
	}
	otel.GetTextMapPropagator().Inject(ctx, amqpTableCarrier(headers))
	if corrID := CorrelationIDFromContext(ctx); corrID != "" {
		headers[amqpCorrelationHeader] = corrID
	}
	return headers
}

// ExtractAMQPHeaders extracts W3C trace headers and x-correlation-id from an amqp.Table into ctx.
func ExtractAMQPHeaders(ctx context.Context, headers amqp.Table) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	if corrID := amqpTableCarrier(headers).Get(amqpCorrelationHeader); corrID != "" {
		ctx = WithCorrelationID(ctx, corrID)
	}
	return otel.GetTextMapPropagator().Extract(ctx, amqpTableCarrier(headers))
}

type amqpTableCarrier amqp.Table

func (c amqpTableCarrier) Get(key string) string {
	switch v := c[key].(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}

func (c amqpTableCarrier) Set(key, val string) { c[key] = val }

func (c amqpTableCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

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

func (r *responseRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// HTTPMiddleware creates a server span per request, propagates correlation_id + RequestMeta,
// and emits trace-correlated structured JSON access logs (skipping /metrics).
func HTTPMiddleware(_ string) func(http.Handler) http.Handler {
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
				trace.WithAttributes(attribute.String("http.request.method", r.Method)),
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

			spanCtx = WithRequestMeta(WithCorrelationID(spanCtx, corrID), &RequestMeta{actor: "anonymous"})
			span.SetAttributes(attribute.String("http.correlation_id", corrID))
			w.Header().Set("X-Correlation-ID", corrID)

			rec := &responseRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r.WithContext(spanCtx))

			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			route := "unmatched"
			if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
				route = rctx.RoutePattern()
			}

			span.SetName(fmt.Sprintf("%s %s", r.Method, route))
			span.SetAttributes(
				attribute.String("http.route", route),
				attribute.Int("http.response.status_code", status),
			)
			if status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(status))
			}

			actor := ActorFromContext(spanCtx)
			if actor == "" {
				actor = "anonymous"
			}
			outcome := "ok"
			if status >= http.StatusInternalServerError {
				outcome = "error"
			} else if status >= http.StatusBadRequest {
				outcome = "rejected"
			}

			logAttrs := []any{
				"event", "http.request",
				"actor", actor,
				"method", r.Method,
				"route", route,
				"http_status", status,
				"status", outcome,
				"duration_ms", RoundMillis(time.Since(start)),
				"bytes", rec.bytes,
			}
			if db := DatabaseFromContext(spanCtx); db != "" {
				logAttrs = append(logAttrs, "database", db)
			}
			switch outcome {
			case "error":
				slog.ErrorContext(spanCtx, "http request failed", logAttrs...)
			case "rejected":
				slog.WarnContext(spanCtx, "http request rejected", logAttrs...)
			default:
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
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == ':') {
			return ""
		}
	}
	return trimmed
}
