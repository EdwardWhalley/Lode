package file_test

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/adapter/file"
)

const twoRows = "id,name\n1,x\n2,y\n"

// folder makes a folder holding the opened file and another, and returns it and the file's
// path.
func folder(t *testing.T) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "a.csv")
	if err := os.WriteFile(path, []byte(twoRows), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.csv"), []byte("o\n7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

// disk is every file and folder under dir, with the contents of the files.
func disk(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() {
			out[rel] = "(a folder)"
			return nil
		}
		b, err := os.ReadFile(path)
		out[rel] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func literal(path string) string { return `'` + strings.ReplaceAll(path, `'`, `''`) + `'` }

// run sends one query through the Adapter and reads it out, as the UI does.
func run(t *testing.T, a *file.Adapter, query string) error {
	t.Helper()
	rows, err := a.Query(t.Context(), query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

// escapes are ways of changing the disk that must be refused; %[1]s to %[4]s are the opened
// file, another file, a new file and a new folder.
var escapes = []struct{ name, query string }{
	{"COPY onto the opened file", `COPY (SELECT 99 AS x) TO %[1]s`},
	{"COPY onto the opened file with no temporary file", `COPY (SELECT 99 AS x) TO %[1]s (USE_TMP_FILE false)`},
	{"COPY onto another file", `COPY (SELECT 99 AS x) TO %[2]s`},
	{"COPY onto another file with no temporary file", `COPY (SELECT 99 AS x) TO %[2]s (USE_TMP_FILE false)`},
	{"COPY to a new path", `COPY (SELECT 99 AS x) TO %[3]s`},
	{"copy in lower case", `copy (select 99 as x) to %[1]s (use_tmp_file false)`},
	{"COPY after a line comment", "-- a note\nCOPY (SELECT 99 AS x) TO %[1]s (USE_TMP_FILE false)"},
	{"COPY after a block comment", `/* a note */ COPY (SELECT 99 AS x) TO %[1]s (USE_TMP_FILE false)`},
	{"COPY first of several Statements", `COPY (SELECT 99 AS x) TO %[1]s (USE_TMP_FILE false); SELECT 1`},
	{"COPY last of several Statements", `SELECT 1; COPY (SELECT 99 AS x) TO %[1]s (USE_TMP_FILE false)`},
	{"COPY in the middle of several Statements", `SELECT 1; COPY (SELECT 99 AS x) TO %[1]s (USE_TMP_FILE false); SELECT 2`},
	{"COPY after a CREATE TABLE it needs", `CREATE TABLE z (i INT); COPY z TO %[1]s (USE_TMP_FILE false)`},
	{"COPY of a table made in the same query", `CREATE TABLE y AS SELECT 99 AS x; COPY y TO %[3]s`},
	{"EXPORT DATABASE", `EXPORT DATABASE %[4]s`},
	{"ATTACH of a new path", `ATTACH %[3]s AS n`},
	{"ATTACH of the opened file", `ATTACH %[1]s AS n`},

	{"EXPLAIN ANALYZE of a COPY", `EXPLAIN ANALYZE COPY (SELECT 99 AS x) TO %[1]s (USE_TMP_FILE false)`},
	{"EXPLAIN with ANALYZE in brackets", `EXPLAIN (ANALYZE, FORMAT json) COPY (SELECT 99 AS x) TO %[1]s (USE_TMP_FILE false)`},
	{"PREPARE of a COPY", `PREPARE p AS COPY (SELECT 99 AS x) TO %[1]s (USE_TMP_FILE false)`},

	{"COPY after a nested comment holding a line comment", "/* /* */ -- */ COPY (SELECT 99 AS x) TO %[1]s (USE_TMP_FILE false);\n"},

	{"COPY after a nested comment holding a quote", `/* /* */ ' */ SELECT 1; COPY (SELECT 99 AS x) TO %[1]s (USE_TMP_FILE false); SELECT 1 --'`},
	{"COPY after an E string", `SELECT E'\''; COPY (SELECT 99 AS x) TO %[1]s (USE_TMP_FILE false); SELECT 1 --'`},
}

func TestEveryWayOfWritingToTheDiskIsRefusedAndChangesNothing(t *testing.T) {
	for _, c := range escapes {
		t.Run(c.name, func(t *testing.T) {
			dir, path := folder(t)
			query := fmt.Sprintf(c.query, literal(path), literal(filepath.Join(dir, "other.csv")),
				literal(filepath.Join(dir, "new.csv")), literal(filepath.Join(dir, "newdir")))
			before := disk(t, dir)
			a, err := file.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			err = run(t, a, query)
			a.Close()
			after := disk(t, dir)
			if !reflect.DeepEqual(before, after) {
				t.Errorf("the disk changed:\n\t%s\n\tbefore %q\n\tafter  %q", query, before, after)
			}
			if err == nil {
				t.Errorf("not refused: %s", query)
			}
		})
	}
}

func TestACopyAfterANestedCommentRefusesTheWholeQuery(t *testing.T) {
	dir, path := folder(t)
	before := disk(t, dir)
	a, err := file.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	query := "SELECT 1 AS one; /* /* */ -- */ COPY (SELECT 99 AS x) TO " + literal(path) + " (USE_TMP_FILE false);\n"
	if err := run(t, a, query); err == nil || !strings.Contains(err.Error(), "COPY is refused") {
		t.Errorf("got %v, want the COPY refused", err)
	}
	if after := disk(t, dir); !reflect.DeepEqual(before, after) {
		t.Errorf("the disk changed: before %q, after %q", before, after)
	}
}

func TestNothingIsWrittenUnderTheHomeFolder(t *testing.T) {
	if query := os.Getenv("LODE_TEST_HOME_QUERY"); query != "" {
		a, err := file.Open(os.Getenv("LODE_TEST_HOME_FILE"))
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		if err := run(t, a, query); err == nil {
			t.Errorf("not refused: %s", query)
		}
		return
	}
	for _, query := range []string{
		`CREATE PERSISTENT SECRET probe (TYPE http, BEARER_TOKEN 'x')`,
		`CREATE PERSISTENT SECRET "../../escaped" (TYPE http, BEARER_TOKEN 'x')`,
		`CREATE OR REPLACE PERSISTENT SECRET probe (TYPE http, BEARER_TOKEN 'x')`,
		`CREATE SECRET s3 (TYPE s3, KEY_ID 'x', SECRET 'y')`,
		`SET s3_region = 'x'`,
		`SELECT * FROM sqlite_scan('x.db', 'y')`,
		`SELECT * FROM read_xlsx('x.xlsx')`,
		`INSTALL httpfs`,
		`FORCE INSTALL httpfs`,
		`LOAD httpfs`,
	} {
		t.Run(query, func(t *testing.T) {
			home := t.TempDir()
			dir, path := folder(t)
			before := disk(t, dir)
			child := exec.Command(os.Args[0], "-test.run=^TestNothingIsWrittenUnderTheHomeFolder$")
			child.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home,
				"LODE_TEST_HOME_QUERY="+query, "LODE_TEST_HOME_FILE="+path)
			if out, err := child.CombinedOutput(); err != nil {
				t.Errorf("%v:\n%s", err, out)
			}
			if written := disk(t, home); len(written) != 0 {
				names := make([]string, 0, len(written))
				for name := range written {
					names = append(names, name)
				}
				t.Errorf("written under the home folder: %q", names)
			}
			if after := disk(t, dir); !reflect.DeepEqual(before, after) {
				t.Errorf("the disk changed: before %q, after %q", before, after)
			}
		})
	}
}

func TestTheStatementsOfTheBugReportNoLongerChangeTheFile(t *testing.T) {
	for _, query := range []string{
		`COPY (SELECT 99 AS x) TO 'a.csv'`,
		`COPY (SELECT 99 AS x) TO 'a.csv' (USE_TMP_FILE false)`,
		`CREATE TABLE z (i INT); COPY z TO 'a.csv' (USE_TMP_FILE false)`,
	} {
		dir, _ := folder(t)
		t.Chdir(dir)
		before := disk(t, dir)
		a, err := file.Open("a.csv")
		if err != nil {
			t.Fatal(err)
		}
		err = run(t, a, query)
		a.Close()
		if after := disk(t, dir); !reflect.DeepEqual(before, after) {
			t.Errorf("the disk changed:\n\t%s\n\tbefore %q\n\tafter  %q", query, before, after)
		}
		if err == nil {
			t.Errorf("not refused: %s", query)
		}
	}
}

func TestARefusalSaysWhyAndWhatToDoInstead(t *testing.T) {
	dir, path := folder(t)
	t.Chdir(dir)
	a, err := file.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, query := range []string{
		`COPY (SELECT 99 AS x) TO 'a.csv'`,
		`COPY (SELECT 99 AS x) TO ` + literal(path),
		`EXPORT DATABASE 'somewhere'`,
		`SELECT * FROM 'other.csv'`,
	} {
		err := run(t, a, query)
		if err == nil {
			t.Fatalf("not refused: %s", query)
		}
		if !strings.Contains(err.Error(), "read-only") || !strings.Contains(err.Error(), "ctrl+s") {
			t.Errorf("%s says %q, want it to say file Sources are read-only and that ctrl+s exports", query, err)
		}
	}
}

func TestARefusalNamesTheStatement(t *testing.T) {
	a := openLimited(t, 0)
	for query, name := range map[string]string{
		`EXPLAIN SELECT 1`:       "EXPLAIN",
		`PREPARE p AS SELECT 1`:  "PREPARE",
		`ATTACH ':memory:' AS m`: "ATTACH",
		`DETACH memory`:          "DETACH",
	} {
		err := run(t, a, query)
		if err == nil || !strings.Contains(err.Error(), name+" is refused") {
			t.Errorf("%s says %v, want it to say %s is refused", query, err, name)
		}
	}
}

func TestAKindOfStatementNotOnTheListIsRefused(t *testing.T) {
	a := openLimited(t, 0)
	if err := run(t, a, `CREATE TABLE t (i INT)`); err != nil {
		t.Fatal(err)
	}
	err := run(t, a, `MERGE INTO t USING (SELECT 1 AS i) s ON t.i = s.i WHEN MATCHED THEN DELETE`)
	if err == nil || !strings.Contains(err.Error(), "this statement is refused") {
		t.Errorf("MERGE says %v, want it to say this statement is refused", err)
	}
}

func TestAPivotRunsWhenItListsItsValues(t *testing.T) {
	a := openLimited(t, 0)
	err := run(t, a, `PIVOT people ON name USING count(*)`)
	if err == nil || !strings.Contains(err.Error(), "several statements") || strings.Contains(err.Error(), "PrepareContext") {
		t.Errorf("a PIVOT with no values says %v, want it to say why in our own words", err)
	}
	if n, _ := rowCount(t, a, `PIVOT people ON name IN ('alice', 'bob') USING count(*)`); n == 0 {
		t.Error("a PIVOT listing its values returned nothing")
	}
}

func TestAFileSourceReachesNoOtherFile(t *testing.T) {
	dir, path := folder(t)
	other := filepath.Join(dir, "other.csv")
	a, err := file.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, query := range []string{
		`SELECT * FROM ` + literal(other),
		`SELECT * FROM read_csv(` + literal(other) + `)`,
		`SELECT * FROM read_text(` + literal(other) + `)`,
		`SELECT * FROM glob(` + literal(filepath.Join(dir, "*")) + `)`,
		`SELECT * FROM glob('*')`,
		`INSTALL httpfs`,
		`LOAD httpfs`,
	} {
		if err := run(t, a, query); err == nil {
			t.Errorf("not refused: %s", query)
		}
	}
}

// limitSwitches are statements that try to undo the lock on a Source in dir, other being
// another file there.
func limitSwitches(dir, other string) []string {
	other = literal(other)
	return []string{
		`SET enable_external_access = true`,
		`SET lock_configuration = false`,
		`SET allowed_paths = [` + other + `]`,
		`SET allowed_directories = [` + literal(dir) + `]`,
		`RESET enable_external_access`,
		`RESET lock_configuration`,
		`RESET allowed_paths`,
		`RESET GLOBAL lock_configuration`,
		`SET log_query_path = ` + other,
		`PRAGMA profiling_output = ` + other,
	}
}

func TestAQueryCannotSwitchTheLimitsOffToReachAnotherFile(t *testing.T) {
	dir, path := folder(t)
	other := filepath.Join(dir, "other.csv")
	for _, query := range limitSwitches(dir, other) {
		a, err := file.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := run(t, a, query); err == nil {
			t.Errorf("not refused: %s", query)
		}
		if err := run(t, a, `SELECT * FROM `+literal(other)); err == nil {
			t.Errorf("after %s another file can be read", query)
		}
		a.Close()
	}
}

func TestAScriptWhoseLaterStatementNeedsAnEarlierOneIsRefused(t *testing.T) {
	a := openLimited(t, 0)
	if err := run(t, a, `CREATE TABLE t AS SELECT 1 AS i; SELECT * FROM t`); err == nil {
		t.Error("expected the script to be refused")
	}
	if err := run(t, a, `SELECT * FROM t`); err == nil {
		t.Error("nothing of a refused query should have run, but its table exists")
	}
	if err := run(t, a, `CREATE TABLE t AS SELECT 1 AS i`); err != nil {
		t.Fatal(err)
	}
	if n, _ := rowCount(t, a, `SELECT * FROM t`); n != 1 {
		t.Errorf("got %d rows from the table, want 1", n)
	}
}

func TestSeveralStatementsThatStandAloneStillRunAndShowTheLast(t *testing.T) {
	n, first := rowCount(t, openLimited(t, 0), "SELECT 1 AS a; -- a note\n SELECT 2 AS a, 3 AS b;")
	if n != 1 || len(first) != 2 || first[0] != int64(2) {
		t.Errorf("got %d rows starting %v, want the last Statement's one row (2, 3)", n, first)
	}
}

func TestInMemoryTablesWorkAndAreLostOnClose(t *testing.T) {
	dir, path := folder(t)
	before := disk(t, dir)
	a, err := file.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`CREATE TABLE t (i INT)`, `INSERT INTO t VALUES (1), (2), (3)`, `UPDATE t SET i = 9`} {
		if err := run(t, a, query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	if n, _ := rowCount(t, a, `SELECT * FROM t`); n != 3 {
		t.Errorf("got %d rows from the table, want 3", n)
	}
	a.Close()
	if after := disk(t, dir); !reflect.DeepEqual(before, after) {
		t.Errorf("the disk changed: before %q, after %q", before, after)
	}

	b, err := file.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := run(t, b, `SELECT * FROM t`); err == nil {
		t.Error("the table outlived the Source")
	}
}

func TestTheOpenedFileStillReadsHoweverItWasNamed(t *testing.T) {
	check := func(t *testing.T, path string) {
		t.Helper()
		a, err := file.Open(path, file.WithRowLimit(1))
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		tables, err := a.Schema(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(tables) != 1 || len(tables[0].Columns) != 2 {
			t.Errorf("schema = %+v, want one table of two columns", tables)
		}
		if n, _ := rowCount(t, a, "SELECT * FROM "+tables[0].Ref); n != 1 {
			t.Errorf("got %d rows through the view, want the row cap's 1 of its 2", n)
		}
		if n, _ := rowCount(t, a, "SELECT count(*) FROM "+tables[0].Ref); n != 1 {
			t.Errorf("got %d rows from count(*), want 1", n)
		}
	}
	write := func(t *testing.T, path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(twoRows), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("an absolute path", func(t *testing.T) {
		_, path := folder(t)
		check(t, path)
	})
	t.Run("a relative path", func(t *testing.T) {
		dir, _ := folder(t)
		t.Chdir(dir)
		check(t, "a.csv")
	})
	t.Run("a relative path into a folder", func(t *testing.T) {
		dir, _ := folder(t)
		t.Chdir(filepath.Dir(dir))
		check(t, filepath.Join(filepath.Base(dir), "a.csv"))
	})
	t.Run("a path holding a space", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "my data.csv")
		write(t, path)
		check(t, path)
	})
	t.Run("a path holding a quote", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "it's.csv")
		write(t, path)
		check(t, path)
	})
	t.Run("a folder holding a space and a quote", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "bob's files")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "a.csv")
		write(t, path)
		check(t, path)
	})
	t.Run("a path starting ~/", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		write(t, filepath.Join(home, "a.csv"))
		check(t, "~/a.csv")
	})
	t.Run("forward slashes", func(t *testing.T) {
		_, path := folder(t)
		check(t, filepath.ToSlash(path))
	})
}

func TestAPatternIsNotAFile(t *testing.T) {
	dir, _ := folder(t)
	if a, err := file.Open(filepath.Join(dir, "*.csv")); err == nil {
		a.Close()
		t.Error("expected an error opening a pattern")
	}
}
