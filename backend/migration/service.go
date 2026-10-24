package migration

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"tubes-wbd/backend/database/targetdb"
)

const (
	MaxSQLBytes     = 1 << 20
	DefaultPageSize = 50
	MaxPageSize     = 100
	maxNameLen      = 200
	maxPage         = math.MaxInt32
	rollbackTimeout = 5 * time.Second
)

type Authorizer interface {
	RequireView(ctx context.Context, userID, managedDatabaseID uint) error
	RequireEdit(ctx context.Context, userID, managedDatabaseID uint) error
}

type PoolProvider interface {
	Pool(ctx context.Context, id targetdb.DatabaseID) (*pgxpool.Pool, error)
}

type Options struct {
	StatementTimeout time.Duration
	LockTimeout      time.Duration
}

func DefaultOptions() Options {
	return Options{
		StatementTimeout: 5 * time.Minute,
		LockTimeout:      10 * time.Second,
	}
}

func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.StatementTimeout <= 0 {
		o.StatementTimeout = d.StatementTimeout
	}
	if o.LockTimeout <= 0 {
		o.LockTimeout = d.LockTimeout
	}
	return o
}

type Service struct {
	authz Authorizer
	pools PoolProvider
	opts  Options
}

func NewService(authz Authorizer, pools PoolProvider, opts Options) (*Service, error) {
	if authz == nil {
		return nil, errors.New("migration authorizer is nil")
	}
	if pools == nil {
		return nil, errors.New("target database pool provider is nil")
	}

	return &Service{authz: authz, pools: pools, opts: opts.withDefaults()}, nil
}

func (s *Service) Execute(ctx context.Context, userID, dbID uint, m Migration) (*HistoryEntry, error) {
	if err := s.authz.RequireEdit(ctx, userID, dbID); err != nil {
		return nil, err
	}
	m, err := validate(m)
	if err != nil {
		return nil, err
	}

	ctx = context.WithoutCancel(ctx)

	pool, err := s.pools.Pool(ctx, dbID)
	if err != nil {
		return nil, err
	}
	return apply(ctx, pool, m, userID, s.opts)
}

func (s *Service) History(ctx context.Context, userID, dbID uint, page, pageSize int) (*HistoryPage, error) {
	if err := s.authz.RequireView(ctx, userID, dbID); err != nil {
		return nil, err
	}
	page, pageSize, err := normalizePage(page, pageSize)
	if err != nil {
		return nil, err
	}

	var result *HistoryPage
	err = s.readOnly(ctx, dbID, func(tx pgx.Tx) (err error) {
		result, err = listHistory(ctx, tx, page, pageSize)
		return err
	})
	return result, err
}

func (s *Service) HistoryEntry(ctx context.Context, userID, dbID uint, version int64) (*HistoryEntry, error) {
	if err := s.authz.RequireView(ctx, userID, dbID); err != nil {
		return nil, err
	}
	if version <= 0 {
		return nil, fmt.Errorf("%w: version must be positive", ErrInvalidInput)
	}

	var entry *HistoryEntry
	err := s.readOnly(ctx, dbID, func(tx pgx.Tx) (err error) {
		entry, err = getHistoryEntry(ctx, tx, version)
		return err
	})
	return entry, err
}

func (s *Service) readOnly(ctx context.Context, dbID uint, fn func(pgx.Tx) error) error {
	pool, err := s.pools.Pool(ctx, dbID)
	if err != nil {
		return err
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("migration: begin read-only transaction: %w", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	return fn(tx)
}

func normalizePage(page, pageSize int) (int, int, error) {
	switch {
	case page < 0 || pageSize < 0:
		return 0, 0, fmt.Errorf("%w: page and page size must not be negative", ErrInvalidInput)
	case page > maxPage:
		return 0, 0, fmt.Errorf("%w: page is too large", ErrInvalidInput)
	}

	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return page, pageSize, nil
}

func validate(m Migration) (Migration, error) {
	m.Name = strings.TrimSpace(m.Name)
	switch {
	case m.Name == "":
		return m, fmt.Errorf("%w: name is required", ErrInvalidInput)
	case utf8.RuneCountInString(m.Name) > maxNameLen:
		return m, fmt.Errorf("%w: name is longer than %d characters", ErrInvalidInput, maxNameLen)
	case len(m.SQL) > MaxSQLBytes:
		return m, fmt.Errorf("%w: SQL is larger than %d bytes", ErrInvalidInput, MaxSQLBytes)
	case !utf8.ValidString(m.SQL) || !utf8.ValidString(m.Name) ||
		strings.ContainsRune(m.SQL, 0) || strings.ContainsRune(m.Name, 0):
		return m, fmt.Errorf("%w: contains invalid characters", ErrInvalidInput)
	}

	stmts := statements(m.SQL)
	if len(stmts) == 0 {
		return m, fmt.Errorf("%w: SQL has no statements", ErrInvalidInput)
	}
	if keyword, found := transactionControl(stmts); found {
		return m, fmt.Errorf("%w: %s", ErrTransactionControl, strings.ToUpper(keyword))
	}
	return m, nil
}