package viewer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"tubes-wbd/internal/database/targetdb"
	"tubes-wbd/internal/telemetry"
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

func startViewerSpan(ctx context.Context, userID, dbID uint, op string, extraAttrs ...any) (context.Context, func(error, ...any)) {
	start := time.Now()
	actor := telemetry.ActorUser(userID)
	dbLabel := telemetry.TargetDatabaseLabel(dbID)
	telemetry.SetRequestDatabase(ctx, dbLabel)

	spanCtx, span := telemetry.Tracer("tubes-wbd/viewer").Start(ctx, "viewer."+op,
		trace.WithAttributes(
			attribute.String("viewer.operation", op),
			attribute.String("actor", actor),
			attribute.Int64("db.target_database_id", int64(dbID)),
		),
	)
	spanCtx = telemetry.WithActor(spanCtx, actor)
	spanCtx = telemetry.WithDatabase(spanCtx, dbLabel)

	return spanCtx, func(err error, resultAttrs ...any) {
		durationMs := telemetry.RoundMillis(time.Since(start))
		status := "ok"
		if err != nil {
			status = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, telemetry.SanitizeError(err))
		}

		logAttrs := []any{
			"event", "viewer." + op,
			"actor", actor,
			"database", dbLabel,
			"status", status,
			"duration_ms", durationMs,
		}
		logAttrs = append(logAttrs, extraAttrs...)
		logAttrs = append(logAttrs, resultAttrs...)
		if err != nil {
			logAttrs = append(logAttrs, "error", err)
			if sqlstate := telemetry.SQLState(err); sqlstate != "" {
				logAttrs = append(logAttrs, "sqlstate", sqlstate)
			}
			slog.WarnContext(spanCtx, "viewer operation failed", logAttrs...)
		} else {
			slog.InfoContext(spanCtx, "viewer operation completed", logAttrs...)
		}
		span.End()
	}
}

func (s *ViewerService) ListSchemas(ctx context.Context, userID, dbID uint) (schemas []string, err error) {
	spanCtx, finish := startViewerSpan(ctx, userID, dbID, "list_schemas")
	defer func() { finish(err) }()

	if err := s.authz.RequireView(spanCtx, userID, dbID); err != nil {
		return nil, err
	}

	err = s.readOnly(spanCtx, dbID, func(tx pgx.Tx) (err error) {
		schemas, err = listSchemas(spanCtx, tx)
		return err
	})
	return schemas, err
}

func (s *ViewerService) ListTables(ctx context.Context, userID, dbID uint, schema string) (tables []Table, err error) {
	spanCtx, finish := startViewerSpan(ctx, userID, dbID, "list_tables", "schema", schema)
	defer func() { finish(err) }()

	if err := s.authz.RequireView(spanCtx, userID, dbID); err != nil {
		return nil, err
	}
	if err := validateIdentifier("schema", schema); err != nil {
		return nil, err
	}

	err = s.readOnly(spanCtx, dbID, func(tx pgx.Tx) error {
		exists, err := schemaExists(spanCtx, tx, schema)
		if err != nil {
			return err
		}
		if !exists {
			return ErrSchemaNotFound
		}
		tables, err = listTables(spanCtx, tx, schema)
		return err
	})
	return tables, err
}

func (s *ViewerService) DescribeTable(ctx context.Context, userID, dbID uint, schema, table string) (detail *TableDetail, err error) {
	spanCtx, finish := startViewerSpan(ctx, userID, dbID, "describe_table", "schema", schema, "table", table)
	defer func() { finish(err) }()

	if err := s.authz.RequireView(spanCtx, userID, dbID); err != nil {
		return nil, err
	}
	if err := validateTable(schema, table); err != nil {
		return nil, err
	}

	err = s.readOnly(spanCtx, dbID, func(tx pgx.Tx) error {
		rel, err := resolveTable(spanCtx, tx, schema, table)
		if err != nil {
			return err
		}

		d := &TableDetail{Table: rel.table}
		if d.Columns, err = columns(spanCtx, tx, rel.oid); err != nil {
			return err
		}
		if d.Constraints, err = constraints(spanCtx, tx, rel.oid); err != nil {
			return err
		}
		if d.Indexes, err = indexes(spanCtx, tx, rel.oid); err != nil {
			return err
		}
		detail = d
		return nil
	})
	return detail, err
}

func (s *ViewerService) TableData(ctx context.Context, userID, dbID uint, schema, table string, page, pageSize int) (result *TablePage, err error) {
	spanCtx, finish := startViewerSpan(ctx, userID, dbID, "table_data", "schema", schema, "table", table, "page", page, "page_size", pageSize)
	defer func() { finish(err) }()

	if err := s.authz.RequireView(spanCtx, userID, dbID); err != nil {
		return nil, err
	}
	if err := validateTable(schema, table); err != nil {
		return nil, err
	}
	page, pageSize, err = normalizePage(page, pageSize, s.config)
	if err != nil {
		return nil, err
	}

	err = s.readOnly(spanCtx, dbID, func(tx pgx.Tx) error {
		rel, err := resolveTable(spanCtx, tx, schema, table)
		if err != nil {
			return err
		}
		result, err = tableData(spanCtx, tx, rel, page, pageSize)
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
