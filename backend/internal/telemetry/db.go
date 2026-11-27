package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

var (
	platformInFlight   atomic.Int64
	platformPeakActive atomic.Int64
	targetInFlight     atomic.Int64
	targetPeakActive   atomic.Int64
)

func resetDBPeakActive() {
	platformInFlight.Store(0)
	platformPeakActive.Store(0)
	targetInFlight.Store(0)
	targetPeakActive.Store(0)
}

func recordQueryActiveStart(inFlight, peak *atomic.Int64) {
	cur := inFlight.Add(1)
	for {
		prev := peak.Load()
		if cur <= prev || peak.CompareAndSwap(prev, cur) {
			return
		}
	}
}

func recordQueryActiveEnd(inFlight *atomic.Int64) {
	for {
		cur := inFlight.Load()
		if cur <= 0 || inFlight.CompareAndSwap(cur, cur-1) {
			return
		}
	}
}

func consumePeakActive(current int64, inFlight, peak *atomic.Int64) int64 {
	p := peak.Swap(inFlight.Load())
	if current > p {
		return current
	}
	return p
}

// TargetPoolStats aggregates connection pool statistics across managed target databases.
type TargetPoolStats struct {
	ActiveConns  int64
	IdleConns    int64
	WaitCount    int64
	WaitDuration time.Duration
}

