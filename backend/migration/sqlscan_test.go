package migration

import (
	"testing"
	"github.com/stretchr/testify/require"
)

func TestTransactionControlIsRejected(t *testing.T) {
	tests := map[string]string{
		"commit":                      "COMMIT",
		"lowercase with semicolon":    "commit;",
		"commit in the middle":        "ALTER TABLE a ADD c int; COMMIT; DROP TABLE b",
		"begin":                       "BEGIN; ALTER TABLE a ADD c int",
		"start transaction":           "start transaction",
		"end":                         "ALTER TABLE a ADD c int;\nEND",
		"rollback":                    "ROLLBACK",
		"rollback to savepoint":       "SAVEPOINT s; ROLLBACK TO s",
		"abort":                       "abort",
		"release":                     "RELEASE s",
		"prepare transaction":         "PREPARE TRANSACTION 'x'",
		"after a block comment":       "/* deploy */ COMMIT",
		"after a line comment":        "SELECT 1;\n-- done\nCommit",
		"after a nested comment":      "/* a /* b */ c */ commit",
		"after a dollar-quoted body":  "DO $$ BEGIN PERFORM 1; END $$; COMMIT",
		"after an escaped string":     `INSERT INTO t VALUES (E'it\'s'); COMMIT`,
		"after a quoted identifier":   `CREATE TABLE "a;b" (c int); COMMIT`,
		"tab and newline separated":   "commit\t\n",
		"after doubled quote escapes": "SELECT 'a''; b'; COMMIT",
	}

	for name, sql := range tests {
		t.Run(name, func(t *testing.T) {
			_, found := transactionControl(statements(sql))
			require.True(t, found, sql)
		})
	}
}

func TestOrdinarySQLIsAllowed(t *testing.T) {
	tests := map[string]string{
		"alter table":             "ALTER TABLE customers ADD COLUMN phone_number text",
		"keyword in string":       "INSERT INTO log VALUES ('commit; begin')",
		"keyword in comment":      "-- commit\nSELECT 1",
		"keyword in block":        "/* begin */ SELECT 1",
		"keyword as identifier":   `SELECT "begin" FROM t`,
		"keyword in escaped str":  `INSERT INTO t VALUES (E'it\'s; commit')`,
		"plpgsql function body":   "CREATE FUNCTION f() RETURNS int AS $$ BEGIN RETURN 1; END $$ LANGUAGE plpgsql",
		"tagged dollar body":      "DO $body$ BEGIN COMMIT; END $body$",
		"dollar in identifier":    "CREATE TABLE a$b$ (c int); SELECT 1",
		"positional placeholder":  "PREPARE q AS SELECT $1::int",
		"column named end_date":   "ALTER TABLE t ADD COLUMN end_date date",
		"statement starting with": "BEGINNING_TABLE_IS_NOT_A_KEYWORD",
	}

	for name, sql := range tests {
		t.Run(name, func(t *testing.T) {
			keyword, found := transactionControl(statements(sql))
			require.False(t, found, "flagged %q in %q", keyword, sql)
		})
	}
}

func TestStatementsIgnoresEmptyAndCommentOnlySQL(t *testing.T) {
	require.Empty(t, statements(""))
	require.Empty(t, statements(" ;; \n"))
	require.Empty(t, statements("-- only a comment\n/* and another */"))
	require.Len(t, statements("SELECT 1; SELECT 2;"), 2)
}