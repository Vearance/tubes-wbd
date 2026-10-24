package viewer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"tubes-wbd/backend/database/targetdb"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxPage         = math.MaxInt32
	rollbackTimeout = 5 * time.Second
)

type Authorizer interface {
	RequireView(ctx context.Context, userID, managedDatabaseID uint) error
}

type PoolProvider interface {
	Pool(ctx context.Context, id targetdb.DatabaseID) (*pgxpool.Pool, error)
}

type Config struct {
	DefaultPageSize int
	MaxPageSize     int
}

type ViewerService struct {
	authz  Authorizer
	pools  PoolProvider
	config Config
}

func NewViewerService(
	authz Authorizer,
	pools PoolProvider,
	config Config,
) (*ViewerService, error) {
	if authz == nil {
		return nil, errors.New("viewer authorizer is nil")
	}
	if pools == nil {
		return nil, errors.New("target database pool provider is nil")
	}
	if config.DefaultPageSize <= 0 {
		return nil, errors.New("viewer default page size must be greater than zero")
	}
	if config.MaxPageSize < config.DefaultPageSize {
		return nil, errors.New("viewer max page size must be greater than or equal to default page size")
	}

	return &ViewerService{authz: authz, pools: pools, config: config}, nil
}

func (s *ViewerService) ListSchemas(ctx context.Context, userID, dbID uint) ([]string, error) {
	if err := s.authz.RequireView(ctx, userID, dbID); err != nil {
		return nil, err
	}

	var schemas []string
	err := s.readOnly(ctx, dbID, func(tx pgx.Tx) (err error) {
		schemas, err = listSchemas(ctx, tx)
		return err
	})
	return schemas, err
}

func (s *ViewerService) ListTables(ctx context.Context, userID, dbID uint, schema string) ([]Table, error) {
	if err := s.authz.RequireView(ctx, userID, dbID); err != nil {
		return nil, err
	}
	if err := validateIdentifier("schema", schema); err != nil {
		return nil, err
	}

	var tables []Table
	err := s.readOnly(ctx, dbID, func(tx pgx.Tx) error {
		exists, err := schemaExists(ctx, tx, schema)
		if err != nil {
			return err
		}
		if !exists {
			return ErrSchemaNotFound
		}
		tables, err = listTables(ctx, tx, schema)
		return err
	})
	return tables, err
}

func (s *ViewerService) DescribeTable(ctx context.Context, userID, dbID uint, schema, table string) (*TableDetail, error) {
	if err := s.authz.RequireView(ctx, userID, dbID); err != nil {
		return nil, err
	}
	if err := validateTable(schema, table); err != nil {
		return nil, err
	}

	var detail *TableDetail
	err := s.readOnly(ctx, dbID, func(tx pgx.Tx) error {
		rel, err := resolveTable(ctx, tx, schema, table)
		if err != nil {
			return err
		}

		d := &TableDetail{Table: rel.table}
		if d.Columns, err = columns(ctx, tx, rel.oid); err != nil {
			return err
		}
		if d.Constraints, err = constraints(ctx, tx, rel.oid); err != nil {
			return err
		}
		if d.Indexes, err = indexes(ctx, tx, rel.oid); err != nil {
			return err
		}
		detail = d
		return nil
	})
	return detail, err
}

func (s *ViewerService) TableData(ctx context.Context, userID, dbID uint, schema, table string, page, pageSize int) (*TablePage, error) {
	if err := s.authz.RequireView(ctx, userID, dbID); err != nil {
		return nil, err
	}
	if err := validateTable(schema, table); err != nil {
		return nil, err
	}
	page, pageSize, err := normalizePage(page, pageSize, s.config)
	if err != nil {
		return nil, err
	}

	var result *TablePage
	err = s.readOnly(ctx, dbID, func(tx pgx.Tx) error {
		rel, err := resolveTable(ctx, tx, schema, table)
		if err != nil {
			return err
		}
		result, err = tableData(ctx, tx, rel, page, pageSize)
		return err
	})
	return result, err
}

func (s *ViewerService) readOnly(ctx context.Context, dbID uint, fn func(pgx.Tx) error) error {
	pool, err := s.pools.Pool(ctx, dbID)
	if err != nil {
		return err
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return fmt.Errorf("viewer: begin read-only transaction: %w", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	return fn(tx)
}

func validateTable(schema, table string) error {
	if err := validateIdentifier("schema", schema); err != nil {
		return err
	}
	return validateIdentifier("table", table)
}

func validateIdentifier(what, value string) error {
	switch {
	case value == "":
		return fmt.Errorf("%w: %s is required", ErrInvalidInput, what)
	case !utf8.ValidString(value) || strings.ContainsRune(value, 0):
		return fmt.Errorf("%w: %s contains invalid characters", ErrInvalidInput, what)
	}
	return nil
}

func normalizePage(page, pageSize int, config Config) (int, int, error) {
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
		pageSize = config.DefaultPageSize
	}
	if pageSize > config.MaxPageSize {
		pageSize = config.MaxPageSize
	}
	return page, pageSize, nil
}
