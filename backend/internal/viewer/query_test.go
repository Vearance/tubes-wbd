package viewer

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"math/big"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"tubes-wbd/internal/access"
	"tubes-wbd/internal/telemetry"
)

func TestQueryLogsStructuredStartAndFailure(t *testing.T) {
	previousLogger := slog.Default()
	defer slog.SetDefault(previousLogger)

	var logs bytes.Buffer
	slog.SetDefault(slog.New(telemetry.NewTraceLogHandler(slog.NewJSONHandler(&logs, nil), "bscheme-api")))

	svc, authz, pools := newTestViewerService(t)
	authz.err = access.ErrInsufficientPermission
	ctx := telemetry.WithCorrelationID(context.Background(), "corr-viewer-query")

	_, err := svc.Query(ctx, 7, 3, "SELECT 'private-value'")

	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	require.Zero(t, pools.calls)
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	require.Len(t, lines, 2)

	var started, failed map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &started))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &failed))
	require.Equal(t, "viewer.query.started", started["event"])
	require.Equal(t, "started", started["status"])
	require.Equal(t, "viewer.query", failed["event"])
	require.Equal(t, "error", failed["status"])
	for _, row := range []map[string]any{started, failed} {
		require.NotEmpty(t, row["time"])
		require.NotEmpty(t, row["level"])
		require.Equal(t, "user:7", row["actor"])
		require.Equal(t, "target:3", row["database"])
		require.Equal(t, "corr-viewer-query", row["correlation_id"])
		require.Equal(t, "SELECT '[REDACTED]'", row["query"])
		require.NotContains(t, row["query"], "private-value")
	}
	require.NotEmpty(t, failed["duration_ms"])
	require.NotEmpty(t, failed["error"])
}

func TestQueryValueTextFormatsNumeric(t *testing.T) {
	value := pgtype.Numeric{
		Int:   big.NewInt(129900),
		Exp:   -2,
		Valid: true,
	}

	formatted := queryValueText(value, 170)

	require.NotNil(t, formatted)
	require.Equal(t, "1299.00", *formatted)
}

func TestValidateReadQuery(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		want    string
		invalid bool
	}{
		{name: "select", sql: "SELECT 1", want: "SELECT 1"},
		{name: "cte", sql: "WITH values AS (SELECT 1) SELECT * FROM values", want: "WITH values AS (SELECT 1) SELECT * FROM values"},
		{name: "leading comment", sql: "-- safe\nSELECT 1", want: "-- safe\nSELECT 1"},
		{name: "trailing semicolon", sql: "SELECT 1;", want: "SELECT 1"},
		{name: "semicolon in literal", sql: "SELECT ';'", want: "SELECT ';'"},
		{name: "semicolon in dollar quote", sql: "SELECT $$;$$", want: "SELECT $$;$$"},
		{name: "trailing comment", sql: "SELECT 1; -- done", want: "SELECT 1 -- done"},
		{name: "nested block comment", sql: "/* outer /* inner */ done */ SELECT 1", want: "/* outer /* inner */ done */ SELECT 1"},
		{name: "multiple statements", sql: "SELECT 1; SELECT 2", invalid: true},
		{name: "insert", sql: "INSERT INTO users VALUES (1)", invalid: true},
		{name: "empty", sql: " /* nothing */ ", invalid: true},
		{name: "unterminated comment", sql: "SELECT 1 /*", invalid: true},
		{name: "unterminated quote", sql: "SELECT 'oops", invalid: true},
		{name: "too large", sql: "SELECT '" + strings.Repeat("a", maxQueryBytes) + "'", invalid: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateReadQuery(tt.sql)
			if tt.invalid {
				require.ErrorIs(t, err, ErrInvalidInput)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
