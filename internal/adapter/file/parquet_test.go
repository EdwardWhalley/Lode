package file_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/adapter/file"
)

// makeParquet writes a real Parquet file of ten rows through the driver, with a column of
// each awkward type.
func makeParquet(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`COPY (
		SELECT i AS id, 'name' || i AS name,
			[i, NULL, i + 1] AS list,
			{'x': i, 'y': 'two'} AS person,
			12.50::DECIMAL(10, 2) AS price,
			TIMESTAMP '2026-11-01 18:30:05' AS seen,
			NULL::INTEGER AS nothing
		FROM range(1, 11) t(i)
	) TO ` + literal(path) + ` (FORMAT parquet)`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAParquetFileOpensAsATableNamedAfterIt(t *testing.T) {
	for _, name := range []string{"data.parquet", "data.PARQUET", "data.Parquet"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			makeParquet(t, path)
			a, err := file.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if a.Table() != "data" {
				t.Errorf("table = %q, want data", a.Table())
			}
			if n, _ := rowCount(t, a, "SELECT * FROM data"); n != 10 {
				t.Errorf("got %d rows, want 10", n)
			}
		})
	}
}

func TestAParquetFilesColumnsAndTypesAreInTheSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.parquet")
	makeParquet(t, path)
	a, err := file.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	tables, err := a.Schema(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0].Name != "data" {
		t.Fatalf("tables = %+v", tables)
	}
	got := map[string]string{}
	var names []string
	for _, c := range tables[0].Columns {
		got[c.Name] = c.Type
		names = append(names, c.Name)
	}
	if want := []string{"id", "name", "list", "person", "price", "seen", "nothing"}; !reflect.DeepEqual(names, want) {
		t.Errorf("columns = %v, want %v", names, want)
	}
	for name, want := range map[string]string{
		"id": "BIGINT", "name": "VARCHAR", "price": "DECIMAL(10,2)", "seen": "TIMESTAMP", "nothing": "INTEGER",
	} {
		if got[name] != want {
			t.Errorf("column %s has type %q, want %q", name, got[name], want)
		}
	}
	if !strings.Contains(got["list"], "[]") || !strings.Contains(got["person"], "STRUCT") {
		t.Errorf("nested types = %q and %q", got["list"], got["person"])
	}
}

func TestAQueryOverAParquetFileIsBoundedByTheRowCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.parquet")
	makeParquet(t, path)
	a, err := file.Open(path, file.WithRowLimit(3))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if n, _ := rowCount(t, a, "SELECT * FROM data"); n != 3 {
		t.Errorf("got %d rows of 10, want the row limit's 3", n)
	}
}

func TestAFileThatIsNotReallyParquetSaysWhichFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.parquet")
	if err := os.WriteFile(path, []byte("id,name\n1,x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := file.Open(path)
	if err == nil {
		a.Close()
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "fake.parquet") {
		t.Errorf("error %q does not name the file", err)
	}
}

func TestACopyOntoAParquetFileIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.parquet")
	makeParquet(t, path)
	before := disk(t, dir)
	a, err := file.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, query := range []string{
		`COPY (SELECT 99 AS x) TO ` + literal(path) + ` (FORMAT parquet)`,
		`COPY (SELECT 99 AS x) TO ` + literal(path) + ` (FORMAT parquet, USE_TMP_FILE false)`,
		`COPY (SELECT 99 AS x) TO ` + literal(path) + ` (USE_TMP_FILE false)`,
	} {
		err := run(t, a, query)
		if err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Errorf("%s says %v, want it refused as read-only", query, err)
		}
	}
	if after := disk(t, dir); !reflect.DeepEqual(before, after) {
		t.Error("the Parquet file changed")
	}
}

func TestAFolderNamedLikeAPartitionChangesNothing(t *testing.T) {
	for name, write := range map[string]func(t *testing.T, path string){
		"part.parquet": func(t *testing.T, path string) {
			t.Helper()
			db, err := sql.Open("duckdb", "")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(`COPY (SELECT 1999 AS year, 'alice' AS name) TO ` + literal(path) + ` (FORMAT parquet)`); err != nil {
				t.Fatal(err)
			}
		},
		"part.csv": func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte("year,name\n1999,alice\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"part.json": func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte(`[{"year":1999,"name":"alice"}]`), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "year=2024", "name=bob", "month=3")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, name)
			write(t, path)
			a, err := file.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			rows, err := a.Query(t.Context(), "SELECT * FROM part")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if cols := rows.Columns(); len(cols) != 2 {
				t.Errorf("columns = %+v, want only the file's own", cols)
			}
			if !rows.Next() {
				t.Fatalf("no row: %v", rows.Err())
			}
			values := rows.Values()
			if got := fmt.Sprint(values[0], " ", values[1]); got != "1999 alice" {
				t.Errorf("read %q, want the file's own values, 1999 alice", got)
			}
		})
	}
}

func TestAParquetPatternIsNotAFile(t *testing.T) {
	dir := t.TempDir()
	makeParquet(t, filepath.Join(dir, "one.parquet"))
	makeParquet(t, filepath.Join(dir, "two.parquet"))
	if a, err := file.Open(filepath.Join(dir, "*.parquet")); err == nil {
		a.Close()
		t.Error("expected an error opening a pattern")
	}
}

func TestTheUnsupportedFileTypeMessageListsParquet(t *testing.T) {
	_, err := file.Open("data.xlsx")
	if err == nil || !strings.Contains(err.Error(), "parquet") {
		t.Errorf("got %v, want it to list parquet", err)
	}
}
