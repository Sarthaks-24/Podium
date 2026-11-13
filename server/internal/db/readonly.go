package db

import (
	"errors"
	"fmt"
	"strings"
)

// The read-only guard. A transaction opened read-only (sql.TxOptions
// ReadOnly) does not take a turn at the write gate, so many of them run at
// once on the pooled connections. A write inside one would reach SQLite
// without the gate and meet its busy timeout, which only shows under load.
// The gate therefore refuses a statement that may write on a connection that
// is inside a read-only transaction, and names the statement kind.
//
// A statement is recognised as a write by its first keyword, not by asking
// SQLite: the pure-Go driver does not expose sqlite3_stmt_readonly, and
// PRAGMA query_only would cost two extra statements per transaction. The
// check reads only the start of the statement and allocates nothing.

// ErrWriteInReadOnly means a statement that may write ran inside a
// transaction opened read-only.
var ErrWriteInReadOnly = errors.New("database: write statement in a read-only transaction")

func writeInReadOnly(kind string) error {
	return fmt.Errorf("%w (%s); begin the transaction without ReadOnly", ErrWriteInReadOnly, kind)
}

// readPragmas take an argument and only read; every other PRAGMA written
// with an argument or an equals sign sets something.
var readPragmas = []string{"table_info", "table_xinfo", "table_list", "index_info", "index_xinfo",
	"index_list", "foreign_key_list", "foreign_key_check", "integrity_check", "quick_check",
	"collation_list", "function_list", "pragma_list", "database_list", "compile_options"}

// writePragmas change the database or its files even when written without
// an argument.
var writePragmas = []string{"wal_checkpoint", "optimize", "incremental_vacuum", "shrink_memory"}

// writeKind names the kind of write a statement may do ("INSERT",
// "PRAGMA", ...), or returns "" for a statement that only reads: SELECT,
// VALUES, EXPLAIN, a WITH whose main statement is a SELECT, and a PRAGMA that
// reads. Anything it does not know is reported as a write, under its first
// word. Leading whitespace and comments are skipped, and a WITH is followed
// to the statement after its common table expressions.
func writeKind(query string) string {
	i := skipSpace(query, 0)
	if i >= len(query) {
		return ""
	}
	w, end := word(query, i)
	switch {
	case w == "":
		return "statement"
	case strings.EqualFold(w, "SELECT"), strings.EqualFold(w, "VALUES"), strings.EqualFold(w, "EXPLAIN"):
		return ""
	case strings.EqualFold(w, "WITH"):
		return withKind(query, end)
	case strings.EqualFold(w, "PRAGMA"):
		return pragmaKind(query, end)
	}
	return strings.ToUpper(w)
}

// withKind finds the statement a WITH leads to. The common table
// expressions are in parentheses, so the first keyword outside any
// parentheses is the main statement.
func withKind(q string, i int) string {
	depth := 0
	for i = skipSpace(q, i); i < len(q); i = skipSpace(q, i) {
		switch c := q[i]; {
		case c == '(':
			depth++
			i++
		case c == ')':
			depth--
			i++
		case c == '\'' || c == '"' || c == '`' || c == '[':
			i = skipQuoted(q, i)
		case isLetter(c):
			w, end := word(q, i)
			if depth == 0 {
				switch {
				case strings.EqualFold(w, "SELECT"), strings.EqualFold(w, "VALUES"):
					return ""
				case strings.EqualFold(w, "INSERT"), strings.EqualFold(w, "UPDATE"), strings.EqualFold(w, "DELETE"), strings.EqualFold(w, "REPLACE"):
					return strings.ToUpper(w)
				}
			}
			i = end
		default:
			i++
		}
	}
	return "WITH"
}

// pragmaKind says whether a PRAGMA reads or sets: an equals sign or an
// argument sets, except for the pragmas that take an argument to read.
func pragmaKind(q string, i int) string {
	i = skipSpace(q, i)
	start := i
	for i < len(q) && (isLetter(q[i]) || q[i] >= '0' && q[i] <= '9' || q[i] == '.') {
		i++
	}
	name := q[start:i]
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 { // schema.name
		name = name[dot+1:]
	}
	i = skipSpace(q, i)
	next := byte(0)
	if i < len(q) {
		next = q[i]
	}
	switch next {
	case '=':
		return "PRAGMA"
	case '(':
		if !inFold(readPragmas, name) {
			return "PRAGMA"
		}
	default:
		if inFold(writePragmas, name) {
			return "PRAGMA"
		}
	}
	return ""
}

func inFold(set []string, name string) bool {
	for _, s := range set {
		if strings.EqualFold(s, name) {
			return true
		}
	}
	return false
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' }

// word returns the run of letters at i and the index after it.
func word(q string, i int) (string, int) {
	j := i
	for j < len(q) && isLetter(q[j]) {
		j++
	}
	return q[i:j], j
}

// skipSpace skips whitespace, "-- ..." lines and "/* ... */" comments.
func skipSpace(q string, i int) int {
	for i < len(q) {
		switch {
		case q[i] == ' ' || q[i] == '\t' || q[i] == '\n' || q[i] == '\r' || q[i] == '\f':
			i++
		case strings.HasPrefix(q[i:], "--"):
			if n := strings.IndexByte(q[i:], '\n'); n >= 0 {
				i += n + 1
			} else {
				return len(q)
			}
		case strings.HasPrefix(q[i:], "/*"):
			if n := strings.Index(q[i+2:], "*/"); n >= 0 {
				i += n + 4
			} else {
				return len(q)
			}
		default:
			return i
		}
	}
	return i
}

// skipQuoted returns the index after the quoted text that starts at i. A
// doubled quote ends it and starts the next piece, which is the same text.
func skipQuoted(q string, i int) int {
	closer := q[i]
	if closer == '[' {
		closer = ']'
	}
	if n := strings.IndexByte(q[i+1:], closer); n >= 0 {
		return i + n + 2
	}
	return len(q)
}
