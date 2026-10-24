package migration

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"tubes-wbd/backend/access"
)

func TestValidateExplainSQL(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want error
	}{
		{name: "empty", sql: "", want: ErrInvalidInput},
		{name: "comment only", sql: "-- no query\n/* still no query */", want: ErrInvalidInput},
		{name: "too large", sql: strings.Repeat("x", MaxSQLBytes+1), want: ErrInvalidInput},
		{name: "invalid utf8", sql: string([]byte{'S', 'E', 'L', 'E', 'C', 'T', ' ', 0xff}), want: ErrInvalidInput},
		{name: "nul", sql: "SELECT 1\x00", want: ErrInvalidInput},
		{name: "transaction control", sql: "SELECT 1; COMMIT", want: ErrTransactionControl},
		{name: "nested explain", sql: "EXPLAIN ANALYZE SELECT 1", want: ErrInvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateExplainSQL(tt.sql)
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestValidateExplainSQLSplitsStatementsWithoutSplittingLiteralsOrComments(t *testing.T) {
	sql := "SELECT 'first; statement'; -- semicolon; in a comment\nSELECT 2 /* another ; semicolon */;"

	statements, err := validateExplainSQL(sql)

	require.NoError(t, err)
	require.Equal(t, []string{
		"SELECT 'first; statement'",
		"-- semicolon; in a comment\nSELECT 2 /* another ; semicolon */",
	}, statements)
}

func TestExplainRequiresViewPermissionBeforeConnecting(t *testing.T) {
	svc, authz, pools := newTestService(t)
	authz.err = access.ErrInsufficientPermission

	_, err := svc.Explain(context.Background(), 7, 3, "SELECT 1")

	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	require.Equal(t, 1, authz.calls)
	require.Equal(t, "view", authz.method)
	require.Equal(t, uint(7), authz.userID)
	require.Equal(t, uint(3), authz.dbID)
	require.Zero(t, pools.calls, "target connection must not be opened without permission")
}

func TestExplainRejectsInvalidInputBeforeConnecting(t *testing.T) {
	svc, _, pools := newTestService(t)

	_, err := svc.Explain(context.Background(), 1, 1, "EXPLAIN SELECT 1")

	require.ErrorIs(t, err, ErrInvalidInput)
	require.Zero(t, pools.calls)
}
