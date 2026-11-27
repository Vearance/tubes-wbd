package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/trace"
)

// TraceLogHandler enriches structured slog records with scrubbed attributes,
// actor/database/correlation_id context, OTel trace_id/span_id, and migration metadata.
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
		if attr.Key != "" {
			seen[attr.Key] = struct{}{}
			cleaned.AddAttrs(scrubAttr(attr))
		}
		return true
	})

	addIfMissing := func(key string, val slog.Attr) {
		if _, ok := seen[key]; !ok {
			cleaned.AddAttrs(val)
		}
	}

	if record.Message != "" {
		addIfMissing("event", slog.String("event", record.Message))
	}
	actor := ActorFromContext(ctx)
	if actor == "" {
		actor = "system"
	}
	addIfMissing("actor", slog.String("actor", actor))
	if db := DatabaseFromContext(ctx); db != "" {
		addIfMissing("database", slog.String("database", db))
	}

	sc := trace.SpanContextFromContext(ctx)
	corrID := CorrelationIDFromContext(ctx)
	if corrID == "" && sc.IsValid() {
		corrID = sc.TraceID().String()
	}
	if corrID != "" {
		addIfMissing("correlation_id", slog.String("correlation_id", corrID))
	}
	if sc.IsValid() {
		addIfMissing("trace_id", slog.String("trace_id", sc.TraceID().String()))
		addIfMissing("span_id", slog.String("span_id", sc.SpanID().String()))
	}

	meta := MigrationMetaFromContext(ctx)
	if meta.ExecutionID > 0 {
		addIfMissing("execution_id", slog.Uint64("execution_id", uint64(meta.ExecutionID)))
	}
	if meta.DraftID > 0 {
		addIfMissing("draft_id", slog.Uint64("draft_id", uint64(meta.DraftID)))
	}
	if meta.Attempt > 0 {
		addIfMissing("attempt", slog.Int("attempt", meta.Attempt))
	}
	if meta.BatchNumber > 0 {
		addIfMissing("batch_number", slog.Int("batch_number", meta.BatchNumber))
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
		val := attr.Value.Any()
		if err, ok := val.(error); ok && err != nil {
			return slog.String(attr.Key, SanitizeError(err))
		}
		if s, ok := val.(fmt.Stringer); ok {
			str := s.String()
			if looksLikeSecretValue(str) {
				return slog.String(attr.Key, "[REDACTED]")
			}
			return slog.String(attr.Key, ScrubString(str))
		}
	}
	return attr
}

// MultiLogHandler fans out structured log records to multiple slog.Handler destinations.
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
		if h.Enabled(ctx, record.Level) {
			errs = errors.Join(errs, h.Handle(ctx, record.Clone()))
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

var (
	sqlStringLiteralPattern = regexp.MustCompile(`'(?:''|[^'])*'`)
	sqlLineCommentPattern   = regexp.MustCompile(`--[^\r\n]*`)
	sqlBlockCommentPattern  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	sqlDollarLiteralPattern = regexp.MustCompile(`(?s)\$[A-Za-z_0-9]*\$.*?\$[A-Za-z_0-9]*\$`)
	sqlNumberPattern        = regexp.MustCompile(`(\$[0-9]+)|\b[0-9]+(?:\.[0-9]+)?\b`)
	sqlDDLPasswordPattern   = regexp.MustCompile(`(?i)\bPASSWORD\s+(?:'\[REDACTED\]'|[^\s;]+)`)

	sessionsTablePattern  = regexp.MustCompile(`(?i)\bsessions\b`)
	sensitiveFieldPattern = regexp.MustCompile(`(?i)\b(password|passwd|password_hash|encrypted_password|token|token_hash|csrf|csrf_token_hash|secret|cookie|credential|api_key|private_key|authorization|managed_databases)\b`)

	urlCredentialPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]+:)([^@\s/]+)(@)`)
	authHeaderPattern    = regexp.MustCompile(`(?i)\b(authorization\s*[:=]\s*)(?:Bearer\s+)?[^\s;,&"']+`)
	kvSecretPattern      = regexp.MustCompile(`(?i)\b(password|passwd|secret|token|csrf|csrf_token|cookie|labpro_sucks|api_key)\b(\s*[:=]\s*)([^\s;,&"']+)`)
	bearerTokenPattern   = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	hexSecretPattern     = regexp.MustCompile(`^[a-fA-F0-9]{32,}$`)
)

