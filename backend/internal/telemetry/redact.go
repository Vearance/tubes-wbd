package telemetry

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maxLoggedSQLBytes   = 4096
	maxLoggedParamRunes = 120
	maxArrayParamShow   = 10
)

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

// SanitizeSQL keeps query shapes debuggable while masking quoted literals, SQL
// comments, dollar-quoted blocks, unquoted DDL passwords, and numeric constants,
// preserving positional $1/$2 bind placeholders.
func SanitizeSQL(rawSQL string) string {
	s := sqlLineCommentPattern.ReplaceAllString(rawSQL, "--[REDACTED]")
	s = sqlBlockCommentPattern.ReplaceAllString(s, "/*[REDACTED]*/")
	s = sqlStringLiteralPattern.ReplaceAllString(s, "'[REDACTED]'")
	s = sqlDollarLiteralPattern.ReplaceAllString(s, "$tag$[REDACTED]$tag$")
	s = sqlDDLPasswordPattern.ReplaceAllString(s, "PASSWORD '[REDACTED]'")
	s = sqlNumberPattern.ReplaceAllStringFunc(s, func(value string) string {
		if len(value) > 0 && value[0] == '$' {
			return value
		}
		return "[NUMBER]"
	})
	s = strings.TrimSpace(s)
	if len(s) > maxLoggedSQLBytes {
		s = strings.TrimSpace(s[:maxLoggedSQLBytes]) + "... [truncated]"
	}
	return s
}

// SanitizeSQLParams formats query parameters safely for structured logs, masking
// session tables, password/token/credential columns, secret-like values, and raw bytes.
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
			continue
		}
		out[i] = sanitizeParamValue(arg, touchSensitive)
	}
	return out
}

func sanitizeParamValue(v any, sensitiveQuery bool) any {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case string:
		if sensitiveQuery || looksLikeSecretValue(val) {
			return "[REDACTED]"
		}
		return truncateRunes(ScrubString(val), maxLoggedParamRunes)
	case []byte:
		return fmt.Sprintf("[BYTES len=%d]", len(val))
	case []int64:
		if len(val) <= maxArrayParamShow {
			cp := make([]int64, len(val))
			copy(cp, val)
			return cp
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
		if sensitiveQuery || looksLikeSecretValue(s) {
			return "[REDACTED]"
		}
		return truncateRunes(ScrubString(s), maxLoggedParamRunes)
	default:
		if sensitiveQuery {
			return "[REDACTED]"
		}
		return fmt.Sprintf("[%T]", v)
	}
}

func looksLikeSecretValue(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "$2a$") ||
		strings.HasPrefix(trimmed, "$2b$") ||
		strings.HasPrefix(trimmed, "$2y$") ||
		strings.HasPrefix(trimmed, "eyJ") ||
		strings.HasPrefix(strings.ToLower(trimmed), "bearer ") ||
		strings.Contains(strings.ToLower(trimmed), "labpro_sucks=") ||
		hexSecretPattern.MatchString(trimmed) {
		return true
	}
	return false
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "...[truncated]"
}

// ScrubString redacts inline URL passwords, key=value credentials, and Bearer tokens.
func ScrubString(s string) string {
	if s == "" {
		return ""
	}
	s = urlCredentialPattern.ReplaceAllString(s, "${1}[REDACTED]${3}")
	s = authHeaderPattern.ReplaceAllString(s, "${1}[REDACTED]")
	s = bearerTokenPattern.ReplaceAllString(s, "Bearer [REDACTED]")
	s = kvSecretPattern.ReplaceAllString(s, "$1$2[REDACTED]")
	return s
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
	if err == nil {
		return ""
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// IsSensitiveLogKey reports whether an attribute key may hold credentials,
// session identifiers, tokens, cookies, or connection secrets.
func IsSensitiveLogKey(key string) bool {
	lower := strings.ToLower(key)
	for _, needle := range []string{
		"password",
		"passwd",
		"secret",
		"token",
		"cookie",
		"session",
		"authorization",
		"csrf",
		"credential",
		"encryption_key",
		"private_key",
		"api_key",
		"dsn",
		"password_hash",
		"encrypted_password",
		"token_hash",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
