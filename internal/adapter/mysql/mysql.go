// Package mysql is the Adapter for MySQL and MariaDB servers.
package mysql

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	driver "github.com/go-sql-driver/mysql"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

const (
	connectTimeout = 10 * time.Second
	killTimeout    = 5 * time.Second
)

// errTransactionLeftOpen refuses what would stay open on an idle connection after a run.
var errTransactionLeftOpen = errors.New("a transaction cannot stay open from one run to the next, because the next may use another connection: use a procedure, or the mysql client")

// Adapter queries a MySQL or MariaDB server.
type Adapter struct {
	db    *sql.DB
	write bool
}

var _ adapter.Adapter = (*Adapter)(nil)

type Option func(*options)

type options struct{ write bool }

func WithWrite() Option { return func(o *options) { o.write = true } }

// Open connects using a mysql:// or mariadb:// DSN, read-only unless WithWrite is given.
func Open(dsn string, opts ...Option) (*Adapter, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	cfg, err := configFromDSN(dsn)
	if err != nil {
		return nil, err
	}
	connector, err := driver.NewConnector(cfg)
	if err != nil {
		return nil, fmt.Errorf("invalid MySQL DSN: %w", err)
	}

	db := sql.OpenDB(connector)
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to MySQL: %w", err)
	}
	if o.write {
		if err := refuseWritesThatWouldNotCommit(ctx, db); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &Adapter{db: db, write: o.write}, nil
}

// refuseWritesThatWouldNotCommit stops a Source opening for writes when new connections
// have autocommit off.
func refuseWritesThatWouldNotCommit(ctx context.Context, db *sql.DB) error {
	var autocommit int
	if err := db.QueryRowContext(ctx, "SELECT @@autocommit").Scan(&autocommit); err != nil {
		return fmt.Errorf("connect to MySQL: %w", err)
	}
	if autocommit == 0 {
		return errors.New("autocommit is off for this connection, by its options or the server's default, so writes would be lost: turn it on, or open the Source read-only")
	}
	return nil
}

// configFromDSN converts a URL-style DSN to the driver's configuration.
func configFromDSN(dsn string) (*driver.Config, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("invalid MySQL DSN: %w", err)
	}
	if u.Scheme != "mysql" && u.Scheme != "mariadb" {
		return nil, fmt.Errorf("invalid MySQL DSN: scheme must be mysql:// or mariadb://")
	}

	cfg := driver.NewConfig()
	cfg.User = u.User.Username()
	cfg.Passwd, _ = u.User.Password()
	host := u.Hostname()
	if host == "" {
		host = "localhost"
	}
	port := u.Port()
	if port == "" {
		port = "3306"
	}
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(host, port)
	cfg.DBName = strings.TrimPrefix(u.Path, "/")
	cfg.Timeout = connectTimeout
	cfg.ParseTime = true
	cfg.Loc = time.UTC

	for key, values := range u.Query() {
		value := values[len(values)-1]
		if key == "tls" {
			cfg.TLSConfig = value
			continue
		}
		if cfg.Params == nil {
			cfg.Params = map[string]string{}
		}
		cfg.Params[key] = value
	}
	return cfg, nil
}

func (a *Adapter) Close() error { return a.db.Close() }

// Query runs query on a dedicated connection, and kills it on the server if ctx is
// cancelled.
func (a *Adapter) Query(ctx context.Context, query string) (adapter.Rows, error) {
	if a.write && sqltext.LeavesTransactionOpen(query, Kind.Dialect) {
		return nil, errTransactionLeftOpen
	}
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return nil, err
	}

	var id uint64
	if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&id); err != nil {
		conn.Close()
		return nil, err
	}
	stop := a.killOnCancel(ctx, id)

	r := &connRows{ctx: ctx, conn: conn, stop: stop}
	var rs *sql.Rows
	if a.write {
		verb := sqltext.LastStatementVerb(query, Kind.Dialect)
		r.counts = rowCountAnswersFor(verb)
		r.settles = (!r.counts && verb != "SELECT") || hasVersionComment(query)
		rs, err = conn.QueryContext(ctx, query)
		if err == nil && verb == "SET" {
			var finished bool
			if finished, err = finishSet(ctx, conn, rs); finished && err == nil {
				r.Rows = adapter.NoRows(0, false, nil)
				return r, nil
			}
		}
	} else {
		if _, err = conn.ExecContext(ctx, "SET SESSION TRANSACTION READ ONLY"); err == nil {
			if r.tx, err = conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true}); err == nil {
				rs, err = r.tx.QueryContext(ctx, query)
			}
		}
	}
	if err == nil {
		r.Rows, err = adapter.FromSQL(rs)
	}
	if err != nil {
		r.release()
		return nil, err
	}
	return r, nil
}