// RegisterDBPoolStats registers observable gauges and counters for both Platform DB
// (database/sql) and Target DB (pgxpool) connection pools.
func (p *Provider) RegisterDBPoolStats(sqlDB *sql.DB, targetFn func() TargetPoolStats) error {
	resetDBPeakActive()

	poolConns, err := p.meter.Int64ObservableGauge(
		"bscheme_db_pool_connections",
		metric.WithDescription("Database connection pool count by role and state (active/idle)"),
	)
	if err != nil {
		return fmt.Errorf("creating bscheme_db_pool_connections gauge: %w", err)
	}
	poolWaitCount, err := p.meter.Int64ObservableCounter(
		"bscheme_db_pool_wait_count",
		metric.WithDescription("Cumulative count of requests waiting for a database connection"),
	)
	if err != nil {
		return fmt.Errorf("creating bscheme_db_pool_wait_count counter: %w", err)
	}
	poolWaitDuration, err := p.meter.Float64ObservableCounter(
		"bscheme_db_pool_wait_duration",
		metric.WithDescription("Cumulative time spent waiting for a database connection in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("creating bscheme_db_pool_wait_duration counter: %w", err)
	}

	_, err = p.meter.RegisterCallback(func(_ context.Context, obs metric.Observer) error {
		if sqlDB != nil {
			st := sqlDB.Stats()
			role := attribute.String("db_role", "platform")
			obs.ObserveInt64(poolConns, consumePeakActive(int64(st.InUse), &platformInFlight, &platformPeakActive), metric.WithAttributes(role, attribute.String("state", "active")))
			obs.ObserveInt64(poolConns, int64(st.Idle), metric.WithAttributes(role, attribute.String("state", "idle")))
			obs.ObserveInt64(poolWaitCount, st.WaitCount, metric.WithAttributes(role))
			obs.ObserveFloat64(poolWaitDuration, st.WaitDuration.Seconds(), metric.WithAttributes(role))
		}
		if targetFn != nil {
			ts := targetFn()
			role := attribute.String("db_role", "target")
			obs.ObserveInt64(poolConns, consumePeakActive(ts.ActiveConns, &targetInFlight, &targetPeakActive), metric.WithAttributes(role, attribute.String("state", "active")))
			obs.ObserveInt64(poolConns, ts.IdleConns, metric.WithAttributes(role, attribute.String("state", "idle")))
			obs.ObserveInt64(poolWaitCount, ts.WaitCount, metric.WithAttributes(role))
			obs.ObserveFloat64(poolWaitDuration, ts.WaitDuration.Seconds(), metric.WithAttributes(role))
		}
		return nil
	}, poolConns, poolWaitCount, poolWaitDuration)
	return err
}

// RunGORMTransaction executes fn inside a Platform DB GORM transaction and logs rollbacks.
func RunGORMTransaction(ctx context.Context, db *gorm.DB, txName string, fn func(*gorm.DB) error) error {
	start := time.Now()
	if err := db.WithContext(ctx).Transaction(fn); err != nil {
		logAttrs := []any{
			"event", "sql.tx.rollback",
			"database", "platform",
			"tx_name", txName,
			"status", "rolled_back",
			"duration_ms", RoundMillis(time.Since(start)),
			"error", err,
		}
		if sqlstate := SQLState(err); sqlstate != "" {
			logAttrs = append(logAttrs, "sqlstate", sqlstate)
		}
		slog.WarnContext(ctx, "database transaction rolled back", logAttrs...)
		return err
	}
	return nil
}

const gormStateKey = "telemetry:gorm_state"

type gormTelemetryPlugin struct{}

// RegisterGORMPlugin attaches lightweight active-connection tracking and error logging to GORM.
func RegisterGORMPlugin(db *gorm.DB) error {
	if db == nil {
		return errors.New("telemetry: gorm db is nil")
	}
	return db.Use(&gormTelemetryPlugin{})
}

func (p *gormTelemetryPlugin) Name() string { return "telemetry_gorm" }

func (p *gormTelemetryPlugin) Initialize(db *gorm.DB) error {
	before := func(tx *gorm.DB) {
		if tx == nil || tx.Statement == nil {
			return
		}
		ctx := tx.Statement.Context
		if ctx == nil {
			ctx = context.Background()
		}
		skip := SkipDBTelemetry(ctx)
		if !skip {
			recordQueryActiveStart(&platformInFlight, &platformPeakActive)
		}
		tx.InstanceSet(gormStateKey, skip)
	}
	after := func(tx *gorm.DB) {
		if tx == nil || tx.Statement == nil {
			return
		}
		if raw, ok := tx.InstanceGet(gormStateKey); ok {
			if skip, _ := raw.(bool); !skip {
				recordQueryActiveEnd(&platformInFlight)
			}
		}
		if tx.Error != nil && !errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			ctx := tx.Statement.Context
			if ctx == nil {
				ctx = context.Background()
			}
			attrs := []any{
				"event", "sql.query.failed",
				"database", "platform",
				"query", SanitizeSQL(tx.Statement.SQL.String()),
				"status", "error",
				"error", tx.Error,
			}
			if sqlstate := SQLState(tx.Error); sqlstate != "" {
				attrs = append(attrs, "sqlstate", sqlstate)
			}
			slog.ErrorContext(ctx, "sql query failed", attrs...)
		}
	}

	for _, reg := range []struct {
		name string
		fn   func(*gorm.DB) error
	}{
		{"create", func(d *gorm.DB) error {
			if err := d.Callback().Create().Before("gorm:create").Register("telemetry:before_create", before); err != nil {
				return err
			}
			return d.Callback().Create().After("gorm:create").Register("telemetry:after_create", after)
		}},
		{"query", func(d *gorm.DB) error {
			if err := d.Callback().Query().Before("gorm:query").Register("telemetry:before_query", before); err != nil {
				return err
			}
			return d.Callback().Query().After("gorm:query").Register("telemetry:after_query", after)
		}},
		{"update", func(d *gorm.DB) error {
			if err := d.Callback().Update().Before("gorm:update").Register("telemetry:before_update", before); err != nil {
				return err
			}
			return d.Callback().Update().After("gorm:update").Register("telemetry:after_update", after)
		}},
		{"delete", func(d *gorm.DB) error {
			if err := d.Callback().Delete().Before("gorm:delete").Register("telemetry:before_delete", before); err != nil {
				return err
			}
			return d.Callback().Delete().After("gorm:delete").Register("telemetry:after_delete", after)
		}},
		{"row", func(d *gorm.DB) error {
			if err := d.Callback().Row().Before("gorm:row").Register("telemetry:before_row", before); err != nil {
				return err
			}
			return d.Callback().Row().After("gorm:row").Register("telemetry:after_row", after)
		}},
		{"raw", func(d *gorm.DB) error {
			if err := d.Callback().Raw().Before("gorm:raw").Register("telemetry:before_raw", before); err != nil {
				return err
			}
			return d.Callback().Raw().After("gorm:raw").Register("telemetry:after_raw", after)
		}},
	} {
		if err := reg.fn(db); err != nil {
			return fmt.Errorf("registering gorm telemetry %s callbacks: %w", reg.name, err)
		}
	}
	return nil
}

