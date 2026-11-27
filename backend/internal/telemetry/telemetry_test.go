package telemetry

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

type stubSQLDriver struct{}
type stubSQLConn struct{}

func (d stubSQLDriver) Open(string) (driver.Conn, error) { return stubSQLConn{}, nil }
func (c stubSQLConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c stubSQLConn) Close() error                       { return nil }
func (c stubSQLConn) Begin() (driver.Tx, error)          { return nil, driver.ErrSkip }

func init() { sql.Register("telemetry_stub", stubSQLDriver{}) }

func TestLoadConfigDefaultsAndValidation(t *testing.T) {
	for _, key := range []string{
		"OTEL_SERVICE_NAME", "OTEL_SERVICE_VERSION", "OTEL_ENVIRONMENT", "APP_ENV",
		"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_TRACES_EXPORTER", "OTEL_TRACES_SAMPLER_ARG", "APP_METRICS_ADDR",
	} {
		t.Setenv(key, "")
	}

	cfg, err := LoadConfig("bscheme-api")
	require.NoError(t, err)
	require.Equal(t, "bscheme-api", cfg.ServiceName)
	require.Equal(t, "dev", cfg.ServiceVersion)
	require.Equal(t, "development", cfg.Environment)
	require.Equal(t, 1.0, cfg.SampleRatio)
	require.Equal(t, ":9091", cfg.MetricsAddr)

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://lgtm:4318")
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp,console")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	cfg, err = LoadConfig("bscheme-api")
	require.NoError(t, err)
	require.Equal(t, "http://lgtm:4318", cfg.OTLPEndpoint)
	require.Equal(t, "otlp,console", cfg.TracesExporter)
	require.Equal(t, 0.25, cfg.SampleRatio)

	ep, insecure := parseOTLPEndpoint("https://otel.example.com:4318/")
	require.Equal(t, "otel.example.com:4318", ep)
	require.False(t, insecure)

	t.Setenv("OTEL_TRACES_EXPORTER", "bogus")
	_, err = LoadConfig("bscheme-api")
	require.ErrorContains(t, err, "OTEL_TRACES_EXPORTER")
}

func TestTraceLogHandlerCorrelatesTraceAndSpanIDs(t *testing.T) {
	ctx := context.Background()
	p, err := Init(ctx, Config{ServiceName: "bscheme-api", ServiceVersion: "test", Environment: "test", SampleRatio: 1.0})
	require.NoError(t, err)
	defer func() { _ = p.Shutdown(ctx) }()

	var buf bytes.Buffer
	logger := slog.New(NewTraceLogHandler(slog.NewJSONHandler(&buf, nil), "bscheme-api"))

	spanCtx, span := Tracer("telemetry-test").Start(ctx, "test.operation")
	logger.InfoContext(spanCtx, "migration execution queued", "execution_id", 42)
	span.End()

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	require.Equal(t, "bscheme-api", entry["service"])
	require.Equal(t, span.SpanContext().TraceID().String(), entry["trace_id"])
	require.Equal(t, span.SpanContext().SpanID().String(), entry["span_id"])
	require.EqualValues(t, 42, entry["execution_id"])
}

