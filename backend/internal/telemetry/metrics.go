package telemetry

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// KnownMigrationStatuses defines every bounded execution status exposed via bscheme_migration_jobs.
var KnownMigrationStatuses = []string{
	"pending_review",
	"rejected",
	"approved",
	"queued",
	"running",
	"success",
	"failed",
	"partial",
	"compensation_pending",
	"compensating",
	"compensated",
}

var (
	fastDurationBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	slowDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}
)

var (
	activeMetrics atomic.Pointer[AppMetrics]
	noopOnce      sync.Once
	noopMetrics   *AppMetrics
)

// AppMetrics holds all bounded-label OpenTelemetry metric instruments for BScheme.
// High-cardinality fields (user_id, execution_id, draft_id, SQL) MUST NEVER be used here.
type AppMetrics struct {
	httpRequests       metric.Int64Counter
	httpDuration       metric.Float64Histogram
	httpInFlight       metric.Int64UpDownCounter
	migrationExecs     metric.Int64Counter
	migrationDuration  metric.Float64Histogram
	migrationRetries   metric.Int64Counter
	migrationQueueWait metric.Float64Histogram
	migrationLockWait  metric.Float64Histogram
	migrationBatches   metric.Int64Counter
	migrationRows      metric.Int64Counter
	outboxPublished    metric.Int64Counter
	workerMessages     metric.Int64Counter
	workerInFlight     metric.Int64UpDownCounter
	dbQueries          metric.Int64Counter
	dbQueryDuration    metric.Float64Histogram
	authLogins         metric.Int64Counter
	viewerOps          metric.Int64Counter
}

// Metrics returns the active AppMetrics instance, falling back to a safe noop meter
// if telemetry.Init has not yet been called (such as in isolated unit tests).
func Metrics() *AppMetrics {
	if m := activeMetrics.Load(); m != nil {
		return m
	}
	noopOnce.Do(func() {
		m, _ := newAppMetrics(noop.NewMeterProvider().Meter("tubes-wbd"))
		noopMetrics = m
	})
	return noopMetrics
}

