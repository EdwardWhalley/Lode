package file_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/EdwardWhalley/lode/internal/adapter/adaptertest"
	"github.com/EdwardWhalley/lode/internal/adapter/file"
)

func TestContract(t *testing.T) {
	adaptertest.Run(t, func(t *testing.T) adaptertest.Target {
		people, err := os.ReadFile(filepath.Join("testdata", "people.csv"))
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		path := filepath.Join(dir, "people.csv")
		if err := os.WriteFile(path, people, 0o600); err != nil {
			t.Fatal(err)
		}
		a, err := file.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		w, err := file.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := w.Query(t.Context(), "CREATE TABLE writable AS SELECT * FROM people")
		if err != nil {
			t.Fatal(err)
		}
		rows.Close()
		copyOnto := "COPY (SELECT 99 AS x) TO " + literal(path) + " (USE_TMP_FILE false)"
		return adaptertest.Target{
			Kind:     file.Kind,
			Adapter:  a,
			Table:    a.Table(),
			WriteSQL: copyOnto,
			Escapes: append(limitSwitches(dir, filepath.Join(dir, "other.csv")),
				"SET lock_configuration = false; "+copyOnto,
				"BEGIN TRANSACTION",
				"SELECT 1; "+copyOnto),
			Secret: adaptertest.Secret{None: "a file Source is a file, and has no password"},
			TypesQuery: `SELECT 1::TINYINT, 1::SMALLINT, 1::INTEGER, 1::BIGINT, 1::HUGEINT, 1::UTINYINT, 1::USMALLINT,
				1::UINTEGER, 1::UBIGINT, 1.5::FLOAT, 1.5::DOUBLE, 1.5::DECIMAL(5,2), 1.5::DECIMAL(30,2), 'x'::VARCHAR,
				'x'::BLOB, true, DATE '2026-01-01', TIME '01:02:03', TIMETZ '01:02:03+01', TIMESTAMP '2026-01-01 01:02:03',
				TIMESTAMPTZ '2026-01-01 01:02:03+00', TIMESTAMP_NS '2026-01-01 01:02:03', INTERVAL '1 day',
				'6ba7b810-9dad-11d1-80b4-00c04fd430c8'::UUID, [1::TINYINT, 2], {'a': 1::SMALLINT, 'b': 1::UTINYINT},
				MAP {'k': 1::INTEGER}, 1::UNION(a INT, b VARCHAR), '{"a":1}'::JSON, [1,2]::INTEGER[2],
				'a'::ENUM('a','b'), NULL`,
			Writable: &adaptertest.Writable{
				Adapter:           w,
				Table:             "writable",
				NoCountSQL:        "CREATE TABLE no_count (i INT)",
				NoOpenTransaction: "a file Source has one connection, so the next run is on the one holding a transaction, and its tables are in memory and gone when it closes",
			},
			// bounded: an aggregate, so the result is one row; the contract test cancels it after 200ms.
			SlowQuery:   "SELECT count(*) FROM range(1000000000) a, range(1000000000) b",
			NoHugeQuery: "DuckDB holds a whole result before it returns a row, so a huge one is never tested (ADR 0001)",
		}
	})
}

func TestColumnRefsQuoteAwkwardNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "awkward.csv")
	if err := os.WriteFile(path, []byte("order,First Name\n1,alice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := file.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	tables, err := a.Schema(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`"order"`, `"First Name"`}
	for i, c := range tables[0].Columns {
		if c.Ref != want[i] {
			t.Errorf("column %q Ref = %s, want %s", c.Name, c.Ref, want[i])
		}
		rows, err := a.Query(t.Context(), "SELECT "+c.Ref+" FROM "+tables[0].Ref)
		if err != nil {
			t.Fatalf("SELECT %s FROM %s: %v", c.Ref, tables[0].Ref, err)
		}
		rows.Close()
	}
}

func TestOpenFormats(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"data.tsv":    "id\tname\n1\talice\n",
		"data.json":   `[{"id":1,"name":"alice"}]`,
		"data.ndjson": "{\"id\":1,\"name\":\"alice\"}\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			a, err := file.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			rows, err := a.Query(t.Context(), "SELECT name FROM data")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if !rows.Next() || rows.Values()[0] != "alice" {
				t.Fatalf("expected a row with name alice, err=%v", rows.Err())
			}
		})
	}
}