func TestRedactionMasksCredentialsSessionsAndSensitiveParams(t *testing.T) {
	scrubbed := ScrubString("failed connecting postgres://target:s3cr3tPass@test-db:5432/targetdb Authorization: Bearer top.jwt.token Cookie: labpro_sucks=abc123")
	require.NotContains(t, scrubbed, "s3cr3tPass")
	require.NotContains(t, scrubbed, "top.jwt.token")
	require.NotContains(t, scrubbed, "abc123")
	require.Contains(t, scrubbed, "postgres://target:[REDACTED]@test-db:5432/targetdb")

	sqlSanitized := SanitizeSQL("ALTER ROLE app WITH PASSWORD mysecret; SELECT 'secret-val' FROM sessions WHERE id = $1 AND age > 10 -- note")
	require.NotContains(t, sqlSanitized, "mysecret")
	require.NotContains(t, sqlSanitized, "secret-val")
	require.NotContains(t, sqlSanitized, "note")
	require.Contains(t, sqlSanitized, "$1")

	sessionParams := SanitizeSQLParams("SELECT id, token_hash FROM sessions WHERE token_hash = $1 AND user_id = $2", []any{"deadbeef", uint(7)})
	require.Equal(t, []any{"[REDACTED]", "[REDACTED]"}, sessionParams)

	var logBuf bytes.Buffer
	logger := slog.New(NewTraceLogHandler(slog.NewJSONHandler(&logBuf, nil), "bscheme-api"))
	logger.Warn("database check failed",
		"password", "plain-pass",
		"session_id", "sess-token-xyz",
		"csrf_token", "csrf-xyz",
		"error", errors.New("dial postgres://target:hiddenpass@db:5432/app failed"),
	)
	out := logBuf.String()
	require.NotContains(t, out, "plain-pass")
	require.NotContains(t, out, "sess-token-xyz")
	require.NotContains(t, out, "csrf-xyz")
	require.NotContains(t, out, "hiddenpass")
	require.Contains(t, out, "[REDACTED]")
}

func TestPGXTracerLogsStartResultCommitRollbackAndSQLState(t *testing.T) {
	prevDefault, prevTP := slog.Default(), otel.GetTracerProvider()
	defer slog.SetDefault(prevDefault)
	defer otel.SetTracerProvider(prevTP)

	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)

	var logBuf bytes.Buffer
	slog.SetDefault(slog.New(NewTraceLogHandler(slog.NewJSONHandler(&logBuf, nil), "bscheme-worker")))

	ctx := WithMigrationMeta(WithCorrelationID(WithDatabase(WithActor(context.Background(), "user:7"), "target:3"), "corr-sql-42"), MigrationLogMeta{
		ExecutionID: 42, DraftID: 15, ExecutionType: "up", Attempt: 1, BatchNumber: 2,
	})
	rootCtx, rootSpan := tp.Tracer("test").Start(ctx, "migration.batch.run")

	tracer := NewPGXTracer(3)
	qBegin := tracer.TraceQueryStart(rootCtx, nil, pgx.TraceQueryStartData{SQL: "begin"})
	tracer.TraceQueryEnd(qBegin, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("BEGIN")})
	qSel := tracer.TraceQueryStart(rootCtx, nil, pgx.TraceQueryStartData{SQL: "SELECT id FROM public.orders WHERE status = $1 LIMIT 10", Args: []any{"pending"}})
	tracer.TraceQueryEnd(qSel, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 3")})
	qFail := tracer.TraceQueryStart(rootCtx, nil, pgx.TraceQueryStartData{SQL: "UPDATE public.orders SET code = 'dup-1' WHERE id = $1", Args: []any{101}})
	tracer.TraceQueryEnd(qFail, nil, pgx.TraceQueryEndData{Err: &pgconn.PgError{Code: "23505", Message: "duplicate key"}})
	qRb := tracer.TraceQueryStart(rootCtx, nil, pgx.TraceQueryStartData{SQL: "rollback"})
	tracer.TraceQueryEnd(qRb, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("ROLLBACK")})
	qCom := tracer.TraceQueryStart(rootCtx, nil, pgx.TraceQueryStartData{SQL: "commit"})
	tracer.TraceQueryEnd(qCom, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("COMMIT")})
	rootSpan.End()

	out := logBuf.String()
	for _, want := range []string{"sql.tx.started", "sql.query.started", "sql.query.completed", "sql.query.failed", "sql.tx.rollback", "sql.tx.commit", `"sqlstate":"23505"`, `"rows_affected":3`} {
		require.Contains(t, out, want)
	}
	require.NotContains(t, out, "dup-1")
}

