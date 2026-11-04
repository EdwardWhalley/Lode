// Package postgres is the Adapter for PostgreSQL servers, using pgx.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

const connectTimeout = 10 * time.Second

// errTransactionLeftOpen refuses what could not last: the driver throws away a connection
// returned with one open.
var errTransactionLeftOpen = errors.New("a transaction cannot stay open from one run to the next, because the next uses another connection: use a DO block or a procedure, or psql")

// Adapter queries a PostgreSQL database.
type Adapter struct {
	db    *sql.DB
	write bool
}

var _ adapter.Adapter = (*Adapter)(nil)

type Option func(*options)

type options struct{ write bool }

func WithWrite() Option { return func(o *options) { o.write = true } }

// Open connects using a postgres:// DSN, read-only unless WithWrite is given.
func Open(dsn string, opts ...Option) (*Adapter, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL DSN: %w", err)
	}
	if !o.write {
		cfg.RuntimeParams["default_transaction_read_only"] = "on"
	}

	db := stdlib.OpenDB(*cfg)
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return &Adapter{db: db, write: o.write}, nil
}

func (a *Adapter) Close() error { return a.db.Close() }

func (a *Adapter) Query(ctx context.Context, query string) (adapter.Rows, error) {
	if a.write {
		return a.queryForWrite(ctx, query)
	}

	tx, err := a.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	rs, err := tx.QueryContext(ctx, query)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	rows, err := adapter.FromSQL(rs)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return &txRows{Rows: rows, tx: tx}, nil
}

// queryForWrite has the server describe query and runs it on the same connection, through
// pgx only if it returns no columns.
func (a *Adapter) queryForWrite(ctx context.Context, query string) (adapter.Rows, error) {
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var answer adapter.Rows
	err = conn.Raw(func(dc any) error {
		pg := dc.(*stdlib.Conn).Conn()
		described, err := pg.Prepare(ctx, "", query)
		if err != nil {
			return err
		}
		if len(described.Fields) > 0 {
			return nil
		}
		tag, err := pg.PgConn().ExecParams(ctx, query, nil, nil, nil, nil).Close()
		if err != nil {
			return err
		}
		if pg.PgConn().TxStatus() != 'I' {
			pg.PgConn().Exec(ctx, "ROLLBACK").Close()
			return errTransactionLeftOpen
		}
		answer = answerFrom(tag)
		return nil
	})
	if err != nil || answer != nil {
		conn.Close()
		return answer, err
	}

	rs, err := conn.QueryContext(ctx, query)
	if err != nil {
		conn.Close()
		return nil, err
	}
	rows, err := adapter.FromSQL(rs)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &connRows{Rows: rows, conn: conn}, nil
}

// answerFrom reads a command tag, where only these tags count rows a statement changed.
func answerFrom(tag pgconn.CommandTag) adapter.Rows {
	if tag.Insert() || tag.Update() || tag.Delete() || strings.HasPrefix(tag.String(), "MERGE ") {
		return adapter.NoRows(tag.RowsAffected(), true, nil)
	}
	return adapter.NoRows(0, false, nil)
}

// connRows gives its connection back when the rows are closed.
type connRows struct {
	adapter.Rows
	conn *sql.Conn
}

func (r *connRows) Close() error {
	err := r.Rows.Close()
	r.conn.Close()
	return err
}

// txRows ends its transaction when the rows are closed.
type txRows struct {
	adapter.Rows
	tx *sql.Tx
}

func (r *txRows) Close() error {
	err := r.Rows.Close()
	r.tx.Rollback()
	return err
}

// Schema lists tables, views and materialized views outside the system schemas.
func (a *Adapter) Schema(ctx context.Context) ([]adapter.Table, error) {
	rs, err := a.db.QueryContext(ctx, `
		SELECT n.nspname, c.relname, a.attname, format_type(a.atttypid, a.atttypmod)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
		  AND n.nspname <> 'information_schema'
		  AND n.nspname NOT LIKE 'pg\_%'
		ORDER BY n.nspname, c.relname, a.attnum`)
	if err != nil {
		return nil, err
	}
	defer rs.Close()

	var tables []adapter.Table
	var lastSchema, lastRel string
	for rs.Next() {
		var schema, rel string
		var col adapter.Column
		if err := rs.Scan(&schema, &rel, &col.Name, &col.Type); err != nil {
			return nil, err
		}
		if len(tables) == 0 || schema != lastSchema || rel != lastRel {
			name := rel
			if schema != "public" {
				name = schema + "." + rel
			}
			tables = append(tables, adapter.Table{
				Name: name,
				Ref:  adapter.QuoteIdent(schema) + "." + adapter.QuoteIdent(rel),
			})
			lastSchema, lastRel = schema, rel
		}
		col.Ref = adapter.QuoteIdent(col.Name)
		t := &tables[len(tables)-1]
		t.Columns = append(t.Columns, col)
	}
	if err := rs.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	return tables, nil
}
