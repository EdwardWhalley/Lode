package mysql_test

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/EdwardWhalley/lode/internal/adapter/adaptertest"
	"github.com/EdwardWhalley/lode/internal/adapter/mysql"
)

const peopleFixture = "CREATE TABLE people (id INT PRIMARY KEY, name VARCHAR(100))"
const peopleRows = "INSERT INTO people VALUES (1, 'alice'), (2, 'bob'), (3, 'carol')"

// hugeQuery is a cartesian product of the three people, far too many rows to read out;
// every test cancels it.
const hugeQuery = "SELECT 1 FROM people a, people b, people c, people d, people e, people f, people g, people h, people i, people j, people k, people l, people m, people n, people o, people p"

// Each escape that switches something off is followed by a CREATE TABLE, because MySQL
// commits before DDL, and by the contract's own INSERT.
const afterEscape = "CREATE TABLE after_escape (a INT)"

const largestUnsigned = "18446744073709551615" // bounded: a value in one row, not a count of rows

// typesTable has a row of each common MySQL type, then a row of NULLs.
var typesTable = []string{
	`CREATE TABLE all_types (ti TINYINT, si SMALLINT, mi MEDIUMINT, i INT, bi BIGINT, ubi BIGINT UNSIGNED,
		ui INT UNSIGNED, f FLOAT, d DOUBLE, dec1 DECIMAL(10,2), c CHAR(3), v VARCHAR(10), tx TEXT, b BLOB,
		bin BINARY(2), vb VARBINARY(4), dt DATE, dtt DATETIME(6), ts TIMESTAMP NULL, tm TIME, y YEAR, bit1 BIT(8),
		e ENUM('a','b'), st SET('a','b'), j JSON, bo BOOLEAN)`,
	`INSERT INTO all_types VALUES (1, 1, 1, 1, 1, ` + largestUnsigned + `, 1, 0.1, 0.1, 1.50, 'x', 'x', 'x', 'x',
		'ab', 'ab', '2026-01-01', '2026-01-01 01:02:03.5', '2026-01-01 01:02:03', '01:02:03', 2026, b'101', 'a',
		'a,b', '{"a": 1}', true)`,
	`INSERT INTO all_types (i) VALUES (NULL)`,
}

// opensTransaction are the statements that would leave a transaction open, or autocommit
// off, on a MySQL or MariaDB connection.
var opensTransaction = []string{
	"BEGIN",
	"begin;",
	"# a note\nBEGIN",
	"START TRANSACTION",
	"COMMIT AND CHAIN",
	"COMMIT AND CHAIN NO RELEASE",
	"XA START 'x'",
	"SET autocommit = 0",
	"SET @@session.autocommit = 0",
	"SET `autocommit` = 0",
	"SET autocommit = 1 - 1",
	"SET autocommit = 1, autocommit = 0",
	"SET @x = 1 /*!, autocommit = 0 */",
}

// secret is a password and locations that hold it and fail to open, some on server s.
func secret(t *testing.T, s server) adaptertest.Secret {
	t.Helper()
	const password = "hunter2"
	u, err := url.Parse(s.adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	wrong := *u
	wrong.User = url.UserPassword(u.User.Username(), password)
	badOption := wrong
	badOption.RawQuery = "tls=bogus"
	return adaptertest.Secret{Password: password, Locations: []string{
		wrong.String(),
		badOption.String(),
		"mysql://reader:hunter2@127.0.0.1:1/app",
		"mariadb://reader:hunter2@127.0.0.1:1/app",
		"mysql://reader:hunter2@host:notaport/app",
		"postgres://reader:hunter2@127.0.0.1:1/app",
	}}
}

func TestContract(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		adaptertest.Run(t, func(t *testing.T) adaptertest.Target {
			dsn := s.newDatabase(t)
			s.seed(t, databaseOf(dsn), append([]string{peopleFixture, peopleRows,
				"CREATE FUNCTION grow() RETURNS INT DETERMINISTIC MODIFIES SQL DATA BEGIN INSERT INTO people VALUES (99, 'mallory'); RETURN 1; END",
				"CREATE PROCEDURE grow_by_call() INSERT INTO people VALUES (98, 'mallory')",
			}, typesTable...)...)
			a, err := mysql.Open(dsn)
			if err != nil {
				t.Fatal(err)
			}
			writableDSN := s.newDatabase(t)
			s.seed(t, databaseOf(writableDSN), peopleFixture, peopleRows)
			w, err := mysql.Open(writableDSN, mysql.WithWrite())
			if err != nil {
				a.Close()
				t.Fatal(err)
			}
			return adaptertest.Target{
				Kind:      mysql.Kind,
				Adapter:   a,
				Table:     "people",
				SlowQuery: "SELECT SLEEP(60)",
				HugeQuery: hugeQuery,
				WriteSQL:  "INSERT INTO people VALUES (4, 'dave')",
				Escapes: []string{
					"SET SESSION TRANSACTION READ WRITE", afterEscape,
					"SET @@session.transaction_read_only = 0", afterEscape,
					"SET TRANSACTION READ WRITE", afterEscape,
					"START TRANSACTION READ WRITE", afterEscape,
					"SET autocommit = 1", afterEscape,
					"COMMIT", afterEscape,
					"COMMIT; INSERT INTO people VALUES (99, 'mallory')",
					"SELECT grow()",
					"CALL grow_by_call()",
					"UPDATE people SET name = 'x'",
					"DELETE FROM people",
					"TRUNCATE TABLE people",
					"CREATE TABLE extra (a INT)",
					"ALTER TABLE people ADD COLUMN extra INT",
					"DROP TABLE people",
				},
				Secret:     secret(t, s),
				TypesQuery: "SELECT * FROM all_types",
				Writable: &adaptertest.Writable{
					Adapter:          w,
					Table:            "people",
					NoCountSQL:       "CREATE TABLE other (i INT)",
					Location:         writableDSN,
					OpensTransaction: opensTransaction,
					ConnectionsInUse: func() int { return mysql.ConnectionsInUse(w) },
				},
			}
		})
	})
}

