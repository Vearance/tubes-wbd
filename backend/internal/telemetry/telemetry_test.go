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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

type stubSQLDriver struct{}
type stubSQLConn struct{}

func (d stubSQLDriver) Open(string) (driver.Conn, error) { return stubSQLConn{}, nil }
func (c stubSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, driver.ErrSkip
}
func (c stubSQLConn) Close() error              { return nil }
func (c stubSQLConn) Begin() (driver.Tx, error) { return nil, driver.ErrSkip }

func init() {
	sql.Register("telemetry_stub", stubSQLDriver{})
}

func TestLoadConfigDefaultsAndValidation(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_SERVICE_VERSION", "")
	t.Setenv("OTEL_ENVIRONMENT", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_TRACES_EXPORTER", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")
	t.Setenv("APP_METRICS_ADDR", "")

	cfg, err := LoadConfig("bscheme-api")
	require.NoError(t, err)
	require.Equal(t, "bscheme-api", cfg.ServiceName)
	require.Equal(t, "dev", cfg.ServiceVersion)
	require.Equal(t, "development", cfg.Environment)
	require.Empty(t, cfg.OTLPEndpoint)
	require.Empty(t, cfg.TracesExporter)
	require.Equal(t, 1.0, cfg.SampleRatio)
	require.Equal(t, ":9091", cfg.MetricsAddr)

	t.Setenv("OTEL_SERVICE_NAME", "custom-api")
	t.Setenv("OTEL_SERVICE_VERSION", "1.2.3")
	t.Setenv("APP_ENV", "staging")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4318")
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp,console")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	t.Setenv("APP_METRICS_ADDR", ":9191")

	cfg, err = LoadConfig("bscheme-api")
	require.NoError(t, err)
	require.Equal(t, "custom-api", cfg.ServiceName)
	require.Equal(t, "1.2.3", cfg.ServiceVersion)
	require.Equal(t, "staging", cfg.Environment)
	require.Equal(t, "http://otel-collector:4318", cfg.OTLPEndpoint)
	require.Equal(t, "otlp,console", cfg.TracesExporter)
	require.Equal(t, 0.25, cfg.SampleRatio)
	require.Equal(t, ":9191", cfg.MetricsAddr)

	t.Setenv("OTEL_TRACES_EXPORTER", "bogus")
	_, err = LoadConfig("bscheme-api")
	require.ErrorContains(t, err, "OTEL_TRACES_EXPORTER")
	t.Setenv("OTEL_TRACES_EXPORTER", "")

	endpoint, insecure := parseOTLPEndpoint("http://otel-collector:4318/")
	require.Equal(t, "otel-collector:4318", endpoint)
	require.True(t, insecure)

	endpoint, insecure = parseOTLPEndpoint("https://otel.example.com:4318")
	require.Equal(t, "otel.example.com:4318", endpoint)
	require.False(t, insecure)

	endpoint, insecure = parseOTLPEndpoint("localhost:4318")
	require.Equal(t, "localhost:4318", endpoint)
	require.True(t, insecure)

	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "1.5")
	_, err = LoadConfig("bscheme-api")
	require.ErrorContains(t, err, "OTEL_TRACES_SAMPLER_ARG")
}

func TestTraceLogHandlerCorrelatesTraceAndSpanIDs(t *testing.T) {
	ctx := context.Background()
	p, err := Init(ctx, Config{
		ServiceName:    "bscheme-api",
		ServiceVersion: "test",
		Environment:    "test",
		SampleRatio:    1.0,
	})
	require.NoError(t, err)
	defer func() { _ = p.Shutdown(ctx) }()

	var buf bytes.Buffer
	logger := slog.New(NewTraceLogHandler(slog.NewJSONHandler(&buf, nil), "bscheme-api"))

	spanCtx, span := Tracer("telemetry-test").Start(ctx, "test.operation")
	expectedTraceID := span.SpanContext().TraceID().String()
	expectedSpanID := span.SpanContext().SpanID().String()
	logger.InfoContext(spanCtx, "migration execution queued", "execution_id", 42)
	span.End()

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	require.Equal(t, "bscheme-api", entry["service"])
	require.Equal(t, "migration execution queued", entry["event"])
	require.Equal(t, "system", entry["actor"])
	require.Equal(t, expectedTraceID, entry["trace_id"])
	require.Equal(t, expectedSpanID, entry["span_id"])
	require.Equal(t, expectedTraceID, entry["correlation_id"])
	require.Equal(t, "migration execution queued", entry["msg"])
	require.EqualValues(t, 42, entry["execution_id"])
}

