package migration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateDefinition(t *testing.T) {
	tests := []struct {
		name       string
		migration  Migration
		wantError  error
		wantDown   string
		wantIrrev  bool
		wantReason string
	}{
		{
			name:      "down migration",
			migration: Migration{Name: "create table", SQL: "CREATE TABLE items (id bigint)", DownSQL: "DROP TABLE items"},
			wantDown:  "DROP TABLE items",
		},
		{
			name:       "legacy definition defaults irreversible",
			migration:  Migration{Name: "legacy", SQL: "SELECT 1"},
			wantIrrev:  true,
			wantReason: "No down SQL was provided.",
		},
		{
			name:      "explicit irreversible requires reason",
			migration: Migration{Name: "irreversible", SQL: "DELETE FROM items", Irreversible: true},
			wantError: ErrInvalidInput,
		},
		{
			name:      "down and irreversible are mutually exclusive",
			migration: Migration{Name: "ambiguous", SQL: "SELECT 1", DownSQL: "SELECT 2", Irreversible: true, IrreversibleReason: "reason"},
			wantError: ErrInvalidInput,
		},
		{
			name:      "invalid down SQL rejected",
			migration: Migration{Name: "bad down", SQL: "SELECT 1", DownSQL: "COMMIT"},
			wantError: ErrTransactionControl,
		},
		{
			name: "batch migration definition",
			migration: Migration{
				Name:    "backfill email",
				SQL:     "UPDATE users SET email = 'x' WHERE id = ANY($1::bigint[])",
				DownSQL: "UPDATE users SET email = NULL WHERE id = ANY($1::bigint[])",
				Batch:   &BatchDefinition{Schema: "public", Table: "users", KeyColumn: "id", BatchSize: 500},
			},
			wantDown: "UPDATE users SET email = NULL WHERE id = ANY($1::bigint[])",
		},
		{
			name: "batch requires an integer cursor query and bounded batch size",
			migration: Migration{
				Name: "bad batch", SQL: "UPDATE users SET email = 'x' WHERE id = ANY($1::bigint[])",
				DownSQL: "UPDATE users SET email = NULL WHERE id = ANY($1::bigint[])",
				Batch:   &BatchDefinition{Schema: "public", Table: "users; DROP TABLE users", KeyColumn: "id", BatchSize: 500},
			},
			wantError: ErrInvalidInput,
		},
		{
			name: "batch size too large",
			migration: Migration{
				Name: "bad size", SQL: "UPDATE users SET email = 'x' WHERE id = ANY($1::bigint[])",
				DownSQL: "UPDATE users SET email = NULL WHERE id = ANY($1::bigint[])",
				Batch:   &BatchDefinition{Schema: "public", Table: "users", KeyColumn: "id", BatchSize: 10001},
			},
			wantError: ErrInvalidInput,
		},
		{
			name: "batch requires down SQL",
			migration: Migration{
				Name: "no down", SQL: "UPDATE users SET email = 'x' WHERE id = ANY($1::bigint[])",
				Batch: &BatchDefinition{Schema: "public", Table: "users", KeyColumn: "id", BatchSize: 500},
			},
			wantError: ErrInvalidInput,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateDefinition(tt.migration)
			if tt.wantError != nil {
				require.ErrorIs(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantDown, got.DownSQL)
			require.Equal(t, tt.wantIrrev, got.Irreversible)
			require.Equal(t, tt.wantReason, got.IrreversibleReason)
		})
	}
}
