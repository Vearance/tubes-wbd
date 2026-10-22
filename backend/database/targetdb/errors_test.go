package targetdb

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestMapError(t *testing.T) {
	require.NoError(t, mapError(nil))

	tests := []struct {
		name string
		err  error
		want error
	}{
		{
			name: "context deadline",
			err:  context.DeadlineExceeded,
			want: ErrTimeout,
		},
		{
			name: "network timeout",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded},
			want: ErrTimeout,
		},
		{
			name: "wrong password",
			err:  fmt.Errorf("connect: %w", &pgconn.PgError{Code: "28P01"}),
			want: ErrAuthFailed,
		},
		{
			name: "rejected by pg_hba",
			err:  &pgconn.PgError{Code: "28000"},
			want: ErrAuthFailed,
		},
		{
			name: "connection refused",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
			want: ErrUnreachable,
		},
		{
			name: "database does not exist",
			err:  &pgconn.PgError{Code: "3D000"},
			want: ErrConnectionFailed,
		},
		{
			name: "unknown error",
			err:  errors.New("boom"),
			want: ErrConnectionFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapError(tt.err)
			require.ErrorIs(t, got, tt.want)
			require.ErrorIs(t, got, tt.err, "original error must stay in the chain")
		})
	}
}