func rowCount(t *testing.T, a *file.Adapter, query string) (n int, first []any) {
	t.Helper()
	rows, err := a.Query(t.Context(), query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	for rows.Next() {
		if n == 0 {
			first = append([]any(nil), rows.Values()...)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return n, first
}

func openLimited(t *testing.T, limit int) *file.Adapter {
	t.Helper()
	var opts []file.Option
	if limit > 0 {
		opts = append(opts, file.WithRowLimit(limit))
	}
	a, err := file.Open(filepath.Join("testdata", "people.csv"), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func TestWithoutARowLimitEverythingIsReturned(t *testing.T) {
	if n, _ := rowCount(t, openLimited(t, 0), "SELECT * FROM range(100000)"); n != 100000 {
		t.Errorf("got %d rows, want all 100000", n)
	}
}

func TestRowLimitBoundsQueries(t *testing.T) {
	a := openLimited(t, 11)
	for _, q := range []string{
		"SELECT * FROM range(1000000)",
		"select * from range(1000000);",
		"FROM range(1000000)",
		"WITH r AS (SELECT * FROM range(1000000)) SELECT * FROM r",
		"SELECT * FROM range(1000000) -- all of them",
		"SELECT * FROM range(1000000) /* ; */",
	} {
		if n, _ := rowCount(t, a, q); n != 11 {
			t.Errorf("%q returned %d rows, want 11", q, n)
		}
	}
}

func TestANestedCommentAfterAQueryStillRunsUnderARowCap(t *testing.T) {
	a := openLimited(t, 5)
	if n, first := rowCount(t, a, "SELECT 1 /* /* */ x -- */\n"); n != 1 || first[0] != int64(1) {
		t.Errorf("got %d rows starting %v, want the one row of SELECT 1", n, first)
	}
}

func TestRowLimitDoesNotAddRowsToSmallResults(t *testing.T) {
	a := openLimited(t, 1000)
	if n, _ := rowCount(t, a, "SELECT * FROM people"); n != 3 {
		t.Errorf("got %d rows from a 3-row file, want 3", n)
	}
}

func TestRowLimitKeepsTheUsersOrdering(t *testing.T) {
	a := openLimited(t, 3)
	n, first := rowCount(t, a, "SELECT i FROM range(1000000) t(i) ORDER BY i DESC")
	if n != 3 || len(first) != 1 || first[0] != int64(999999) {
		t.Errorf("got %d rows starting %v, want 3 rows starting 999999", n, first)
	}
}

func TestRowLimitLeavesOtherStatementsAlone(t *testing.T) {
	a := openLimited(t, 1)
	for _, q := range []string{"DESCRIBE people", "SHOW TABLES"} {
		if n, _ := rowCount(t, a, q); n < 1 {
			t.Errorf("%q returned nothing", q)
		}
	}
	if n, _ := rowCount(t, a, "DESCRIBE people"); n != 2 {
		t.Errorf("DESCRIBE people returned %d rows, want its 2 columns (it is not a query to bound)", n)
	}
}

func TestRowLimitDoesNotChangeTheSchema(t *testing.T) {
	tables, err := openLimited(t, 1).Schema(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || len(tables[0].Columns) != 2 {
		t.Errorf("schema = %+v, want the people table with 2 columns", tables)
	}
}

func TestOpenRejectsUnsupportedAndMissing(t *testing.T) {
	if _, err := file.Open("data.xlsx"); err == nil {
		t.Error("expected an error for an unsupported file type")
	}
	if _, err := file.Open(filepath.Join(t.TempDir(), "missing.csv")); err == nil {
		t.Error("expected an error for a missing file")
	}
}
