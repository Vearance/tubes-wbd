//go:build integration

package viewer

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"tubes-wbd/backend/access"
	"tubes-wbd/backend/database/targetdb"
)

const (
	testUser     = "target"
	testPassword = "target_password"
	testDBName   = "viewer_target"
)

const seedSQL = `
CREATE SCHEMA shop;

CREATE TABLE shop.customers (
	id      integer PRIMARY KEY,
	email   text NOT NULL UNIQUE,
	name    text,
	age     integer CHECK (age >= 0),
	created timestamptz DEFAULT now()
);
INSERT INTO shop.customers (id, email, name, age) VALUES
	(1, 'ann@example.com', 'Ann', 30),
	(2, 'bob@example.com', NULL, NULL);

CREATE TABLE shop.orders (
	id          integer PRIMARY KEY,
	customer_id integer NOT NULL REFERENCES shop.customers (id),
	total       numeric(10, 2) NOT NULL DEFAULT 0
);
CREATE INDEX orders_customer_idx ON shop.orders (customer_id);

CREATE TABLE shop.numbers (n integer PRIMARY KEY);
INSERT INTO shop.numbers SELECT g FROM generate_series(1, 250) g;

CREATE TABLE shop.events (message text);
INSERT INTO shop.events SELECT 'event ' || g FROM generate_series(1, 5) g;

CREATE TABLE shop.types (
	id   integer PRIMARY KEY,
	doc  jsonb,
	raw  bytea,
	tags integer[],
	note text
);
INSERT INTO shop.types VALUES (1, '{"a": 1}', '\xdeadbeef', '{1,2,3}', NULL);

CREATE TABLE shop.empty (id integer PRIMARY KEY);

CREATE VIEW shop.customer_names AS SELECT id, name FROM shop.customers;

CREATE TABLE public."Weird Table" ("Odd ""Col""" integer PRIMARY KEY, "select" text);
INSERT INTO public."Weird Table" VALUES (2, 'b'), (1, 'a');

CREATE TABLE shop.audit (id serial PRIMARY KEY, note text);
CREATE FUNCTION shop.sneaky() RETURNS integer LANGUAGE sql VOLATILE AS
	$$ INSERT INTO shop.audit (note) VALUES ('written') RETURNING id $$;
CREATE VIEW shop.sneaky_view AS SELECT shop.sneaky() AS id;
`

var (
	target    targetdb.ConnConfig
	adminPool *pgxpool.Pool
)

func TestMain(m *testing.M) {
	os.Exit(runWithTarget(m))
}

func runWithTarget(m *testing.M) int {
	ctx := context.Background()

	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase(testDBName),
		tcpostgres.WithUsername(testUser),
		tcpostgres.WithPassword(testPassword),
		tcpostgres.BasicWaitStrategies(),
	)
	defer testcontainers.TerminateContainer(ctr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start target:", err)
		return 1
	}

	host, err := ctr.Host(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "target host:", err)
		return 1
	}
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	if err != nil {
		fmt.Fprintln(os.Stderr, "target port:", err)
		return 1
	}
	target = targetdb.ConnConfig{
		Host:     host,
		Port:     port.Num(),
		User:     testUser,
		Password: testPassword,
		DBName:   testDBName,
		SSLMode:  "disable",
	}

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "target dsn:", err)
		return 1
	}
	adminPool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "admin pool:", err)
		return 1
	}
	defer adminPool.Close()

	if _, err := adminPool.Exec(ctx, seedSQL); err != nil {
		fmt.Fprintln(os.Stderr, "seed target:", err)
		return 1
	}

	return m.Run()
}

type staticSource map[targetdb.DatabaseID]targetdb.ConnConfig

func (s staticSource) ConnConfig(_ context.Context, id targetdb.DatabaseID) (targetdb.ConnConfig, error) {
	cfg, ok := s[id]
	if !ok {
		return targetdb.ConnConfig{}, targetdb.ErrUnknownDatabase
	}
	return cfg, nil
}

func newIntegrationService(t *testing.T) (*ViewerService, *fakeAuthorizer) {
	t.Helper()

	manager := targetdb.NewManager(staticSource{1: target}, targetdb.Options{})
	t.Cleanup(manager.Close)

	authz := &fakeAuthorizer{}
	svc, err := NewViewerService(authz, manager, testViewerConfig())
	require.NoError(t, err)
	return svc, authz
}

