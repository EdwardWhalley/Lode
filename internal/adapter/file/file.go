// Package file is the Adapter for file Sources, backed by embedded DuckDB. A file
// Source reaches only its own file and changes nothing on disk: see lock and checked.
package file

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	duckdb "github.com/marcboeker/go-duckdb/v2"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

// Adapter queries a single data file, exposed as a view named after its basename.
type Adapter struct {
	db       *sql.DB
	table    string
	rowLimit int
}

var _ adapter.Adapter = (*Adapter)(nil)

type Option func(*Adapter)

// WithRowLimit makes queries return at most n rows, and makes DuckDB itself stop early.
func WithRowLimit(n int) Option { return func(a *Adapter) { a.rowLimit = n } }

// Open opens a CSV, TSV, JSON, NDJSON or Parquet file as a Source.
func Open(path string, opts ...Option) (*Adapter, error) {
	reader, err := readerFor(path)
	if err != nil {
		return nil, err
	}
	abs, err := absolute(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	db.SetMaxOpenConns(1)

	table := tableName(path)
	view := adapter.QuoteIdent(table)
	if _, err := db.Exec(fmt.Sprintf("CREATE VIEW %s AS SELECT * FROM %s(%s, hive_partitioning = false)", view, reader, quoteLiteral(abs))); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := lock(db, abs); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	rs, err := db.Query("SELECT * FROM " + view + " LIMIT 0")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	rs.Close()
	a := &Adapter{db: db, table: table}
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

// Table is the name the file is queried by.
func (a *Adapter) Table() string { return a.table }

func (a *Adapter) Close() error { return a.db.Close() }

func (a *Adapter) Query(ctx context.Context, query string) (adapter.Rows, error) {
	q, err := a.checked(ctx, query)
	if err != nil {
		return nil, err
	}
	text := q.text
	if a.rowLimit > 0 {
		text = limitQuery(text, a.rowLimit)
	}
	rs, err := a.db.QueryContext(ctx, text)
	if err != nil {
		return nil, err
	}
	rows, err := adapter.FromSQL(rs)
	if err != nil {
		return nil, err
	}
	return plainValues(q.answer(rows)), nil
}

// checkedQuery is a query DuckDB has looked at: the text to run and what its last Statement
// is.
type checkedQuery struct {
	text          string
	lastStatement string
	stmtType      duckdb.StmtType
}

// counted are the statements DuckDB answers with a single Count row, unless they say
// RETURNING.
var counted = map[duckdb.StmtType]bool{
	duckdb.STATEMENT_TYPE_INSERT: true,
	duckdb.STATEMENT_TYPE_UPDATE: true,
	duckdb.STATEMENT_TYPE_DELETE: true,
}

// noResult are the statements whose result says nothing: a Success column, or a Count that
// CREATE TABLE AS fills in.
var noResult = map[duckdb.StmtType]bool{
	duckdb.STATEMENT_TYPE_CREATE:       true,
	duckdb.STATEMENT_TYPE_CREATE_FUNC:  true,
	duckdb.STATEMENT_TYPE_ALTER:        true,
	duckdb.STATEMENT_TYPE_DROP:         true,
	duckdb.STATEMENT_TYPE_TRANSACTION:  true,
	duckdb.STATEMENT_TYPE_ANALYZE:      true,
	duckdb.STATEMENT_TYPE_VACUUM:       true,
	duckdb.STATEMENT_TYPE_VARIABLE_SET: true,
	duckdb.STATEMENT_TYPE_SET:          true,
}

// answer turns what DuckDB returned into what the statement means, so a write reports as on
// every other Source.
func (q checkedQuery) answer(rows adapter.Rows) adapter.Rows {
	switch {
	case q.isCount(rows):
		defer rows.Close()
		if !rows.Next() {
			return adapter.NoRows(0, false, rows.Err())
		}
		n, ok := rows.Values()[0].(int64)
		return adapter.NoRows(n, ok, rows.Err())
	case noResult[q.stmtType], q.saysOnlySuccess(rows):
		defer rows.Close()
		for rows.Next() {
		}
		return adapter.NoRows(0, false, rows.Err())
	}
	return rows
}

// isCount says rows is the one Count row of a write.
func (q checkedQuery) isCount(rows adapter.Rows) bool {
	cols := rows.Columns()
	return counted[q.stmtType] && !sqltext.MayReturnRows(q.lastStatement, Kind.Dialect) &&
		len(cols) == 1 && cols[0].Name == "Count"
}

// saysOnlySuccess is a PRAGMA or CALL, such as CHECKPOINT, with nothing to report.
func (q checkedQuery) saysOnlySuccess(rows adapter.Rows) bool {
	cols := rows.Columns()
	return (q.stmtType == duckdb.STATEMENT_TYPE_PRAGMA || q.stmtType == duckdb.STATEMENT_TYPE_CALL) &&
		len(cols) == 1 && cols[0].Name == "Success"
}

// Schema returns the single table the file is exposed as.
func (a *Adapter) Schema(ctx context.Context) ([]adapter.Table, error) {
	rs, err := a.db.QueryContext(ctx,
		"SELECT column_name, data_type FROM information_schema.columns WHERE table_name = ? ORDER BY ordinal_position",
		a.table)
	if err != nil {
		return nil, err
	}
	defer rs.Close()

	t := adapter.Table{Name: a.table, Ref: adapter.QuoteIdent(a.table)}
	for rs.Next() {
		var c adapter.Column
		if err := rs.Scan(&c.Name, &c.Type); err != nil {
			return nil, err
		}
		c.Ref = adapter.QuoteIdent(c.Name)
		t.Columns = append(t.Columns, c)
	}
	return []adapter.Table{t}, rs.Err()
}

// lock leaves the connection able to reach only the file at path, and a query unable to
// change that.
func lock(db *sql.DB, path string) error {
	for _, statement := range []string{
		"SET allow_persistent_secrets = false",
		"SET autoinstall_known_extensions = false",
		"SET autoload_known_extensions = false",
		"SET allowed_paths = [" + quoteLiteral(path) + "]",
		"SET enable_external_access = false",
		"SET lock_configuration = true",
	} {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

// allowed are the statements a file Source runs; anything else, including a kind not
// listed, is refused.
var allowed = map[duckdb.StmtType]bool{
	duckdb.STATEMENT_TYPE_SELECT:       true,
	duckdb.STATEMENT_TYPE_INSERT:       true,
	duckdb.STATEMENT_TYPE_UPDATE:       true,
	duckdb.STATEMENT_TYPE_DELETE:       true,
	duckdb.STATEMENT_TYPE_CREATE:       true,
	duckdb.STATEMENT_TYPE_CREATE_FUNC:  true,
	duckdb.STATEMENT_TYPE_ALTER:        true,
	duckdb.STATEMENT_TYPE_DROP:         true,
	duckdb.STATEMENT_TYPE_TRANSACTION:  true,
	duckdb.STATEMENT_TYPE_ANALYZE:      true,
	duckdb.STATEMENT_TYPE_VACUUM:       true,
	duckdb.STATEMENT_TYPE_VARIABLE_SET: true,
	duckdb.STATEMENT_TYPE_PRAGMA:       true,
	duckdb.STATEMENT_TYPE_SET:          true,
	duckdb.STATEMENT_TYPE_CALL:         true,
}

// refusedName is how a refusal names the statements people are likely to try.
var refusedName = map[duckdb.StmtType]string{
	duckdb.STATEMENT_TYPE_COPY:    "COPY",
	duckdb.STATEMENT_TYPE_EXPORT:  "EXPORT",
	duckdb.STATEMENT_TYPE_EXPLAIN: "EXPLAIN",
	duckdb.STATEMENT_TYPE_PREPARE: "PREPARE",
	duckdb.STATEMENT_TYPE_EXECUTE: "EXECUTE",
	duckdb.STATEMENT_TYPE_ATTACH:  "ATTACH",
	duckdb.STATEMENT_TYPE_DETACH:  "DETACH",
}

// checked has DuckDB classify each Statement of query and returns the text to run, or the
// reason it must not.
func (a *Adapter) checked(ctx context.Context, query string) (checkedQuery, error) {
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return checkedQuery{}, err
	}
	defer conn.Close()

	var q checkedQuery
	var pieces []string
	for _, s := range sqltext.Statements(query, Kind.Dialect) {
		if s.Blank {
			continue
		}
		piece := query[s.Start:s.End]
		stmtType, err := statementType(conn, piece)
		if err != nil {
			return checkedQuery{}, sayWhy(err)
		}
		if !allowed[stmtType] {
			return checkedQuery{}, refusal(stmtType)
		}
		pieces = append(pieces, piece)
		q.lastStatement, q.stmtType = piece, stmtType
	}
	q.text = strings.Join(pieces, "\n;\n")
	return q, nil
}

// statementType asks DuckDB what piece is with plain Prepare, which runs nothing, unlike
// PrepareContext.
func statementType(conn *sql.Conn, piece string) (t duckdb.StmtType, err error) {
	err = conn.Raw(func(dc any) error {
		stmt, err := dc.(driver.Conn).Prepare(piece)
		if err != nil {
			return err
		}
		defer stmt.Close()
		t, err = stmt.(*duckdb.Stmt).StatementType()
		return err
	})
	return t, err
}

// readOnly is what a refusal says about file Sources.
const readOnly = "file Sources are read-only and reach only their own file"

// refusal is the error for a statement of type t that may not run.
func refusal(t duckdb.StmtType) error {
	name, ok := refusedName[t]
	if !ok {
		name = "this statement"
	}
	return fmt.Errorf("%s, so %s is refused; ctrl+s exports a result", readOnly, name)
}

// sayWhy puts a reason on the two answers from Prepare that do not give one.
func sayWhy(err error) error {
	var engine *duckdb.Error
	switch {
	case errors.As(err, &engine) && engine.Type == duckdb.ErrorTypePermission:
		return fmt.Errorf("%s; ctrl+s exports a result (%w)", readOnly, err)
	case strings.Contains(err.Error(), "multi-statement"):
		return fmt.Errorf("%s, and DuckDB runs this as several statements, which cannot be checked first, so it is refused", readOnly)
	}
	return err
}

// absolute is the path the file is reached by, expanding a leading ~/ a shell left alone.
func absolute(path string) (string, error) {
	if rest, ok := strings.CutPrefix(filepath.ToSlash(path), "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, filepath.FromSlash(rest))
	}
	return filepath.Abs(path)
}

// readerFor is the DuckDB function that reads path, by its extension.
func readerFor(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv", ".tsv", ".txt":
		return "read_csv", nil
	case ".json", ".ndjson", ".jsonl":
		return "read_json_auto", nil
	case ".parquet":
		return "read_parquet", nil
	default:
		return "", fmt.Errorf("unsupported file type %q (supported: csv, tsv, json, ndjson, parquet)", filepath.Ext(path))
	}
}

// tableName derives a SQL-friendly name from the file's basename.
func tableName(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var b strings.Builder
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	name := b.String()
	if name == "" || unicode.IsDigit(rune(name[0])) {
		name = "t_" + name
	}
	return name
}

func quoteLiteral(s string) string { return `'` + strings.ReplaceAll(s, `'`, `''`) + `'` }
