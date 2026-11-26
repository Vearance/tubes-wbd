package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// TraceLogHandler wraps a base slog.Handler and enriches every record with:
//   - scrubbed attributes (masking credentials, tokens, sessions, and DSN secrets)
//   - standard context fields (event, actor, database, correlation_id)
//   - active OpenTelemetry trace_id and span_id
//   - active migration execution/draft/attempt/batch identifiers
type TraceLogHandler struct {
	inner slog.Handler
}

// NewTraceLogHandler wraps inner with trace/correlation injection, redaction, and service metadata.
func NewTraceLogHandler(inner slog.Handler, serviceName string) slog.Handler {
	if serviceName != "" {
		inner = inner.WithAttrs([]slog.Attr{slog.String("service", serviceName)})
	}
	return &TraceLogHandler{inner: inner}
}

func (h *TraceLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *TraceLogHandler) Handle(ctx context.Context, record slog.Record) error {
	cleaned := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	seen := make(map[string]struct{}, record.NumAttrs()+8)

	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "" {
			return true
		}
		seen[attr.Key] = struct{}{}
		cleaned.AddAttrs(scrubAttr(attr))
		return true
	})

	if _, ok := seen["event"]; !ok && record.Message != "" {
		cleaned.AddAttrs(slog.String("event", record.Message))
	}
	if _, ok := seen["actor"]; !ok {
		actor := ActorFromContext(ctx)
		if actor == "" {
			actor = "system"
		}
		cleaned.AddAttrs(slog.String("actor", actor))
	}
	if _, ok := seen["database"]; !ok {
		if db := DatabaseFromContext(ctx); db != "" {
			cleaned.AddAttrs(slog.String("database", db))
		}
	}

	sc := trace.SpanContextFromContext(ctx)
	if _, ok := seen["correlation_id"]; !ok {
		corrID := CorrelationIDFromContext(ctx)
		if corrID == "" && sc.IsValid() {
			corrID = sc.TraceID().String()
		}
		if corrID != "" {
			cleaned.AddAttrs(slog.String("correlation_id", corrID))
		}
	}

	if sc.IsValid() {
		if _, ok := seen["trace_id"]; !ok {
			cleaned.AddAttrs(slog.String("trace_id", sc.TraceID().String()))
		}
		if _, ok := seen["span_id"]; !ok {
			cleaned.AddAttrs(slog.String("span_id", sc.SpanID().String()))
		}
	}

	meta := MigrationMetaFromContext(ctx)
	if meta.ExecutionID > 0 {
		if _, ok := seen["execution_id"]; !ok {
			cleaned.AddAttrs(slog.Uint64("execution_id", uint64(meta.ExecutionID)))
		}
	}
	if meta.DraftID > 0 {
		if _, ok := seen["draft_id"]; !ok {
			cleaned.AddAttrs(slog.Uint64("draft_id", uint64(meta.DraftID)))
		}
	}
	if meta.Attempt > 0 {
		if _, ok := seen["attempt"]; !ok {
			cleaned.AddAttrs(slog.Int("attempt", meta.Attempt))
		}
	}
	if meta.BatchNumber > 0 {
		if _, ok := seen["batch_number"]; !ok {
			cleaned.AddAttrs(slog.Int("batch_number", meta.BatchNumber))
		}
	}

	return h.inner.Handle(ctx, cleaned)
}

func (h *TraceLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	scrubbed := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		scrubbed[i] = scrubAttr(attr)
	}
	return &TraceLogHandler{inner: h.inner.WithAttrs(scrubbed)}
}

func (h *TraceLogHandler) WithGroup(name string) slog.Handler {
	return &TraceLogHandler{inner: h.inner.WithGroup(name)}
}

func scrubAttr(attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	if IsSensitiveLogKey(attr.Key) {
		return slog.String(attr.Key, "[REDACTED]")
	}

	switch attr.Value.Kind() {
	case slog.KindString:
		s := attr.Value.String()
		if looksLikeSecretValue(s) {
			return slog.String(attr.Key, "[REDACTED]")
		}
		return slog.String(attr.Key, ScrubString(s))
	case slog.KindGroup:
		group := attr.Value.Group()
		next := make([]slog.Attr, len(group))
		for i, item := range group {
			next[i] = scrubAttr(item)
		}
		return slog.Attr{Key: attr.Key, Value: slog.GroupValue(next...)}
	case slog.KindAny:
		anyVal := attr.Value.Any()
		if err, ok := anyVal.(error); ok && err != nil {
			return slog.String(attr.Key, SanitizeError(err))
		}
		if anyVal != nil {
			if s, ok := anyVal.(fmt.Stringer); ok {
				str := s.String()
				if looksLikeSecretValue(str) {
					return slog.String(attr.Key, "[REDACTED]")
				}
				return slog.String(attr.Key, ScrubString(str))
			}
		}
	}
	return attr
}

// MultiLogHandler fans out structured log records to multiple slog.Handler destinations
// (such as stdout JSON logging and the OpenTelemetry OTLP logs bridge).
type MultiLogHandler struct {
	handlers []slog.Handler
}

// NewMultiLogHandler constructs a fan-out slog.Handler, ignoring nil entries.
func NewMultiLogHandler(handlers ...slog.Handler) slog.Handler {
	filtered := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			filtered = append(filtered, h)
		}
	}
	return &MultiLogHandler{handlers: filtered}
}

func (m *MultiLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *MultiLogHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs error
	for _, h := range m.handlers {
		if !h.Enabled(ctx, record.Level) {
			continue
		}
		if err := h.Handle(ctx, record.Clone()); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

func (m *MultiLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return &MultiLogHandler{handlers: next}
}

func (m *MultiLogHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		next[i] = h.WithGroup(name)
	}
	return &MultiLogHandler{handlers: next}
}