func cells(page *TablePage) [][]string {
	out := make([][]string, len(page.Rows))
	for i, row := range page.Rows {
		out[i] = make([]string, len(row))
		for j, cell := range row {
			if cell == nil {
				out[i][j] = "NULL"
			} else {
				out[i][j] = *cell
			}
		}
	}
	return out
}

func columnNames(detail *TableDetail) []string {
	names := make([]string, len(detail.Columns))
	for i, c := range detail.Columns {
		names[i] = c.Name
	}
	return names
}

func TestIntegrationListSchemas(t *testing.T) {
	svc, _ := newIntegrationService(t)

	schemas, err := svc.ListSchemas(context.Background(), 1, 1)

	require.NoError(t, err)
	require.Contains(t, schemas, "public")
	require.Contains(t, schemas, "shop")
	for _, system := range []string{"pg_catalog", "information_schema", "pg_toast"} {
		require.NotContains(t, schemas, system)
	}
}

func TestIntegrationListTables(t *testing.T) {
	svc, _ := newIntegrationService(t)
	ctx := context.Background()

	tables, err := svc.ListTables(ctx, 1, 1, "shop")
	require.NoError(t, err)
	require.Contains(t, tables, Table{Schema: "shop", Name: "customers", Kind: KindTable})
	require.Contains(t, tables, Table{Schema: "shop", Name: "customer_names", Kind: KindView})
	require.NotContains(t, tables, Table{Schema: "shop", Name: "customers_pkey", Kind: KindTable})

	_, err = svc.ListTables(ctx, 1, 1, "does_not_exist")
	require.ErrorIs(t, err, ErrSchemaNotFound)
	for _, system := range []string{"pg_catalog", "information_schema", "pg_toast"} {
		_, err = svc.ListTables(ctx, 1, 1, system)
		require.ErrorIs(t, err, ErrSchemaNotFound, system)
	}
}

func TestIntegrationDescribeTable(t *testing.T) {
	svc, _ := newIntegrationService(t)
	ctx := context.Background()

	t.Run("orders", func(t *testing.T) {
		detail, err := svc.DescribeTable(ctx, 1, 1, "shop", "orders")
		require.NoError(t, err)

		require.Equal(t, Table{Schema: "shop", Name: "orders", Kind: KindTable}, detail.Table)
		require.Equal(t, []string{"id", "customer_id", "total"}, columnNames(detail))
		require.Equal(t, "integer", detail.Columns[0].Type)
		require.Equal(t, "numeric(10,2)", detail.Columns[2].Type)
		for _, c := range detail.Columns {
			require.False(t, c.Nullable, c.Name)
		}
		require.Nil(t, detail.Columns[0].Default)
		require.NotNil(t, detail.Columns[2].Default)
		require.Equal(t, "0", *detail.Columns[2].Default)

		require.Len(t, detail.Constraints, 2)
		require.Equal(t, "PRIMARY KEY", detail.Constraints[0].Type)
		require.Equal(t, "orders_pkey", detail.Constraints[0].Name)
		require.Equal(t, "FOREIGN KEY", detail.Constraints[1].Type)
		require.Equal(t, "orders_customer_id_fkey", detail.Constraints[1].Name)
		require.Contains(t, detail.Constraints[1].Definition, "REFERENCES shop.customers(id)")

		require.Len(t, detail.Indexes, 2)
		require.Equal(t, "orders_pkey", detail.Indexes[0].Name)
		require.True(t, detail.Indexes[0].Primary)
		require.True(t, detail.Indexes[0].Unique)
		require.Equal(t, "orders_customer_idx", detail.Indexes[1].Name)
		require.False(t, detail.Indexes[1].Unique)
		require.Contains(t, detail.Indexes[1].Definition, "(customer_id)")
	})

	t.Run("customers", func(t *testing.T) {
		detail, err := svc.DescribeTable(ctx, 1, 1, "shop", "customers")
		require.NoError(t, err)

		byName := map[string]Column{}
		for _, c := range detail.Columns {
			byName[c.Name] = c
		}
		require.True(t, byName["name"].Nullable)
		require.False(t, byName["email"].Nullable)
		require.NotNil(t, byName["created"].Default)
		require.Contains(t, *byName["created"].Default, "now()")

		types := map[string]string{}
		for _, c := range detail.Constraints {
			types[c.Name] = c.Type
		}
		require.Equal(t, map[string]string{
			"customers_pkey":      "PRIMARY KEY",
			"customers_email_key": "UNIQUE",
			"customers_age_check": "CHECK",
		}, types)
	})

	t.Run("view", func(t *testing.T) {
		detail, err := svc.DescribeTable(ctx, 1, 1, "shop", "customer_names")
		require.NoError(t, err)

		require.Equal(t, KindView, detail.Kind)
		require.Equal(t, []string{"id", "name"}, columnNames(detail))
		require.Empty(t, detail.Constraints)
		require.Empty(t, detail.Indexes)
	})

	t.Run("not found", func(t *testing.T) {
		_, err := svc.DescribeTable(ctx, 1, 1, "shop", "nope")
		require.ErrorIs(t, err, ErrTableNotFound)
		_, err = svc.DescribeTable(ctx, 1, 1, "nope", "orders")
		require.ErrorIs(t, err, ErrTableNotFound)
	})
}

