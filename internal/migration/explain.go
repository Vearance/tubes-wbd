package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type ExplainResult struct {
	Statements []ExplainStatement
}

type ExplainStatement struct {
	SQL  string
	Plan []map[string]any
}

func (s *MigrationService) Explain(
	ctx context.Context,
	userID uint,
	dbID uint,
	sql string,
) (*ExplainResult, error) {
	if err := s.authz.RequireView(ctx, userID, dbID); err != nil {
		return nil, err
	}

	statements, err := validateExplainSQL(sql)
	if err != nil {
		return nil, err
	}

	pool, err := s.pools.Pool(ctx, dbID)
	if err != nil {
		return nil, err
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("migration: begin explain transaction: %w", err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	result := &ExplainResult{
		Statements: make([]ExplainStatement, 0, len(statements)),
	}
	for _, statement := range statements {
		var rawPlan []byte
		if err := tx.QueryRow(
			ctx,
			"EXPLAIN (FORMAT JSON) "+statement,
		).Scan(&rawPlan); err != nil {
			return nil, executionError(err)
		}

		var plan []map[string]any
		if err := json.Unmarshal(rawPlan, &plan); err != nil {
			return nil, fmt.Errorf("migration: decoding explain plan: %w", err)
		}

		result.Statements = append(result.Statements, ExplainStatement{
			SQL:  statement,
			Plan: plan,
		})
	}

	return result, nil
}

func validateExplainSQL(sql string) ([]string, error) {
	switch {
	case len(sql) > MaxSQLBytes:
		return nil, fmt.Errorf("%w: SQL is larger than %d bytes", ErrInvalidInput, MaxSQLBytes)
	case !utf8.ValidString(sql) || strings.ContainsRune(sql, 0):
		return nil, fmt.Errorf("%w: SQL contains invalid characters", ErrInvalidInput)
	}

	words := statements(sql)
	if len(words) == 0 {
		return nil, fmt.Errorf("%w: SQL has no statements", ErrInvalidInput)
	}
	if keyword, found := transactionControl(words); found {
		return nil, fmt.Errorf("%w: %s", ErrTransactionControl, strings.ToUpper(keyword))
	}

	queryStatements := splitStatements(sql)
	for _, statement := range queryStatements {
		if first := statements(statement); len(first) > 0 && first[0][0] == "explain" {
			return nil, fmt.Errorf("%w: SQL must not contain EXPLAIN", ErrInvalidInput)
		}
	}
	return queryStatements, nil
}

func splitStatements(sql string) []string {
	masked := blankLiterals(sql)
	result := make([]string, 0)
	start := 0

	for index := 0; index <= len(masked); index++ {
		if index < len(masked) && masked[index] != ';' {
			continue
		}

		statement := strings.TrimSpace(sql[start:index])
		if len(statements(statement)) > 0 {
			result = append(result, statement)
		}
		start = index + 1
	}

	return result
}
