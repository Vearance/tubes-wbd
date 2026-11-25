package migration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeSQLForLogRedactsValuesAndPreservesParameters(t *testing.T) {
	got := safeSQLForLog("UPDATE users SET email = 'private@example.com' WHERE id = $1 AND score > 10 -- private note")

	require.Equal(t, "UPDATE users SET email = '[REDACTED]' WHERE id = $1 AND score > [NUMBER] --[REDACTED]", got)
}

func TestSafeSQLForLogRedactsDollarQuotedValues(t *testing.T) {
	got := safeSQLForLog("DO $$BEGIN RAISE NOTICE 'secret'; END$$")
	require.NotContains(t, got, "secret")
}