func TestRedactionMasksCredentialsSessionsAndSensitiveParams(t *testing.T) {
	scrubbed := ScrubString("failed connecting postgres://target:s3cr3tPass@test-db:5432/targetdb Authorization: Bearer top.jwt.token Cookie: labpro_sucks=abc123")
	require.NotContains(t, scrubbed, "s3cr3tPass")
	require.NotContains(t, scrubbed, "top.jwt.token")
	require.NotContains(t, scrubbed, "abc123")
	require.Contains(t, scrubbed, "postgres://target:[REDACTED]@test-db:5432/targetdb")
	require.Contains(t, scrubbed, "Authorization: [REDACTED]")
	require.Equal(t, "Bearer [REDACTED]", ScrubString("Bearer secret.jwt.token"))

	sqlSanitized := SanitizeSQL("ALTER ROLE app WITH PASSWORD mysecret; SELECT 'secret-val' FROM sessions WHERE id = $1 AND age > 10 -- note")
	require.NotContains(t, sqlSanitized, "mysecret")
	require.NotContains(t, sqlSanitized, "secret-val")
	require.NotContains(t, sqlSanitized, "note")
	require.Contains(t, sqlSanitized, "$1")
	require.Contains(t, sqlSanitized, "[NUMBER]")

	sessionParams := SanitizeSQLParams("SELECT id, token_hash FROM sessions WHERE token_hash = $1 AND user_id = $2", []any{"deadbeef", uint(7)})
	require.Equal(t, []any{"[REDACTED]", "[REDACTED]"}, sessionParams)

	userParams := SanitizeSQLParams("INSERT INTO users (username, password_hash) VALUES ($1, $2)", []any{"alice", "$2a$10$hash"})
	require.Equal(t, []any{"[REDACTED]", "[REDACTED]"}, userParams)

	safeParams := SanitizeSQLParams("UPDATE orders SET status = $1 WHERE id = ANY($2)", []any{"processed", []int64{101, 102}})
	require.Equal(t, []any{"processed", []int64{101, 102}}, safeParams)

	var logBuf bytes.Buffer
	logger := slog.New(NewTraceLogHandler(slog.NewJSONHandler(&logBuf, nil), "bscheme-api"))
	logger.Warn("database check failed",
		"event", "database.connection.tested",
		"password", "plain-pass",
		"session_id", 991,
		"csrf_token", "csrf-xyz",
		"error", errors.New("dial postgres://target:hiddenpass@db:5432/app failed"),
	)
	out := logBuf.String()
	require.NotContains(t, out, "plain-pass")
	require.NotContains(t, out, "991")
	require.NotContains(t, out, "csrf-xyz")
	require.NotContains(t, out, "hiddenpass")
	require.Contains(t, out, "[REDACTED]")
}

