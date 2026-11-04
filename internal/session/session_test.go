package session_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/EdwardWhalley/lode/internal/adapter/file"
	"github.com/EdwardWhalley/lode/internal/adapter/mysql"
	"github.com/EdwardWhalley/lode/internal/adapter/postgres"
	"github.com/EdwardWhalley/lode/internal/config"
	"github.com/EdwardWhalley/lode/internal/rowcap"
	"github.com/EdwardWhalley/lode/internal/session"
	"github.com/EdwardWhalley/lode/internal/source"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

func TestResolveMaxRowsPrecedence(t *testing.T) {
	ten, hundred := 10, 100
	cases := []struct {
		name     string
		override *int
		source   source.Source
		global   int
		want     int
	}{
		{"nothing set", nil, source.Source{}, 0, rowcap.Default},
		{"top level", nil, source.Source{}, 500, 500},
		{"source beats top level", nil, source.Source{MaxRows: 200}, 500, 200},
		{"flag beats source", &ten, source.Source{MaxRows: 200}, 500, 10},
		{"flag beats everything", &hundred, source.Source{}, 0, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := session.ResolveMaxRows(tc.override, tc.source, tc.global)
			if err != nil || got != tc.want {
				t.Errorf("ResolveMaxRows = %d, %v; want %d", got, err, tc.want)
			}
		})
	}
}

