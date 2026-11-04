package source_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/EdwardWhalley/lode/internal/source"
)

func TestResolve(t *testing.T) {
	named := []source.Source{{Name: "people", Path: "/data/people.csv"}}

	if got := source.Resolve("people", named); got.Path != "/data/people.csv" {
		t.Errorf("a configured name should select its Source, got %+v", got)
	}
	if got := source.Resolve("./people", named); got.Name != "" || got.Path != "./people" {
		t.Errorf("./people should be an ad-hoc path, got %+v", got)
	}
	if got := source.Resolve("other.csv", named); got.Path != "other.csv" {
		t.Errorf("unknown arg should be an ad-hoc path, got %+v", got)
	}
	if got := source.Resolve("postgres://u@h/d", named); got.DSN != "postgres://u@h/d" || got.Path != "" {
		t.Errorf("a URL should be an ad-hoc DSN, got %+v", got)
	}
}

func TestResolveTakesAPathWithAColonForAPath(t *testing.T) {
	for _, arg := range []string{`C:\data\x.csv`, "C:/data/x.csv", `\\server\share\x.csv`, "dir/with:colon.csv"} {
		if got := source.Resolve(arg, nil); got.Path != arg || got.DSN != "" {
			t.Errorf("Resolve(%q) = %+v, want an ad-hoc path", arg, got)
		}
	}
}

func TestLabelNeverShowsPasswords(t *testing.T) {
	s := source.Source{DSN: "postgres://reader:hunter2@db.example.com/app"}
	label := s.Label()
	if strings.Contains(label, "hunter2") {
		t.Errorf("label leaks the password: %q", label)
	}
	if !strings.Contains(label, "reader") || !strings.Contains(label, "db.example.com") {
		t.Errorf("label lost the useful parts: %q", label)
	}

	if bad := (source.Source{DSN: "postgres://u:hunter2@h:notaport/d"}).Label(); strings.Contains(bad, "hunter2") {
		t.Errorf("unparseable DSN leaks the password: %q", bad)
	}
	if got := (source.Source{Path: "a.csv"}).Label(); got != "a.csv" {
		t.Errorf("path label = %q", got)
	}

	for _, dsn := range []string{
		"postgres://127.0.0.1:1/db?user=u&password=hunter2",
		"postgres://u@127.0.0.1:1/db?sslpassword=hunter2",
		"postgres://u@127.0.0.1:1/db?hunter2",
		"postgres://u@127.0.0.1:1/db#hunter2",
		"mysql:u:hunter2@tcp(127.0.0.1)/db",
		"postgres://u:12345/hunter2@h/db",
		"postgres://u@h/a;password=hunter2",
		"sqlserver://h;password=hunter2",
		"sqlserver://u;password=hunter2@h/db",
		"postgres://u@h/db%3Fpassword%3Dhunter2",
	} {
		if got := (source.Source{DSN: dsn}).Label(); strings.Contains(got, "hunter2") {
			t.Errorf("Label of %q leaks the secret: %q", dsn, got)
		}
	}
}

func TestALabelShowsWhereASourceLivesAndHidesItsOptions(t *testing.T) {
	for _, tc := range []struct{ dsn, want string }{
		{"postgres://reader:hunter2@db.example.com/app", "postgres://reader:xxxxx@db.example.com/app"},
		{"postgres://u@h/db?sslmode=disable", "postgres://u@h/db?…"},
		{"postgres://u@h/db", "postgres://u@h/db"},
		{"mysql:u:hunter2@tcp(127.0.0.1)/db", "(unparseable DSN)"},
	} {
		if got := (source.Source{DSN: tc.dsn}).Label(); got != tc.want {
			t.Errorf("Label of %q = %q, want %q", tc.dsn, got, tc.want)
		}
	}
}

// A Label is read from the stored text: a scheme no Kind claims is masked the same way, so a
// new Adapter cannot get it wrong.
func TestALabelAsksNoKind(t *testing.T) {
	s := source.Source{DSN: "nosuchkind://u:hunter2@h/d?token=hunter2"}
	if _, claimed := source.KindOf(s); claimed {
		t.Fatal("a Kind claims the scheme this test relies on nothing claiming")
	}
	if got, want := s.Label(), "nosuchkind://u:xxxxx@h/d?…"; got != want {
		t.Errorf("Label = %q, want %q", got, want)
	}
}