func TestIntegrationTableDataPagination(t *testing.T) {
	svc, _ := newIntegrationService(t)
	ctx := context.Background()

	first, err := svc.TableData(ctx, 1, 1, "shop", "numbers", 1, 100)
	require.NoError(t, err)
	require.Len(t, first.Rows, 100)
	require.True(t, first.HasNext)
	require.Equal(t, []string{"n"}, first.Columns)
	require.Equal(t, []string{"1"}, cells(first)[0], "rows must follow the numeric primary key, not text order")
	require.Equal(t, []string{"2"}, cells(first)[1])
	require.Equal(t, []string{"100"}, cells(first)[99])

	last, err := svc.TableData(ctx, 1, 1, "shop", "numbers", 3, 100)
	require.NoError(t, err)
	require.Len(t, last.Rows, 50)
	require.False(t, last.HasNext)
	require.Equal(t, []string{"201"}, cells(last)[0])
	require.Equal(t, []string{"250"}, cells(last)[49])

	exact, err := svc.TableData(ctx, 1, 1, "shop", "numbers", 5, 50)
	require.NoError(t, err)
	require.Len(t, exact.Rows, 50)
	require.False(t, exact.HasNext, "a full final page must not report a next page")

	beyond, err := svc.TableData(ctx, 1, 1, "shop", "numbers", 99, 100)
	require.NoError(t, err)
	require.NotNil(t, beyond.Rows)
	require.Empty(t, beyond.Rows)
	require.False(t, beyond.HasNext)

	defaults, err := svc.TableData(ctx, 1, 1, "shop", "numbers", 0, 0)
	require.NoError(t, err)
	require.Equal(t, 1, defaults.Page)
	require.Equal(t, testViewerConfig().DefaultPageSize, defaults.PageSize)
	require.Len(t, defaults.Rows, testViewerConfig().DefaultPageSize)

	capped, err := svc.TableData(ctx, 1, 1, "shop", "numbers", 1, 1000)
	require.NoError(t, err)
	require.Equal(t, testViewerConfig().MaxPageSize, capped.PageSize)
	require.Len(t, capped.Rows, testViewerConfig().MaxPageSize)
}

func TestIntegrationTableDataWithoutPrimaryKeyIsStable(t *testing.T) {
	svc, _ := newIntegrationService(t)
	ctx := context.Background()

	var got [][]string
	for page := 1; page <= 3; page++ {
		result, err := svc.TableData(ctx, 1, 1, "shop", "events", page, 2)
		require.NoError(t, err)
		require.Equal(t, page < 3, result.HasNext)
		got = append(got, cells(result)...)
	}

	require.Equal(t, [][]string{
		{"event 1"}, {"event 2"}, {"event 3"}, {"event 4"}, {"event 5"},
	}, got)
}

func TestIntegrationTableDataFormatsValuesAsText(t *testing.T) {
	svc, _ := newIntegrationService(t)
	ctx := context.Background()

	types, err := svc.TableData(ctx, 1, 1, "shop", "types", 1, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"id", "doc", "raw", "tags", "note"}, types.Columns)
	require.Equal(t, [][]string{{"1", `{"a": 1}`, `\xdeadbeef`, "{1,2,3}", "NULL"}}, cells(types))
	require.Nil(t, types.Rows[0][4], "SQL NULL must stay distinguishable from the text NULL")

	customers, err := svc.TableData(ctx, 1, 1, "shop", "customers", 1, 10)
	require.NoError(t, err)
	require.Len(t, customers.Rows, 2)
	require.Equal(t, "ann@example.com", *customers.Rows[0][1])
	require.Nil(t, customers.Rows[1][2])
	require.Nil(t, customers.Rows[1][3])

	empty, err := svc.TableData(ctx, 1, 1, "shop", "empty", 1, 10)
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, empty.Columns)
	require.Empty(t, empty.Rows)
}

func TestIntegrationTableDataQuotedIdentifiers(t *testing.T) {
	svc, _ := newIntegrationService(t)

	result, err := svc.TableData(context.Background(), 1, 1, "public", "Weird Table", 1, 10)

	require.NoError(t, err)
	require.Equal(t, []string{`Odd "Col"`, "select"}, result.Columns)
	require.Equal(t, [][]string{{"1", "a"}, {"2", "b"}}, cells(result))
}

func TestIntegrationTableDataFromView(t *testing.T) {
	svc, _ := newIntegrationService(t)

	result, err := svc.TableData(context.Background(), 1, 1, "shop", "customer_names", 1, 10)

	require.NoError(t, err)
	require.ElementsMatch(t, [][]string{{"1", "Ann"}, {"2", "NULL"}}, cells(result))
}

func TestIntegrationQueriesRunInReadOnlyTransaction(t *testing.T) {
	svc, _ := newIntegrationService(t)
	ctx := context.Background()

	_, err := svc.TableData(ctx, 1, 1, "shop", "sneaky_view", 1, 10)

	require.ErrorContains(t, err, "read-only transaction")
	var written int
	require.NoError(t, adminPool.QueryRow(ctx, "SELECT count(*) FROM shop.audit").Scan(&written))
	require.Zero(t, written, "a View Only read must never write")
}

func TestIntegrationRejectsHostileIdentifiers(t *testing.T) {
	svc, _ := newIntegrationService(t)
	ctx := context.Background()

	tests := []struct{ name, schema, table string }{
		{"statement in table", "shop", "customers; DROP TABLE shop.customers"},
		{"quote breakout in table", "shop", `customers"; DROP TABLE shop.customers; --`},
		{"quote breakout in schema", `shop"; DROP TABLE shop.customers; --`, "customers"},
		{"qualified name in table", "public", "shop.customers"},
		{"pg_authid", "pg_catalog", "pg_authid"},
		{"pg_class", "pg_catalog", "pg_class"},
		{"information_schema", "information_schema", "tables"},
		{"pg_toast", "pg_toast", "pg_toast_1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.DescribeTable(ctx, 1, 1, tt.schema, tt.table)
			require.ErrorIs(t, err, ErrTableNotFound)

			_, err = svc.TableData(ctx, 1, 1, tt.schema, tt.table, 1, 10)
			require.ErrorIs(t, err, ErrTableNotFound)
		})
	}

	_, err := svc.ListTables(ctx, 1, 1, `shop'; DROP TABLE shop.customers; --`)
	require.ErrorIs(t, err, ErrSchemaNotFound)

	var customers int
	require.NoError(t, adminPool.QueryRow(ctx, "SELECT count(*) FROM shop.customers").Scan(&customers))
	require.Equal(t, 2, customers, "hostile identifiers must not change the target")
}

func TestIntegrationUnknownDatabase(t *testing.T) {
	svc, _ := newIntegrationService(t)

	_, err := svc.ListSchemas(context.Background(), 1, 99)

	require.ErrorIs(t, err, targetdb.ErrUnknownDatabase)
}

func TestIntegrationPermissionDenied(t *testing.T) {
	manager := targetdb.NewManager(staticSource{1: target}, targetdb.Options{})
	t.Cleanup(manager.Close)
	authz := &fakeAuthorizer{err: access.ErrInsufficientPermission}
	svc, err := NewViewerService(authz, manager, testViewerConfig())
	require.NoError(t, err)

	_, err = svc.TableData(context.Background(), 5, 1, "shop", "customers", 1, 10)

	require.ErrorIs(t, err, access.ErrInsufficientPermission)
	require.Equal(t, uint(5), authz.userID)
}