type pgxTraceCtxKey struct{}

type pgxQueryState struct {
	start           time.Time
	op              string
	txAction        string
	sanitizedSQL    string
	sanitizedParams []any
	span            trace.Span
}

type pgxQueryTracer struct {
	databaseID uint
	tracer     trace.Tracer
}

// NewPGXTracer creates a pgx.QueryTracer for target databases emitting client spans
// and scrubbed SQL lifecycle structured logs.
func NewPGXTracer(databaseID uint) pgx.QueryTracer {
	return &pgxQueryTracer{databaseID: databaseID, tracer: Tracer("tubes-wbd/targetdb")}
}

func (t *pgxQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	op := classifySQLVerb(data.SQL)
	txAction := classifyTxAction(data.SQL)
	sanitizedSQL := SanitizeSQL(data.SQL)
	sanitizedParams := SanitizeSQLParams(data.SQL, data.Args)

	if SkipDBTelemetry(ctx) {
		return context.WithValue(ctx, pgxTraceCtxKey{}, pgxQueryState{
			start: time.Now(), op: op, txAction: txAction, sanitizedSQL: sanitizedSQL, sanitizedParams: sanitizedParams,
		})
	}

	recordQueryActiveStart(&targetInFlight, &targetPeakActive)
	dbLabel := TargetDatabaseLabel(t.databaseID)
	spanName := "db.target." + op
	if txAction != "" {
		spanName = "db.target.tx." + txAction
	}

	attrs := []attribute.KeyValue{
		attribute.String("db.system.name", "postgresql"),
		attribute.String("db.role", "target"),
		attribute.String("db.operation.name", op),
		attribute.Int64("db.target_database_id", int64(t.databaseID)),
	}
	meta := MigrationMetaFromContext(ctx)
	if meta.ExecutionID > 0 {
		attrs = append(attrs, attribute.Int64("migration.execution_id", int64(meta.ExecutionID)))
	}
	if meta.Attempt > 0 {
		attrs = append(attrs, attribute.Int("migration.attempt", meta.Attempt))
	}
	if meta.BatchNumber > 0 {
		attrs = append(attrs, attribute.Int("migration.batch_number", meta.BatchNumber))
	}

	spanCtx, span := t.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	switch txAction {
	case "begin":
		slog.InfoContext(spanCtx, "database transaction started", "event", "sql.tx.started", "database", dbLabel, "status", "started", "query", sanitizedSQL)
	case "":
		logAttrs := []any{"event", "sql.query.started", "database", dbLabel, "operation", op, "status", "started", "query", sanitizedSQL}
		if len(sanitizedParams) > 0 {
			logAttrs = append(logAttrs, "parameters", sanitizedParams)
		}
		slog.InfoContext(spanCtx, "sql query started", logAttrs...)
	}

	return context.WithValue(spanCtx, pgxTraceCtxKey{}, pgxQueryState{
		start: time.Now(), op: op, txAction: txAction, sanitizedSQL: sanitizedSQL, sanitizedParams: sanitizedParams, span: span,
	})
}