func TestPGXTracerLogsStartResultCommitRollbackAndSQLState(t *testing.T) {
	prevDefault := slog.Default()
	defer slog.SetDefault(prevDefault)

	prevTP := otel.GetTracerProvider()
	defer otel.SetTracerProvider(prevTP)
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)

	var logBuf bytes.Buffer
	slog.SetDefault(slog.New(NewTraceLogHandler(slog.NewJSONHandler(&logBuf, nil), "bscheme-worker")))

	ctx := context.Background()
	ctx = WithActor(ctx, "user:7")
	ctx = WithDatabase(ctx, "target:3")
	ctx = WithCorrelationID(ctx, "corr-sql-42")
	ctx = WithMigrationMeta(ctx, MigrationLogMeta{
		ExecutionID:   42,
		DraftID:       15,
		ExecutionType: "up",
		Attempt:       1,
		BatchNumber:   2,
	})
	rootCtx, rootSpan := tp.Tracer("test").Start(ctx, "migration.batch.run")

	tracer := NewPGXTracer(3)

	// 1. BEGIN transaction
	beginCtx := tracer.TraceQueryStart(rootCtx, nil, pgx.TraceQueryStartData{SQL: "begin isolation level repeatable read read only"})
	tracer.TraceQueryEnd(beginCtx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("BEGIN")})

	// 2. Read-only SELECT with parameter and row count
	selectCtx := tracer.TraceQueryStart(rootCtx, nil, pgx.TraceQueryStartData{
		SQL:  "SELECT id, status FROM public.orders WHERE status = $1 LIMIT 10",
		Args: []any{"pending"},
	})
	tracer.TraceQueryEnd(selectCtx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 3")})

	// 3. Failing DML with SQLSTATE 23505
	failCtx := tracer.TraceQueryStart(rootCtx, nil, pgx.TraceQueryStartData{
		SQL:  "UPDATE public.orders SET code = 'dup-1' WHERE id = $1",
		Args: []any{101},
	})
	tracer.TraceQueryEnd(failCtx, nil, pgx.TraceQueryEndData{
		Err: &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"},
	})

	// 4. ROLLBACK and 5. COMMIT logged separately
	rbCtx := tracer.TraceQueryStart(rootCtx, nil, pgx.TraceQueryStartData{SQL: "rollback"})
	tracer.TraceQueryEnd(rbCtx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("ROLLBACK")})

	commitCtx := tracer.TraceQueryStart(rootCtx, nil, pgx.TraceQueryStartData{SQL: "commit"})
	tracer.TraceQueryEnd(commitCtx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("COMMIT")})

	rootSpan.End()

	lines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
	require.GreaterOrEqual(t, len(lines), 7)

	events := make([]string, 0, len(lines))
	for _, line := range lines {
		var row map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &row))
		require.NotEmpty(t, row["time"])
		require.NotEmpty(t, row["level"])
		require.Equal(t, "user:7", row["actor"])
		require.Equal(t, "target:3", row["database"])
		require.Equal(t, "corr-sql-42", row["correlation_id"])
		require.Equal(t, rootSpan.SpanContext().TraceID().String(), row["trace_id"])
		require.NotEmpty(t, row["span_id"])
		require.EqualValues(t, 42, row["execution_id"])
		require.EqualValues(t, 1, row["attempt"])
		require.EqualValues(t, 2, row["batch_number"])
		for _, redundant := range []string{"trace_flags", "db_role", "command_tag", "execution_type"} {
			require.NotContains(t, row, redundant)
		}
		if row["event"] == "sql.tx.started" || row["event"] == "sql.tx.commit" || row["event"] == "sql.tx.rollback" {
			require.NotContains(t, row, "parameters")
			require.NotContains(t, row, "operation")
		}
		events = append(events, row["event"].(string))
	}

	require.Contains(t, events, "sql.tx.started")
	require.Contains(t, events, "sql.query.started")
	require.Contains(t, events, "sql.query.completed")
	require.Contains(t, events, "sql.query.failed")
	require.Contains(t, events, "sql.tx.rollback")
	require.Contains(t, events, "sql.tx.commit")
	require.Contains(t, logBuf.String(), `"sqlstate":"23505"`)
	require.Contains(t, logBuf.String(), `"rows_affected":3`)
	require.Contains(t, logBuf.String(), `"parameters":["pending"]`)
	require.NotContains(t, logBuf.String(), "dup-1")
	t.Logf("SQL_LIFECYCLE_LOGS:\n%s", logBuf.String())
}

func TestSkipDBTelemetrySuppressesIdlePollNoise(t *testing.T) {
	prevDefault := slog.Default()
	defer slog.SetDefault(prevDefault)
	prevTP := otel.GetTracerProvider()
	defer otel.SetTracerProvider(prevTP)

	var spanBuf, logBuf bytes.Buffer
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(NewConsoleSpanExporter(&spanBuf)))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	slog.SetDefault(slog.New(NewTraceLogHandler(slog.NewJSONHandler(&logBuf, nil), "bscheme-worker")))

	idleCtx := WithSkipDBTelemetry(context.Background())
	tracer := NewPGXTracer(3)

	// Idle polling SELECT should emit zero log lines and zero spans.
	qOk := tracer.TraceQueryStart(idleCtx, nil, pgx.TraceQueryStartData{
		SQL: "SELECT id FROM migration_execution_outbox WHERE published_at IS NULL LIMIT 50",
	})
	tracer.TraceQueryEnd(qOk, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 0")})
	require.Empty(t, logBuf.String())
	require.Empty(t, spanBuf.String())

	// An error on a background poll MUST still be logged with sqlstate.
	qErr := tracer.TraceQueryStart(idleCtx, nil, pgx.TraceQueryStartData{
		SQL: "SELECT id FROM migration_execution_outbox",
	})
	tracer.TraceQueryEnd(qErr, nil, pgx.TraceQueryEndData{
		Err: &pgconn.PgError{Code: "42P01", Message: "relation does not exist"},
	})
	require.Contains(t, logBuf.String(), `"event":"sql.query.failed"`)
	require.Contains(t, logBuf.String(), `"sqlstate":"42P01"`)
	require.Empty(t, spanBuf.String())
}

