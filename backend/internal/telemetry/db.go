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
	ActivePools  int64
	ActiveConns  int64
	IdleConns    int64
	TotalConns   int64
	MaxConns     int64
	WaitCount    int64
	WaitDuration time.Duration
}

// RegisterDBPoolStats registers observable gauges and counters for both Platform DB
// (database/sql) and Target DB (pgxpool) connection pools using a single callback registration.
func (p *Provider) RegisterDBPoolStats(sqlDB *sql.DB, targetFn func() TargetPoolStats) error {
	resetDBPeakActive()

	poolConns, err := p.meter.Int64ObservableGauge(
		"bscheme_db_pool_connections",
		metric.WithDescription("Database connection pool count by role and state"),
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

	targetPoolsActive, err := p.meter.Int64ObservableGauge(
		"bscheme_targetdb_pools_active",
		metric.WithDescription("Number of open target database connection pools"),
	)
	if err != nil {
		return fmt.Errorf("creating bscheme_targetdb_pools_active gauge: %w", err)
	}

	_, err = p.meter.RegisterCallback(func(_ context.Context, obs metric.Observer) error {
		if sqlDB != nil {
			st := sqlDB.Stats()
			role := attribute.String("db_role", "platform")
			obs.ObserveInt64(poolConns, consumePeakActive(int64(st.InUse), &platformInFlight, &platformPeakActive), metric.WithAttributes(role, attribute.String("state", "active")))
			obs.ObserveInt64(poolConns, int64(st.Idle), metric.WithAttributes(role, attribute.String("state", "idle")))
			obs.ObserveInt64(poolConns, int64(st.OpenConnections), metric.WithAttributes(role, attribute.String("state", "open")))
			obs.ObserveInt64(poolConns, int64(st.MaxOpenConnections), metric.WithAttributes(role, attribute.String("state", "max_open")))
			obs.ObserveInt64(poolWaitCount, st.WaitCount, metric.WithAttributes(role))
			obs.ObserveFloat64(poolWaitDuration, st.WaitDuration.Seconds(), metric.WithAttributes(role))
		}
		if targetFn != nil {
			ts := targetFn()
			role := attribute.String("db_role", "target")
			obs.ObserveInt64(poolConns, consumePeakActive(ts.ActiveConns, &targetInFlight, &targetPeakActive), metric.WithAttributes(role, attribute.String("state", "active")))
			obs.ObserveInt64(poolConns, ts.IdleConns, metric.WithAttributes(role, attribute.String("state", "idle")))
			obs.ObserveInt64(poolConns, ts.TotalConns, metric.WithAttributes(role, attribute.String("state", "total")))
			obs.ObserveInt64(poolConns, ts.MaxConns, metric.WithAttributes(role, attribute.String("state", "max_conns")))
			obs.ObserveInt64(poolWaitCount, ts.WaitCount, metric.WithAttributes(role))
			obs.ObserveFloat64(poolWaitDuration, ts.WaitDuration.Seconds(), metric.WithAttributes(role))
			obs.ObserveInt64(targetPoolsActive, ts.ActivePools)
		}
		return nil
	}, poolConns, poolWaitCount, poolWaitDuration, targetPoolsActive)
	if err != nil {
		return fmt.Errorf("registering database pool stats callback: %w", err)
	}
	return nil
}

// RunGORMTransaction executes fn inside a Platform DB GORM transaction while emitting
// an OpenTelemetry transaction span and explicit sql.tx.started / sql.tx.commit / sql.tx.rollback logs.
func RunGORMTransaction(ctx context.Context, db *gorm.DB, txName string, fn func(*gorm.DB) error) error {
	skipNormal := SkipDBTelemetry(ctx)
	spanCtx := ctx
	var span trace.Span
	if !skipNormal {
		attrs := []attribute.KeyValue{
			attribute.String("db.system.name", "postgresql"),
			attribute.String("db.role", "platform"),
			attribute.String("db.tx_name", txName),
		}
		attrs = appendMigrationSpanAttrs(attrs, MigrationMetaFromContext(ctx))

		spanCtx, span = Tracer("tubes-wbd/platformdb").Start(ctx, "db.platform.tx",
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(attrs...),
		)
		defer span.End()

		slog.InfoContext(spanCtx, "database transaction started",
			"event", "sql.tx.started",
			"database", "platform",
			"tx_name", txName,
			"status", "started",
		)
	}

	start := time.Now()
	err := db.WithContext(spanCtx).Transaction(fn)
	durationMs := RoundMillis(time.Since(start))

	if err != nil {
		sqlstate := SQLState(err)
		if span != nil {
			if sqlstate != "" {
				span.SetAttributes(attribute.String("db.response.status_code", sqlstate))
			}
			span.RecordError(err)
			span.SetStatus(codes.Error, SanitizeError(err))
		}
		logAttrs := []any{
			"event", "sql.tx.rollback",
			"database", "platform",
			"tx_name", txName,
			"status", "rolled_back",
			"duration_ms", durationMs,
			"error", err,
		}
		if sqlstate != "" {
			logAttrs = append(logAttrs, "sqlstate", sqlstate)
		}
		slog.WarnContext(spanCtx, "database transaction rolled back", logAttrs...)
		return err
	}

	if !skipNormal {
		slog.InfoContext(spanCtx, "database transaction committed",
			"event", "sql.tx.commit",
			"database", "platform",
			"tx_name", txName,
			"status", "committed",
			"duration_ms", durationMs,
		)
	}
	return nil
}

const gormSpanInstanceKey = "telemetry:gorm_state"

type gormQueryState struct {
	start     time.Time
	operation string
	span      trace.Span
	parentCtx context.Context
}

type gormTelemetryPlugin struct {
	tracer trace.Tracer
}

// RegisterGORMPlugin attaches span and metric hooks to a GORM Platform DB instance.
func RegisterGORMPlugin(db *gorm.DB) error {
	if db == nil {
		return errors.New("telemetry: gorm db is nil")
	}
	return db.Use(&gormTelemetryPlugin{tracer: Tracer("tubes-wbd/platformdb")})
}

func (p *gormTelemetryPlugin) Name() string { return "telemetry_gorm" }

func (p *gormTelemetryPlugin) Initialize(db *gorm.DB) error {
	ops := []struct {
		name  string
		op    string
		setup func(*gorm.DB, string) error
	}{
		{"create", "insert", func(d *gorm.DB, op string) error {
			if err := d.Callback().Create().Before("gorm:create").Register("telemetry:before_create", p.before(op)); err != nil {
				return err
			}
			return d.Callback().Create().After("gorm:create").Register("telemetry:after_create", p.after())
		}},
		{"query", "select", func(d *gorm.DB, op string) error {
			if err := d.Callback().Query().Before("gorm:query").Register("telemetry:before_query", p.before(op)); err != nil {
				return err
			}
			return d.Callback().Query().After("gorm:query").Register("telemetry:after_query", p.after())
		}},
		{"update", "update", func(d *gorm.DB, op string) error {
			if err := d.Callback().Update().Before("gorm:update").Register("telemetry:before_update", p.before(op)); err != nil {
				return err
			}
			return d.Callback().Update().After("gorm:update").Register("telemetry:after_update", p.after())
		}},
		{"delete", "delete", func(d *gorm.DB, op string) error {
			if err := d.Callback().Delete().Before("gorm:delete").Register("telemetry:before_delete", p.before(op)); err != nil {
				return err
			}
			return d.Callback().Delete().After("gorm:delete").Register("telemetry:after_delete", p.after())
		}},
		{"row", "row", func(d *gorm.DB, op string) error {
			if err := d.Callback().Row().Before("gorm:row").Register("telemetry:before_row", p.before(op)); err != nil {
				return err
			}
			return d.Callback().Row().After("gorm:row").Register("telemetry:after_row", p.after())
		}},
		{"raw", "raw", func(d *gorm.DB, op string) error {
			if err := d.Callback().Raw().Before("gorm:raw").Register("telemetry:before_raw", p.before(op)); err != nil {
				return err
			}
			return d.Callback().Raw().After("gorm:raw").Register("telemetry:after_raw", p.after())
		}},
	}

	for _, item := range ops {
		if err := item.setup(db, item.op); err != nil {
			return fmt.Errorf("registering gorm telemetry %s callbacks: %w", item.name, err)
		}
	}
	return nil
}

func (p *gormTelemetryPlugin) before(operation string) func(*gorm.DB) {
	return func(tx *gorm.DB) {
		if tx == nil || tx.Statement == nil {
			return
		}
		parentCtx := tx.Statement.Context
		if parentCtx == nil {
			parentCtx = context.Background()
		}

		if SkipDBTelemetry(parentCtx) {
			tx.InstanceSet(gormSpanInstanceKey, gormQueryState{
				start:     time.Now(),
				operation: operation,
				parentCtx: parentCtx,
			})
			return
		}

		recordQueryActiveStart(&platformInFlight, &platformPeakActive)

		attrs := []attribute.KeyValue{
			attribute.String("db.system.name", "postgresql"),
			attribute.String("db.role", "platform"),
			attribute.String("db.operation.name", operation),
		}
		if tx.Statement.Table != "" {
			attrs = append(attrs, attribute.String("db.collection.name", tx.Statement.Table))
		}
		attrs = appendMigrationSpanAttrs(attrs, MigrationMetaFromContext(parentCtx))

		spanCtx, span := p.tracer.Start(parentCtx, "db.platform."+operation,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(attrs...),
		)
		tx.Statement.Context = spanCtx
		tx.InstanceSet(gormSpanInstanceKey, gormQueryState{
			start:     time.Now(),
			operation: operation,
			span:      span,
			parentCtx: parentCtx,
		})

		startAttrs := []any{
			"event", "sql.query.started",
			"database", "platform",
			"operation", operation,
			"status", "started",
		}
		if tx.Statement.Table != "" {
			startAttrs = append(startAttrs, "table", tx.Statement.Table)
		}
		slog.InfoContext(spanCtx, "sql query started", startAttrs...)
	}
}

func (p *gormTelemetryPlugin) after() func(*gorm.DB) {
	return func(tx *gorm.DB) {
		if tx == nil || tx.Statement == nil {
			return
		}
		rawState, ok := tx.InstanceGet(gormSpanInstanceKey)
		if !ok {
			return
		}
		state, ok := rawState.(gormQueryState)
		if !ok {
			return
		}
		defer func() {
			if !SkipDBTelemetry(state.parentCtx) {
				recordQueryActiveEnd(&platformInFlight)
			}
			if state.span != nil {
				state.span.End()
			}
			tx.Statement.Context = state.parentCtx
		}()

		ctx := tx.Statement.Context
		if ctx == nil {
			ctx = state.parentCtx
		}
		duration := time.Since(state.start)
		durationMs := RoundMillis(duration)
		sqlstate := SQLState(tx.Error)
		isErr := tx.Error != nil && !errors.Is(tx.Error, gorm.ErrRecordNotFound)

		if state.span != nil {
			state.span.SetAttributes(attribute.Int64("db.rows_affected", tx.RowsAffected))
			if isErr {
				if sqlstate != "" {
					state.span.SetAttributes(attribute.String("db.response.status_code", sqlstate))
				}
				state.span.RecordError(tx.Error)
				state.span.SetStatus(codes.Error, SanitizeError(tx.Error))
			}
		}

		metricStatus := "ok"
		if isErr {
			metricStatus = "error"
		}
		Metrics().RecordDBQuery(ctx, "platform", state.operation, metricStatus, duration)

		if SkipDBTelemetry(state.parentCtx) && !isErr {
			return
		}

		rawSQL := tx.Statement.SQL.String()
		sanitizedSQL := SanitizeSQL(rawSQL)
		sanitizedParams := SanitizeSQLParams(rawSQL, tx.Statement.Vars)

		logAttrs := []any{
			"database", "platform",
			"operation", state.operation,
			"query", sanitizedSQL,
			"duration_ms", durationMs,
			"rows_affected", tx.RowsAffected,
		}
		if len(sanitizedParams) > 0 {
			logAttrs = append(logAttrs, "parameters", sanitizedParams)
		}
		if isErr {
			logAttrs = append(logAttrs, "event", "sql.query.failed", "status", "error", "error", tx.Error)
			if sqlstate != "" {
				logAttrs = append(logAttrs, "sqlstate", sqlstate)
			}
			slog.ErrorContext(ctx, "sql query failed", logAttrs...)
			return
		}

		logAttrs = append(logAttrs, "event", "sql.query.completed", "status", "ok")
		slog.InfoContext(ctx, "sql query completed", logAttrs...)
	}
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

// NewPGXTracer creates a pgx.QueryTracer for target databases that emits client spans,
// separate query/transaction structured logs, SQLSTATE on errors, and bounded-verb metrics
// without ever placing raw SQL literals, credentials, or session values into logs or metrics.
func NewPGXTracer(databaseID uint) pgx.QueryTracer {
	return &pgxQueryTracer{
		databaseID: databaseID,
		tracer:     Tracer("tubes-wbd/targetdb"),
	}
}

func (t *pgxQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	op := classifySQLVerb(data.SQL)
	txAction := classifyTxAction(data.SQL)
	sanitizedSQL := SanitizeSQL(data.SQL)
	sanitizedParams := SanitizeSQLParams(data.SQL, data.Args)

	if SkipDBTelemetry(ctx) {
		return context.WithValue(ctx, pgxTraceCtxKey{}, pgxQueryState{
			start:           time.Now(),
			op:              op,
			txAction:        txAction,
			sanitizedSQL:    sanitizedSQL,
			sanitizedParams: sanitizedParams,
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
	attrs = appendMigrationSpanAttrs(attrs, MigrationMetaFromContext(ctx))

	spanCtx, span := t.tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)

	switch txAction {
	case "begin":
		slog.InfoContext(spanCtx, "database transaction started",
			"event", "sql.tx.started",
			"database", dbLabel,
			"status", "started",
			"query", sanitizedSQL,
		)
	case "":
		logAttrs := []any{
			"event", "sql.query.started",
			"database", dbLabel,
			"operation", op,
			"status", "started",
			"query", sanitizedSQL,
		}
		if len(sanitizedParams) > 0 {
			logAttrs = append(logAttrs, "parameters", sanitizedParams)
		}
		slog.InfoContext(spanCtx, "sql query started", logAttrs...)
	}

	return context.WithValue(spanCtx, pgxTraceCtxKey{}, pgxQueryState{
		start:           time.Now(),
		op:              op,
		txAction:        txAction,
		sanitizedSQL:    sanitizedSQL,
		sanitizedParams: sanitizedParams,
		span:            span,
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

	duration := time.Since(state.start)
	durationMs := RoundMillis(duration)
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

	metricStatus := "ok"
	if isErr {
		metricStatus = "error"
	}
	Metrics().RecordDBQuery(ctx, "target", state.op, metricStatus, duration)

	if SkipDBTelemetry(ctx) && !isErr {
		return
	}

	dbLabel := TargetDatabaseLabel(t.databaseID)
	switch state.txAction {
	case "commit", "rollback":
		event := "sql.tx." + state.txAction
		okStatus := "committed"
		okMsg := "database transaction committed"
		if state.txAction == "rollback" {
			okStatus = "rolled_back"
			okMsg = "database transaction rolled back"
		}
		if isErr {
			logAttrs := []any{
				"event", event,
				"database", dbLabel,
				"status", "error",
				"query", state.sanitizedSQL,
				"duration_ms", durationMs,
				"error", data.Err,
			}
			if sqlstate != "" {
				logAttrs = append(logAttrs, "sqlstate", sqlstate)
			}
			slog.ErrorContext(ctx, "database transaction "+state.txAction+" failed", logAttrs...)
			return
		}
		slog.InfoContext(ctx, okMsg,
			"event", event,
			"database", dbLabel,
			"status", okStatus,
			"query", state.sanitizedSQL,
			"duration_ms", durationMs,
		)
	case "begin":
		if isErr {
			logAttrs := []any{
				"event", "sql.tx.failed",
				"database", dbLabel,
				"status", "error",
				"query", state.sanitizedSQL,
				"duration_ms", durationMs,
				"error", data.Err,
			}
			if sqlstate != "" {
				logAttrs = append(logAttrs, "sqlstate", sqlstate)
			}
			slog.ErrorContext(ctx, "database transaction begin failed", logAttrs...)
		}
	default:
		logAttrs := []any{
			"database", dbLabel,
			"operation", state.op,
			"query", state.sanitizedSQL,
			"duration_ms", durationMs,
		}
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
		logAttrs = append(logAttrs, "event", "sql.query.completed", "status", "ok")
		slog.InfoContext(ctx, "sql query completed", logAttrs...)
	}
}

func appendMigrationSpanAttrs(attrs []attribute.KeyValue, meta MigrationLogMeta) []attribute.KeyValue {
	if meta.ExecutionID > 0 {
		attrs = append(attrs, attribute.Int64("migration.execution_id", int64(meta.ExecutionID)))
	}
	if meta.Attempt > 0 {
		attrs = append(attrs, attribute.Int("migration.attempt", meta.Attempt))
	}
	if meta.BatchNumber > 0 {
		attrs = append(attrs, attribute.Int("migration.batch_number", meta.BatchNumber))
	}
	return attrs
}

func classifyTxAction(rawSQL string) string {
	s := stripLeadingSQLComments(rawSQL)
	if s == "" {
		return ""
	}
	fields := strings.Fields(strings.ToLower(s))
	if len(fields) == 0 {
		return ""
	}
	switch strings.Trim(fields[0], ";(") {
	case "begin":
		return "begin"
	case "commit":
		return "commit"
	case "rollback":
		return "rollback"
	default:
		return ""
	}
}

func classifySQLVerb(rawSQL string) string {
	s := stripLeadingSQLComments(rawSQL)
	if s == "" {
		return "other"
	}
	fields := strings.Fields(strings.ToLower(s))
	if len(fields) == 0 {
		return "other"
	}
	switch strings.Trim(fields[0], ";(") {
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

func stripLeadingSQLComments(s string) string {
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
			return s
		}
	}
}
