package viewer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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
