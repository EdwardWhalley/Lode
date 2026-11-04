package postgres_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/adapter/adaptertest"
	"github.com/EdwardWhalley/lode/internal/adapter/postgres"
)

const peopleFixture = "CREATE TABLE people (id integer PRIMARY KEY, name text)"
const peopleRows = "INSERT INTO people VALUES (1, 'alice'), (2, 'bob'), (3, 'carol')"

// typesQuery returns one column of each common PostgreSQL type, then NULL.
const typesQuery = `SELECT 1::smallint, 1::integer, 1::bigint, 1.5::numeric(10,2), 0.1::real, 0.1::double precision,
	'x'::text, 'x'::varchar(5), 'x'::char(3), true, '\xff00'::bytea, now()::timestamp, now()::timestamptz,
	current_date, '18:30:05'::time, '18:30:05+01'::timetz, '1 day'::interval, gen_random_uuid(),
	'{"a":1}'::json, '{"a":1}'::jsonb, ARRAY[1,2], ARRAY['a','b'], 'a'::name, 1::oid, '127.0.0.1'::inet,
	'(1,2)'::point, '[1,2)'::int4range, 1::money, B'101'::bit(3), '<a/>'::xml, 'a'::"char", '{1.5}'::real[],
	'abc'::tsvector, NULL`

// secret is a password and locations that hold it and fail to open, some of them on the
// test server.
func secret(t *testing.T) adaptertest.Secret {
	t.Helper()
	const password = "hunter2"
	u, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	wrong := *u
	wrong.User = url.UserPassword(u.User.Username(), password)
	inOptions := *u
	inOptions.User = url.User(u.User.Username())
	query := inOptions.Query()
	query.Set("password", password)
	inOptions.RawQuery = query.Encode()
	return adaptertest.Secret{Password: password, Locations: []string{
		wrong.String(),
		inOptions.String(),
		"postgres://reader:hunter2@127.0.0.1:1/app?sslmode=disable",
		"postgres://reader@127.0.0.1:1/app?password=hunter2&sslmode=disable",
		"postgres://reader:hunter2@host:notaport/app",
		"postgres://reader:hunter2@127.0.0.1:1/app?sslmode=bogus",
	}}
}

// opensTransaction are the statements that open a transaction on PostgreSQL.
var opensTransaction = []string{
	"BEGIN",
	"begin;",
	"-- a note\nBEGIN",
	"BEGIN ISOLATION LEVEL SERIALIZABLE",
	"START TRANSACTION",
	"/* /* a comment PostgreSQL nests */ */ BEGIN",
}

func TestContract(t *testing.T) {
	adaptertest.Run(t, func(t *testing.T) adaptertest.Target {
		dsn := newDatabase(t)
		seed(t, dsn, peopleFixture, peopleRows)
		a, err := postgres.Open(dsn)
		if err != nil {
			t.Fatal(err)
		}
		w, writableDSN := openWritableAt(t)
		return adaptertest.Target{
			Kind:    postgres.Kind,
			Adapter: a,
			Table:   "people",
			// bounded: an aggregate, so the result is one row; the contract test cancels it after 200ms.
			SlowQuery: "SELECT count(*) FROM generate_series(1, 100000000000) a, generate_series(1, 100000000000) b",
			// bounded: streamed one row at a time, and the test cancels it after reading three.
			HugeQuery: "SELECT generate_series(1, 100000000000)",
			WriteSQL:  "INSERT INTO people VALUES (4, 'dave')",
			Escapes: []string{
				"SET default_transaction_read_only = off",
				"SET SESSION CHARACTERISTICS AS TRANSACTION READ WRITE",
				"BEGIN READ WRITE",
				"SET TRANSACTION READ WRITE",
				"RESET default_transaction_read_only",
				"SELECT set_config('default_transaction_read_only', 'off', false)",
				"COMMIT",
				"COMMIT; INSERT INTO people VALUES (99, 'mallory')",
				"DO $$ BEGIN INSERT INTO people VALUES (99, 'mallory'); END $$",
				"SELECT lo_create(0)",
				"WITH gone AS (DELETE FROM people RETURNING *) SELECT * FROM gone",
				"CREATE TABLE extra (i integer)",
				"ALTER TABLE people ADD COLUMN extra integer",
			},
			Secret:     secret(t),
			TypesQuery: typesQuery,
			Writable: &adaptertest.Writable{
				Adapter:          w,
				Table:            "people",
				NoCountSQL:       "CREATE TABLE no_count (i integer)",
				Location:         writableDSN,
				OpensTransaction: opensTransaction,
				ConnectionsInUse: func() int { return postgres.ConnectionsInUse(w) },
			},
		}
	})
}

