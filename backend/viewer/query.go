package viewer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	maxQueryBytes = 1 << 20
	maxQueryRows  = 100
	queryTimeout  = 5 * time.Second
	byteaOID      = 17
	jsonOID       = 114
	jsonbOID      = 3802
)

type QueryResult struct {
	Columns   []string
	Rows      [][]*string
	Truncated bool
}

// Query executes one row-returning statement in a bounded read-only transaction.
// Both View Only and View + Edit users are authorized through RequireView.
func (s *ViewerService) Query(ctx context.Context, userID, dbID uint, sql string) (*QueryResult, error) {
	if err := s.authz.RequireView(ctx, userID, dbID); err != nil {
		return nil, err
	}

	statement, err := validateReadQuery(sql)
	if err != nil {
		return nil, err
	}

	pool, err := s.pools.Pool(ctx, dbID)
	if err != nil {
		return nil, err
	}

	queryCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	tx, err := pool.BeginTx(queryCtx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("viewer: begin query transaction: %w", err)
	}
	defer func() {
		rollbackCtx, rollbackCancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer rollbackCancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	if _, err := tx.Exec(queryCtx, "SELECT set_config('statement_timeout', $1, true)", queryTimeout.String()); err != nil {
		return nil, fmt.Errorf("viewer: set query timeout: %w", err)
	}

	wrapped := fmt.Sprintf("SELECT * FROM (\n%s\n) AS bscheme_query LIMIT %d", statement, maxQueryRows+1)
	rows, err := tx.Query(queryCtx, wrapped)
	if err != nil {
		return nil, fmt.Errorf("viewer: execute read-only query: %w", err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	result := &QueryResult{
		Columns: make([]string, len(fields)),
		Rows:    make([][]*string, 0, maxQueryRows),
	}
	for index, field := range fields {
		result.Columns[index] = field.Name
	}

	for rows.Next() {
		if len(result.Rows) == maxQueryRows {
			result.Truncated = true
			break
		}
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("viewer: read query row: %w", err)
		}
		row := make([]*string, len(values))
		for index, value := range values {
			row[index] = queryValueText(value, fields[index].DataTypeOID)
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("viewer: read query rows: %w", err)
	}
	return result, nil
}

func validateReadQuery(sql string) (string, error) {
	switch {
	case len(sql) > maxQueryBytes:
		return "", fmt.Errorf("%w: SQL is larger than %d bytes", ErrInvalidInput, maxQueryBytes)
	case !utf8.ValidString(sql) || strings.ContainsRune(sql, 0):
		return "", fmt.Errorf("%w: SQL contains invalid characters", ErrInvalidInput)
	}

	statement, separators, err := inspectQuery(sql)
	if err != nil {
		return "", err
	}
	if len(separators) > 1 || len(separators) == 1 && !onlySQLTrivia(statement[separators[0]+1:]) {
		return "", fmt.Errorf("%w: exactly one statement is allowed", ErrInvalidInput)
	}
	if len(separators) == 1 {
		statement = statement[:separators[0]] + statement[separators[0]+1:]
	}
	statement = strings.TrimSpace(statement)
	keyword := firstSQLKeyword(statement)
	switch keyword {
	case "select", "with", "values", "table":
		return statement, nil
	case "":
		return "", fmt.Errorf("%w: SQL query is required", ErrInvalidInput)
	default:
		return "", fmt.Errorf("%w: only SELECT, WITH, VALUES, or TABLE queries are allowed", ErrInvalidInput)
	}
}

func inspectQuery(sql string) (string, []int, error) {
	separators := make([]int, 0, 1)
	for index := 0; index < len(sql); {
		switch {
		case strings.HasPrefix(sql[index:], "--"):
			if end := strings.IndexByte(sql[index+2:], '\n'); end >= 0 {
				index += end + 3
			} else {
				return sql, separators, nil
			}
		case strings.HasPrefix(sql[index:], "/*"):
			end := blockCommentEnd(sql, index)
			if end < 0 {
				return "", nil, fmt.Errorf("%w: unterminated comment", ErrInvalidInput)
			}
			index = end
		case sql[index] == '\'' || sql[index] == '"':
			quote := sql[index]
			index++
			closed := false
			for index < len(sql) {
				if sql[index] != quote {
					index++
					continue
				}
				if index+1 < len(sql) && sql[index+1] == quote {
					index += 2
					continue
				}
				index++
				closed = true
				break
			}
			if !closed {
				return "", nil, fmt.Errorf("%w: unterminated quoted value", ErrInvalidInput)
			}
		case sql[index] == '$':
			delimiter := dollarQuoteDelimiter(sql[index:])
			if delimiter == "" {
				index++
				continue
			}
			end := strings.Index(sql[index+len(delimiter):], delimiter)
			if end < 0 {
				return "", nil, fmt.Errorf("%w: unterminated dollar-quoted value", ErrInvalidInput)
			}
			index += len(delimiter) + end + len(delimiter)
		case sql[index] == ';':
			separators = append(separators, index)
			index++
		default:
			index++
		}
	}
	return sql, separators, nil
}

func blockCommentEnd(sql string, start int) int {
	depth := 1
	for index := start + 2; index < len(sql)-1; index++ {
		switch {
		case strings.HasPrefix(sql[index:], "/*"):
			depth++
			index++
		case strings.HasPrefix(sql[index:], "*/"):
			depth--
			index++
			if depth == 0 {
				return index + 1
			}
		}
	}
	return -1
}

func dollarQuoteDelimiter(sql string) string {
	if len(sql) < 2 || sql[0] != '$' {
		return ""
	}
	for index := 1; index < len(sql); index++ {
		switch {
		case sql[index] == '$':
			return sql[:index+1]
		case sql[index] == '_' || sql[index] >= 'a' && sql[index] <= 'z' || sql[index] >= 'A' && sql[index] <= 'Z' || index > 1 && sql[index] >= '0' && sql[index] <= '9':
			continue
		default:
			return ""
		}
	}
	return ""
}

func onlySQLTrivia(sql string) bool {
	for {
		sql = strings.TrimLeftFunc(sql, unicode.IsSpace)
		switch {
		case sql == "":
			return true
		case strings.HasPrefix(sql, "--"):
			if end := strings.IndexByte(sql, '\n'); end >= 0 {
				sql = sql[end+1:]
				continue
			}
			return true
		case strings.HasPrefix(sql, "/*"):
			end := blockCommentEnd(sql, 0)
			if end < 0 {
				return false
			}
			sql = sql[end:]
		default:
			return false
		}
	}
}

func firstSQLKeyword(sql string) string {
	for {
		sql = strings.TrimLeftFunc(sql, unicode.IsSpace)
		switch {
		case strings.HasPrefix(sql, "--"):
			end := strings.IndexByte(sql, '\n')
			if end < 0 {
				return ""
			}
			sql = sql[end+1:]
		case strings.HasPrefix(sql, "/*"):
			end := blockCommentEnd(sql, 0)
			if end < 0 {
				return ""
			}
			sql = sql[end:]
		default:
			end := 0
			for end < len(sql) && (sql[end] == '_' || sql[end] >= 'a' && sql[end] <= 'z' || sql[end] >= 'A' && sql[end] <= 'Z') {
				end++
			}
			return strings.ToLower(sql[:end])
		}
	}
}

func queryValueText(value any, oid uint32) *string {
	if value == nil {
		return nil
	}
	var text string
	switch typed := value.(type) {
	case []byte:
		switch oid {
		case byteaOID:
			text = `\x` + hex.EncodeToString(typed)
		case jsonOID, jsonbOID:
			text = string(typed)
		default:
			text = string(typed)
		}
	case time.Time:
		text = typed.Format(time.RFC3339Nano)
	case map[string]any, []any:
		encoded, err := json.Marshal(typed)
		if err != nil {
			text = fmt.Sprint(typed)
		} else {
			text = string(encoded)
		}
	default:
		text = fmt.Sprint(typed)
	}
	return &text
}
