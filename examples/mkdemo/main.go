// Command mkdemo writes a SQLite database of made-up shop data for trying out lode: go run
// ./examples/mkdemo examples/demo.sqlite
package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/EdwardWhalley/lode/examples/internal/demodata"
)

const schema = `
CREATE TABLE customers (
	id        INTEGER PRIMARY KEY,
	name      TEXT NOT NULL,
	email     TEXT,
	country   TEXT NOT NULL,
	signed_up TEXT NOT NULL,
	notes     TEXT
);
CREATE TABLE products (
	id       INTEGER PRIMARY KEY,
	name     TEXT NOT NULL,
	category TEXT NOT NULL,
	price    REAL NOT NULL
);
CREATE TABLE orders (
	id          INTEGER PRIMARY KEY,
	customer_id INTEGER NOT NULL REFERENCES customers(id),
	ordered_at  TEXT NOT NULL,
	status      TEXT NOT NULL
);
CREATE TABLE order_items (
	order_id   INTEGER NOT NULL REFERENCES orders(id),
	product_id INTEGER NOT NULL REFERENCES products(id),
	quantity   INTEGER NOT NULL
);
CREATE TABLE events (
	id      INTEGER PRIMARY KEY,
	at      TEXT NOT NULL,
	kind    TEXT NOT NULL,
	payload TEXT
);
CREATE VIEW customer_totals AS
	SELECT c.name, c.country, count(DISTINCT o.id) AS orders, round(sum(p.price * i.quantity), 2) AS total
	FROM customers c
	JOIN orders o ON o.customer_id = c.id
	JOIN order_items i ON i.order_id = o.id
	JOIN products p ON p.id = i.product_id
	WHERE o.status <> 'cancelled'
	GROUP BY c.id;
`

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: mkdemo <output.sqlite>")
		os.Exit(2)
	}
	if err := build(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "mkdemo:", err)
		os.Exit(1)
	}
}

// build replaces the database at path with one holding the demo data.
func build(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(schema); err != nil {
		return err
	}

	data := demodata.Generate()
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
	return tx.Commit()
}

func insert(tx *sql.Tx, table string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(rows[0])), ", ")
	stmt, err := tx.Prepare(fmt.Sprintf("INSERT INTO %s VALUES (%s)", table, placeholders))
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