func TestEndToEndTraceAndConsoleSpanExport(t *testing.T) {
	prevDefault := slog.Default()
	defer slog.SetDefault(prevDefault)
	prevTP := otel.GetTracerProvider()
	defer otel.SetTracerProvider(prevTP)
	prevProp := otel.GetTextMapPropagator()
	defer otel.SetTextMapPropagator(prevProp)

	var spanBuf, apiBuf, workerBuf bytes.Buffer
	consoleExp := NewConsoleSpanExporter(&spanBuf)
	res := sdkresource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName("bscheme"))
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSyncer(consoleExp),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	apiLogger := slog.New(NewTraceLogHandler(slog.NewJSONHandler(&apiBuf, nil), "bscheme-api"))
	workerLogger := slog.New(NewTraceLogHandler(slog.NewJSONHandler(&workerBuf, nil), "bscheme-worker"))

	// 1. API enqueue request
	reqCtx, httpSpan := Tracer("tubes-wbd/http").Start(context.Background(), "POST /migrations/executions/{executionId}/enqueue",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("http.route", "/migrations/executions/{executionId}/enqueue")),
	)
	reqCtx = WithMigrationMeta(reqCtx, MigrationLogMeta{
		ExecutionID:   42,
		DraftID:       15,
		ExecutionType: "up",
	})
	reqCtx = WithDatabase(reqCtx, "target:3")
	reqCtx = WithActor(reqCtx, "user:7")
	reqCtx = WithCorrelationID(reqCtx, "corr-e2e-42")
	rootTraceID := httpSpan.SpanContext().TraceID().String()
	apiLogger.InfoContext(reqCtx, "migration execution queued", "event", "migration.execution.queued", "status", "queued")
	outboxCarrier := InjectTraceContext(reqCtx)
	httpSpan.End()

	// 2. Outbox -> RabbitMQ
	relayCtx := ExtractTraceContext(context.Background(), outboxCarrier)
	pubCtx, pubSpan := Tracer("tubes-wbd/outbox").Start(relayCtx, "migration.outbox.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(attribute.String("messaging.system", "rabbitmq")),
	)
	pubCtx = WithActor(WithDatabase(WithMigrationMeta(pubCtx, MigrationLogMeta{ExecutionID: 42}), "platform"), "system:outbox-relay")
	apiLogger.InfoContext(pubCtx, "published migration execution to queue", "event", "migration.outbox.published", "queue", "migration.executions", "status", "ok", "duration_ms", 3.412)
	amqpHeaders := InjectAMQPHeaders(pubCtx, amqp.Table{})
	pubSpan.End()

	// 3. Worker consume -> migration run -> lock -> batch -> target SQL
	consumeCtx := ExtractAMQPHeaders(context.Background(), amqpHeaders)
	workerConsumeCtx, consumeSpan := Tracer("tubes-wbd/worker").Start(consumeCtx, "migration.worker.consume",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attribute.String("messaging.system", "rabbitmq")),
	)
	runCtx, runSpan := Tracer("tubes-wbd/migration").Start(workerConsumeCtx, "migration.execution.run")
	runCtx = WithActor(WithDatabase(WithMigrationMeta(runCtx, MigrationLogMeta{
		ExecutionID:   42,
		DraftID:       15,
		ExecutionType: "up",
		Attempt:       1,
	}), "target:3"), "user:7")
	workerLogger.InfoContext(runCtx, "migration execution started", "event", "migration.execution.started", "queue_wait_ms", 18, "status", "started")

	lockCtx, lockSpan := Tracer("tubes-wbd/migration").Start(runCtx, "migration.lock.acquire",
		trace.WithAttributes(attribute.String("migration.lock_scope", "advisory")),
	)
	workerLogger.InfoContext(lockCtx, "migration target lock acquired", "event", "migration.lock.acquired", "lock_scope", "advisory", "status", "ok", "duration_ms", 1.205)
	lockSpan.End()

	batchCtx, batchSpan := Tracer("tubes-wbd/migration").Start(runCtx, "migration.batch.run",
		trace.WithAttributes(attribute.Int("migration.batch_number", 1)),
	)
	batchCtx = WithBatchNumber(batchCtx, 1)

	// Redirect default slog so PGXTracer logs flow into workerBuf
	slog.SetDefault(workerLogger)
	pgxTracer := NewPGXTracer(3)
	qBegin := pgxTracer.TraceQueryStart(batchCtx, nil, pgx.TraceQueryStartData{SQL: "begin"})
	pgxTracer.TraceQueryEnd(qBegin, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("BEGIN")})
	qUpdate := pgxTracer.TraceQueryStart(batchCtx, nil, pgx.TraceQueryStartData{
		SQL:  "UPDATE public.orders SET status = 'archived' WHERE id = ANY($1)",
		Args: []any{[]int64{101, 102}},
	})
	pgxTracer.TraceQueryEnd(qUpdate, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("UPDATE 2")})
	qCommit := pgxTracer.TraceQueryStart(batchCtx, nil, pgx.TraceQueryStartData{SQL: "commit"})
	pgxTracer.TraceQueryEnd(qCommit, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("COMMIT")})

	workerLogger.InfoContext(batchCtx, "migration batch committed", "event", "migration.batch.committed", "rows_affected", 2, "status", "committed", "duration_ms", 12.6)
	batchSpan.End()

	workerLogger.InfoContext(runCtx, "migration execution succeeded", "event", "migration.execution.completed", "execution_type", "up", "execution_status", "success", "version", 7, "status", "ok", "duration_ms", 19.8)
	runSpan.End()
	consumeSpan.End()

	combinedLogs := strings.TrimSpace(apiBuf.String()) + "\n" + strings.TrimSpace(workerBuf.String())
	for _, line := range strings.Split(combinedLogs, "\n") {
		var row map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &row))
		require.Equal(t, rootTraceID, row["trace_id"])
		require.Equal(t, "corr-e2e-42", row["correlation_id"])
		require.NotEmpty(t, row["span_id"])
		require.NotContains(t, row, "trace_flags")
		require.NotContains(t, row, "db_role")
		require.NotContains(t, row, "command_tag")
	}

	spanLines := strings.Split(strings.TrimSpace(spanBuf.String()), "\n")
	require.GreaterOrEqual(t, len(spanLines), 7)
	spanNames := make([]string, 0, len(spanLines))
	for _, line := range spanLines {
		var s map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &s))
		require.Equal(t, "otel_span", s["type"])
		require.Equal(t, rootTraceID, s["trace_id"])
		require.NotEmpty(t, s["span_id"])
		if attrs, ok := s["attributes"].(map[string]any); ok {
			require.NotContains(t, attrs, "db.duration_ms")
			if s["name"] == "db.target.update" {
				require.Equal(t, "postgresql", attrs["db.system.name"])
				require.Equal(t, "update", attrs["db.operation.name"])
			}
		}
		spanNames = append(spanNames, s["name"].(string))
	}
	require.Contains(t, spanNames, "POST /migrations/executions/{executionId}/enqueue")
	require.Contains(t, spanNames, "migration.outbox.publish")
	require.Contains(t, spanNames, "migration.worker.consume")
	require.Contains(t, spanNames, "migration.execution.run")
	require.Contains(t, spanNames, "migration.lock.acquire")
	require.Contains(t, spanNames, "migration.batch.run")
	require.Contains(t, spanNames, "db.target.update")

	t.Logf("E2E_JSON_LOGS:\n%s\nE2E_CONSOLE_SPANS:\n%s", combinedLogs, spanBuf.String())
}

