package viewer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateReadOnlyQuery(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantQuery string
		wantError error
	}{
		{
			name:      "select query",
			query:     "SELECT id, name FROM public.customers;",
			wantQuery: "SELECT id, name FROM public.customers",
		},
		{
			name:      "with query",
			query:     "-- customer query\nWITH selected AS (SELECT id FROM public.customers) SELECT id FROM selected",
			wantQuery: "-- customer query\nWITH selected AS (SELECT id FROM public.customers) SELECT id FROM selected",
		},
		{
			name:      "empty query",
			query:     "   ",
			wantError: ErrInvalidInput,
		},
		{
			name:      "mutation query",
			query:     "ALTER TABLE public.customers ADD COLUMN phone_number TEXT",
			wantError: ErrReadOnlyQuery,
		},
		{
			name:      "invalid utf8",
			query:     "SELECT '\xff'",
			wantError: ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateReadOnlyQuery(tt.query)
			if tt.wantError != nil {
				require.ErrorIs(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantQuery, got)
		})
	}
}
