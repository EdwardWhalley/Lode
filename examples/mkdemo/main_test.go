package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func scalar(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestBuildProducesUsableDemoData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.sqlite")
	if err := build(path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for table, want := range map[string]int{"customers": 40, "products": 15, "orders": 200, "events": 25000} {
		if got := scalar(t, db, "SELECT count(*) FROM "+table); got != want {
			t.Errorf("%s has %d rows, want %d", table, got, want)
		}
	}
	if got := scalar(t, db, "SELECT count(*) FROM order_items"); got < 200 {
		t.Errorf("order_items has %d rows, want at least one per order", got)
	}
	if got := scalar(t, db, "SELECT count(*) FROM customer_totals"); got == 0 {
		t.Error("view customer_totals is empty")
	}
	if scalar(t, db, "SELECT count(*) FROM customers WHERE email IS NULL") == 0 {
		t.Error("no NULL emails to look at")
	}
	if scalar(t, db, "SELECT count(*) FROM events WHERE payload IS NULL") == 0 {
		t.Error("no NULL payloads to look at")
	}
	if got := scalar(t, db, "SELECT count(*) FROM orders WHERE customer_id NOT IN (SELECT id FROM customers)"); got != 0 {
		t.Errorf("%d orders reference missing customers", got)
	}
}

func TestBuildIsDeterministicAndReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	sum := func(name string) string {
		path := filepath.Join(dir, name)
		if err := build(path); err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var s string
		if err := db.QueryRow("SELECT group_concat(name || email, '|') FROM (SELECT * FROM customers ORDER BY id)").Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if a, b := sum("a.sqlite"), sum("a.sqlite"); a != b {
		t.Error("rebuilding over an existing file changed the data")
	}
	if a, b := sum("x.sqlite"), sum("y.sqlite"); a != b {
		t.Error("two builds produced different data")
	}
}