func (t *pgxQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	state, ok := ctx.Value(pgxTraceCtxKey{}).(pgxQueryState)
	if !ok {
		return
	}
	defer func() {
		if !SkipDBTelemetry(ctx) {
			recordQueryActiveEnd(&targetInFlight)
		}
		if state.span != nil {
			state.span.End()
		}
	}()

	durationMs := RoundMillis(time.Since(state.start))
	sqlstate := SQLState(data.Err)
	isErr := data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows)

	tagStr := data.CommandTag.String()
	hasTag := tagStr != ""
	var rowsAffected int64
	if hasTag {
		rowsAffected = data.CommandTag.RowsAffected()
	}

	if state.span != nil {
		if hasTag {
			state.span.SetAttributes(attribute.Int64("db.rows_affected", rowsAffected))
		}
		if isErr {
			if sqlstate != "" {
				state.span.SetAttributes(attribute.String("db.response.status_code", sqlstate))
			}
			state.span.RecordError(data.Err)
			state.span.SetStatus(codes.Error, SanitizeError(data.Err))
		}
	}
	if SkipDBTelemetry(ctx) && !isErr {
		return
	}

	dbLabel := TargetDatabaseLabel(t.databaseID)
	switch state.txAction {
	case "commit", "rollback":
		event, okStatus, okMsg := "sql.tx."+state.txAction, "committed", "database transaction committed"
		if state.txAction == "rollback" {
			okStatus, okMsg = "rolled_back", "database transaction rolled back"
		}
		logAttrs := []any{"event", event, "database", dbLabel, "query", state.sanitizedSQL, "duration_ms", durationMs}
		if isErr {
			logAttrs = append(logAttrs, "status", "error", "error", data.Err)
			if sqlstate != "" {
				logAttrs = append(logAttrs, "sqlstate", sqlstate)
			}
			slog.ErrorContext(ctx, "database transaction "+state.txAction+" failed", logAttrs...)
			return
		}
		slog.InfoContext(ctx, okMsg, append(logAttrs, "status", okStatus)...)
	case "begin":
		if isErr {
			logAttrs := []any{"event", "sql.tx.failed", "database", dbLabel, "status", "error", "query", state.sanitizedSQL, "duration_ms", durationMs, "error", data.Err}
			if sqlstate != "" {
				logAttrs = append(logAttrs, "sqlstate", sqlstate)
			}
			slog.ErrorContext(ctx, "database transaction begin failed", logAttrs...)
		}
	default:
		logAttrs := []any{"database", dbLabel, "operation", state.op, "query", state.sanitizedSQL, "duration_ms", durationMs}
		if len(state.sanitizedParams) > 0 {
			logAttrs = append(logAttrs, "parameters", state.sanitizedParams)
		}
		if hasTag {
			logAttrs = append(logAttrs, "rows_affected", rowsAffected)
		} else if errors.Is(data.Err, pgx.ErrNoRows) {
			logAttrs = append(logAttrs, "rows_affected", int64(0))
		}
		if isErr {
			logAttrs = append(logAttrs, "event", "sql.query.failed", "status", "error", "error", data.Err)
			if sqlstate != "" {
				logAttrs = append(logAttrs, "sqlstate", sqlstate)
			}
			slog.ErrorContext(ctx, "sql query failed", logAttrs...)
			return
		}
		slog.InfoContext(ctx, "sql query completed", append(logAttrs, "event", "sql.query.completed", "status", "ok")...)
	}
}

func classifyTxAction(rawSQL string) string {
	switch firstSQLKeyword(rawSQL) {
	case "begin", "commit", "rollback":
		return firstSQLKeyword(rawSQL)
	default:
		return ""
	}
}

func classifySQLVerb(rawSQL string) string {
	switch firstSQLKeyword(rawSQL) {
	case "select", "with", "explain", "show":
		return "select"
	case "insert":
		return "insert"
	case "update":
		return "update"
	case "delete":
		return "delete"
	case "create", "alter", "drop", "truncate", "comment", "grant", "revoke", "set":
		return "ddl"
	case "lock":
		return "lock"
	case "begin", "commit", "rollback", "savepoint", "release":
		return "tx"
	default:
		return "other"
	}
}

func firstSQLKeyword(s string) string {
	for {
		s = strings.TrimSpace(s)
		switch {
		case strings.HasPrefix(s, "--"):
			idx := strings.IndexByte(s, '\n')
			if idx < 0 {
				return ""
			}
			s = s[idx+1:]
		case strings.HasPrefix(s, "/*"):
			end := strings.Index(s, "*/")
			if end < 0 {
				return ""
			}
			s = s[end+2:]
		default:
			fields := strings.Fields(strings.ToLower(s))
			if len(fields) == 0 {
				return ""
			}
			return strings.Trim(fields[0], ";(")
		}
	}
}
