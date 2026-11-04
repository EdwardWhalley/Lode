package session_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/config"
	"github.com/EdwardWhalley/lode/internal/session"
	"github.com/EdwardWhalley/lode/internal/source"
	"github.com/EdwardWhalley/lode/internal/tui"
)

// secret is the value of a password variable in these tests.
const secret = "hunter2-sekrit"

// rig is a Session over a sources.toml in a temporary directory and a made-up
// environment.
type rig struct {
	t    *testing.T
	path string
	env  map[string]string
	s    *session.Session
}

func newRig(t *testing.T, initial string) *rig {
	t.Helper()
	r := &rig{t: t, path: filepath.Join(t.TempDir(), "lode", "sources.toml"), env: map[string]string{}}
	if initial != "" {
		r.write(initial)
	}
	lookup := func(name string) (string, bool) { v, ok := r.env[name]; return v, ok }
	r.s = session.New(session.Options{
		ConfigPath: r.path,
		Lookup:     lookup,
		Load: func() (config.Config, error) {
			cfg, err := config.Load(r.path, lookup)
			if errors.Is(err, os.ErrNotExist) {
				return config.Config{}, nil
			}
			return cfg, err
		},
	})
	return r
}

func (r *rig) write(text string) {
	r.t.Helper()
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(r.path, []byte(text), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) text() string {
	r.t.Helper()
	b, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

// kind finds a Kind's index by name.
func (r *rig) kind(name string) int {
	r.t.Helper()
	for i, k := range r.s.Kinds() {
		if k.Name == name {
			return i
		}
	}
	r.t.Fatalf("no Kind called %q", name)
	return -1
}

func (r *rig) postgres(name string) tui.NewSource {
	return tui.NewSource{Kind: r.kind("PostgreSQL"), Name: name, Values: adapter.Values{
		"host": "127.0.0.1", "port": "1", "database": "d", "user": "u", "password_var": "PGPW", "options": "sslmode=disable",
	}}
}

func (r *rig) sqlite(name, path string) tui.NewSource {
	return tui.NewSource{Kind: r.kind("SQLite"), Name: name, Values: adapter.Values{"path": path}}
}

func TestTheFormSavesAParquetPathAndItOpensByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.parquet")
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("COPY (SELECT 1 AS id) TO '" + strings.ReplaceAll(path, "'", "''") + "' (FORMAT parquet)"); err != nil {
		t.Fatal(err)
	}

	r := newRig(t, "")
	if _, err := r.s.Sources(); err != nil {
		t.Fatal(err)
	}
	n := tui.NewSource{Kind: r.kind("file"), Name: "mine", Values: adapter.Values{"path": path}}
	if err := r.s.TestSource(t.Context(), n); err != nil {
		t.Fatalf("TestSource = %v", err)
	}
	c, err := r.s.AddSource(n)
	if err != nil {
		t.Fatalf("AddSource = %v", err)
	}
	o, err := r.s.Open(c.ID)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	defer o.Adapter.Close()
	rows, err := o.Adapter.Query(t.Context(), "SELECT id FROM data")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() || rows.Values()[0] != int64(1) {
		t.Errorf("read %v, err %v", rows.Values(), rows.Err())
	}
}

func TestKindsAreTheRegisteredOnesInOrderWithTheirFormsAndReadOnlyFlag(t *testing.T) {
	got := newRig(t, "").s.Kinds()
	if len(got) != len(source.Kinds) {
		t.Fatalf("%d kinds, want %d", len(got), len(source.Kinds))
	}
	for i, k := range source.Kinds {
		if got[i].Name != k.Name || got[i].ReadOnly != k.ReadOnly || len(got[i].Fields) != len(k.Fields) {
			t.Errorf("kind %d = %+v, want %q with %d fields, read-only %v", i, got[i], k.Name, len(k.Fields), k.ReadOnly)
		}
	}
	for _, k := range got {
		if (k.Name == "file") != k.ReadOnly {
			t.Errorf("%s: ReadOnly = %v; only file Sources are read-only", k.Name, k.ReadOnly)
		}
	}
}

func TestTestingAWorkingSourceOpensAndClosesItAndWritesNothing(t *testing.T) {
	r := newRig(t, "")
	if err := r.s.TestSource(t.Context(), r.sqlite("mine", makeSQLite(t))); err != nil {
		t.Fatalf("TestSource = %v", err)
	}
	if r.text() != "" {
		t.Error("testing a Source must not write the file")
	}
}

func TestAnUnsetVariableBlocksTestAndSaveWithoutNamingIt(t *testing.T) {
	r := newRig(t, "")
	n := r.postgres("prod")
	if err := r.s.TestSource(t.Context(), n); !errors.Is(err, tui.ErrVariableUnset) || strings.Contains(err.Error(), "PGPW") {
		t.Errorf("TestSource = %v, want ErrVariableUnset without the name", err)
	}
	if _, err := r.s.AddSource(n); !errors.Is(err, tui.ErrVariableUnset) || strings.Contains(err.Error(), "PGPW") {
		t.Errorf("AddSource = %v, want ErrVariableUnset without the name", err)
	}
	if r.text() != "" {
		t.Errorf("nothing may be written for an unset variable:\n%s", r.text())
	}
}

func TestAFailedTestSaysSoWithoutThePasswordAndSaveAnywayStillWorks(t *testing.T) {
	r := newRig(t, "")
	r.env["PGPW"] = secret
	n := r.postgres("prod")
	err := r.s.TestSource(t.Context(), n)
	if err == nil {
		t.Fatal("TestSource succeeded against a closed port")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error contains the password: %v", err)
	}
	c, err := r.s.AddSource(n)
	if err != nil {
		t.Fatalf("AddSource = %v", err)
	}
	if strings.Contains(c.Label, secret) || strings.Contains(r.text(), secret) {
		t.Error("the password reached the label or the file")
	}
	cfg, err := config.Load(r.path, func(string) (string, bool) { return secret, true })
	if err != nil || len(cfg.Sources) != 1 || cfg.Sources[0].Name != "prod" {
		t.Errorf("the written section should load: %v %+v", err, cfg.Sources)
	}
}

func TestAnErrorNeverRepeatsTheValueOfAVariableTheSourceUses(t *testing.T) {
	r := newRig(t, "")
	r.env["PGPW"] = secret
	r.env["DBHOST"] = secret + ".invalid"
	n := r.postgres("prod")
	n.Values["host"] = "${DBHOST}"
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	err := r.s.TestSource(ctx, n)
	if err == nil {
		t.Fatal("TestSource succeeded against a host that does not exist")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error repeats a variable's value: %v", err)
	}
}

func TestACreatedSQLiteSourceIsWrittenWithTheRestOfTheFileUntouchedAndCanBeOpened(t *testing.T) {
	old := "# my sources\ntheme = \"classic\"\n\n[sources.old]  # the first\npath = \"x.csv\"\n"
	r := newRig(t, old)
	db := makeSQLite(t)
	if _, err := r.s.Sources(); err != nil {
		t.Fatal(err)
	}
	n := r.sqlite("mine", db)
	n.MaxRows = 50
	c, err := r.s.AddSource(n)
	if err != nil {
		t.Fatalf("AddSource = %v", err)
	}
	if !strings.HasPrefix(r.text(), old) {
		t.Errorf("the old contents must stay as they were:\n%s", r.text())
	}
	if c.Name != "mine" || c.ID != "named:mine" {
		t.Errorf("choice = %+v", c)
	}
	list, err := r.s.Sources()
	if err != nil || len(list) != 2 {
		t.Fatalf("Sources = %v, %v; want old and mine", list, err)
	}
	o, err := r.s.Open(c.ID)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	if o.MaxRows != 50 {
		t.Errorf("cap = %d, want the Source's own 50", o.MaxRows)
	}
	o.Adapter.Close()
}

func TestTheFileStoresTheVariableNameNeverItsValue(t *testing.T) {
	r := newRig(t, "")
	r.env["PGPW"] = secret
	if _, err := r.s.AddSource(r.postgres("prod")); err != nil {
		t.Fatal(err)
	}
	if got := r.text(); !strings.Contains(got, "${PGPW}") || strings.Contains(got, secret) {
		t.Errorf("sources.toml should hold ${PGPW} and not its value:\n%s", got)
	}
}

func TestANameInUseIsRefused(t *testing.T) {
	r := newRig(t, "[sources.mine]\npath = \"x.csv\"\n")
	if _, err := r.s.AddSource(r.sqlite("mine", makeSQLite(t))); !errors.Is(err, tui.ErrNameTaken) {
		t.Errorf("AddSource = %v, want ErrNameTaken", err)
	}
}

func TestAFileChangedSinceItWasReadIsRefusedAndTheRetryWorks(t *testing.T) {
	r := newRig(t, "[sources.a]\npath = \"x.csv\"\n")
	if _, err := r.s.Sources(); err != nil {
		t.Fatal(err)
	}
	r.write("[sources.a]\npath = \"x.csv\"\n\n[sources.b]\npath = \"y.csv\"\n")
	n := r.sqlite("mine", makeSQLite(t))
	_, err := r.s.AddSource(n)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("AddSource = %v, want a refusal saying the file changed", err)
	}
	if strings.Contains(r.text(), "mine") {
		t.Error("a refused save wrote anyway")
	}
	if _, err := r.s.AddSource(n); err != nil {
		t.Errorf("retry = %v, want it to work now that the new contents have been read", err)
	}
	if !strings.Contains(r.text(), "[sources.b]") || !strings.Contains(r.text(), "[sources.mine]") {
		t.Errorf("the other program's edit and the new Source should both be there:\n%s", r.text())
	}
}

