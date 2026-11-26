package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// MigrationLogMeta carries execution, draft, attempt, and batch identifiers on
// context so every log line and database span inside a migration run automatically
// includes correlation metadata without requiring callers to repeat it.
type MigrationLogMeta struct {
	ExecutionID   uint
	DraftID       uint
	ExecutionType string
	Attempt       int
	BatchNumber   int
}

// RequestMeta holds mutable per-request actor and target database labels
// populated by inner auth/service layers and read by the outer HTTP access logger.
type RequestMeta struct {
	mu       sync.RWMutex
	actor    string
	database string
}

// SetActor records the resolved request actor (e.g., "user:42").
func (m *RequestMeta) SetActor(actor string) {
	if m == nil || actor == "" {
		return
	}
	m.mu.Lock()
	m.actor = actor
	m.mu.Unlock()
}

// Actor returns the resolved request actor if set.
func (m *RequestMeta) Actor() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	actor := m.actor
	m.mu.RUnlock()
	return actor
}

// SetDatabase records the resolved request target database label (e.g., "target:3").
func (m *RequestMeta) SetDatabase(db string) {
	if m == nil || db == "" {
		return
	}
	m.mu.Lock()
	m.database = db
	m.mu.Unlock()
}

// Database returns the resolved request database label if set.
func (m *RequestMeta) Database() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	db := m.database
	m.mu.RUnlock()
	return db
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

// WithRequestMeta attaches a RequestMeta container onto ctx.
func WithRequestMeta(ctx context.Context, meta *RequestMeta) context.Context {
	if meta == nil {
		return ctx
	}
	return context.WithValue(ctx, requestMetaCtxKey{}, meta)
}

// SetRequestActor updates the enclosing RequestMeta's actor if present in ctx.
func SetRequestActor(ctx context.Context, actor string) {
	if ctx == nil || actor == "" {
		return
	}
	if meta, ok := ctx.Value(requestMetaCtxKey{}).(*RequestMeta); ok && meta != nil {
		meta.SetActor(actor)
	}
}

// SetRequestDatabase updates the enclosing RequestMeta's database if present in ctx.
func SetRequestDatabase(ctx context.Context, db string) {
	if ctx == nil || db == "" {
		return
	}
	if meta, ok := ctx.Value(requestMetaCtxKey{}).(*RequestMeta); ok && meta != nil {
		meta.SetDatabase(db)
	}
}

// WithActor attaches an actor label onto ctx.
func WithActor(ctx context.Context, actor string) context.Context {
	if actor == "" {
		return ctx
	}
	return context.WithValue(ctx, actorCtxKey{}, actor)
}

// ActorFromContext resolves the actor label from ctx or the enclosing RequestMeta.
func ActorFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if actor, ok := ctx.Value(actorCtxKey{}).(string); ok && actor != "" {
		return actor
	}
	if meta, ok := ctx.Value(requestMetaCtxKey{}).(*RequestMeta); ok && meta != nil {
		return meta.Actor()
	}
	return ""
}

// WithDatabase attaches a logical database label ("platform" or "target:<id>") onto ctx.
func WithDatabase(ctx context.Context, db string) context.Context {
	if db == "" {
		return ctx
	}
	return context.WithValue(ctx, databaseCtxKey{}, db)
}

// DatabaseFromContext resolves the database label from ctx or the enclosing RequestMeta.
func DatabaseFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if db, ok := ctx.Value(databaseCtxKey{}).(string); ok && db != "" {
		return db
	}
	if meta, ok := ctx.Value(requestMetaCtxKey{}).(*RequestMeta); ok && meta != nil {
		return meta.Database()
	}
	return ""
}

// WithCorrelationID attaches a correlation ID onto ctx.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, correlationCtxKey{}, id)
}

// CorrelationIDFromContext returns the correlation ID stored in ctx, if any.
func CorrelationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(correlationCtxKey{}).(string); ok {
		return id
	}
	return ""
}

// WithMigrationMeta attaches migration execution metadata onto ctx.
func WithMigrationMeta(ctx context.Context, meta MigrationLogMeta) context.Context {
	return context.WithValue(ctx, migrationMetaCtxKey{}, meta)
}

// WithBatchNumber copies existing MigrationLogMeta in ctx and updates BatchNumber.
func WithBatchNumber(ctx context.Context, batchNumber int) context.Context {
	meta := MigrationMetaFromContext(ctx)
	meta.BatchNumber = batchNumber
	return WithMigrationMeta(ctx, meta)
}

// MigrationMetaFromContext returns the MigrationLogMeta stored in ctx.
func MigrationMetaFromContext(ctx context.Context) MigrationLogMeta {
	if ctx == nil {
		return MigrationLogMeta{}
	}
	meta, _ := ctx.Value(migrationMetaCtxKey{}).(MigrationLogMeta)
	return meta
}

// WithSkipDBTelemetry marks idle background poll contexts so empty polls
// suppress routine SQL logs and spans while still emitting errors and metrics.
func WithSkipDBTelemetry(ctx context.Context) context.Context {
	return context.WithValue(ctx, skipDBTelemetryKey{}, true)
}

// SkipDBTelemetry reports whether routine DB telemetry is suppressed on ctx.
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