func TestSkipDBTelemetrySuppressesIdlePollNoise(t *testing.T) {
	prevDefault, prevTP := slog.Default(), otel.GetTracerProvider()
	defer slog.SetDefault(prevDefault)
	defer otel.SetTracerProvider(prevTP)

	var spanBuf, logBuf bytes.Buffer
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(NewConsoleSpanExporter(&spanBuf)))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()
	slog.SetDefault(slog.New(NewTraceLogHandler(slog.NewJSONHandler(&logBuf, nil), "bscheme-worker")))

	idleCtx := WithSkipDBTelemetry(context.Background())
	tracer := NewPGXTracer(3)

	qOk := tracer.TraceQueryStart(idleCtx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tracer.TraceQueryEnd(qOk, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 0")})
	require.Empty(t, logBuf.String())
	require.Empty(t, spanBuf.String())

	qErr := tracer.TraceQueryStart(idleCtx, nil, pgx.TraceQueryStartData{SQL: "SELECT * FROM missing"})
	tracer.TraceQueryEnd(qErr, nil, pgx.TraceQueryEndData{Err: &pgconn.PgError{Code: "42P01", Message: "relation does not exist"}})
	require.Contains(t, logBuf.String(), `"sqlstate":"42P01"`)
	require.Empty(t, spanBuf.String())
}

func TestEndToEndTracePropagationAndHTTPMiddleware(t *testing.T) {
	prevDefault, prevTP, prevProp := slog.Default(), otel.GetTracerProvider(), otel.GetTextMapPropagator()
	defer slog.SetDefault(prevDefault)
	defer otel.SetTracerProvider(prevTP)
	defer otel.SetTextMapPropagator(prevProp)

	var spanBuf, logBuf bytes.Buffer
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(sdkresource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName("bscheme"))),
		sdktrace.WithSyncer(NewConsoleSpanExporter(&spanBuf)),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	logger := slog.New(NewTraceLogHandler(slog.NewJSONHandler(&logBuf, nil), "bscheme-api"))
	slog.SetDefault(logger)

	r := chi.NewRouter()
	r.Use(HTTPMiddleware("bscheme-api"))
	var outboxCarrier string
	r.Post("/migrations/executions/{executionId}/enqueue", func(w http.ResponseWriter, req *http.Request) {
		ctx := WithActor(WithDatabase(WithMigrationMeta(req.Context(), MigrationLogMeta{ExecutionID: 42}), "target:3"), "user:7")
		outboxCarrier = InjectTraceContext(ctx)
		w.WriteHeader(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodPost, "/migrations/executions/42/enqueue", nil)
	req.Header.Set("X-Correlation-ID", "corr-e2e-42")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.NotEmpty(t, outboxCarrier)

	headers := InjectAMQPHeaders(ExtractTraceContext(context.Background(), outboxCarrier), amqp.Table{})
	workerCtx, workerSpan := Tracer("tubes-wbd/worker").Start(ExtractAMQPHeaders(context.Background(), headers), "migration.worker.consume", trace.WithSpanKind(trace.SpanKindConsumer))
	logger.InfoContext(workerCtx, "migration execution started", "execution_id", 42)
	workerSpan.End()

	lines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
	require.Len(t, lines, 2)
	var apiRow, workerRow map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &apiRow))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &workerRow))
	require.Equal(t, apiRow["trace_id"], workerRow["trace_id"])
	require.Equal(t, "corr-e2e-42", workerRow["correlation_id"])
	require.Contains(t, spanBuf.String(), `POST /migrations/executions/{executionId}/enqueue`)
	require.Contains(t, spanBuf.String(), `migration.worker.consume`)
}

func TestPGXTracerRedactsSQLToBoundedVerb(t *testing.T) {
	prevDefault := slog.Default()
	defer slog.SetDefault(prevDefault)

	var logBuf bytes.Buffer
	slog.SetDefault(slog.New(NewTraceLogHandler(slog.NewJSONHandler(&logBuf, nil), "bscheme-worker")))

	tracer := NewPGXTracer(9)
	for _, tc := range []struct {
		sql, want string
	}{
		{`INSERT INTO secrets VALUES ('top-secret-password')`, "insert"},
		{"-- comment\nSELECT id FROM users WHERE id = $1", "select"},
		{"/* comment */ UPDATE accounts SET active = false", "update"},
		{"DELETE FROM sessions WHERE id = $1", "delete"},
		{"ALTER TABLE users ADD COLUMN role text", "ddl"},
		{"LOCK TABLE _dbm.migration_history IN EXCLUSIVE MODE", "lock"},
		{"BEGIN", "tx"},
	} {
		require.Equal(t, tc.want, classifySQLVerb(tc.sql))
		qCtx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: tc.sql})
		tracer.TraceQueryEnd(qCtx, nil, pgx.TraceQueryEndData{})
	}
	require.NotContains(t, logBuf.String(), "top-secret-password")
}