func count(t *testing.T, a *mysql.Adapter, table string) string {
	t.Helper()
	rows, err := a.Query(t.Context(), "SELECT count(*) FROM "+table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("no row from count: %v", rows.Err())
	}
	if b, ok := rows.Values()[0].([]byte); ok {
		return string(b)
	}
	return fmt.Sprint(rows.Values()[0])
}

func TestWriteEnabledAllowsWrites(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		dsn := s.newDatabase(t)
		s.seed(t, databaseOf(dsn), peopleFixture, peopleRows)

		a, err := mysql.Open(dsn, mysql.WithWrite())
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()

		rows, err := a.Query(t.Context(), "INSERT INTO people VALUES (4, 'dave')")
		if err != nil {
			t.Fatalf("write failed on a write-enabled Source: %v", err)
		}
		rows.Close()
		if got := count(t, a, "people"); got != "4" {
			t.Errorf("count after write = %s, want 4", got)
		}
	})
}

func TestCancelKillsTheQueryOnTheServer(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		dsn := s.newDatabase(t)
		a, err := mysql.Open(dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()

		ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
		defer cancel()
		if rows, err := a.Query(ctx, "SELECT SLEEP(120) AS lode_marker"); err == nil {
			for rows.Next() {
			}
			rows.Close()
		}

		admin := s.admin(t, "")
		deadline := time.Now().Add(10 * time.Second)
		for {
			var n int
			err := admin.QueryRow(`SELECT count(*) FROM information_schema.processlist WHERE info LIKE 'SELECT SLEEP(120)%'`).Scan(&n)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("the cancelled query is still running on the server")
			}
			time.Sleep(200 * time.Millisecond)
		}
	})
}

func TestSchemaWithDefaultDatabaseListsBareNamesAndViews(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		dsn := s.newDatabase(t)
		db := databaseOf(dsn)
		s.seed(t, db, peopleFixture, peopleRows,
			"CREATE TABLE `order items` (id INT, `Mixed Case` VARCHAR(10))",
			"CREATE VIEW names AS SELECT name FROM people",
		)
		a, err := mysql.Open(dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()

		tables, err := a.Schema(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tb := range tables {
			names = append(names, tb.Name)
		}
		want := []string{"names", "order items", "people"}
		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Fatalf("tables = %q, want %q (only the current database, views included)", names, want)
		}
		for _, tb := range tables {
			rows, err := a.Query(t.Context(), "SELECT * FROM "+tb.Ref)
			if err != nil {
				t.Fatalf("SELECT * FROM %s: %v", tb.Ref, err)
			}
			rows.Close()

			for _, c := range tb.Columns {
				if tb.Name == "order items" && c.Name == "Mixed Case" && c.Ref != "`Mixed Case`" {
					t.Errorf("column Ref = %s, want `Mixed Case`", c.Ref)
				}
				rows, err := a.Query(t.Context(), "SELECT "+c.Ref+" FROM "+tb.Ref)
				if err != nil {
					t.Fatalf("SELECT %s FROM %s: %v", c.Ref, tb.Ref, err)
				}
				rows.Close()
			}
		}
	})
}

func TestSchemaWithoutDefaultDatabaseQualifiesNames(t *testing.T) {
	eachServer(t, func(t *testing.T, s server) {
		dsn := s.newDatabase(t)
		db := databaseOf(dsn)
		s.seed(t, db, peopleFixture, peopleRows)

		a, err := mysql.Open(s.dsnFor(t, ""))
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()

		tables, err := a.Schema(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, tb := range tables {
			if tb.Name == db+".people" {
				found = true
				if want := "`" + db + "`.`people`"; tb.Ref != want {
					t.Errorf("Ref = %s, want %s", tb.Ref, want)
				}
			}
			for _, system := range []string{"mysql.", "information_schema.", "performance_schema.", "sys."} {
				if strings.HasPrefix(tb.Name, system) {
					t.Errorf("system table %q should be hidden", tb.Name)
				}
			}
		}
		if !found {
			t.Errorf("%s.people not listed", db)
		}
	})
}