// openWritable is a writable Source on its own database, closed by the contract suite.
func openWritable(t *testing.T) *postgres.Adapter {
	t.Helper()
	a, _ := openWritableAt(t)
	return a
}

// openWritableAt is openWritable, and the DSN it opened.
func openWritableAt(t *testing.T) (*postgres.Adapter, string) {
	t.Helper()
	dsn := newDatabase(t)
	seed(t, dsn, peopleFixture, peopleRows)
	a, err := postgres.Open(dsn, postgres.WithWrite())
	if err != nil {
		t.Fatal(err)
	}
	return a, dsn
}

func TestWriteEnabledAllowsWrites(t *testing.T) {
	dsn := newDatabase(t)
	seed(t, dsn, peopleFixture, peopleRows)

	a, err := postgres.Open(dsn, postgres.WithWrite())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	rows, err := a.Query(t.Context(), "INSERT INTO people VALUES (4, 'dave')")
	if err != nil {
		t.Fatalf("write failed on a write-enabled Source: %v", err)
	}
	rows.Close()

	rows, err = a.Query(t.Context(), "SELECT count(*) FROM people")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() || rows.Values()[0] != int64(4) {
		t.Errorf("count after write = %v, want 4", rows.Values())
	}
}

func TestSchemaQualifiesOtherSchemasAndIncludesViews(t *testing.T) {
	dsn := newDatabase(t)
	seed(t, dsn,
		peopleFixture, peopleRows,
		"CREATE SCHEMA sales",
		`CREATE TABLE sales."Order Items" (id integer, "Mixed Case" text)`,
		"INSERT INTO sales.\"Order Items\" VALUES (1, 'x')",
		"CREATE VIEW names AS SELECT name FROM people",
		"CREATE MATERIALIZED VIEW name_count AS SELECT count(*) AS n FROM people",
	)
	a, err := postgres.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	tables, err := a.Schema(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]string{}
	var names []string
	for _, tb := range tables {
		byName[tb.Name] = tb.Ref
		names = append(names, tb.Name)
	}
	want := []string{"name_count", "names", "people", `sales.Order Items`}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("tables = %q, want %q (system schemas hidden, others qualified, views and materialized views included)", names, want)
	}
	if got, want := byName["sales.Order Items"], `"sales"."Order Items"`; got != want {
		t.Errorf("Ref = %s, want %s", got, want)
	}

	for _, ref := range byName {
		rows, err := a.Query(t.Context(), "SELECT * FROM "+ref)
		if err != nil {
			t.Fatalf("SELECT * FROM %s: %v", ref, err)
		}
		rows.Close()
	}

	for _, tb := range tables {
		if tb.Name == "sales.Order Items" && (len(tb.Columns) != 2 || tb.Columns[1].Name != "Mixed Case") {
			t.Errorf("columns = %+v", tb.Columns)
		}
		if tb.Name != "sales.Order Items" {
			continue
		}
		if got, want := tb.Columns[1].Ref, `"Mixed Case"`; got != want {
			t.Errorf("column Ref = %s, want %s", got, want)
		}
		for _, c := range tb.Columns {
			rows, err := a.Query(t.Context(), "SELECT "+c.Ref+" FROM "+tb.Ref)
			if err != nil {
				t.Fatalf("SELECT %s FROM %s: %v", c.Ref, tb.Ref, err)
			}
			rows.Close()
		}
	}
}

func TestOpenRejectsWrongCredentials(t *testing.T) {
	dsn := strings.Replace(newDatabase(t), "postgres:postgres@", "postgres:wrong@", 1)
	if !strings.Contains(dsn, ":wrong@") {
		adaptertest.NoServer(t, "LODE_TEST_POSTGRES_DSN", needsDefaultCredentials)
	}
	if _, err := postgres.Open(dsn); err == nil {
		t.Error("expected authentication to fail")
	}
}