func TestPrometheusMetricsExpositionAndCardinalityGuard(t *testing.T) {
	ctx := context.Background()
	p, err := Init(ctx, Config{ServiceName: "bscheme-api", ServiceVersion: "test", Environment: "test", SampleRatio: 1.0})
	require.NoError(t, err)
	defer func() { _ = p.Shutdown(ctx) }()

	sqlDB, err := sql.Open("telemetry_stub", "")
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	require.NoError(t, p.RegisterDBPoolStats(sqlDB, func() TargetPoolStats {
		return TargetPoolStats{ActiveConns: 3, IdleConns: 4, WaitCount: 1, WaitDuration: 45 * time.Millisecond}
	}))
	require.NoError(t, p.RegisterMigrationStatusCollector(func(context.Context) (map[string]int64, error) {
		return map[string]int64{"queued": 3, "running": 1, "success": 14, "failed": 1}, nil
	}))

	m := Metrics()
	m.RecordMigrationQueueWait(ctx, "up", 120*time.Millisecond)
	m.RecordMigrationLockWait(ctx, "up", "advisory", 18*time.Millisecond)
	m.RecordMigrationBatch(ctx, "up", 500)
	m.RecordMigrationRetry(ctx, "up", "retryable_error")
	m.RecordMigrationExecution(ctx, "up", "success", 2400*time.Millisecond)

	rec := httptest.NewRecorder()
	p.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	for _, name := range []string{
		"bscheme_migration_jobs",
		"bscheme_migration_executions_total",
		"bscheme_migration_execution_duration_seconds_bucket",
		"bscheme_migration_retries_total",
		"bscheme_migration_queue_wait_duration_seconds_bucket",
		"bscheme_migration_lock_wait_duration_seconds_bucket",
		"bscheme_migration_batches_processed_total",
		"bscheme_migration_batch_rows_processed_total",
		"bscheme_db_pool_connections",
		"bscheme_db_pool_wait_count_total",
		"bscheme_db_pool_wait_duration_seconds_total",
		"go_goroutines",
		"process_resident_memory_bytes",
	} {
		require.Contains(t, body, name)
	}
	for _, status := range KnownMigrationStatuses {
		require.Contains(t, body, `bscheme_migration_jobs{status="`+status+`"}`)
	}
	require.Contains(t, body, `bscheme_db_pool_connections{db_role="target",state="active"} 3`)
	require.Contains(t, body, `bscheme_db_pool_connections{db_role="target",state="idle"} 4`)
	require.Contains(t, body, `bscheme_db_pool_wait_count_total{db_role="target"} 1`)
	require.Contains(t, body, `bscheme_db_pool_wait_duration_seconds_total{db_role="target"} 0.045`)

	for _, forbidden := range []string{"user_id=", "execution_id=", "job_id=", "draft_id=", "sql="} {
		require.NotContains(t, body, forbidden)
	}

	// Ensure unused io import is not needed; keep HTTPMiddleware /metrics skip check cleanly.
	r := chi.NewRouter()
	r.Use(HTTPMiddleware("bscheme-api"))
	r.Handle("/metrics", p.MetricsHandler())
	r.Get("/ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "pong") })
	pingRec := httptest.NewRecorder()
	r.ServeHTTP(pingRec, httptest.NewRequest(http.MethodGet, "/ping", nil))
	require.Equal(t, http.StatusOK, pingRec.Code)
}
