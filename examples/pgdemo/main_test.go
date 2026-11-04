package main

import (
	"database/sql"
	"net"
	"testing"

	"github.com/EdwardWhalley/lode/examples/internal/demodata"
)

func TestStartLoadsTheDemoData(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a PostgreSQL server")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	dsn, stop, err := start(uint32(port))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := stop(); err != nil {
			t.Error(err)
		}
	}()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	count := func(query string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(query).Scan(&n); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return n
	}
	for table, want := range map[string]int{
		"customers": demodata.NumCustomers,
		"products":  demodata.NumProducts,
		"orders":    demodata.NumOrders,
		"events":    demodata.NumEvents,
	} {
		if got := count("SELECT count(*) FROM " + table); got != want {
			t.Errorf("%s has %d rows, want %d", table, got, want)
		}
	}
	if count("SELECT count(*) FROM reporting.customer_totals") == 0 {
		t.Error("reporting.customer_totals is empty")
	}
	if count("SELECT count(*) FROM reporting.daily_events") == 0 {
		t.Error("reporting.daily_events is empty (materialized view not refreshed?)")
	}
	if count("SELECT count(*) FROM customers WHERE email IS NULL") == 0 {
		t.Error("no NULL emails to look at")
	}
	if count("SELECT count(*) FROM events WHERE payload->>'user' IS NOT NULL") == 0 {
		t.Error("jsonb payloads were not stored as JSON")
	}
}