func makeSQLite(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE t (n INTEGER); INSERT INTO t VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	return path
}

func count(t *testing.T, s source.Source) string {
	t.Helper()
	a, err := source.Open(s)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	rows, err := a.Query(t.Context(), "SELECT count(*) FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	rows.Next()
	return strings.TrimSpace(strings.Trim(sprint(rows.Values()[0]), "\n"))
}

func TestSQLiteIsReadOnlyUnlessWriteIsEnabled(t *testing.T) {
	path := makeSQLite(t)

	ro, err := source.Open(source.Source{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := ro.Query(t.Context(), "INSERT INTO t VALUES (2)"); err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
		if err == nil {
			t.Error("write succeeded on a read-only Source")
		}
	}
	ro.Close()
	if got := count(t, source.Source{Path: path}); got != "1" {
		t.Fatalf("rejected write changed the data: count = %s", got)
	}

	rw, err := source.Open(source.Source{Path: path, Write: true})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := rw.Query(t.Context(), "INSERT INTO t VALUES (2)")
	if err != nil {
		t.Fatalf("write failed on a write-enabled Source: %v", err)
	}
	rows.Close()
	rw.Close()
	if got := count(t, source.Source{Path: path}); got != "2" {
		t.Errorf("count after write = %s, want 2", got)
	}
}

func countRows(t *testing.T, s source.Source, query string, opts ...source.OpenOption) int {
	t.Helper()
	a, err := source.Open(s, opts...)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	rows, err := a.Query(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRowCapLetsFileSourcesReturnOneRowMore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(path, []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := source.Source{Path: path}
	if got := countRows(t, s, "SELECT * FROM range(100000)", source.WithRowCap(5)); got != 6 {
		t.Errorf("with a cap of 5 a file Source returned %d rows, want 6 (cap+1)", got)
	}
	if got := countRows(t, s, "SELECT * FROM range(1000)"); got != 1000 {
		t.Errorf("without a cap a file Source returned %d rows, want all 1000", got)
	}
}

func TestRowCapDoesNotAffectSourcesThatStream(t *testing.T) {
	s := source.Source{Path: makeSQLite(t)}
	const fifty = "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 50) SELECT x FROM c"
	if got := countRows(t, s, fifty, source.WithRowCap(5)); got != 50 {
		t.Errorf("SQLite returned %d rows, want all 50: the UI does its own capping for sources that stream", got)
	}
}

func TestWriteIsRejectedForFileSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(path, []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := source.Open(source.Source{Path: path, Write: true})
	if err == nil || !strings.Contains(err.Error(), "writes are not supported for file Sources") {
		t.Errorf("err = %v, want a clear refusal", err)
	}
}

func TestMySQLDSNsAreOpenedByTheMySQLAdapter(t *testing.T) {
	for _, scheme := range []string{"mysql", "mariadb"} {
		_, err := source.Open(source.Source{DSN: scheme + "://u:hunter2@127.0.0.1:1/d"})
		if err == nil || !strings.Contains(err.Error(), "MySQL") {
			t.Errorf("%s: err = %v, want a MySQL connection error", scheme, err)
		}
		if err != nil && strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%s: error leaks the password: %v", scheme, err)
		}
	}
}

func TestDSNWithoutAdapterIsAClearError(t *testing.T) {
	_, err := source.Open(source.Source{DSN: "sqlserver://u:hunter2@h/d"})
	if err == nil || !strings.Contains(err.Error(), "sqlserver") {
		t.Fatalf("err = %v, want a message naming the scheme", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error leaks the password: %v", err)
	}
	if _, err := source.Open(source.Source{DSN: "not a url hunter2"}); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("err = %v, want a refusal that does not echo the DSN", err)
	}
}

func TestPostgresDSNsAreOpenedByThePostgresAdapter(t *testing.T) {
	for _, scheme := range []string{"postgres", "postgresql"} {
		_, err := source.Open(source.Source{DSN: scheme + "://u:hunter2@127.0.0.1:1/d?sslmode=disable"})
		if err == nil || !strings.Contains(err.Error(), "PostgreSQL") {
			t.Errorf("%s: err = %v, want a PostgreSQL connection error", scheme, err)
		}
		if err != nil && strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%s: error leaks the password: %v", scheme, err)
		}
	}
}