// killOnCancel sends KILL QUERY for connection id if ctx ends before stop is called; stop
// waits for any kill in progress.
func (a *Adapter) killOnCancel(ctx context.Context, id uint64) (stop func()) {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case <-ctx.Done():
			killCtx, cancel := context.WithTimeout(context.Background(), killTimeout)
			defer cancel()
			a.db.ExecContext(killCtx, fmt.Sprintf("KILL QUERY %d", id))
		case <-done:
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-finished
	}
}

// connRows releases the connection (and ends its transaction) when closed.
type connRows struct {
	adapter.Rows
	tx      *sql.Tx
	ctx     context.Context
	conn    *sql.Conn
	stop    func()
	counts  bool
	settles bool
}

// RowsAffected asks ROW_COUNT() on the connection the statement ran on, and only for a
// write with no columns.
func (r *connRows) RowsAffected() (int64, bool) {
	if !r.counts || len(r.Columns()) > 0 {
		return 0, false
	}
	var n int64
	if err := r.conn.QueryRowContext(r.ctx, "SELECT ROW_COUNT()").Scan(&n); err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func (r *connRows) Close() error {
	var err error
	if r.Rows != nil {
		err = r.Rows.Close()
	}
	r.release()
	return err
}

// release ends the transaction, stops the kill watcher, settles the connection if needed
// and returns it to the pool.
func (r *connRows) release() {
	if r.tx != nil {
		r.tx.Rollback()
	}
	r.stop()
	if r.settles {
		settle(r.conn)
	}
	r.conn.Close()
}

// finishSet ends a SET that returned no rows, refusing if it left autocommit off.
func finishSet(ctx context.Context, conn *sql.Conn, rs *sql.Rows) (finished bool, err error) {
	cols, err := rs.Columns()
	if err != nil || len(cols) > 0 {
		return false, err
	}
	rs.Close()
	return true, refuseAutocommitOff(ctx, conn)
}

func refuseAutocommitOff(ctx context.Context, conn *sql.Conn) error {
	var autocommit int
	if err := conn.QueryRowContext(ctx, "SELECT @@autocommit").Scan(&autocommit); err != nil {
		return err
	}
	if autocommit == 0 {
		return errTransactionLeftOpen
	}
	return nil
}

// hasVersionComment says text holds a comment the server runs as SQL.
func hasVersionComment(text string) bool {
	return strings.Contains(text, "/*!") || strings.Contains(text, "/*M!")
}

// settle clears what a statement could leave on a connection (a transaction, a table lock,
// autocommit off), or drops the connection.
func settle(conn *sql.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), killTimeout)
	defer cancel()
	for _, clear := range []string{"ROLLBACK", "UNLOCK TABLES", "SET autocommit = 1"} {
		if _, err := conn.ExecContext(ctx, clear); err != nil {
			conn.Raw(func(any) error { return sqldriver.ErrBadConn })
			return
		}
	}
}

// rowCountAnswersFor says verb is one that ROW_COUNT() answers for.
func rowCountAnswersFor(verb string) bool {
	switch verb {
	case "INSERT", "UPDATE", "DELETE", "REPLACE":
		return true
	}
	return false
}

// Schema lists tables and views: bare for the selected database, else as database.table.
func (a *Adapter) Schema(ctx context.Context) ([]adapter.Table, error) {
	var current sql.NullString
	if err := a.db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&current); err != nil {
		return nil, err
	}

	query := `SELECT table_schema, table_name, column_name, column_type
		FROM information_schema.columns
		WHERE table_schema NOT IN ('mysql', 'information_schema', 'performance_schema', 'sys')`
	var args []any
	if current.Valid {
		query += " AND table_schema = ?"
		args = append(args, current.String)
	}
	query += " ORDER BY table_schema, table_name, ordinal_position"

	rs, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()

	var tables []adapter.Table
	var lastSchema, lastTable string
	for rs.Next() {
		var schema, table string
		var col adapter.Column
		if err := rs.Scan(&schema, &table, &col.Name, &col.Type); err != nil {
			return nil, err
		}
		if len(tables) == 0 || schema != lastSchema || table != lastTable {
			t := adapter.Table{Name: table, Ref: quoteIdent(table)}
			if !current.Valid {
				t.Name = schema + "." + table
				t.Ref = quoteIdent(schema) + "." + quoteIdent(table)
			}
			tables = append(tables, t)
			lastSchema, lastTable = schema, table
		}
		col.Ref = quoteIdent(col.Name)
		t := &tables[len(tables)-1]
		t.Columns = append(t.Columns, col)
	}
	if err := rs.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	return tables, nil
}

// quoteIdent quotes an identifier in backticks.
func quoteIdent(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }
