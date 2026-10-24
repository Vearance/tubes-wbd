package viewer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"github.com/jackc/pgx/v5"
)

const (
	userSchemas   = `n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'`
	userRelations = `c.relkind IN ('r', 'p', 'v', 'm', 'f')`
)

var relationKinds = map[string]TableKind{
	"r": KindTable,
	"p": KindPartitionedTable,
	"v": KindView,
	"m": KindMaterializedView,
	"f": KindForeignTable,
}

var constraintTypes = map[string]string{
	"p": "PRIMARY KEY",
	"f": "FOREIGN KEY",
	"u": "UNIQUE",
	"c": "CHECK",
	"x": "EXCLUDE",
}

type relation struct {
	oid   uint32
	table Table
}

func listSchemas(ctx context.Context, tx pgx.Tx) ([]string, error) {
	rows, err := tx.Query(ctx,
		`SELECT n.nspname FROM pg_namespace n WHERE `+userSchemas+` ORDER BY n.nspname`)
	if err != nil {
		return nil, fmt.Errorf("viewer: list schemas: %w", err)
	}
	schemas, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("viewer: list schemas: %w", err)
	}
	return schemas, nil
}

func schemaExists(ctx context.Context, tx pgx.Tx, schema string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_namespace n WHERE n.nspname = $1 AND `+userSchemas+`)`,
		schema).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("viewer: check schema: %w", err)
	}
	return exists, nil
}

func listTables(ctx context.Context, tx pgx.Tx, schema string) ([]Table, error) {
	rows, err := tx.Query(ctx,
		`SELECT c.relname, c.relkind::text
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND `+userRelations+` AND `+userSchemas+`
		ORDER BY c.relname`, schema)
	if err != nil {
		return nil, fmt.Errorf("viewer: list tables: %w", err)
	}
	tables, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Table, error) {
		var name, kind string
		if err := row.Scan(&name, &kind); err != nil {
			return Table{}, err
		}
		return Table{Schema: schema, Name: name, Kind: relationKinds[kind]}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("viewer: list tables: %w", err)
	}
	return tables, nil
}

func resolveTable(ctx context.Context, tx pgx.Tx, schema, name string) (relation, error) {
	var (
		oid  uint32
		kind string
	)
	err := tx.QueryRow(ctx,
		`SELECT c.oid, c.relkind::text
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND `+userRelations+` AND `+userSchemas,
		schema, name).Scan(&oid, &kind)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return relation{}, ErrTableNotFound
		}
		return relation{}, fmt.Errorf("viewer: resolve table: %w", err)
	}
	return relation{
		oid:   oid,
		table: Table{Schema: schema, Name: name, Kind: relationKinds[kind]},
	}, nil
}

func columns(ctx context.Context, tx pgx.Tx, oid uint32) ([]Column, error) {
	rows, err := tx.Query(ctx,
		`SELECT a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull,
			pg_get_expr(d.adbin, d.adrelid)
		FROM pg_attribute a
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE a.attrelid = $1 AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, oid)
	if err != nil {
		return nil, fmt.Errorf("viewer: list columns: %w", err)
	}
	cols, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Column, error) {
		var c Column
		err := row.Scan(&c.Name, &c.Type, &c.Nullable, &c.Default)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("viewer: list columns: %w", err)
	}
	return cols, nil
}

func constraints(ctx context.Context, tx pgx.Tx, oid uint32) ([]Constraint, error) {
	rows, err := tx.Query(ctx,
		`SELECT conname, contype::text, pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = $1 AND contype IN ('p', 'f', 'u', 'c', 'x')
		ORDER BY CASE contype WHEN 'p' THEN 0 WHEN 'f' THEN 1 WHEN 'u' THEN 2 WHEN 'c' THEN 3 ELSE 4 END,
			conname`, oid)
	if err != nil {
		return nil, fmt.Errorf("viewer: list constraints: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Constraint, error) {
		var c Constraint
		var kind string
		if err := row.Scan(&c.Name, &kind, &c.Definition); err != nil {
			return Constraint{}, err
		}
		c.Type = constraintTypes[kind]
		return c, nil
	})
	if err != nil {
		return nil, fmt.Errorf("viewer: list constraints: %w", err)
	}
	return list, nil
}

func indexes(ctx context.Context, tx pgx.Tx, oid uint32) ([]Index, error) {
	rows, err := tx.Query(ctx,
		`SELECT ic.relname, pg_get_indexdef(i.indexrelid), i.indisunique, i.indisprimary
		FROM pg_index i JOIN pg_class ic ON ic.oid = i.indexrelid
		WHERE i.indrelid = $1
		ORDER BY i.indisprimary DESC, ic.relname`, oid)
	if err != nil {
		return nil, fmt.Errorf("viewer: list indexes: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Index, error) {
		var i Index
		err := row.Scan(&i.Name, &i.Definition, &i.Unique, &i.Primary)
		return i, err
	})
	if err != nil {
		return nil, fmt.Errorf("viewer: list indexes: %w", err)
	}
	return list, nil
}

func primaryKey(ctx context.Context, tx pgx.Tx, oid uint32) ([]string, error) {
	rows, err := tx.Query(ctx,
		`SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey)
		WHERE i.indrelid = $1 AND i.indisprimary
		ORDER BY array_position(i.indkey::int2[], a.attnum)`, oid)
	if err != nil {
		return nil, fmt.Errorf("viewer: find primary key: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("viewer: find primary key: %w", err)
	}
	return names, nil
}

func tableData(ctx context.Context, tx pgx.Tx, rel relation, page, pageSize int) (*TablePage, error) {
	cols, err := columns(ctx, tx, rel.oid)
	if err != nil {
		return nil, err
	}

	result := &TablePage{
		Columns:  make([]string, len(cols)),
		Rows:     [][]*string{},
		Page:     page,
		PageSize: pageSize,
	}
	for i, c := range cols {
		result.Columns[i] = c.Name
	}
	if len(cols) == 0 {
		return result, nil
	}

	pk, err := primaryKey(ctx, tx, rel.oid)
	if err != nil {
		return nil, err
	}

	offset := int64(page-1) * int64(pageSize)
	rows, err := tx.Query(ctx, dataQuery(rel, result.Columns, pk), pageSize+1, offset)
	if err != nil {
		return nil, fmt.Errorf("viewer: read table data: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		row := make([]*string, len(cols))
		dest := make([]any, len(cols))
		for i := range row {
			dest[i] = &row[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("viewer: read table data: %w", err)
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("viewer: read table data: %w", err)
	}

	if len(result.Rows) > pageSize {
		result.HasNext = true
		result.Rows = result.Rows[:pageSize]
	}
	return result, nil
}

func dataQuery(rel relation, cols, pk []string) string {
	selects := make([]string, len(cols))
	for i, c := range cols {
		selects[i] = "t." + pgx.Identifier{c}.Sanitize() + "::text"
	}

	query := "SELECT " + strings.Join(selects, ", ") +
		" FROM " + pgx.Identifier{rel.table.Schema, rel.table.Name}.Sanitize() + " AS t"

	switch {
	case len(pk) > 0:
		orderBy := make([]string, len(pk))
		for i, c := range pk {
			orderBy[i] = "t." + pgx.Identifier{c}.Sanitize()
		}
		query += " ORDER BY " + strings.Join(orderBy, ", ")
	case rel.table.Kind == KindTable:
		query += " ORDER BY t.ctid"
	}

	return query + " LIMIT $1 OFFSET $2"
}