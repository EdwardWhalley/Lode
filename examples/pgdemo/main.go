// Command pgdemo runs a throwaway PostgreSQL server loaded with the demo shop data, until
// interrupted: go run ./examples/pgdemo
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/EdwardWhalley/lode/examples/internal/demodata"
)

const ddl = `
CREATE TABLE customers (
	id        integer PRIMARY KEY,
	name      text NOT NULL,
	email     text,
	country   text NOT NULL,
	signed_up date NOT NULL,
	notes     text
);
CREATE TABLE products (
	id       integer PRIMARY KEY,
	name     text NOT NULL,
	category text NOT NULL,
	price    numeric(10,2) NOT NULL
);
CREATE TABLE orders (
	id          integer PRIMARY KEY,
	customer_id integer NOT NULL REFERENCES customers(id),
	ordered_at  timestamp NOT NULL,
	status      text NOT NULL
);
CREATE TABLE order_items (
	order_id   integer NOT NULL REFERENCES orders(id),
	product_id integer NOT NULL REFERENCES products(id),
	quantity   integer NOT NULL
);
CREATE TABLE events (
	id      integer PRIMARY KEY,
	at      timestamp NOT NULL,
	kind    text NOT NULL,
	payload jsonb
);

-- Not in "public", so lode shows these as reporting.<name>.
CREATE SCHEMA reporting;
CREATE VIEW reporting.customer_totals AS
	SELECT c.name, c.country, count(DISTINCT o.id) AS orders, round(sum(p.price * i.quantity), 2) AS total
	FROM customers c
	JOIN orders o ON o.customer_id = c.id
	JOIN order_items i ON i.order_id = o.id
	JOIN products p ON p.id = i.product_id
	WHERE o.status <> 'cancelled'
	GROUP BY c.id;
CREATE MATERIALIZED VIEW reporting.daily_events AS
	SELECT at::date AS day, kind, count(*) AS events FROM events GROUP BY 1, 2;
`

func main() {
	port := flag.Uint("port", 54329, "port to listen on")
	flag.Parse()

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopSignals()

	fmt.Println("starting PostgreSQL (the first run downloads it)...")
	dsn, stop, err := start(uint32(*port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "pgdemo:", err)
		os.Exit(1)
	}

	fmt.Printf(`
PostgreSQL is ready, loaded with the demo shop data.

Add to ~/.config/lode/sources.toml:

  [sources.pg-demo]
  dsn = "%s"

  [sources.pg-demo-rw]
  dsn = "%s"
  write = true

Then, in another terminal:  go run ./cmd/lode pg-demo

Press Ctrl+C here to stop the server (its data is discarded).
`, dsn, dsn)

	<-ctx.Done()
	fmt.Println("\nstopping...")
	if err := stop(); err != nil {
		fmt.Fprintln(os.Stderr, "pgdemo:", err)
		os.Exit(1)
	}
}

// start runs a PostgreSQL server on port loaded with the demo data, and returns a DSN and a
// function that stops it.
func start(port uint32) (dsn string, stop func() error, err error) {
	runtime, err := os.MkdirTemp("", "lode-pgdemo-")
	if err != nil {
		return "", nil, err
	}
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Port(port).
		Database("demo").
		RuntimePath(runtime).
		Version(embeddedpostgres.V16))
	if err := pg.Start(); err != nil {
		os.RemoveAll(runtime)
		return "", nil, fmt.Errorf("start PostgreSQL: %w", err)
	}
	stop = func() error {
		err := pg.Stop()
		removeAll(runtime)
		return err
	}

	dsn = fmt.Sprintf("postgres://postgres:postgres@localhost:%d/demo?sslmode=disable", port)
	if err := load(dsn, demodata.Generate()); err != nil {
		stop()
		return "", nil, fmt.Errorf("load demo data: %w", err)
	}
	return dsn, stop, nil
}

// removeAll deletes dir, retrying briefly because Windows keeps the server's files locked
// for a moment.
func removeAll(dir string) {
	for i := 0; i < 25; i++ {
		if os.RemoveAll(dir) == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func load(dsn string, data demodata.Data) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(ddl); err != nil {
		return err
	}
	for _, t := range []struct {
		name string
		rows [][]any
	}{
		{"customers", data.Customers},
		{"products", data.Products},
		{"orders", data.Orders},
		{"order_items", data.OrderItems},
		{"events", data.Events},
	} {
		if err := insert(tx, t.name, t.rows); err != nil {
			return fmt.Errorf("%s: %w", t.name, err)
		}
	}
	if _, err := tx.Exec("REFRESH MATERIALIZED VIEW reporting.daily_events"); err != nil {
		return err
	}
	return tx.Commit()
}

func insert(tx *sql.Tx, table string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	params := make([]string, len(rows[0]))
	for i := range params {
		params[i] = fmt.Sprintf("$%d", i+1)
	}
	stmt, err := tx.Prepare(fmt.Sprintf("INSERT INTO %s VALUES (%s)", table, strings.Join(params, ", ")))
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.Exec(row...); err != nil {
			return err
		}
	}
	return nil
}