// SanitizeSQL keeps query shapes debuggable while masking literals, comments, passwords, and numbers.
func SanitizeSQL(rawSQL string) string {
	s := sqlLineCommentPattern.ReplaceAllString(rawSQL, "--[REDACTED]")
	s = sqlBlockCommentPattern.ReplaceAllString(s, "/*[REDACTED]*/")
	s = sqlStringLiteralPattern.ReplaceAllString(s, "'[REDACTED]'")
	s = sqlDollarLiteralPattern.ReplaceAllString(s, "$tag$[REDACTED]$tag$")
	s = sqlDDLPasswordPattern.ReplaceAllString(s, "PASSWORD '[REDACTED]'")
	s = sqlNumberPattern.ReplaceAllStringFunc(s, func(val string) string {
		if len(val) > 0 && val[0] == '$' {
			return val
		}
		return "[NUMBER]"
	})
	s = strings.TrimSpace(s)
	if len(s) > 4096 {
		s = strings.TrimSpace(s[:4096]) + "... [truncated]"
	}
	return s
}

// SanitizeSQLParams formats query parameters safely, masking sessions and secret-like values.
func SanitizeSQLParams(rawSQL string, args []any) []any {
	if len(args) == 0 {
		return []any{}
	}
	touchSessions := sessionsTablePattern.MatchString(rawSQL)
	touchSensitive := touchSessions || sensitiveFieldPattern.MatchString(rawSQL)
	out := make([]any, len(args))
	for i, arg := range args {
		if touchSessions {
			out[i] = "[REDACTED]"
		} else {
			out[i] = sanitizeParamValue(arg, touchSensitive)
		}
	}
	return out
}

func sanitizeParamValue(v any, sensitive bool) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case string:
		if sensitive || looksLikeSecretValue(val) {
			return "[REDACTED]"
		}
		return truncateRunes(ScrubString(val), 120)
	case []byte:
		return fmt.Sprintf("[BYTES len=%d]", len(val))
	case []int64:
		if len(val) <= 10 {
			return append([]int64(nil), val...)
		}
		return fmt.Sprintf("[int64 len=%d first=%d last=%d]", len(val), val[0], val[len(val)-1])
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		return val
	case time.Time:
		return val.UTC().Format(time.RFC3339Nano)
	case *time.Time:
		if val == nil {
			return nil
		}
		return val.UTC().Format(time.RFC3339Nano)
	case fmt.Stringer:
		s := val.String()
		if sensitive || looksLikeSecretValue(s) {
			return "[REDACTED]"
		}
		return truncateRunes(ScrubString(s), 120)
	default:
		if sensitive {
			return "[REDACTED]"
		}
		return fmt.Sprintf("[%T]", v)
	}
}

func looksLikeSecretValue(s string) bool {
	t := strings.TrimSpace(s)
	lower := strings.ToLower(t)
	return t != "" && (strings.HasPrefix(t, "$2a$") || strings.HasPrefix(t, "$2b$") || strings.HasPrefix(t, "$2y$") ||
		strings.HasPrefix(t, "eyJ") || strings.HasPrefix(lower, "bearer ") ||
		strings.Contains(lower, "labpro_sucks=") || hexSecretPattern.MatchString(t))
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "...[truncated]"
}

// ScrubString redacts inline URL passwords, key=value credentials, and Bearer tokens.
func ScrubString(s string) string {
	if s == "" {
		return ""
	}
	s = urlCredentialPattern.ReplaceAllString(s, "${1}[REDACTED]${3}")
	s = authHeaderPattern.ReplaceAllString(s, "${1}[REDACTED]")
	s = bearerTokenPattern.ReplaceAllString(s, "Bearer [REDACTED]")
	return kvSecretPattern.ReplaceAllString(s, "$1$2[REDACTED]")
}

// SanitizeError formats an error message with embedded credentials or tokens scrubbed.
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	return ScrubString(err.Error())
}

// SQLState extracts the PostgreSQL SQLSTATE code from err when available.
func SQLState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// IsSensitiveLogKey reports whether an attribute key may hold credentials, tokens, or sessions.
func IsSensitiveLogKey(key string) bool {
	lower := strings.ToLower(key)
	for _, needle := range []string{
		"password", "passwd", "secret", "token", "cookie", "session",
		"authorization", "csrf", "credential", "encryption_key", "private_key", "api_key", "dsn",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