func TestResolveMaxRowsRejectsBadFlagValues(t *testing.T) {
	for _, n := range []int{0, -1, rowcap.Limit + 1} {
		if _, err := session.ResolveMaxRows(&n, source.Source{}, 0); err == nil || !strings.Contains(err.Error(), "--max-rows") {
			t.Errorf("--max-rows %d: err = %v, want a --max-rows error", n, err)
		}
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

func staticConfig(c config.Config) func() (config.Config, error) {
	return func() (config.Config, error) { return c, nil }
}

// canWrite reports whether a statement that modifies the Source succeeds.
func canWrite(t *testing.T, opened session.Opened) bool {
	t.Helper()
	defer opened.Adapter.Close()
	rows, err := opened.Adapter.Query(t.Context(), "INSERT INTO t VALUES (2)")
	if err != nil {
		return false
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	return err == nil
}

func TestANamedFileSourceWithAHomePathReadsAndIsBounded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.WriteFile(filepath.Join(home, "a.csv"), []byte("id\n1\n2\n3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	toml := filepath.Join(t.TempDir(), "sources.toml")
	text := "[sources.mine]\npath = \"~/a.csv\"\nmax_rows = 2\n"
	if err := os.WriteFile(toml, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(toml, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	opened, err := session.New(session.Options{Load: staticConfig(cfg), Config: cfg}).Open("named:mine")
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Adapter.Close()

	tables, err := opened.Adapter.Schema(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0].Name != "a" {
		t.Fatalf("schema = %+v, want the table a", tables)
	}
	rows, err := opened.Adapter.Query(t.Context(), "SELECT * FROM a")
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
	if n != 3 {
		t.Errorf("got %d rows of the file's 3, want the row cap's 3", n)
	}
}

func TestWriteFlagAppliesOnlyToTheLaunchSource(t *testing.T) {
	launch, other := makeSQLite(t), makeSQLite(t)
	cfg := config.Config{Sources: []source.Source{
		{Name: "other", Path: other},
		{Name: "writable", Path: makeSQLite(t), Write: true},
	}}
	s := session.New(session.Options{
		Load:   staticConfig(cfg),
		Config: cfg,
		Launch: source.Source{Path: launch},
		Write:  true,
	})

	l, ok := s.Launch()
	if !ok || !l.Write {
		t.Fatalf("launch = %+v, %v; want a write-enabled Choice", l, ok)
	}
	opened, err := s.Open(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !opened.Choice.Write || !canWrite(t, opened) {
		t.Error("the launch Source should be writable with --write")
	}

	opened, err = s.Open("named:other")
	if err != nil {
		t.Fatal(err)
	}
	if opened.Choice.Write || canWrite(t, opened) {
		t.Error("--write must not make another Source writable")
	}

	opened, err = s.Open("named:writable")
	if err != nil {
		t.Fatal(err)
	}
	if !opened.Choice.Write || !canWrite(t, opened) {
		t.Error("a Source with write = true should be writable")
	}
}

func TestWriteFlagOnANamedLaunchSource(t *testing.T) {
	cfg := config.Config{Sources: []source.Source{
		{Name: "a", Path: makeSQLite(t)},
		{Name: "b", Path: makeSQLite(t)},
	}}
	s := session.New(session.Options{Load: staticConfig(cfg), Config: cfg, Launch: cfg.Sources[0], Write: true})
	l, _ := s.Launch()
	if l.ID != "named:a" {
		t.Fatalf("launch ID = %q", l.ID)
	}
	a, _ := s.Open("named:a")
	b, _ := s.Open("named:b")
	if !canWrite(t, a) {
		t.Error("the named launch Source should be writable with --write")
	}
	if canWrite(t, b) {
		t.Error("--write leaked to the other named Source")
	}
}

func TestWriteFlagWithNoLaunchSourceGoesToTheFirstSourceOpened(t *testing.T) {
	cfg := config.Config{Sources: []source.Source{
		{Name: "a", Path: makeSQLite(t)},
		{Name: "b", Path: makeSQLite(t)},
	}}
	s := session.New(session.Options{Load: staticConfig(cfg), Config: cfg, Write: true})
	if _, ok := s.Launch(); ok {
		t.Fatal("there is no launch Source")
	}

	b, err := s.Open("named:b")
	if err != nil {
		t.Fatal(err)
	}
	if !b.Choice.Write || !canWrite(t, b) {
		t.Error("--write should apply to the first Source opened")
	}
	a, err := s.Open("named:a")
	if err != nil {
		t.Fatal(err)
	}
	if a.Choice.Write || canWrite(t, a) {
		t.Error("--write must not spread to the second Source opened")
	}
}

func TestMaxRowsFlagOverridesEverySourceElseEachUsesItsOwnCap(t *testing.T) {
	cfg := config.Config{MaxRows: 500, Sources: []source.Source{
		{Name: "own", Path: makeSQLite(t), MaxRows: 20},
		{Name: "plain", Path: makeSQLite(t)},
	}}
	without := session.New(session.Options{Load: staticConfig(cfg), Config: cfg})
	for id, want := range map[string]int{"named:own": 20, "named:plain": 500} {
		o, err := without.Open(id)
		if err != nil {
			t.Fatal(err)
		}
		o.Adapter.Close()
		if o.MaxRows != want {
			t.Errorf("%s: cap = %d, want %d", id, o.MaxRows, want)
		}
	}

	three := 3
	with := session.New(session.Options{Load: staticConfig(cfg), Config: cfg, MaxRows: &three})
	for _, id := range []string{"named:own", "named:plain"} {
		o, err := with.Open(id)
		if err != nil {
			t.Fatal(err)
		}
		o.Adapter.Close()
		if o.MaxRows != 3 {
			t.Errorf("%s: cap = %d, want the --max-rows value 3", id, o.MaxRows)
		}
	}
}

func TestSourcesReReadsTheConfigAndKeepsTheLastGoodOneOnError(t *testing.T) {
	good := config.Config{Sources: []source.Source{{Name: "a", Path: makeSQLite(t)}}}
	bigger := config.Config{Sources: []source.Source{
		{Name: "a", Path: makeSQLite(t)},
		{Name: "b", Path: makeSQLite(t)},
	}}
	cfg, loadErr := good, error(nil)
	s := session.New(session.Options{
		Load:   func() (config.Config, error) { return cfg, loadErr },
		Config: good,
	})

	choices, err := s.Sources()
	if err != nil || len(choices) != 1 {
		t.Fatalf("Sources = %+v, %v; want one", choices, err)
	}

	cfg = bigger
	choices, err = s.Sources()
	if err != nil || len(choices) != 2 || choices[1].Name != "b" {
		t.Fatalf("after editing the config Sources = %+v, %v; want a and b", choices, err)
	}

	cfg, loadErr = config.Config{}, errors.New("sources.toml: line 3: unset variable ${PW}")
	if _, err := s.Sources(); err == nil {
		t.Fatal("a broken config should be an error")
	}
	o, err := s.Open("named:b")
	if err != nil {
		t.Fatalf("a failed re-read must not lose the last good config: %v", err)
	}
	o.Adapter.Close()
}

func TestAnAdHocLaunchSourceIsListedFirstWithASafeLabel(t *testing.T) {
	cfg := config.Config{Sources: []source.Source{{Name: "a", Path: makeSQLite(t)}}}
	s := session.New(session.Options{
		Load:   staticConfig(cfg),
		Config: cfg,
		Launch: source.Source{DSN: "postgres://reader:hunter2@db.example.com/app"},
	})
	choices, err := s.Sources()
	if err != nil || len(choices) != 2 {
		t.Fatalf("Sources = %+v, %v", choices, err)
	}
	if choices[0].Name != "" || !strings.Contains(choices[0].Label, "db.example.com") {
		t.Errorf("first choice = %+v, want the ad-hoc launch Source", choices[0])
	}
	for _, c := range choices {
		if strings.Contains(c.Label, "hunter2") || strings.Contains(c.ID, "hunter2") {
			t.Errorf("choice leaks the password: %+v", c)
		}
	}
}

func TestOpenFailuresAreErrorsWithoutThePassword(t *testing.T) {
	cfg := config.Config{Sources: []source.Source{{Name: "down", DSN: "postgres://u:hunter2@127.0.0.1:1/d?sslmode=disable"}}}
	s := session.New(session.Options{Load: staticConfig(cfg), Config: cfg})

	if _, err := s.Open("named:down"); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("err = %v, want a connection error without the password", err)
	}
	if _, err := s.Open("named:gone"); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("err = %v, want a message naming the missing Source", err)
	}
}

func TestOpenedFileSourceIsBoundedByTheRowCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(path, []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{MaxRows: 5, Sources: []source.Source{{Name: "f", Path: path}}}
	s := session.New(session.Options{Load: staticConfig(cfg), Config: cfg})
	o, err := s.Open("named:f")
	if err != nil {
		t.Fatal(err)
	}
	defer o.Adapter.Close()
	rows, err := o.Adapter.Query(t.Context(), "SELECT * FROM range(100000)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if n != 6 {
		t.Errorf("a file Source with a cap of 5 returned %d rows, want 6 (cap+1)", n)
	}
}

// isMySQL below is what the scheme check the Kind replaced said for each Source: the DSN's
// text before its first colon, lower-cased, is mysql or mariadb.
func TestAChoiceCarriesTheDialectOfItsKind(t *testing.T) {
	cfg := config.Config{Sources: []source.Source{
		{Name: "my", DSN: "mysql://u@h/d"},
		{Name: "maria", DSN: "MariaDB://u@h/d"},
		{Name: "pg", DSN: "postgres://u@h/d"},
		{Name: "file", Path: "a.csv"},
		{Name: "upper", DSN: "MYSQL://h/d"},
		{Name: "variable", DSN: "mariadb://${USER}@h/d"},
		{Name: "go-driver", DSN: "mysql:u@tcp(h)/d"},
		{Name: "path", Path: "mysql:dump.sqlite"},
		{Name: "unclaimed", DSN: "nosuch://h/d"},
	}}
	s := session.New(session.Options{Load: staticConfig(cfg), Config: cfg})
	choices, err := s.Sources()
	if err != nil {
		t.Fatal(err)
	}
	isMySQL := map[string]bool{
		"my": true, "maria": true, "pg": false, "file": false,
		"upper": true, "variable": true, "go-driver": true, "path": false, "unclaimed": false,
	}
	if len(choices) != len(isMySQL) {
		t.Fatalf("got %d Choices, want %d", len(choices), len(isMySQL))
	}
	others := map[string]sqltext.Dialect{"pg": postgres.Kind.Dialect, "file": file.Kind.Dialect}
	if mysql.Kind.Dialect == (sqltext.Dialect{}) {
		t.Fatal("mysql.Kind sets no Dialect")
	}
	for name, d := range others {
		if d == (sqltext.Dialect{}) || d == mysql.Kind.Dialect {
			t.Fatalf("%s: its Kind sets the zero or MySQL's Dialect, so this test cannot tell them apart", name)
		}
	}
	for _, c := range choices {
		want := others[c.Name]
		if isMySQL[c.Name] {
			want = mysql.Kind.Dialect
		}
		if c.Dialect != want {
			t.Errorf("%s: Dialect = %+v, want %+v", c.Name, c.Dialect, want)
		}
	}
}
