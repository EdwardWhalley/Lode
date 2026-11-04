package sqlite_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/EdwardWhalley/lode/internal/adapter/adaptertest"
	"github.com/EdwardWhalley/lode/internal/adapter/sqlite"
)

// makeDB creates a database file with the contract fixture, a second table and a view.
func makeDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		"CREATE TABLE people (id INTEGER PRIMARY KEY, name TEXT)",
		"INSERT INTO people VALUES (1, 'alice'), (2, 'bob'), (3, 'carol')",
		"CREATE TABLE animals (kind TEXT)",
		"CREATE VIEW names AS SELECT name FROM people",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// seed runs statements against the database file at path.
func seed(t *testing.T, path string, statements ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
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

func TestContract(t *testing.T) {
	adaptertest.Run(t, func(t *testing.T) adaptertest.Target {
		path := makeDB(t)
		seed(t, path,
			`CREATE TABLE all_types (i INTEGER, r REAL, t TEXT, b BLOB, n NUMERIC, d DATE, dt DATETIME, ts TIMESTAMP, bo BOOLEAN, v VARCHAR(5), x)`,
			`INSERT INTO all_types VALUES (1, 1.5, 'x', x'ff00', 1.5, '2026-01-01', '2026-01-01 01:02:03', '2026-01-01 01:02:03', 1, 'v', 'any'),
				(NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`)
		a, err := sqlite.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		writablePath := makeDB(t)
		w, err := sqlite.Open(writablePath, sqlite.WithWrite())
		if err != nil {
			a.Close()
			t.Fatal(err)
		}
		itself := "'" + filepath.ToSlash(path) + "'"
		return adaptertest.Target{
			Kind:      sqlite.Kind,
			Adapter:   a,
			Table:     "people",
			SlowQuery: "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT count(*) FROM c",
			HugeQuery: "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT x FROM c",
			WriteSQL:  "INSERT INTO people VALUES (4, 'dave')",
			Escapes: []string{
				"PRAGMA query_only = 0; ATTACH " + itself + " AS again; INSERT INTO again.people VALUES (99, 'mallory')",
				"PRAGMA query_only = 0; ATTACH 'file:" + filepath.ToSlash(path) + "?mode=rw' AS again; INSERT INTO again.people VALUES (99, 'mallory')",
				"PRAGMA query_only = 0",
				"PRAGMA query_only = OFF; INSERT INTO people VALUES (99, 'mallory')",
				"PRAGMA writable_schema = ON",
				"PRAGMA query_only = 0; CREATE TABLE extra (i INTEGER)",
				"ALTER TABLE people ADD COLUMN extra INTEGER",
				"DELETE FROM people",
				"BEGIN; INSERT INTO people VALUES (99, 'mallory'); COMMIT",
				"BEGIN IMMEDIATE",
			},
			Secret:     adaptertest.Secret{None: "a SQLite Source is a file, and has no password"},
			TypesQuery: "SELECT * FROM all_types",
			Writable: &adaptertest.Writable{
				Adapter:    w,
				Table:      "people",
				NoCountSQL: "CREATE TABLE no_count (i INTEGER)",
				Location:   writablePath,
				OpensTransaction: []string{
					"BEGIN",
					"begin;",
					"-- a note\nBEGIN",
					"BEGIN IMMEDIATE",
					"BEGIN; UPDATE people SET name = 'lost'",
					"BEGIN; UPDATE no_such_table SET a = 1; COMMIT",
					"SAVEPOINT s",
					"SAVEPOINT s; UPDATE people SET name = 'undone'",
				},
				ConnectionsInUse: func() int { return sqlite.ConnectionsInUse(w) },
			},
		}
	})
}

func openWritable(t *testing.T) *sqlite.Adapter {
	t.Helper()
	a, err := sqlite.Open(makeDB(t), sqlite.WithWrite())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSchemaListsTablesAndViewsSorted(t *testing.T) {
	a, err := sqlite.Open(makeDB(t))
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
	want := []string{"animals", "names", "people"}
	if len(names) != len(want) {
		t.Fatalf("tables = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("tables = %v, want %v", names, want)
		}
	}
}

func TestColumnRefsQuoteAwkwardNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "awkward.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE "order items" ("order" INTEGER, "First Name" TEXT, "say ""hi""" TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	a, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	tables, err := a.Schema(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || len(tables[0].Columns) != 3 {
		t.Fatalf("schema = %+v", tables)
	}
	want := []string{`"order"`, `"First Name"`, `"say ""hi"""`}
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

func TestOpenRejectsMissingAndNonDatabaseFiles(t *testing.T) {
	if _, err := sqlite.Open(filepath.Join(t.TempDir(), "missing.db")); err == nil {
		t.Error("expected an error for a missing file")
	}
	bad := filepath.Join(t.TempDir(), "bad.db")
	if err := writeFile(bad, "this is not a database, just some text padding it out"); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlite.Open(bad); err == nil {
		t.Error("expected an error for a file that is not a database")
	}
}

func TestAReadOnlySourceWritesNoOtherFile(t *testing.T) {
	path := makeDB(t)
	a, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	dir := t.TempDir()
	for name, query := range map[string]string{
		"attached.db": "PRAGMA query_only = 0; ATTACH %s AS o; CREATE TABLE o.t (i INTEGER)",
		"copy.db":     "VACUUM INTO %s",
	} {
		other := filepath.Join(dir, name)
		_, err := a.Query(t.Context(), fmt.Sprintf(query, "'"+filepath.ToSlash(other)+"'"))
		if err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Errorf("%s: got %v, want it refused, saying why", query, err)
		}
		if fileExists(other) {
			t.Errorf("%s made %s", query, name)
		}
	}
}

func TestOpenDoesNotCreateMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	sqlite.Open(path)
	if fileExists(path) {
		t.Error("opening a Source must never create the file")
	}
}
