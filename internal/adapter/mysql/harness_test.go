package mysql_test

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/adapter/adaptertest"
	driver "github.com/go-sql-driver/mysql"
)

// server is a server the integration tests can use, through an admin account.
type server struct {
	name     string
	envVar   string
	scheme   string
	adminDSN string
}

var servers = []server{
	{name: "MySQL", envVar: "LODE_TEST_MYSQL_DSN", scheme: "mysql"},
	{name: "MariaDB", envVar: "LODE_TEST_MARIADB_DSN", scheme: "mariadb"},
}

// eachServer runs fn once per server, each a subtest that adaptertest.NoServer ends if its
// variable is unset.
func eachServer(t *testing.T, fn func(t *testing.T, s server)) {
	t.Helper()
	for _, s := range servers {
		t.Run(s.name, func(t *testing.T) {
			s.adminDSN = os.Getenv(s.envVar)
			if s.adminDSN == "" {
				adaptertest.NoServer(t, s.envVar, "set it to a "+s.scheme+":// DSN of an account that can create databases")
			}
			fn(t, s)
		})
	}
}

// admin opens a direct connection with the admin account (driver DSN form).
func (s server) admin(t *testing.T, database string) *sql.DB {
	t.Helper()
	u, err := url.Parse(s.adminDSN)
	if err != nil {
		t.Fatalf("%s is not a valid URL", s.envVar)
	}
	cfg := driver.NewConfig()
	cfg.User = u.User.Username()
	cfg.Passwd, _ = u.User.Password()
	cfg.Net = "tcp"
	cfg.Addr = u.Host
	if u.Port() == "" {
		cfg.Addr += ":3306"
	}
	cfg.DBName = database
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

var dbCounter int

// newDatabase creates an empty database for one test, dropped when it ends, and returns a
// mysql:// DSN for it.
func (s server) newDatabase(t *testing.T) string {
	t.Helper()
	admin := s.admin(t, "")
	dbCounter++
	name := fmt.Sprintf("lode_test_%d_%d", os.Getpid(), dbCounter)
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE " + name); err != nil {
			t.Logf("drop %s: %v", name, err)
		}
	})
	return s.dsnFor(t, name)
}

// dsnFor returns the admin DSN pointed at the given database ("" for none).
func (s server) dsnFor(t *testing.T, database string) string {
	t.Helper()
	u, err := url.Parse(s.adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + database
	return u.String()
}

// seed runs statements against the named database as the admin account.
func (s server) seed(t *testing.T, database string, statements ...string) {
	t.Helper()
	db := s.admin(t, database)
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func databaseOf(dsn string) string {
	u, _ := url.Parse(dsn)
	return strings.TrimPrefix(u.Path, "/")
}
