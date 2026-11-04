// Package sqlite is the Adapter for SQLite database files, using a pure-Go driver.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"

	sqlitedriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

// errTransactionLeftOpen refuses what would stay open on an idle connection after a run.
var errTransactionLeftOpen = errors.New("a transaction cannot stay open from one run to the next, because the next may use another connection: select everything from BEGIN to COMMIT and run that together")

// readOnlyDriver opens read-only Sources. Its connections refuse ATTACH, and so VACUUM INTO,
// which reach a file without the mode=ro the Source was opened with: a query could attach
// the Source's own file again and write it, or create any other file.
var readOnlyDriver = &sqlitedriver.Driver{}

const readOnlyDriverName = "lode-sqlite-read-only"

func init() {
	readOnlyDriver.RegisterConnectionHook(refuseAttach)
	sql.Register(readOnlyDriverName, readOnlyDriver)
}

// refuseAttach has a new connection refuse to attach a database.
func refuseAttach(conn sqlitedriver.ExecQuerierContext, _ string) error {
	r, ok := conn.(sqlitedriver.AuthorizerRegisterer)
	if !ok {
		return errors.New("open sqlite: the driver cannot refuse ATTACH, so the Source cannot be read-only")
	}
	return r.RegisterAuthorizer(func(action sqlitedriver.AuthorizerActionCode, _, _, _, _ string) sqlitedriver.AuthorizerReturnCode {
		if action == sqlitedriver.AuthAttach {
			return sqlitedriver.AuthorizerDeny
		}
		return sqlitedriver.AuthorizerOK
	})
}

// errNoAttach says why a read-only Source refused what the authorizer did.
var errNoAttach = errors.New("a read-only SQLite Source cannot attach a database or VACUUM INTO a file: either reaches a file without the Source's read-only setting")

// Adapter queries a SQLite database file.
type Adapter struct {
	db    *sql.DB
	write bool
}

var _ adapter.Adapter = (*Adapter)(nil)

type Option func(*options)

type options struct{ write bool }

func WithWrite() Option { return func(o *options) { o.write = true } }

// Open opens the existing database file, read-only unless WithWrite is given.
func Open(path string, opts ...Option) (*Adapter, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	query, driverName := "mode=ro&_pragma=query_only(1)", readOnlyDriverName
	if o.write {
		query, driverName = "mode=rw", "sqlite"
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: query}
	if runtime.GOOS == "windows" {
		u.Path = "/" + u.Path
	}

	db, err := sql.Open(driverName, u.String())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := db.Exec("SELECT 1 FROM sqlite_master LIMIT 1"); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &Adapter{db: db, write: o.write}, nil
}

func (a *Adapter) Close() error { return a.db.Close() }

func (a *Adapter) Query(ctx context.Context, query string) (adapter.Rows, error) {
	if a.write {
		return a.queryForWrite(ctx, query)
	}
	rs, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, sayWhy(err)
	}
	return adapter.FromSQL(rs)
}

// sayWhy puts the reason on the authorizer's refusal, which says only "not authorized".
func sayWhy(err error) error {
	var engine *sqlitedriver.Error
	if errors.As(err, &engine) && engine.Code() == sqlite3.SQLITE_AUTH {
		return errNoAttach
	}
	return err
}

// queryForWrite runs query on one dedicated connection, because changes() answers for
// the connection it is asked on.
func (a *Adapter) queryForWrite(ctx context.Context, query string) (adapter.Rows, error) {
	if sqltext.LeavesTransactionOpen(query, Kind.Dialect) {
		return nil, errTransactionLeftOpen
	}
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	rs, err := conn.QueryContext(ctx, query)
	if err != nil {
		giveBack(conn)
		return nil, err
	}
	rows, err := adapter.FromSQL(rs)
	if err != nil {
		giveBack(conn)
		return nil, err
	}
	if len(rows.Columns()) > 0 {
		return &connRows{Rows: rows, conn: conn}, nil
	}
	return finishWrite(ctx, conn, rows, changesAnswersFor(query))
}

// finishWrite ends a statement with no result: it asks changes() for the count and refuses
// a transaction left open.
func finishWrite(ctx context.Context, conn *sql.Conn, rows adapter.Rows, counts bool) (adapter.Rows, error) {
	for rows.Next() {
	}
	err := rows.Err()
	rows.Close()
	if err != nil {
		giveBack(conn)
		return nil, err
	}
	var n int64
	known := counts && conn.QueryRowContext(ctx, "SELECT changes()").Scan(&n) == nil
	if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err == nil {
		conn.Close()
		return nil, errTransactionLeftOpen
	}
	conn.Close()
	return adapter.NoRows(n, known, nil), nil
}

// giveBack returns conn to the pool with no transaction open.
func giveBack(conn *sql.Conn) {
	conn.ExecContext(context.Background(), "ROLLBACK")
	conn.Close()
}

// connRows holds its connection until the rows are closed.
type connRows struct {
	adapter.Rows
	conn *sql.Conn
}

func (r *connRows) Close() error {
	err := r.Rows.Close()
	giveBack(r.conn)
	return err
}

// changesAnswersFor says the last Statement of query is one that changes() answers for.
func changesAnswersFor(query string) bool {
	switch sqltext.LastStatementVerb(query, Kind.Dialect) {
	case "INSERT", "UPDATE", "DELETE", "REPLACE":
		return true
	}
	return false
}

func (a *Adapter) Schema(ctx context.Context) ([]adapter.Table, error) {
	rs, err := a.db.QueryContext(ctx,
		`SELECT name FROM sqlite_master
		 WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite\_%' ESCAPE '\'
		 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var names []string
	for rs.Next() {
		var name string
		if err := rs.Scan(&name); err != nil {
			rs.Close()
			return nil, err
		}
		names = append(names, name)
	}
	if err := rs.Err(); err != nil {
		rs.Close()
		return nil, err
	}
	rs.Close()

	tables := make([]adapter.Table, 0, len(names))
	for _, name := range names {
		cols, err := a.columns(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("columns of %s: %w", name, err)
		}
		tables = append(tables, adapter.Table{Name: name, Ref: adapter.QuoteIdent(name), Columns: cols})
	}
	return tables, nil
}

func (a *Adapter) columns(ctx context.Context, table string) ([]adapter.Column, error) {
	rs, err := a.db.QueryContext(ctx, "SELECT name, type FROM pragma_table_info(?) ORDER BY cid", table)
	if err != nil {
		return nil, err
	}
	defer rs.Close()

	var cols []adapter.Column
	for rs.Next() {
		var c adapter.Column
		if err := rs.Scan(&c.Name, &c.Type); err != nil {
			return nil, err
		}
		c.Type = strings.ToUpper(c.Type)
		c.Ref = adapter.QuoteIdent(c.Name)
		cols = append(cols, c)
	}
	return cols, rs.Err()
}
