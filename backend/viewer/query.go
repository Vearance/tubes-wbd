package viewer

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const maxQueryBytes = 1 << 20

func validateReadOnlyQuery(sql string) (string, error) {
	if len(sql) > maxQueryBytes {
		return "", fmt.Errorf("%w: query is larger than %d bytes", ErrInvalidInput, maxQueryBytes)
	}
	if !utf8.ValidString(sql) || strings.ContainsRune(sql, 0) {
		return "", fmt.Errorf("%w: query contains invalid characters", ErrInvalidInput)
	}

	query := strings.TrimSpace(sql)
	if strings.HasSuffix(query, ";") {
		query = strings.TrimSpace(strings.TrimSuffix(query, ";"))
	}
	if query == "" {
		return "", fmt.Errorf("%w: query is required", ErrInvalidInput)
	}

	keyword := firstKeyword(query)
	if keyword != "select" && keyword != "with" {
		return "", ErrReadOnlyQuery
	}

	return query, nil
}

func firstKeyword(sql string) string {
	for {
		sql = strings.TrimLeftFunc(sql, unicode.IsSpace)
		switch {
		case strings.HasPrefix(sql, "--"):
			newline := strings.IndexByte(sql, '\n')
			if newline < 0 {
				return ""
			}
			sql = sql[newline+1:]
		case strings.HasPrefix(sql, "/*"):
			end := strings.Index(sql[2:], "*/")
			if end < 0 {
				return ""
			}
			sql = sql[end+4:]
		default:
			end := 0
			for end < len(sql) && (sql[end] == '_' || sql[end] >= 'a' && sql[end] <= 'z' || sql[end] >= 'A' && sql[end] <= 'Z') {
				end++
			}
			return strings.ToLower(sql[:end])
		}
	}
}

func queryRows(ctx context.Context, tx pgx.Tx, query string, maxRows int) (*QueryResult, error) {
	rows, err := tx.Query(ctx,
		"SELECT * FROM ("+query+") AS read_only_query LIMIT $1",
		maxRows+1,
	)
	if err != nil {
		return nil, fmt.Errorf("viewer: execute read-only query: %w", err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	result := &QueryResult{
		Columns: make([]string, len(fields)),
		Rows:    make([][]*string, 0, maxRows),
	}
	for index := range fields {
		result.Columns[index] = fields[index].Name
	}

	for rows.Next() {
		values := make([]any, len(fields))
		destinations := make([]any, len(fields))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, fmt.Errorf("viewer: scan read-only query result: %w", err)
		}

		row := make([]*string, len(values))
		for index, value := range values {
			if value == nil {
				continue
			}
			text := fmt.Sprint(value)
			row[index] = &text
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("viewer: read read-only query result: %w", err)
	}

	if len(result.Rows) > maxRows {
		result.HasNext = true
		result.Rows = result.Rows[:maxRows]
	}
	return result, nil
}