func TestTracePropagationOutboxAndAMQPRoundTrip(t *testing.T) {
	ctx := context.Background()
	p, err := Init(ctx, Config{
		ServiceName:    "bscheme-api",
		ServiceVersion: "test",
		Environment:    "test",
		SampleRatio:    1.0,
	})
	require.NoError(t, err)
	defer func() { _ = p.Shutdown(ctx) }()

	var apiBuf, workerBuf bytes.Buffer
	apiLogger := slog.New(NewTraceLogHandler(slog.NewJSONHandler(&apiBuf, nil), "bscheme-api"))
	workerLogger := slog.New(NewTraceLogHandler(slog.NewJSONHandler(&workerBuf, nil), "bscheme-worker"))

	// 1. API request span enqueues outbox record.
	reqCtx, httpSpan := Tracer("tubes-wbd/http").Start(ctx, "POST /migrations/executions/{executionId}/enqueue",
		trace.WithSpanKind(trace.SpanKindServer))
	rootTraceID := httpSpan.SpanContext().TraceID()
	apiLogger.InfoContext(reqCtx, "migration execution queued",
		"execution_id", 42, "database_id", 3, "execution_type", "up")
	serialized := InjectTraceContext(reqCtx)
	require.NotEmpty(t, serialized)
	httpSpan.End()

	// 2. Outbox relay restores trace context, creates Producer span, injects AMQP headers.
	relayBaseCtx := ExtractTraceContext(context.Background(), serialized)
	pubCtx, pubSpan := Tracer("tubes-wbd/outbox").Start(relayBaseCtx, "migration.outbox.publish",
		trace.WithSpanKind(trace.SpanKindProducer))
	require.Equal(t, rootTraceID, pubSpan.SpanContext().TraceID())
	apiLogger.InfoContext(pubCtx, "published migration execution to queue",
		"execution_id", 42, "outbox_id", 108, "queue", "migration.executions")
	headers := InjectAMQPHeaders(pubCtx, amqp.Table{})
	pubSpan.End()

	// 3. Worker consumes RabbitMQ delivery with same trace_id across services.
	deliveryCtx := ExtractAMQPHeaders(context.Background(), headers)
	consumeCtx, consumeSpan := Tracer("tubes-wbd/worker").Start(deliveryCtx, "migration.worker.consume",
		trace.WithSpanKind(trace.SpanKindConsumer))
	require.Equal(t, rootTraceID, consumeSpan.SpanContext().TraceID())
	runCtx, runSpan := Tracer("tubes-wbd/migration").Start(consumeCtx, "migration.execution.run")
	require.Equal(t, rootTraceID, runSpan.SpanContext().TraceID())
	workerLogger.InfoContext(runCtx, "migration execution started",
		"execution_id", 42, "database_id", 3, "execution_type", "up", "attempt", 1, "queue_wait_ms", 18)
	workerLogger.InfoContext(runCtx, "migration execution succeeded",
		"execution_id", 42, "database_id", 3, "execution_type", "up", "attempt", 1, "version", 7, "duration", 42*time.Millisecond)
	runSpan.End()
	consumeSpan.End()

	combined := strings.TrimSpace(apiBuf.String()) + "\n" + strings.TrimSpace(workerBuf.String())
	for _, line := range strings.Split(combined, "\n") {
		var row map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &row))
		require.Equal(t, rootTraceID.String(), row["trace_id"])
	}

	t.Logf("CORRELATED_API_LOGS:\n%sCORRELATED_WORKER_LOGS:\n%s", apiBuf.String(), workerBuf.String())
}

