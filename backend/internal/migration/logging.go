package migration

import "tubes-wbd/internal/telemetry"

// safeSQLForLog keeps query shape useful for debugging without logging quoted
// data literals, comments, or numeric constants that may contain sensitive data.
func safeSQLForLog(sql string) string {
	return telemetry.SanitizeSQL(sql)
}