func newAppMetrics(meter metric.Meter) (*AppMetrics, error) {
	httpRequests, err := meter.Int64Counter(
		"bscheme_http_requests",
		metric.WithDescription("Total HTTP requests handled by route and status"),
	)
	if err != nil {
		return nil, err
	}
	httpDuration, err := meter.Float64Histogram(
		"bscheme_http_request_duration",
		metric.WithDescription("HTTP request latency in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(fastDurationBuckets...),
	)
	if err != nil {
		return nil, err
	}
	httpInFlight, err := meter.Int64UpDownCounter(
		"bscheme_http_requests_in_flight",
		metric.WithDescription("Current number of HTTP requests in flight"),
	)
	if err != nil {
		return nil, err
	}

	migrationExecs, err := meter.Int64Counter(
		"bscheme_migration_executions",
		metric.WithDescription("Total terminal migration executions by type and outcome status"),
	)
	if err != nil {
		return nil, err
	}
	migrationDuration, err := meter.Float64Histogram(
		"bscheme_migration_execution_duration",
		metric.WithDescription("Migration execution duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(slowDurationBuckets...),
	)
	if err != nil {
		return nil, err
	}
	migrationRetries, err := meter.Int64Counter(
		"bscheme_migration_retries",
		metric.WithDescription("Total migration execution retry attempts by type and reason"),
	)
	if err != nil {
		return nil, err
	}
	migrationQueueWait, err := meter.Float64Histogram(
		"bscheme_migration_queue_wait_duration",
		metric.WithDescription("Time spent waiting in the migration queue before worker claim in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(slowDurationBuckets...),
	)
	if err != nil {
		return nil, err
	}
	migrationLockWait, err := meter.Float64Histogram(
		"bscheme_migration_lock_wait_duration",
		metric.WithDescription("Time spent acquiring target database migration locks in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(slowDurationBuckets...),
	)
	if err != nil {
		return nil, err
	}
	migrationBatches, err := meter.Int64Counter(
		"bscheme_migration_batches_processed",
		metric.WithDescription("Total committed migration batches processed"),
	)
	if err != nil {
		return nil, err
	}
	migrationRows, err := meter.Int64Counter(
		"bscheme_migration_batch_rows_processed",
		metric.WithDescription("Total rows modified across committed migration batches"),
	)
	if err != nil {
		return nil, err
	}
	outboxPublished, err := meter.Int64Counter(
		"bscheme_migration_outbox_published",
		metric.WithDescription("Total migration outbox publish attempts to RabbitMQ by result"),
	)
	if err != nil {
		return nil, err
	}
	workerMessages, err := meter.Int64Counter(
		"bscheme_migration_worker_messages",
		metric.WithDescription("Total RabbitMQ worker message dispositions by outcome"),
	)
	if err != nil {
		return nil, err
	}
	workerInFlight, err := meter.Int64UpDownCounter(
		"bscheme_migration_worker_in_flight",
		metric.WithDescription("Current number of migration executions running in the worker"),
	)
	if err != nil {
		return nil, err
	}

	dbQueries, err := meter.Int64Counter(
		"bscheme_db_queries",
		metric.WithDescription("Total database queries executed by role, operation verb, and status"),
	)
	if err != nil {
		return nil, err
	}
	dbQueryDuration, err := meter.Float64Histogram(
		"bscheme_db_query_duration",
		metric.WithDescription("Database query latency in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(fastDurationBuckets...),
	)
	if err != nil {
		return nil, err
	}

	authLogins, err := meter.Int64Counter(
		"bscheme_auth_logins",
		metric.WithDescription("Total authentication login attempts by outcome"),
	)
	if err != nil {
		return nil, err
	}
	viewerOps, err := meter.Int64Counter(
		"bscheme_viewer_operations",
		metric.WithDescription("Total read-only database viewer operations by type and status"),
	)
	if err != nil {
		return nil, err
	}

	m := &AppMetrics{
		httpRequests:       httpRequests,
		httpDuration:       httpDuration,
		httpInFlight:       httpInFlight,
		migrationExecs:     migrationExecs,
		migrationDuration:  migrationDuration,
		migrationRetries:   migrationRetries,
		migrationQueueWait: migrationQueueWait,
		migrationLockWait:  migrationLockWait,
		migrationBatches:   migrationBatches,
		migrationRows:      migrationRows,
		outboxPublished:    outboxPublished,
		workerMessages:     workerMessages,
		workerInFlight:     workerInFlight,
		dbQueries:          dbQueries,
		dbQueryDuration:    dbQueryDuration,
		authLogins:         authLogins,
		viewerOps:          viewerOps,
	}
	m.seedZeroCounters(context.Background())
	return m, nil
}

// seedZeroCounters emits an initial zero measurement for small, bounded label enumerations
// so Prometheus records a baseline 0 before the first event and rate()/increase() do not miss it.
func (m *AppMetrics) seedZeroCounters(ctx context.Context) {
	for _, outcome := range []string{"success", "invalid_credentials", "user_disabled", "error"} {
		m.authLogins.Add(ctx, 0, metric.WithAttributes(attribute.String("outcome", outcome)))
	}
	for _, op := range []string{"list_schemas", "list_tables", "describe_table", "table_data"} {
		for _, status := range []string{"ok", "error"} {
			m.viewerOps.Add(ctx, 0, metric.WithAttributes(
				attribute.String("operation", op),
				attribute.String("status", status),
			))
		}
	}

	execTypes := []string{"up", "rollback", "forward"}
	for _, execType := range execTypes {
		for _, status := range []string{"success", "failed", "partial"} {
			m.migrationExecs.Add(ctx, 0, metric.WithAttributes(
				attribute.String("type", execType),
				attribute.String("status", status),
			))
		}
		for _, reason := range []string{"retryable_error", "stale_recovery", "execution_busy"} {
			m.migrationRetries.Add(ctx, 0, metric.WithAttributes(
				attribute.String("type", execType),
				attribute.String("reason", reason),
			))
		}
		typeAttr := metric.WithAttributes(attribute.String("type", execType))
		m.migrationBatches.Add(ctx, 0, typeAttr)
		m.migrationRows.Add(ctx, 0, typeAttr)
		m.workerInFlight.Add(ctx, 0, typeAttr)
	}

	for _, result := range []string{"success", "error"} {
		m.outboxPublished.Add(ctx, 0, metric.WithAttributes(attribute.String("result", result)))
	}
	for _, outcome := range []string{"ack", "nack", "reject"} {
		m.workerMessages.Add(ctx, 0, metric.WithAttributes(attribute.String("outcome", outcome)))
	}

	for _, role := range []string{"platform", "target"} {
		for _, op := range []string{"select", "insert", "update", "delete", "ddl", "lock", "tx", "other"} {
			m.dbQueries.Add(ctx, 0, metric.WithAttributes(
				attribute.String("db_role", role),
				attribute.String("operation", op),
				attribute.String("status", "error"),
			))
		}
	}
}

// AddHTTPInFlight adjusts the current HTTP in-flight counter by delta (+1 or -1).
func (m *AppMetrics) AddHTTPInFlight(ctx context.Context, method string, delta int64) {
	m.httpInFlight.Add(ctx, delta, metric.WithAttributes(
		attribute.String("method", normalizeHTTPMethod(method)),
	))
}

// RecordHTTPRequest records HTTP request count and duration using bounded route and status labels.
func (m *AppMetrics) RecordHTTPRequest(ctx context.Context, method, route string, statusCode int, duration time.Duration) {
	normMethod := normalizeHTTPMethod(method)
	if route == "" {
		route = "unmatched"
	}
	statusClass := statusClassLabel(statusCode)
	statusStr := strconv.Itoa(statusCode)

	m.httpRequests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("method", normMethod),
		attribute.String("route", route),
		attribute.String("status_code", statusStr),
		attribute.String("status_class", statusClass),
	))
	m.httpDuration.Record(ctx, clampDurationSeconds(duration), metric.WithAttributes(
		attribute.String("method", normMethod),
		attribute.String("route", route),
		attribute.String("status_class", statusClass),
	))
}

// AddWorkerInFlight adjusts the active worker execution gauge by delta (+1 or -1).
func (m *AppMetrics) AddWorkerInFlight(ctx context.Context, execType string, delta int64) {
	m.workerInFlight.Add(ctx, delta, metric.WithAttributes(
		attribute.String("type", normalizeExecutionType(execType)),
	))
}

// RecordMigrationExecution records terminal execution count and duration.
func (m *AppMetrics) RecordMigrationExecution(ctx context.Context, execType, status string, duration time.Duration) {
	attrs := metric.WithAttributes(
		attribute.String("type", normalizeExecutionType(execType)),
		attribute.String("status", normalizeTerminalStatus(status)),
	)
	m.migrationExecs.Add(ctx, 1, attrs)
	m.migrationDuration.Record(ctx, clampDurationSeconds(duration), attrs)
}

// RecordMigrationRetry increments the migration retry counter by type and bounded reason.
func (m *AppMetrics) RecordMigrationRetry(ctx context.Context, execType, reason string) {
	m.migrationRetries.Add(ctx, 1, metric.WithAttributes(
		attribute.String("type", normalizeExecutionType(execType)),
		attribute.String("reason", normalizeRetryReason(reason)),
	))
}

// RecordMigrationQueueWait records time spent queued prior to worker execution claim.
func (m *AppMetrics) RecordMigrationQueueWait(ctx context.Context, execType string, wait time.Duration) {
	m.migrationQueueWait.Record(ctx, clampDurationSeconds(wait), metric.WithAttributes(
		attribute.String("type", normalizeExecutionType(execType)),
	))
}

// RecordMigrationLockWait records time spent acquiring advisory or history-table locks.
func (m *AppMetrics) RecordMigrationLockWait(ctx context.Context, execType, lockScope string, wait time.Duration) {
	m.migrationLockWait.Record(ctx, clampDurationSeconds(wait), metric.WithAttributes(
		attribute.String("type", normalizeExecutionType(execType)),
		attribute.String("lock_scope", normalizeLockScope(lockScope)),
	))
}

// RecordMigrationBatch increments processed batches (by 1) and processed rows (by rows).
func (m *AppMetrics) RecordMigrationBatch(ctx context.Context, execType string, rows int64) {
	attrs := metric.WithAttributes(
		attribute.String("type", normalizeExecutionType(execType)),
	)
	m.migrationBatches.Add(ctx, 1, attrs)
	if rows > 0 {
		m.migrationRows.Add(ctx, rows, attrs)
	}
}

// RecordOutboxPublish records whether an outbox publish attempt succeeded or failed.
func (m *AppMetrics) RecordOutboxPublish(ctx context.Context, result string) {
	if result != "success" {
		result = "error"
	}
	m.outboxPublished.Add(ctx, 1, metric.WithAttributes(
		attribute.String("result", result),
	))
}

// RecordWorkerMessage records RabbitMQ message disposition outcomes (ack, nack, reject).
func (m *AppMetrics) RecordWorkerMessage(ctx context.Context, outcome string) {
	switch outcome {
	case "ack", "nack", "reject":
	default:
		outcome = "error"
	}
	m.workerMessages.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
	))
}

// RecordDBQuery records database query count and latency with bounded role and SQL verb labels.
func (m *AppMetrics) RecordDBQuery(ctx context.Context, role, op, status string, duration time.Duration) {
	if role != "platform" && role != "target" {
		role = "other"
	}
	if op == "" {
		op = "other"
	}
	if status != "ok" {
		status = "error"
	}
	m.dbQueries.Add(ctx, 1, metric.WithAttributes(
		attribute.String("db_role", role),
		attribute.String("operation", op),
		attribute.String("status", status),
	))
	m.dbQueryDuration.Record(ctx, clampDurationSeconds(duration), metric.WithAttributes(
		attribute.String("db_role", role),
		attribute.String("operation", op),
	))
}

// RecordAuthLogin increments authentication login counts by outcome.
func (m *AppMetrics) RecordAuthLogin(ctx context.Context, outcome string) {
	switch outcome {
	case "success", "invalid_credentials", "user_disabled", "error":
	default:
		outcome = "error"
	}
	m.authLogins.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
	))
}

