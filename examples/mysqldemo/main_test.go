package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/examples/internal/demodata"
	"github.com/EdwardWhalley/lode/internal/adapter/adaptertest"
)

func TestRedactNeverPrintsThePassword(t *testing.T) {
	got := redact("mysql://root:hunter2@127.0.0.1:33306/shop")
	if strings.Contains(got, "hunter2") {
		t.Fatalf("redact leaks the password: %s", got)
	}
	if want := "mysql://root:${LODE_DEMO_PASSWORD}@127.0.0.1:33306/shop"; got != want {
		t.Errorf("redact = %s, want %s", got, want)
	}
	if got := redact("mysql://root@127.0.0.1:33306/"); got != "mysql://root@127.0.0.1:33306/" {
		t.Errorf("a DSN without a password should be unchanged, got %s", got)
	}
}

func TestWithDatabase(t *testing.T) {
	if got, want := withDatabase("mariadb://u:p@h:1/old", "shop"), "mariadb://u:p@h:1/shop"; got != want {
		t.Errorf("withDatabase = %s, want %s", got, want)
	}
	if got, want := withDatabase("mysql://u:p@h:1/old", ""), "mysql://u:p@h:1/"; got != want {
		t.Errorf("withDatabase(empty) = %s, want %s", got, want)
	}
}

// servers are the MySQL and MariaDB test servers and the variables that name them.
var servers = []struct{ name, envVar, scheme string }{
	{"MySQL", "LODE_TEST_MYSQL_DSN", "mysql"},
	{"MariaDB", "LODE_TEST_MARIADB_DSN", "mariadb"},
}

// eachServer runs fn against every server with fresh databases, each a subtest that
// adaptertest.NoServer ends if its variable is unset.
func eachServer(t *testing.T, fn func(t *testing.T, db *sql.DB, shop string)) {
	t.Helper()
	n := 0
	for _, server := range servers {
		t.Run(server.name, func(t *testing.T) {
			dsn := os.Getenv(server.envVar)
			if dsn == "" {
				adaptertest.NoServer(t, server.envVar, "set it to a "+server.scheme+":// DSN of an account that can create databases (CI runs these)")
			}
			db, err := open(dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			n++
			shop := fmt.Sprintf("lode_demo_test_%d_%d", os.Getpid(), n)
			t.Cleanup(func() {
				db.Exec("DROP DATABASE IF EXISTS " + quote(shop))
				db.Exec("DROP DATABASE IF EXISTS " + quote(shop+"_reporting"))
			})
			fn(t, db, shop)
		})
	}
}

func count(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestLoadCreatesTheDemoData(t *testing.T) {
	eachServer(t, func(t *testing.T, db *sql.DB, shop string) {
		if err := load(context.Background(), db, shop); err != nil {
			t.Fatal(err)
		}
		for table, want := range map[string]int{
			"customers": demodata.NumCustomers,
			"products":  demodata.NumProducts,
			"orders":    demodata.NumOrders,
			"events":    demodata.NumEvents,
		} {
			if got := count(t, db, fmt.Sprintf("SELECT count(*) FROM %s.%s", quote(shop), table)); got != want {
				t.Errorf("%s has %d rows, want %d", table, got, want)
			}
		}
		if count(t, db, fmt.Sprintf("SELECT count(*) FROM %s.order_items", quote(shop))) < demodata.NumOrders {
			t.Error("order_items has fewer rows than orders")
		}
		if count(t, db, fmt.Sprintf("SELECT count(*) FROM %s.customer_totals", quote(shop))) == 0 {
			t.Error("view customer_totals is empty")
		}
		if count(t, db, fmt.Sprintf("SELECT count(*) FROM %s.daily_events", quote(shop+"_reporting"))) == 0 {
			t.Error("view shop_reporting.daily_events is empty")
		}
		if count(t, db, fmt.Sprintf("SELECT count(*) FROM %s.customers WHERE email IS NULL", quote(shop))) == 0 {
			t.Error("no NULL emails to look at")
		}
		if count(t, db, fmt.Sprintf("SELECT count(*) FROM %s.events WHERE JSON_EXTRACT(payload, '$.user') IS NOT NULL", quote(shop))) == 0 {
			t.Error("payloads were not stored as JSON")
		}
	})
}

func TestLoadCanBeRunAgain(t *testing.T) {
	eachServer(t, func(t *testing.T, db *sql.DB, shop string) {
		for i := 0; i < 2; i++ {
			if err := load(context.Background(), db, shop); err != nil {
				t.Fatalf("run %d: %v", i+1, err)
			}
		}
		if got := count(t, db, fmt.Sprintf("SELECT count(*) FROM %s.customers", quote(shop))); got != demodata.NumCustomers {
			t.Errorf("customers has %d rows after a second run, want %d (not doubled)", got, demodata.NumCustomers)
		}
	})
}

func TestLoadRefusesToReplaceADatabaseItDidNotCreate(t *testing.T) {
	eachServer(t, func(t *testing.T, db *sql.DB, shop string) {
		for _, victim := range []string{shop, shop + "_reporting"} {
			if _, err := db.Exec("CREATE DATABASE " + quote(victim)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("CREATE TABLE " + quote(victim) + ".precious (v INT)"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("INSERT INTO " + quote(victim) + ".precious VALUES (42)"); err != nil {
				t.Fatal(err)
			}

			err := load(context.Background(), db, shop)
			if err == nil || !strings.Contains(err.Error(), "refusing") {
				t.Fatalf("load over an unmarked %q: err = %v, want a refusal", victim, err)
			}
			if got := count(t, db, fmt.Sprintf("SELECT v FROM %s.precious", quote(victim))); got != 42 {
				t.Errorf("the existing database %q was modified", victim)
			}
			if _, err := db.Exec("DROP DATABASE " + quote(victim)); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestLoadRejectsAnInvalidName(t *testing.T) {
	if err := load(context.Background(), nil, ""); err == nil {
		t.Error("an empty database name should be rejected")
	}
	if err := load(context.Background(), nil, "a`b"); err == nil {
		t.Error("a name containing a backtick should be rejected")
	}
}
