// Package adapter defines the contract every kind of Source implements.
package adapter

import (
	"context"
	"strings"
)

// Column is one result column; Ref is how to name it in SQL, already quoted, and a query
// result's columns have none.
type Column struct {
	Name string
	Type string
	Ref  string
}

// Rows is a forward-only stream of query results.
type Rows interface {
	Columns() []Column
	Next() bool
	Values() []any
	Err() error
	RowsAffected() (n int64, known bool)
	Close() error
}

// Table is a queryable table or view; Name is shown to the user and Ref is how to name it
// in SQL, already quoted.
type Table struct {
	Name    string
	Ref     string
	Columns []Column
}

// QuoteIdent quotes an identifier in standard SQL, which SQLite, DuckDB and
// PostgreSQL all accept.
func QuoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// Adapter opens a Source, runs SQL against it, and releases it.
type Adapter interface {
	Query(ctx context.Context, sql string) (Rows, error)
	Schema(ctx context.Context) ([]Table, error)
	Close() error
}
