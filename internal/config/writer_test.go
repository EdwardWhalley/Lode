package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/config"
	"github.com/EdwardWhalley/lode/internal/source"
)

// anyEnv answers every ${NAME}, so a fixture loads whatever the environment holds.
func anyEnv(string) (string, bool) { return "pw", true }

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

// setup writes content to a new sources.toml and returns what a caller would hold
// after reading it.
func setup(t *testing.T, content string) (string, config.Snapshot) {
	t.Helper()
	path := write(t, content)
	snap, err := config.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, snap
}

func contents(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func loadAll(t *testing.T, path string) config.Config {
	t.Helper()
	cfg, err := config.Load(path, anyEnv)
	if err != nil {
		t.Fatalf("the file no longer loads: %v\n%s", err, contents(t, path))
	}
	return cfg
}

// fixture has every kind of thing the writer must leave alone.
const fixture = `# lode sources
theme = "light"
history = false
max_rows = 200   # default cap
mouse = true

# the people file
[sources.people]
path = "data/people.csv"   # relative

# Production, read only.
# Password comes from the environment.
[sources.prod]  # primary
dsn = "postgres://reader:${PROD_PASSWORD}@db.example.com/app"
max_rows = 500

[sources.app]
path = "~/db/app.sqlite"
write = true
# end of file
`

const (
	head = `# lode sources
theme = "light"
history = false
max_rows = 200   # default cap
mouse = true

`
	people = `# the people file
[sources.people]
path = "data/people.csv"   # relative

`
	prod = `# Production, read only.
# Password comes from the environment.
[sources.prod]  # primary
dsn = "postgres://reader:${PROD_PASSWORD}@db.example.com/app"
max_rows = 500

`
	app = `[sources.app]
path = "~/db/app.sqlite"
write = true
# end of file
`
)

type op struct {
	name string
	run  func(path string, snap config.Snapshot) (config.Snapshot, error)
	want string
}

func ops() []op {
	add := func(s config.Section) func(string, config.Snapshot) (config.Snapshot, error) {
		return func(p string, snap config.Snapshot) (config.Snapshot, error) { return config.Add(p, snap, s) }
	}
	replace := func(s config.Section) func(string, config.Snapshot) (config.Snapshot, error) {
		return func(p string, snap config.Snapshot) (config.Snapshot, error) { return config.Replace(p, snap, s) }
	}
	remove := func(name string) func(string, config.Snapshot) (config.Snapshot, error) {
		return func(p string, snap config.Snapshot) (config.Snapshot, error) { return config.Remove(p, snap, name) }
	}
	return []op{
		{"add a path", add(config.Section{Name: "new", Path: "n.csv"}),
			fixture + "\n[sources.new]\npath = \"n.csv\"\n"},
		{"add a dsn with write and max_rows", add(config.Section{Name: "db_2", DSN: "mysql://u:${PW}@h/d", Write: true, MaxRows: 25}),
			fixture + "\n[sources.db_2]\ndsn = \"mysql://u:${PW}@h/d\"\nwrite = true\nmax_rows = 25\n"},
		{"replace the people source and add keys", replace(config.Section{Name: "people", Path: "other.csv", Write: true, MaxRows: 5}),
			head + `# the people file
[sources.people]
path = "other.csv"   # relative
write = true
max_rows = 5

` + prod + app},
		{"replace prod and drop its max_rows", replace(config.Section{Name: "prod", DSN: "postgres://reader:${PROD_PASSWORD}@db.example.com/app"}),
			head + people + `# Production, read only.
# Password comes from the environment.
[sources.prod]  # primary
dsn = "postgres://reader:${PROD_PASSWORD}@db.example.com/app"

` + app},
		{"replace app's path with a dsn", replace(config.Section{Name: "app", DSN: "mysql://u:${PW}@h/db", Write: true}),
			head + people + prod + `[sources.app]
dsn = "mysql://u:${PW}@h/db"
write = true
# end of file
`},
		{"replace and switch write off", replace(config.Section{Name: "app", Path: "~/db/app.sqlite"}),
			head + people + prod + `[sources.app]
path = "~/db/app.sqlite"
# end of file
`},
		{"replace with nothing changed", replace(config.Section{Name: "people", Path: "data/people.csv"}), fixture},
		{"remove people and its comment", remove("people"), head + prod + app},
		{"remove prod, its comments and the blank line after it", remove("prod"), head + people + app},
		{"remove app and leave the comment after it", remove("app"),
			head + people + `# Production, read only.
# Password comes from the environment.
[sources.prod]  # primary
dsn = "postgres://reader:${PROD_PASSWORD}@db.example.com/app"
max_rows = 500

# end of file
`},
	}
}

func TestEveryOperationChangesNothingButItsOwnLines(t *testing.T) {
	for _, eol := range []struct {
		name string
		conv func(string) string
	}{{"LF", func(s string) string { return s }}, {"CRLF", crlf}} {
		for _, c := range ops() {
			t.Run(eol.name+"/"+c.name, func(t *testing.T) {
				path, snap := setup(t, eol.conv(fixture))
				if _, err := c.run(path, snap); err != nil {
					t.Fatal(err)
				}
				if got, want := contents(t, path), eol.conv(c.want); got != want {
					t.Errorf("file after the operation:\n%q\nwant:\n%q", got, want)
				}
			})
		}
	}
}

func TestEveryOperationLoadsAsTheOldConfigPlusTheChange(t *testing.T) {
	byName := func(cfg config.Config, name string) (source.Source, bool) {
		for _, s := range cfg.Sources {
			if s.Name == name {
				return s, true
			}
		}
		return source.Source{}, false
	}
	others := func(cfg config.Config, name string) []source.Source {
		var out []source.Source
		for _, s := range cfg.Sources {
			if s.Name != name {
				out = append(out, s)
			}
		}
		return out
	}

	changes := []struct {
		name string
		run  func(string, config.Snapshot) (config.Snapshot, error)
		want *source.Source
	}{
		{"new", func(p string, s config.Snapshot) (config.Snapshot, error) {
			return config.Add(p, s, config.Section{Name: "new", DSN: "mysql://u:${PW}@h/d", Write: true, MaxRows: 25})
		}, &source.Source{Name: "new", DSN: "mysql://u:pw@h/d", Write: true, MaxRows: 25}},
		{"prod", func(p string, s config.Snapshot) (config.Snapshot, error) {
			return config.Replace(p, s, config.Section{Name: "prod", DSN: "postgres://r@h/other", MaxRows: 7})
		}, &source.Source{Name: "prod", DSN: "postgres://r@h/other", MaxRows: 7}},
		{"people", func(p string, s config.Snapshot) (config.Snapshot, error) { return config.Remove(p, s, "people") }, nil},
	}
	for _, c := range changes {
		t.Run(c.name, func(t *testing.T) {
			path, snap := setup(t, fixture)
			before := loadAll(t, path)
			if _, err := c.run(path, snap); err != nil {
				t.Fatal(err)
			}
			after := loadAll(t, path)
			if !reflect.DeepEqual(others(after, c.name), others(before, c.name)) {
				t.Errorf("another Source changed:\n got %+v\nwant %+v", others(after, c.name), others(before, c.name))
			}
			got, ok := byName(after, c.name)
			switch {
			case c.want == nil && ok:
				t.Errorf("%s is still there: %+v", c.name, got)
			case c.want != nil && got != *c.want:
				t.Errorf("%s = %+v, want %+v", c.name, got, *c.want)
			}
			if after.Theme != before.Theme || after.MaxRows != before.MaxRows ||
				after.HistoryOff != before.HistoryOff || after.MouseOff != before.MouseOff || after.HomeOff != before.HomeOff {
				t.Errorf("a top-level setting changed: %+v -> %+v", before, after)
			}
		})
	}
}

func TestValuesWithBackslashesAndQuotesRoundTrip(t *testing.T) {
	path, snap := setup(t, "")
	dsn := "postgres://u:${PW}@h/d?x=a\"b\\c\nline\ttab\x1b[31m\x7f"
	if _, err := config.Add(path, snap, config.Section{Name: "odd", DSN: dsn}); err != nil {
		t.Fatal(err)
	}
	got := loadAll(t, path).Sources
	if want := strings.Replace(dsn, "${PW}", "pw", 1); len(got) != 1 || got[0].DSN != want {
		t.Errorf("dsn after a round trip = %+v, want %q", got, want)
	}
}

func TestAddsInTheFilesOwnLineEndingAndKeepsBlankLinesFromPiling(t *testing.T) {
	path, snap := setup(t, "[sources.a]\r\npath = \"a.csv\"")
	snap, err := config.Add(path, snap, config.Section{Name: "b", Path: "b.csv"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := contents(t, path), "[sources.a]\r\npath = \"a.csv\"\r\n\r\n[sources.b]\r\npath = \"b.csv\"\r\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	snap, err = config.Remove(path, snap, "b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Add(path, snap, config.Section{Name: "c", Path: "c.csv"}); err != nil {
		t.Fatal(err)
	}
	if got, want := contents(t, path), "[sources.a]\r\npath = \"a.csv\"\r\n\r\n[sources.c]\r\npath = \"c.csv\"\r\n"; got != want {
		t.Errorf("after add, remove, add: %q, want %q", got, want)
	}
}

func TestANewFileGetsLFAndNoLeadingBlankLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.toml")
	snap, err := config.Read(path)
	if err != nil {
		t.Fatalf("a missing file is a snapshot of nothing, got %v", err)
	}
	if _, err := config.Add(path, snap, config.Section{Name: "n", Path: "n.csv"}); err != nil {
		t.Fatal(err)
	}
	if got, want := contents(t, path), "[sources.n]\npath = \"n.csv\"\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestIndentOfTheSectionIsKeptForInsertedKeys(t *testing.T) {
	path, snap := setup(t, "[sources.a]\n  path = \"a.csv\"\n")
	if _, err := config.Replace(path, snap, config.Section{Name: "a", Path: "a.csv", MaxRows: 3}); err != nil {
		t.Fatal(err)
	}
	if got, want := contents(t, path), "[sources.a]\n  path = \"a.csv\"\n  max_rows = 3\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInsertingAtTheEndOfAFileWithoutALineBreak(t *testing.T) {
	path, snap := setup(t, "[sources.a]\npath = \"a.csv\"")
	if _, err := config.Replace(path, snap, config.Section{Name: "a", Path: "a.csv", Write: true}); err != nil {
		t.Fatal(err)
	}
	if got, want := contents(t, path), "[sources.a]\npath = \"a.csv\"\nwrite = true\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestACommentIsOwnedOnlyWhenItIsUnambiguous(t *testing.T) {
	for _, c := range []struct {
		name, in, want string
	}{
		{"touching the header, after a blank line",
			"[sources.a]\npath = \"a\"\n\n# b's\n[sources.b]\npath = \"b\"\n",
			"[sources.a]\npath = \"a\"\n"},
		{"first thing in the file (a banner)",
			"# banner\n[sources.b]\npath = \"b\"\n\n[sources.a]\npath = \"a\"\n",
			"# banner\n[sources.a]\npath = \"a\"\n"},
		{"straight after another section's keys",
			"[sources.a]\npath = \"a\"\n# maybe a's, maybe b's\n[sources.b]\npath = \"b\"\n",
			"[sources.a]\npath = \"a\"\n# maybe a's, maybe b's\n"},
		{"a blank line between the comment and the header",
			"[sources.a]\npath = \"a\"\n\n# about b\n\n[sources.b]\npath = \"b\"\n",
			"[sources.a]\npath = \"a\"\n\n# about b\n"},
		{"a comment indented inside the section's keys is not a separate block",
			"[sources.b]\n# why\npath = \"b\"\n\n[sources.a]\npath = \"a\"\n",
			"[sources.a]\npath = \"a\"\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path, snap := setup(t, c.in)
			if _, err := config.Remove(path, snap, "b"); err != nil {
				t.Fatal(err)
			}
			if got := contents(t, path); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestCommentsInsideASectionSurviveAReplace(t *testing.T) {
	path, snap := setup(t, "[sources.a]\n# where it lives\npath = \"a.csv\"\n# be careful\nwrite = true\n")
	if _, err := config.Replace(path, snap, config.Section{Name: "a", Path: "b.csv", Write: true}); err != nil {
		t.Fatal(err)
	}
	if got, want := contents(t, path), "[sources.a]\n# where it lives\npath = \"b.csv\"\n# be careful\nwrite = true\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAHeaderLookalikeInsideAMultiLineStringIsNotAHeader(t *testing.T) {
	in := "[sources.a]\npath = \"\"\"\n[sources.fake]\nx.csv\"\"\"\n\n[sources.b]\npath = \"b.csv\"\n"
	t.Run("removing the one after it", func(t *testing.T) {
		path, snap := setup(t, in)
		if _, err := config.Remove(path, snap, "b"); err != nil {
			t.Fatal(err)
		}
		if got, want := contents(t, path), "[sources.a]\npath = \"\"\"\n[sources.fake]\nx.csv\"\"\"\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("removing the one that holds it", func(t *testing.T) {
		path, snap := setup(t, in)
		if _, err := config.Remove(path, snap, "a"); err != nil {
			t.Fatal(err)
		}
		if got, want := contents(t, path), "[sources.b]\npath = \"b.csv\"\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("a value that needs rewriting is refused", func(t *testing.T) {
		path, snap := setup(t, in)
		_, err := config.Replace(path, snap, config.Section{Name: "a", Path: "other.csv"})
		if !errors.Is(err, config.ErrUnsupported) {
			t.Errorf("err = %v, want ErrUnsupported", err)
		}
		if got := contents(t, path); got != in {
			t.Errorf("file changed: %q", got)
		}
	})
	t.Run("a value that stays is left as it is", func(t *testing.T) {
		path, snap := setup(t, in)
		if _, err := config.Replace(path, snap, config.Section{Name: "a", Path: "[sources.fake]\nx.csv", MaxRows: 4}); err != nil {
			t.Fatal(err)
		}
		want := "[sources.a]\npath = \"\"\"\n[sources.fake]\nx.csv\"\"\"\nmax_rows = 4\n\n[sources.b]\npath = \"b.csv\"\n"
		if got := contents(t, path); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestOtherSpellingsOfAHeader(t *testing.T) {
	path, snap := setup(t, "[ sources . \"q\" ]   # quoted\npath = 'q.csv'\n")
	if _, err := config.Replace(path, snap, config.Section{Name: "q", Path: "r.csv"}); err != nil {
		t.Fatal(err)
	}
	if got, want := contents(t, path), "[ sources . \"q\" ]   # quoted\npath = \"r.csv\"\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnsupportedShapesAreRefusedAndNeverAltered(t *testing.T) {
	shapes := map[string]string{
		"inline table":   "sources = { a = { path = \"a.csv\" } }\n",
		"dotted keys":    "sources.a.path = \"a.csv\"\n",
		"dotted in head": "[sources]\na.path = \"a.csv\"\n",
		"inline entry":   "[sources]\na = { path = \"a.csv\" }\n",
	}
	for name, in := range shapes {
		t.Run(name, func(t *testing.T) {
			for opName, run := range map[string]func(string, config.Snapshot) (config.Snapshot, error){
				"replace": func(p string, s config.Snapshot) (config.Snapshot, error) {
					return config.Replace(p, s, config.Section{Name: "a", Path: "z.csv"})
				},
				"remove": func(p string, s config.Snapshot) (config.Snapshot, error) { return config.Remove(p, s, "a") },
			} {
				path, snap := setup(t, in)
				if _, err := run(path, snap); !errors.Is(err, config.ErrUnsupported) {
					t.Errorf("%s: err = %v, want ErrUnsupported", opName, err)
				}
				if got := contents(t, path); got != in {
					t.Errorf("%s changed the file: %q", opName, got)
				}
			}
		})
	}

	for name, in := range shapes {
		t.Run("add beside "+name, func(t *testing.T) {
			path, snap := setup(t, in)
			_, err := config.Add(path, snap, config.Section{Name: "b", Path: "b.csv"})
			got := contents(t, path)
			if err != nil {
				if !errors.Is(err, config.ErrUnsupported) {
					t.Errorf("err = %v, want nil or ErrUnsupported", err)
				}
				if got != in {
					t.Errorf("a refused add changed the file: %q", got)
				}
				return
			}
			if !strings.HasPrefix(got, in) {
				t.Errorf("the add changed what was there: %q", got)
			}
			cfg := loadAll(t, path)
			if len(cfg.Sources) != 2 {
				t.Errorf("sources = %+v, want a and b", cfg.Sources)
			}
		})
	}
	for _, in := range []string{shapes["inline table"], shapes["dotted keys"]} {
		path, snap := setup(t, in)
		if _, err := config.Add(path, snap, config.Section{Name: "b", Path: "b.csv"}); !errors.Is(err, config.ErrUnsupported) {
			t.Errorf("add beside %q: err = %v, want ErrUnsupported", in, err)
		}
	}
}

func TestASectionNeedsExactlyOneOfPathAndDSN(t *testing.T) {
	const secret = "hunter2"
	path, snap := setup(t, fixture)
	for _, tc := range []struct {
		name string
		s    config.Section
		want string
	}{
		{"neither", config.Section{Name: "new"}, "new: needs a path or a dsn"},
		{"both", config.Section{Name: "new", Path: "x.csv", DSN: "postgres://u:" + secret + "@h/d"}, "new: has both a path and a dsn; use one"},
	} {
		for op, f := range map[string]func(string, config.Snapshot, config.Section) (config.Snapshot, error){"Add": config.Add, "Replace": config.Replace} {
			s := tc.s
			if op == "Replace" {
				s.Name = "app"
			}
			_, err := f(path, snap, s)
			want := strings.Replace(tc.want, "new:", s.Name+":", 1)
			if err == nil || err.Error() != want {
				t.Errorf("%s %s: err = %v, want %q", op, tc.name, err, want)
			}
		}
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != fixture {
		t.Errorf("the file changed (err %v):\n%s", err, after)
	}
}

func TestNamesAreChecked(t *testing.T) {
	path, snap := setup(t, fixture)
	for _, name := range []string{"", "a b", "a.b", "a/b", "é", "a]\n[sources.x", `"q"`, "a=b"} {
		if _, err := config.Add(path, snap, config.Section{Name: name, Path: "x"}); !errors.Is(err, config.ErrBadName) {
			t.Errorf("Add %q: err = %v, want ErrBadName", name, err)
		}
		if _, err := config.Replace(path, snap, config.Section{Name: name, Path: "x"}); !errors.Is(err, config.ErrBadName) {
			t.Errorf("Replace %q: err = %v, want ErrBadName", name, err)
		}
		if _, err := config.Remove(path, snap, name); !errors.Is(err, config.ErrBadName) {
			t.Errorf("Remove %q: err = %v, want ErrBadName", name, err)
		}
	}
	for _, name := range []string{"a", "A-1", "x_y", "9"} {
		if _, err := config.Add(t.TempDir()+"/s.toml", config.Snapshot{}, config.Section{Name: name, Path: "x"}); err != nil {
			t.Errorf("Add %q: %v", name, err)
		}
	}
	if _, err := config.Add(path, snap, config.Section{Name: "people", Path: "x"}); !errors.Is(err, config.ErrExists) {
		t.Errorf("adding an existing name: err = %v, want ErrExists", err)
	}
	if _, err := config.Replace(path, snap, config.Section{Name: "nope", Path: "x"}); !errors.Is(err, config.ErrNotFound) {
		t.Errorf("replacing a missing name: err = %v, want ErrNotFound", err)
	}
	if _, err := config.Remove(path, snap, "nope"); !errors.Is(err, config.ErrNotFound) {
		t.Errorf("removing a missing name: err = %v, want ErrNotFound", err)
	}
	if got := contents(t, path); got != fixture {
		t.Errorf("a refused operation changed the file: %q", got)
	}
}

func TestASectionMustBeOneThatLoadAccepts(t *testing.T) {
	path, snap := setup(t, fixture)
	for name, s := range map[string]config.Section{
		"neither path nor dsn": {Name: "n"},
		"both":                 {Name: "n", Path: "a", DSN: "b"},
		"not valid UTF-8":      {Name: "n", Path: "a\xffb"},
		"negative max rows":    {Name: "n", Path: "a", MaxRows: -1},
		"too many max rows":    {Name: "n", Path: "a", MaxRows: 1_000_001},
	} {
		if _, err := config.Add(path, snap, s); err == nil {
			t.Errorf("Add with %s: accepted", name)
		}
		s.Name = "people"
		if _, err := config.Replace(path, snap, s); err == nil {
			t.Errorf("Replace with %s: accepted", name)
		}
	}
	if got := contents(t, path); got != fixture {
		t.Errorf("a refused operation changed the file: %q", got)
	}
}

func TestAFileThatDoesNotLoadIsNotTouched(t *testing.T) {
	in := "bogus = 1\n[sources.a]\npath = \"a\"\n"
	path, snap := setup(t, in)
	if _, err := config.Add(path, snap, config.Section{Name: "b", Path: "b"}); err == nil {
		t.Error("Add to a file that does not load was accepted")
	}
	if got := contents(t, path); got != in {
		t.Errorf("file changed: %q", got)
	}
}

func TestVerifyRefusesAResultThatIsNotTheOldConfigPlusTheChange(t *testing.T) {
	const before = "theme = \"light\"\n\n[sources.a]\npath = \"a.csv\"\n\n[sources.b]\npath = \"b.csv\"\n"
	add := config.Section{Name: "c", Path: "c.csv"}
	path := filepath.Join(t.TempDir(), "sources.toml")

	if err := config.Verify(path, []byte(before), []byte(before+"\n[sources.c]\npath = \"c.csv\"\n"), "add", add); err != nil {
		t.Fatalf("the right result was refused: %v", err)
	}
	for name, c := range map[string]struct {
		after, op string
		s         config.Section
	}{
		"another source dropped":  {"theme = \"light\"\n\n[sources.a]\npath = \"a.csv\"\n\n[sources.c]\npath = \"c.csv\"\n", "add", add},
		"another source changed":  {strings.Replace(before, "b.csv", "x.csv", 1) + "\n[sources.c]\npath = \"c.csv\"\n", "add", add},
		"a setting changed":       {strings.Replace(before, "light", "mono", 1) + "\n[sources.c]\npath = \"c.csv\"\n", "add", add},
		"a setting dropped":       {strings.Replace(before, "theme = \"light\"\n", "", 1) + "\n[sources.c]\npath = \"c.csv\"\n", "add", add},
		"the change missing":      {before, "add", add},
		"the wrong change":        {before + "\n[sources.c]\npath = \"other.csv\"\n", "add", add},
		"does not load":           {before + "\n[sources.c\npath = \"c.csv\"\n", "add", add},
		"remove removed nothing":  {before, "remove", config.Section{Name: "b"}},
		"remove removed another":  {"theme = \"light\"\n\n[sources.b]\npath = \"b.csv\"\n", "remove", config.Section{Name: "b"}},
		"replace changed nothing": {before, "replace", config.Section{Name: "a", Path: "z.csv"}},
	} {
		err := config.Verify(path, []byte(before), []byte(c.after), c.op, c.s)
		if !errors.Is(err, config.ErrUnverified) {
			t.Errorf("%s: err = %v, want ErrUnverified", name, err)
		}
	}
}

func TestAnEditThatGoesWrongIsCaughtBeforeAnythingIsWritten(t *testing.T) {
	for name, damage := range map[string]func([]byte) []byte{
		"another source changed": func(b []byte) []byte {
			return []byte(strings.Replace(string(b), "data/people.csv", "data/other.csv", 1))
		},
		"a comment lost and a setting changed": func(b []byte) []byte {
			return []byte(strings.Replace(string(b), "theme = \"light\"", "theme = \"mono\"", 1))
		},
		"the file cut short": func(b []byte) []byte { return b[:len(b)/2] },
	} {
		t.Run(name, func(t *testing.T) {
			path, snap := setup(t, fixture)
			defer config.SetBuilt(damage)()
			_, err := config.Add(path, snap, config.Section{Name: "n", Path: "n.csv"})
			if !errors.Is(err, config.ErrUnverified) {
				t.Errorf("err = %v, want ErrUnverified", err)
			}
			if got := contents(t, path); got != fixture {
				t.Errorf("the file was written anyway: %q", got)
			}
			if names := dirNames(t, filepath.Dir(path)); len(names) != 1 {
				t.Errorf("directory holds %v", names)
			}
		})
	}
}

func TestAMissingFileAndDirectoryAreCreatedOwnerOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "b", "sources.toml")
	snap, err := config.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Add(path, snap, config.Section{Name: "n", Path: "n.csv"}); err != nil {
		t.Fatal(err)
	}
	if got := loadAll(t, path).Sources; len(got) != 1 || got[0].Name != "n" {
		t.Fatalf("sources = %+v", got)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no owner-only modes to check")
	}
	for _, p := range []string{filepath.Join(root, "a"), filepath.Join(root, "a", "b")} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v, want 0700", p, fi, err)
		}
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("file: %v %v, want 0600", fi, err)
	}
}

func TestAnExistingFilesModeIsKept(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no modes to keep")
	}
	path, snap := setup(t, fixture)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Add(path, snap, config.Section{Name: "n", Path: "n"}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", fi.Mode().Perm())
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestAFailedRenameLeavesTheOriginalAndNoTempFile(t *testing.T) {
	path, snap := setup(t, fixture)
	boom := errors.New("injected rename failure")
	defer config.SetRename(func(string, string) error { return boom })()

	_, err := config.Add(path, snap, config.Section{Name: "n", Path: "n.csv"})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the rename failure", err)
	}
	if got := contents(t, path); got != fixture {
		t.Errorf("the original changed: %q", got)
	}
	if names := dirNames(t, filepath.Dir(path)); len(names) != 1 || names[0] != "sources.toml" {
		t.Errorf("directory holds %v, want only sources.toml", names)
	}
}

func TestAFailedFirstWriteLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.toml")
	snap, _ := config.Read(path)
	defer config.SetRename(func(string, string) error { return errors.New("injected") })()

	if _, err := config.Add(path, snap, config.Section{Name: "n", Path: "n.csv"}); err == nil {
		t.Fatal("want an error")
	}
	if names := dirNames(t, dir); len(names) != 0 {
		t.Errorf("directory holds %v, want nothing", names)
	}
}

func TestAReadOnlyDirectoryLeavesTheOriginalAlone(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a platform where a read-only directory refuses new files, and not root")
	}
	path, snap := setup(t, fixture)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)

	if _, err := config.Add(path, snap, config.Section{Name: "n", Path: "n.csv"}); err == nil {
		t.Fatal("want an error")
	}
	os.Chmod(dir, 0o700)
	if got := contents(t, path); got != fixture {
		t.Errorf("the original changed: %q", got)
	}
	if names := dirNames(t, dir); len(names) != 1 {
		t.Errorf("directory holds %v", names)
	}
}

func TestAFileChangedSinceItWasReadIsRefused(t *testing.T) {
	t.Run("edited", func(t *testing.T) {
		path, snap := setup(t, fixture)
		changed := fixture + "# someone else was here\n"
		if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
			t.Fatal(err)
		}
		for name, run := range map[string]func() error{
			"add": func() error { _, err := config.Add(path, snap, config.Section{Name: "n", Path: "n"}); return err },
			"replace": func() error {
				_, err := config.Replace(path, snap, config.Section{Name: "people", Path: "n"})
				return err
			},
			"remove": func() error { _, err := config.Remove(path, snap, "people"); return err },
		} {
			if err := run(); !errors.Is(err, config.ErrStale) {
				t.Errorf("%s: err = %v, want ErrStale", name, err)
			}
		}
		if got := contents(t, path); got != changed {
			t.Errorf("a stale write changed the file: %q", got)
		}
	})
	t.Run("created since a missing file was read", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sources.toml")
		snap, _ := config.Read(path)
		if err := os.WriteFile(path, []byte("mouse = false\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.Add(path, snap, config.Section{Name: "n", Path: "n"}); !errors.Is(err, config.ErrStale) {
			t.Errorf("err = %v, want ErrStale", err)
		}
		if got := contents(t, path); got != "mouse = false\n" {
			t.Errorf("file changed: %q", got)
		}
	})
	t.Run("deleted since it was read", func(t *testing.T) {
		path, snap := setup(t, fixture)
		os.Remove(path)
		if _, err := config.Add(path, snap, config.Section{Name: "n", Path: "n"}); !errors.Is(err, config.ErrStale) {
			t.Errorf("err = %v, want ErrStale", err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the writer recreated a file that was deleted under it: %v", err)
		}
	})
}

func TestTheSnapshotAnOperationReturnsIsTheNextOnesStartingPoint(t *testing.T) {
	path, snap := setup(t, fixture)
	snap2, err := config.Add(path, snap, config.Section{Name: "n1", Path: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Add(path, snap, config.Section{Name: "n2", Path: "2"}); !errors.Is(err, config.ErrStale) {
		t.Errorf("the old snapshot: err = %v, want ErrStale", err)
	}
	if _, err := config.Add(path, snap2, config.Section{Name: "n2", Path: "2"}); err != nil {
		t.Errorf("the returned snapshot: %v", err)
	}
}

func TestAPasswordInAnUnrelatedSourceIsUntouchedAndNeverInAnError(t *testing.T) {
	const secret = "hunter2secret"
	in := "[sources.leaky]\ndsn = \"postgres://admin:" + secret + "@db.example.com/app\"\n"
	path, snap := setup(t, in)

	var errs []error
	collect := func(_ config.Snapshot, err error) { errs = append(errs, err) }
	collect(config.Add(path, snap, config.Section{Name: "leaky", Path: "x"}))
	collect(config.Add(path, snap, config.Section{Name: "bad name", Path: "x"}))
	collect(config.Add(path, snap, config.Section{Name: "n"}))
	collect(config.Replace(path, snap, config.Section{Name: "gone", Path: "x"}))
	collect(config.Remove(path, snap, "gone"))
	collect(config.Replace(path, snap, config.Section{Name: "leaky", Path: "x", DSN: "y"}))
	os.WriteFile(path, []byte(in+"# edited\n"), 0o600)
	collect(config.Add(path, snap, config.Section{Name: "n", Path: "x"}))
	os.WriteFile(path, []byte(in+"bogus = 1\n"), 0o600)
	stale, _ := config.Read(path)
	collect(config.Add(path, stale, config.Section{Name: "n", Path: "x"}))
	errs = append(errs, config.Verify(path, []byte(in), []byte(in+"[sources.n\n"), "add", config.Section{Name: "n", Path: "x"}))

	for i, err := range errs {
		if err == nil {
			t.Errorf("error %d: none", i)
		} else if strings.Contains(err.Error(), secret) {
			t.Errorf("error %d repeats the password: %v", i, err)
		}
	}

	os.WriteFile(path, []byte(in), 0o600)
	snap, _ = config.Read(path)
	if _, err := config.Add(path, snap, config.Section{Name: "n", Path: "n.csv"}); err != nil {
		t.Fatal(err)
	}
	if got := contents(t, path); !strings.HasPrefix(got, in) {
		t.Errorf("the unrelated source changed: %q", got)
	}
}

func TestDroppingTheLastLineOfAFileWithoutALineBreakLeavesNoBlankLine(t *testing.T) {
	path, snap := setup(t, "[sources.a]\npath=\"x\"\nwrite = true")
	if _, err := config.Replace(path, snap, config.Section{Name: "a", Path: "x", MaxRows: 3}); err != nil {
		t.Fatal(err)
	}
	if got, want := contents(t, path), "[sources.a]\npath=\"x\"\nmax_rows = 3\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAByteOrderMarkIsKeptAndDoesNotHideTheFirstLine(t *testing.T) {
	const bom = "\xef\xbb\xbf"
	t.Run("a section on the first line", func(t *testing.T) {
		path, snap := setup(t, bom+"[sources.a]\npath = \"x\"\n")
		snap, err := config.Replace(path, snap, config.Section{Name: "a", Path: "y"})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := contents(t, path), bom+"[sources.a]\npath = \"y\"\n"; got != want {
			t.Fatalf("replace: got %q, want %q", got, want)
		}
		if _, err := config.Remove(path, snap, "a"); err != nil {
			t.Fatal(err)
		}
		if got := contents(t, path); got != bom {
			t.Errorf("remove: got %q, want the byte order mark alone", got)
		}
	})
	t.Run("a comment on the first line that looks like the start of a string", func(t *testing.T) {
		path, snap := setup(t, bom+"# x = \"\"\"\n[sources.a]\npath = \"x\"\n[sources.b]\npath = \"y\"\n")
		if _, err := config.Replace(path, snap, config.Section{Name: "b", Path: "z"}); err != nil {
			t.Fatal(err)
		}
		if got, want := contents(t, path), bom+"# x = \"\"\"\n[sources.a]\npath = \"x\"\n[sources.b]\npath = \"z\"\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestEditingAValueLeavesTheKeyAsWritten(t *testing.T) {
	path, snap := setup(t, "[sources.a]\n\"path\" = 'x'\n")
	if _, err := config.Replace(path, snap, config.Section{Name: "a", Path: "y"}); err != nil {
		t.Fatal(err)
	}
	if got, want := contents(t, path), "[sources.a]\n\"path\" = \"y\"\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnInsertedLineEndsLikeTheLineBeforeIt(t *testing.T) {
	path, snap := setup(t, "[sources.a]\r\npath = \"x\"\n[sources.b]\r\npath = \"q\"\r\n")
	if _, err := config.Replace(path, snap, config.Section{Name: "a", Path: "x", Write: true}); err != nil {
		t.Fatal(err)
	}
	if got, want := contents(t, path), "[sources.a]\r\npath = \"x\"\nwrite = true\n[sources.b]\r\npath = \"q\"\r\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAHandWrittenNameIsNotCalledBadButIsLeftToTheUser(t *testing.T) {
	in := "[sources.\"my db\"]\npath = \"x\"\n"
	path, snap := setup(t, in)
	if _, err := config.Replace(path, snap, config.Section{Name: "my db", Path: "y"}); !errors.Is(err, config.ErrUnsupported) {
		t.Errorf("replace: err = %v, want ErrUnsupported", err)
	}
	if _, err := config.Remove(path, snap, "my db"); !errors.Is(err, config.ErrUnsupported) {
		t.Errorf("remove: err = %v, want ErrUnsupported", err)
	}
	if _, err := config.Add(path, snap, config.Section{Name: "my db", Path: "y"}); !errors.Is(err, config.ErrBadName) {
		t.Errorf("add: err = %v, want ErrBadName", err)
	}
	if got := contents(t, path); got != in {
		t.Errorf("file changed: %q", got)
	}
}

func TestRemovingWhatWasAddedGivesTheFileBack(t *testing.T) {
	in := "[sources.b]\npath = \"q\"\n"
	path, snap := setup(t, in)
	snap, err := config.Add(path, snap, config.Section{Name: "a", Path: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Remove(path, snap, "a"); err != nil {
		t.Fatal(err)
	}
	if got := contents(t, path); got != in {
		t.Errorf("got %q, want %q", got, in)
	}
}

func TestAFileThatDoesNotParseNeverEchoesWhatIsInIt(t *testing.T) {
	const secret = "SECRETPW"
	in := "[sources.a]\ndsn = \"postgres://u:pw@h/d\"\nwrite = " + secret + "\n"
	path, snap := setup(t, in)
	for name, run := range map[string]func() error{
		"add":     func() error { _, err := config.Add(path, snap, config.Section{Name: "n", Path: "n"}); return err },
		"replace": func() error { _, err := config.Replace(path, snap, config.Section{Name: "a", Path: "n"}); return err },
		"remove":  func() error { _, err := config.Remove(path, snap, "a"); return err },
	} {
		err := run()
		if err == nil {
			t.Errorf("%s: accepted a file that does not parse", name)
		} else if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: error repeats what is in the file: %v", name, err)
		}
	}
}

func TestAFileChangedWhileTheNewContentsAreBuiltIsRefused(t *testing.T) {
	path, snap := setup(t, fixture)
	other := fixture + "# changed meanwhile\n"
	defer config.SetBuilt(func(b []byte) []byte {
		if err := os.WriteFile(path, []byte(other), 0o600); err != nil {
			t.Fatal(err)
		}
		return b
	})()

	if _, err := config.Add(path, snap, config.Section{Name: "n", Path: "n"}); !errors.Is(err, config.ErrStale) {
		t.Errorf("err = %v, want ErrStale", err)
	}
	if got := contents(t, path); got != other {
		t.Errorf("the file was overwritten: %q", got)
	}
}

func TestSectionReturnsWhatIsStoredUnexpanded(t *testing.T) {
	_, snap := setup(t, fixture)
	got, err := snap.Section("prod")
	if err != nil {
		t.Fatal(err)
	}
	want := config.Section{
		Name: "prod", DSN: "postgres://reader:${PROD_PASSWORD}@db.example.com/app", MaxRows: 500,
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	app, err := snap.Section("app")
	if err != nil {
		t.Fatal(err)
	}
	if want := (config.Section{Name: "app", Path: "~/db/app.sqlite", Write: true}); app != want {
		t.Errorf("got %+v, want %+v", app, want)
	}
}

func TestSectionOfASourceThatIsNotThereOrNotPlainIsRefused(t *testing.T) {
	_, snap := setup(t, "[sources.a]\npath = \"x\"\n")
	if _, err := snap.Section("nope"); !errors.Is(err, config.ErrNotFound) {
		t.Errorf("missing: err = %v, want ErrNotFound", err)
	}
	_, inline := setup(t, "sources = { a = { path = \"x\" } }\n")
	if _, err := inline.Section("a"); !errors.Is(err, config.ErrUnsupported) {
		t.Errorf("inline table: err = %v, want ErrUnsupported", err)
	}
	_, odd := setup(t, "[sources.\"my db\"]\npath = \"x\"\n")
	if _, err := odd.Section("my db"); !errors.Is(err, config.ErrUnsupported) {
		t.Errorf("hand-written name: err = %v, want ErrUnsupported", err)
	}
}

func TestSectionNeverEchoesAFileThatDoesNotParse(t *testing.T) {
	_, snap := setup(t, "[sources.a]\npath = SECRETPW\n")
	_, err := snap.Section("a")
	if err == nil || strings.Contains(err.Error(), "SECRETPW") {
		t.Errorf("err = %v", err)
	}
}

func TestReplaceAsRenamesInPlaceAndChangesNothingElse(t *testing.T) {
	path, snap := setup(t, fixture)
	_, err := config.ReplaceAs(path, snap, "prod", config.Section{
		Name: "live", DSN: "postgres://reader:${PROD_PASSWORD}@db.example.com/app", MaxRows: 500,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(fixture, "[sources.prod]", "[sources.live]", 1)
	if got := contents(t, path); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if cfg := loadAll(t, path); len(cfg.Sources) != 3 {
		t.Errorf("sources = %+v", cfg.Sources)
	}
}

func TestReplaceAsRenamesAndEditsInOneWrite(t *testing.T) {
	path, snap := setup(t, fixture)
	_, err := config.ReplaceAs(path, snap, "prod", config.Section{
		Name: "live", DSN: "postgres://reader:${PROD_PASSWORD}@db.example.com/app", MaxRows: 900, Write: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(fixture, "[sources.prod]", "[sources.live]", 1)
	want = strings.Replace(want, "max_rows = 500", "max_rows = 900\nwrite = true", 1)
	if got := contents(t, path); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestReplaceAsKeepsTheSpacingAndCommentOfAnOddHeader(t *testing.T) {
	path, snap := setup(t, "[ sources . \"a\" ]  # mine\r\npath = \"x\"\r\n")
	if _, err := config.ReplaceAs(path, snap, "a", config.Section{Name: "b", Path: "x"}); err != nil {
		t.Fatal(err)
	}
	if got, want := contents(t, path), "[ sources . b ]  # mine\r\npath = \"x\"\r\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReplaceAsRefusesAClashAndAMissingSourceAndWritesNothing(t *testing.T) {
	path, snap := setup(t, fixture)
	if _, err := config.ReplaceAs(path, snap, "prod", config.Section{Name: "app", Path: "x"}); !errors.Is(err, config.ErrExists) {
		t.Errorf("clash: err = %v, want ErrExists", err)
	}
	if _, err := config.ReplaceAs(path, snap, "nope", config.Section{Name: "fresh", Path: "x"}); !errors.Is(err, config.ErrNotFound) {
		t.Errorf("missing: err = %v, want ErrNotFound", err)
	}
	if _, err := config.ReplaceAs(path, snap, "prod", config.Section{Name: "bad name", Path: "x"}); !errors.Is(err, config.ErrBadName) {
		t.Errorf("bad name: err = %v, want ErrBadName", err)
	}
	if got := contents(t, path); got != fixture {
		t.Errorf("file changed: %q", got)
	}
}

func TestReplaceAsOfTheSameNameIsAReplace(t *testing.T) {
	path, snap := setup(t, fixture)
	if _, err := config.ReplaceAs(path, snap, "app", config.Section{Name: "app", Path: "~/db/app.sqlite", Write: true}); err != nil {
		t.Fatal(err)
	}
	if got := contents(t, path); got != fixture {
		t.Errorf("file changed: %q", got)
	}
}

func TestReplaceAsOnAStaleFileIsRefused(t *testing.T) {
	path, snap := setup(t, fixture)
	if err := os.WriteFile(path, []byte(fixture+"# more\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.ReplaceAs(path, snap, "prod", config.Section{Name: "live", Path: "x"}); !errors.Is(err, config.ErrStale) {
		t.Errorf("err = %v, want ErrStale", err)
	}
}
