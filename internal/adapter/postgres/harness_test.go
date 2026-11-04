package postgres_test

import (
	"database/sql"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// needsDefaultCredentials is what a test that logs in as someone else says when adminDSN's
// account is not postgres:postgres, as the embedded server's and docker-compose.yml's are.
const needsDefaultCredentials = "point it at a server whose admin account is postgres:postgres, such as docker-compose.yml's, or unset it for an embedded one"

// adminDSN connects to a server where tests may create databases: LODE_TEST_POSTGRES_DSN,
// else an embedded Postgres.
var adminDSN string

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		fmt.Println("skipping PostgreSQL tests in -short mode")
		os.Exit(0)
	}
	os.Exit(runWithServer(m))
}

func runWithServer(m *testing.M) int {
	if dsn := os.Getenv("LODE_TEST_POSTGRES_DSN"); dsn != "" {
		adminDSN = dsn
		return m.Run()
	}

	port, err := freePort()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no free port:", err)
		return 1
	}
	runtime, err := os.MkdirTemp("", "lode-pg-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(runtime)

	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Port(uint32(port)).
		RuntimePath(runtime).
		Version(embeddedpostgres.V16))
	if err := pg.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "cannot start embedded PostgreSQL (needs network on first run;",
			"set LODE_TEST_POSTGRES_DSN to use an existing server, or run with -short):", err)
		return 1
	}
	defer pg.Stop()

	adminDSN = fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", port)
	return m.Run()
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

var dbCounter int

// newDatabase creates an empty database for one test, dropped when it ends, and returns a
// DSN for it.
func newDatabase(t *testing.T) string {
	t.Helper()
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })

	dbCounter++
	name := fmt.Sprintf("lode_test_%d_%d", os.Getpid(), dbCounter)
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE " + name + " WITH (FORCE)"); err != nil {
			t.Logf("drop %s: %v", name, err)
		}
	})

	u, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}

// seed runs statements against the database with the given DSN.
func seed(t *testing.T, dsn string, statements ...string) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range statements {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}
