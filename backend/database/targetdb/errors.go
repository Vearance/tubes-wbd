package targetdb

import (
	"context"
	"errors"
	"fmt"
	"net"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrUnknownDatabase  = errors.New("targetdb: unknown database")
	ErrInvalidConfig    = errors.New("targetdb: invalid connection config")
	ErrAuthFailed       = errors.New("targetdb: authentication failed")
	ErrUnreachable      = errors.New("targetdb: database unreachable")
	ErrTimeout          = errors.New("targetdb: connection timed out")
	ErrConnectionFailed = errors.New("targetdb: connection failed")
	ErrManagerClosed    = errors.New("targetdb: manager closed")
)

// psql SQLSTATE codes for rejected credentials.
const (
	sqlstateInvalidPassword          = "28P01"
	sqlstateInvalidAuthSpecification = "28000"
)

func mapError(err error) error {
	if err == nil {
		return nil
	}
	var (
		netErr net.Error
		opErr  *net.OpError
		pgErr  *pgconn.PgError
		mapped error
	)
	switch {
	case errors.Is(err, context.DeadlineExceeded),
		errors.As(err, &netErr) && netErr.Timeout():
		mapped = ErrTimeout
	case errors.As(err, &pgErr) &&
		(pgErr.Code == sqlstateInvalidPassword || pgErr.Code == sqlstateInvalidAuthSpecification):
		mapped = ErrAuthFailed
	case errors.As(err, &opErr):
		mapped = ErrUnreachable
	default:
		mapped = ErrConnectionFailed
	}
	return fmt.Errorf("%w: %w", mapped, err)
}