func TestHTTPMiddlewareLowCardinalityAndSkipsMetrics(t *testing.T) {
	ctx := context.Background()
	p, err := Init(ctx, Config{
		ServiceName:    "bscheme-api",
		ServiceVersion: "test",
		Environment:    "test",
		SampleRatio:    1.0,
	})
	require.NoError(t, err)
	defer func() { _ = p.Shutdown(ctx) }()

	r := chi.NewRouter()
	r.Use(HTTPMiddleware("bscheme-api"))
	r.Handle("/metrics", p.MetricsHandler())
	r.Post("/migrations/executions/{executionId}/enqueue", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"status":"queued"}`)
	})

	// Mute default slog during test so JSON access log does not clutter test output.
	prevDefault := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	defer slog.SetDefault(prevDefault)

	req := httptest.NewRequest(http.MethodPost, "/migrations/executions/42/enqueue", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	r.ServeHTTP(metricsRec, metricsReq)
	require.Equal(t, http.StatusOK, metricsRec.Code)

	body := metricsRec.Body.String()
	require.Contains(t, body, `route="/migrations/executions/{executionId}/enqueue"`)
	require.NotContains(t, body, `route="/migrations/executions/42/enqueue"`)
	require.NotContains(t, body, `route="/metrics"`)
}

func TestPGXTracerRedactsSQLToBoundedVerb(t *testing.T) {
	ctx := context.Background()
	p, err := Init(ctx, Config{
		ServiceName:    "bscheme-worker",
		ServiceVersion: "test",
		Environment:    "test",
		SampleRatio:    1.0,
	})
	require.NoError(t, err)
	defer func() { _ = p.Shutdown(ctx) }()

	cases := []struct {
		sql  string
		want string
	}{
		{`INSERT INTO secrets VALUES ('top-secret-password')`, "insert"},
		{"-- header comment\nSELECT id FROM users WHERE id = $1", "select"},
		{"/* block comment */ UPDATE accounts SET active = false", "update"},
		{"DELETE FROM sessions WHERE id = $1", "delete"},
		{"ALTER TABLE users ADD COLUMN role text", "ddl"},
		{"LOCK TABLE _dbm.migration_history IN EXCLUSIVE MODE", "lock"},
		{"BEGIN", "tx"},
		{"", "other"},
	}

	tracer := NewPGXTracer(9)
	for _, tc := range cases {
		require.Equal(t, tc.want, classifySQLVerb(tc.sql), tc.sql)
		qCtx := tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: tc.sql})
		tracer.TraceQueryEnd(qCtx, nil, pgx.TraceQueryEndData{})
	}

	metricsRec := httptest.NewRecorder()
	p.MetricsHandler().ServeHTTP(metricsRec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := metricsRec.Body.String()

	require.Contains(t, body, `bscheme_db_queries_total{db_role="target",operation="insert",status="ok"} 1`)
	require.NotContains(t, body, "top-secret-password")
	require.NotContains(t, body, "sql=")
}

func TestPrometheusMetricsExpositionAndCardinalityGuard(t *testing.T) {
	ctx := context.Background()
	p, err := Init(ctx, Config{
		ServiceName:    "bscheme-api",
		ServiceVersion: "test",
		Environment:    "test",
		SampleRatio:    1.0,
	})
	require.NoError(t, err)
	defer func() { _ = p.Shutdown(ctx) }()

	sqlDB, err := sql.Open("telemetry_stub", "")
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	err = p.RegisterDBPoolStats(sqlDB, func() TargetPoolStats {
		return TargetPoolStats{
			ActivePools:  2,
			ActiveConns:  3,
			IdleConns:    4,
			TotalConns:   7,
			MaxConns:     10,
			WaitCount:    1,
			WaitDuration: 45 * time.Millisecond,
		}
	})
	require.NoError(t, err)

	err = p.RegisterMigrationStatusCollector(func(context.Context) (map[string]int64, int64, error) {
		return map[string]int64{
			"pending_review": 1,
			"approved":       2,
			"queued":         3,
			"running":        1,
			"success":        14,
			"failed":         1,
		}, 2, nil
	})
	require.NoError(t, err)

	m := Metrics()
	m.AddHTTPInFlight(ctx, http.MethodPost, 1)
	m.RecordHTTPRequest(ctx, http.MethodPost, "/migrations/executions/{executionId}/enqueue", http.StatusAccepted, 35*time.Millisecond)
	m.AddHTTPInFlight(ctx, http.MethodPost, -1)

	m.AddWorkerInFlight(ctx, "up", 1)
	m.RecordMigrationQueueWait(ctx, "up", 120*time.Millisecond)
	m.RecordMigrationLockWait(ctx, "up", "advisory", 18*time.Millisecond)
	m.RecordMigrationLockWait(ctx, "up", "history_table", 9*time.Millisecond)
	m.RecordMigrationBatch(ctx, "up", 500)
	m.RecordMigrationRetry(ctx, "up", "retryable_error")
	m.RecordMigrationExecution(ctx, "up", "success", 2400*time.Millisecond)
	m.AddWorkerInFlight(ctx, "up", -1)

	m.RecordOutboxPublish(ctx, "success")
	m.RecordWorkerMessage(ctx, "ack")
	m.RecordDBQuery(ctx, "platform", "select", "ok", 4*time.Millisecond)
	m.RecordDBQuery(ctx, "target", "ddl", "ok", 150*time.Millisecond)
	m.RecordAuthLogin(ctx, "success")
	m.RecordViewerOperation(ctx, "table_data", "ok")

	rec := httptest.NewRecorder()
	p.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()

	expectedMetrics := []string{
		"bscheme_http_requests_total",
		"bscheme_http_request_duration_seconds_bucket",
		"bscheme_http_requests_in_flight",
		"bscheme_migration_jobs",
		"bscheme_migration_executions_total",
		"bscheme_migration_execution_duration_seconds_bucket",
		"bscheme_migration_retries_total",
		"bscheme_migration_queue_wait_duration_seconds_bucket",
		"bscheme_migration_lock_wait_duration_seconds_bucket",
		"bscheme_migration_batches_processed_total",
		"bscheme_migration_batch_rows_processed_total",
		"bscheme_migration_outbox_published_total",
		"bscheme_migration_outbox_pending",
		"bscheme_migration_worker_messages_total",
		"bscheme_migration_worker_in_flight",
		"bscheme_db_queries_total",
		"bscheme_db_query_duration_seconds_bucket",
		"bscheme_db_pool_connections",
		"bscheme_db_pool_wait_count_total",
		"bscheme_db_pool_wait_duration_seconds_total",
		"bscheme_targetdb_pools_active",
		"bscheme_auth_logins_total",
		"bscheme_viewer_operations_total",
		"go_goroutines",
		"process_resident_memory_bytes",
	}
	for _, name := range expectedMetrics {
		require.Contains(t, body, name, "missing expected metric %s", name)
	}

	for _, status := range KnownMigrationStatuses {
		require.Contains(t, body, `bscheme_migration_jobs{status="`+status+`"}`)
	}

	require.Contains(t, body, `bscheme_db_pool_connections{db_role="platform",state="active"}`)
	require.Contains(t, body, `bscheme_db_pool_connections{db_role="platform",state="idle"}`)
	require.Contains(t, body, `bscheme_db_pool_connections{db_role="target",state="active"} 3`)
	require.Contains(t, body, `bscheme_db_pool_connections{db_role="target",state="idle"} 4`)
	require.Contains(t, body, `bscheme_db_pool_wait_count_total{db_role="platform"}`)
	require.Contains(t, body, `bscheme_db_pool_wait_count_total{db_role="target"} 1`)
	require.Contains(t, body, `bscheme_db_pool_wait_duration_seconds_total{db_role="platform"}`)
	require.Contains(t, body, `bscheme_db_pool_wait_duration_seconds_total{db_role="target"} 0.045`)

	// Bounded counters without traffic in this run should still be seeded at 0 so
	// Prometheus rate()/increase() never misses the first real event.
	require.Contains(t, body, `bscheme_auth_logins_total{outcome="success"} 1`)
	require.Contains(t, body, `bscheme_auth_logins_total{outcome="invalid_credentials"} 0`)
	require.Contains(t, body, `bscheme_auth_logins_total{outcome="user_disabled"} 0`)
	require.Contains(t, body, `bscheme_auth_logins_total{outcome="error"} 0`)
	require.Contains(t, body, `bscheme_migration_executions_total{status="failed",type="up"} 0`)
	require.Contains(t, body, `bscheme_migration_worker_messages_total{outcome="nack"} 0`)
	require.Contains(t, body, `bscheme_migration_worker_messages_total{outcome="reject"} 0`)
	require.Contains(t, body, `bscheme_viewer_operations_total{operation="table_data",status="error"} 0`)

	// Enforce bounded-label cardinality guard: forbid user/job/draft/SQL labels in metrics.
	for _, forbiddenLabel := range []string{"user_id=", "execution_id=", "job_id=", "draft_id=", "sql="} {
		require.NotContains(t, body, forbiddenLabel, "forbidden high-cardinality label %s found in /metrics", forbiddenLabel)
	}

	var excerpt []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "bscheme_") && !strings.Contains(line, "_bucket{") {
			excerpt = append(excerpt, line)
		}
	}
	t.Logf("PROMETHEUS_EXCERPT:\n%s", strings.Join(excerpt, "\n"))
}
