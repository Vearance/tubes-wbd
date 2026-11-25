package migration

import "regexp"

var (
	sqlStringLiteralPattern = regexp.MustCompile(`'(?:''|[^'])*'`)
	sqlLineCommentPattern   = regexp.MustCompile(`--[^\r\n]*`)
	sqlBlockCommentPattern  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	sqlDollarLiteralPattern = regexp.MustCompile(`(?s)\$[A-Za-z_0-9]*\$.*?\$[A-Za-z_0-9]*\$`)
	sqlNumberPattern        = regexp.MustCompile(`(\$[0-9]+)|\b[0-9]+(?:\.[0-9]+)?\b`)
)

// safeSQLForLog keeps query shape useful for debugging without logging quoted
// data literals, comments, or numeric constants that may contain sensitive data.
func safeSQLForLog(sql string) string {
	sql = sqlLineCommentPattern.ReplaceAllString(sql, "--[REDACTED]")
	sql = sqlBlockCommentPattern.ReplaceAllString(sql, "/*[REDACTED]*/")
	sql = sqlStringLiteralPattern.ReplaceAllString(sql, "'[REDACTED]'")
	sql = sqlDollarLiteralPattern.ReplaceAllString(sql, "$tag$[REDACTED]$tag$")
	return sqlNumberPattern.ReplaceAllStringFunc(sql, func(value string) string {
		if len(value) > 0 && value[0] == '$' {
			return value
		}
		return "[NUMBER]"
	})
}