// RecordViewerOperation increments viewer operation counts by operation name and status.
func (m *AppMetrics) RecordViewerOperation(ctx context.Context, operation, status string) {
	switch operation {
	case "list_schemas", "list_tables", "describe_table", "table_data":
	default:
		operation = "other"
	}
	if status != "ok" {
		status = "error"
	}
	m.viewerOps.Add(ctx, 1, metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.String("status", status),
	))
}

func clampDurationSeconds(d time.Duration) float64 {
	if d < 0 {
		return 0
	}
	return d.Seconds()
}

func normalizeHTTPMethod(method string) string {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return method
	default:
		return "OTHER"
	}
}

func statusClassLabel(statusCode int) string {
	switch {
	case statusCode >= 500:
		return "5xx"
	case statusCode >= 400:
		return "4xx"
	case statusCode >= 300:
		return "3xx"
	default:
		return "2xx"
	}
}

func normalizeExecutionType(execType string) string {
	switch execType {
	case "up", "rollback", "forward", "unknown":
		return execType
	default:
		return "unknown"
	}
}

func normalizeTerminalStatus(status string) string {
	switch status {
	case "success", "failed", "partial":
		return status
	default:
		return "failed"
	}
}

func normalizeRetryReason(reason string) string {
	switch reason {
	case "retryable_error", "stale_recovery", "execution_busy":
		return reason
	default:
		return "other"
	}
}

func normalizeLockScope(scope string) string {
	switch scope {
	case "advisory", "history_table":
		return scope
	default:
		return "other"
	}
}
