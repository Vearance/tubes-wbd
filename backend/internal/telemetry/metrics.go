package telemetry

import (
	"context"
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

var slowDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}

var (
	activeMetrics atomic.Pointer[AppMetrics]
	noopOnce      sync.Once
	noopMetrics   *AppMetrics
)

// AppMetrics holds bounded-label OpenTelemetry metric instruments for queue and migration tracking.
// High-cardinality fields (user_id, execution_id, draft_id, SQL) MUST NEVER be used as labels.
type AppMetrics struct {
	migrationExecs     metric.Int64Counter
	migrationDuration  metric.Float64Histogram
	migrationRetries   metric.Int64Counter
	migrationQueueWait metric.Float64Histogram
	migrationLockWait  metric.Float64Histogram
	migrationBatches   metric.Int64Counter
	migrationRows      metric.Int64Counter
}

// Metrics returns the active AppMetrics instance, falling back to a noop meter if Init was not called.
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

	m := &AppMetrics{
		migrationExecs:     migrationExecs,
		migrationDuration:  migrationDuration,
		migrationRetries:   migrationRetries,
		migrationQueueWait: migrationQueueWait,
		migrationLockWait:  migrationLockWait,
		migrationBatches:   migrationBatches,
		migrationRows:      migrationRows,
	}
	m.seedZeroCounters(context.Background())
	return m, nil
}

func (m *AppMetrics) seedZeroCounters(ctx context.Context) {
	for _, execType := range []string{"up", "rollback", "forward"} {
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
	}
}

// RecordMigrationExecution records terminal execution count and duration.
func (m *AppMetrics) RecordMigrationExecution(ctx context.Context, execType, status string, duration time.Duration) {
	attrs := metric.WithAttributes(
		attribute.String("type", normalizeExecutionType(execType)),
		attribute.String("status", normalizeTerminalStatus(status)),
	)
	m.migrationExecs.Add(ctx, 1, attrs)
	m.migrationDuration.Record(ctx, clampSeconds(duration), attrs)
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
	m.migrationQueueWait.Record(ctx, clampSeconds(wait), metric.WithAttributes(
		attribute.String("type", normalizeExecutionType(execType)),
	))
}

// RecordMigrationLockWait records time spent acquiring advisory or history-table locks.
func (m *AppMetrics) RecordMigrationLockWait(ctx context.Context, execType, lockScope string, wait time.Duration) {
	m.migrationLockWait.Record(ctx, clampSeconds(wait), metric.WithAttributes(
		attribute.String("type", normalizeExecutionType(execType)),
		attribute.String("lock_scope", normalizeLockScope(lockScope)),
	))
}

// RecordMigrationBatch increments processed batches (by 1) and processed rows (by rows).
func (m *AppMetrics) RecordMigrationBatch(ctx context.Context, execType string, rows int64) {
	attrs := metric.WithAttributes(attribute.String("type", normalizeExecutionType(execType)))
	m.migrationBatches.Add(ctx, 1, attrs)
	if rows > 0 {
		m.migrationRows.Add(ctx, rows, attrs)
	}
}

func clampSeconds(d time.Duration) float64 {
	if d < 0 {
		return 0
	}
	return d.Seconds()
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
