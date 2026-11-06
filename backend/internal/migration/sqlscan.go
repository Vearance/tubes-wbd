package migration

import "strings"

var transactionKeywords = map[string]bool{
	"begin":     true,
	"start":     true,
	"commit":    true,
	"end":       true,
	"rollback":  true,
	"abort":     true,
	"savepoint": true,
	"release":   true,
}

func statements(sql string) [][]string {
	var stmts [][]string
	for _, stmt := range strings.Split(blankLiterals(sql), ";") {
		if words := strings.Fields(strings.ToLower(stmt)); len(words) > 0 {
			stmts = append(stmts, words)
		}
	}
	return stmts
}

func transactionControl(stmts [][]string) (string, bool) {
	for _, words := range stmts {
		if transactionKeywords[words[0]] {
			return words[0], true
		}
		if words[0] == "prepare" && len(words) > 1 && words[1] == "transaction" {
			return "prepare transaction", true
		}
	}
	return "", false
}

func blankLiterals(sql string) string {
	out := []byte(sql)
	blank := func(from, to int) {
		for i := from; i < to && i < len(out); i++ {
			out[i] = ' '
		}
	}

	for i := 0; i < len(sql); {
		switch {
		case strings.HasPrefix(sql[i:], "--"):
			end := strings.IndexByte(sql[i:], '\n')
			if end < 0 {
				end = len(sql) - i
			}
			blank(i, i+end)
			i += end
		case strings.HasPrefix(sql[i:], "/*"):
			end := blockCommentEnd(sql, i)
			blank(i, end)
			i = end
		case sql[i] == '\'':
			escapes := i > 0 && (sql[i-1] == 'e' || sql[i-1] == 'E') && (i < 2 || !isIdentChar(sql[i-2]))
			end := quotedEnd(sql, i, '\'', escapes)
			blank(i, end)
			i = end
		case sql[i] == '"':
			end := quotedEnd(sql, i, '"', false)
			blank(i, end)
			i = end
		case sql[i] == '$' && (i == 0 || !isIdentChar(sql[i-1])):
			if end, ok := dollarQuotedEnd(sql, i); ok {
				blank(i, end)
				i = end
			} else {
				i++
			}
		default:
			i++
		}
	}
	return string(out)
}

func blockCommentEnd(sql string, start int) int {
	depth := 0
	for i := start; i < len(sql)-1; i++ {
		switch {
		case sql[i] == '/' && sql[i+1] == '*':
			depth++
			i++
		case sql[i] == '*' && sql[i+1] == '/':
			depth--
			i++
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(sql)
}

func quotedEnd(sql string, start int, quote byte, backslashEscapes bool) int {
	for i := start + 1; i < len(sql); i++ {
		switch {
		case backslashEscapes && sql[i] == '\\':
			i++
		case sql[i] == quote:
			if i+1 < len(sql) && sql[i+1] == quote {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(sql)
}

func dollarQuotedEnd(sql string, start int) (int, bool) {
	j := start + 1
	if j < len(sql) && sql[j] >= '0' && sql[j] <= '9' {
		return 0, false
	}
	for j < len(sql) && isIdentChar(sql[j]) {
		j++
	}
	if j >= len(sql) || sql[j] != '$' {
		return 0, false
	}

	tag := sql[start : j+1]
	end := strings.Index(sql[j+1:], tag)
	if end < 0 {
		return len(sql), true
	}
	return j + 1 + end + len(tag), true
}

func isIdentChar(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}