func TestAFileLodeWillNotEditSaysToEditItByHand(t *testing.T) {
	r := newRig(t, "sources = { a = { path = \"x.csv\" } }\n")
	_, err := r.s.AddSource(r.sqlite("mine", makeSQLite(t)))
	if err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Errorf("AddSource = %v, want advice to edit sources.toml by hand", err)
	}
}

func TestWithNoConfigFileThePathIsCreated(t *testing.T) {
	r := newRig(t, "")
	if _, err := r.s.AddSource(r.sqlite("mine", makeSQLite(t))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.text(), "[sources.mine]") {
		t.Errorf("file:\n%s", r.text())
	}
}

func TestASessionWithNoConfigPathCannotSave(t *testing.T) {
	s := session.New(session.Options{Load: staticConfig(config.Config{})})
	if _, err := s.AddSource(tui.NewSource{Kind: 1, Name: "x", Values: adapter.Values{"path": "a.db"}}); err == nil {
		t.Error("AddSource succeeded with nowhere to save")
	}
}

func TestATildePathIsStoredAsTypedAndMeansTheHomeFolderWhenLoaded(t *testing.T) {
	r := newRig(t, "")
	if _, err := r.s.AddSource(r.sqlite("mine", "~/lode-example.db")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.text(), `path = "~/lode-example.db"`) {
		t.Errorf("the path should be stored with its ~/:\n%s", r.text())
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	cfg, err := config.Load(r.path, func(string) (string, bool) { return "", false })
	if err != nil || len(cfg.Sources) != 1 || cfg.Sources[0].Path != filepath.Join(home, "lode-example.db") {
		t.Errorf("loaded %+v, %v; want the path under %s", cfg.Sources, err, home)
	}
}

func TestAWindowsPathIsStoredAsAPathNotADSN(t *testing.T) {
	r := newRig(t, "")
	if _, err := r.s.AddSource(r.sqlite("win", `C:\data\x.db`)); err != nil {
		t.Fatal(err)
	}
	if got := r.text(); !strings.Contains(got, "path = ") || strings.Contains(got, "dsn") {
		t.Errorf("the drive-letter path should be stored as a path:\n%s", got)
	}
	cfg, err := config.Load(r.path, func(string) (string, bool) { return "", false })
	if err != nil || len(cfg.Sources) != 1 || cfg.Sources[0].DSN != "" || cfg.Sources[0].Path == "" {
		t.Errorf("loaded %+v, %v; want a Source with a path and no DSN", cfg.Sources, err)
	}
}

func TestAPasswordWithURLCharactersLeavesNoFragmentInTheError(t *testing.T) {
	for _, pw := range []string{"p@ss/w:rd-sekrit", "a b?c#sekrit", "100%sekrit", "sekrit@sekrit", "p%40ss%2Fw%3Ard-sekrit"} {
		r := newRig(t, "")
		r.env["PGPW"] = pw
		err := r.s.TestSource(t.Context(), r.postgres("prod"))
		if err == nil {
			t.Fatalf("password %q: TestSource succeeded against a closed port", pw)
		}
		if strings.Contains(err.Error(), "sekrit") {
			t.Errorf("password %q: the error holds part of it: %v", pw, err)
		}
	}
}
