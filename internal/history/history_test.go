package history_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EdwardWhalley/lode/internal/history"
)

func open(t *testing.T, path string) *history.Store {
	t.Helper()
	s, err := history.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func tempPath(t *testing.T) string { return filepath.Join(t.TempDir(), "state", "lode", "history") }

func sqls(es []history.Entry) string {
	var out []string
	for _, e := range es {
		out = append(out, e.SQL)
	}
	return strings.Join(out, "|")
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join("x", "state"))
	got, err := history.DefaultPath()
	if err != nil || got != filepath.Join("x", "state", "lode", "history") {
		t.Errorf("DefaultPath = %q, %v; want it under XDG_STATE_HOME", got, err)
	}

	t.Setenv("XDG_STATE_HOME", "")
	home, _ := os.UserHomeDir()
	got, err = history.DefaultPath()
	if err != nil || got != filepath.Join(home, ".local", "state", "lode", "history") {
		t.Errorf("DefaultPath = %q, %v; want ~/.local/state/lode/history", got, err)
	}
}

func TestEntriesSurviveAReopenInOrderIncludingMultiLineSQL(t *testing.T) {
	path := tempPath(t)
	s := open(t, path)
	queries := []string{"SELECT 1", "SELECT *\nFROM t\nWHERE x = 'a\tb'", "SELECT 'ünï ✓', \"q\""}
	for i, q := range queries {
		if err := s.Add(fmt.Sprintf("src%d", i), q); err != nil {
			t.Fatal(err)
		}
	}

	got := open(t, path).Entries()
	if sqls(got) != strings.Join(queries, "|") {
		t.Errorf("entries = %q, want %q", sqls(got), queries)
	}
	if got[1].Source != "src1" || got[1].Time.IsZero() {
		t.Errorf("entry = %+v, want its Source and a time", got[1])
	}
}

func TestOnlyTheNewest1000EntriesAreKept(t *testing.T) {
	path := tempPath(t)
	s := open(t, path)
	for i := 0; i < history.Max+100; i++ {
		if err := s.Add("s", fmt.Sprintf("SELECT %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(s.Entries()); n != history.Max {
		t.Errorf("held %d entries in the session, want %d", n, history.Max)
	}
	got := open(t, path).Entries()
	if len(got) != history.Max || got[0].SQL != "SELECT 100" || got[len(got)-1].SQL != fmt.Sprintf("SELECT %d", history.Max+99) {
		t.Errorf("after reopen: %d entries from %q to %q; want the newest %d", len(got), got[0].SQL, got[len(got)-1].SQL, history.Max)
	}

	b, _ := os.ReadFile(path)
	if lines := strings.Count(string(b), "\n"); lines != history.Max {
		t.Errorf("file has %d lines after loading, want %d", lines, history.Max)
	}
}

func TestConsecutiveDuplicatesCollapse(t *testing.T) {
	path := tempPath(t)
	s := open(t, path)
	for _, e := range [][2]string{
		{"a", "SELECT 1"}, {"a", "SELECT 1"}, {"a", "SELECT 1"},
		{"b", "SELECT 1"},
		{"a", "SELECT 2"}, {"a", "SELECT 1"},
	} {
		if err := s.Add(e[0], e[1]); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"a:SELECT 1", "b:SELECT 1", "a:SELECT 2", "a:SELECT 1"}
	check := func(label string, es []history.Entry) {
		var got []string
		for _, e := range es {
			got = append(got, e.Source+":"+e.SQL)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: entries = %v, want %v", label, got, want)
		}
	}
	check("in the session", s.Entries())
	check("after reopen", open(t, path).Entries())
}

func TestFileAndDirectoryAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
	path := tempPath(t)
	s := open(t, path)
	if err := s.Add("s", "SELECT secret_password_here"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, filepath.Dir(path)} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o600)
		if info.IsDir() {
			want = 0o700
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want %o: queries can contain secrets", p, info.Mode().Perm(), want)
		}
	}
	for i := 0; i < history.Max+5; i++ {
		s.Add("s", fmt.Sprint(i))
	}
	open(t, path)
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("compacted file mode = %o, want 600", info.Mode().Perm())
	}
}

func TestACorruptLineIsSkippedNotFatal(t *testing.T) {
	path := tempPath(t)
	s := open(t, path)
	s.Add("s", "SELECT 1")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("this is not json\n\n{\"sql\":\n")
	f.Close()
	s.Add("s", "SELECT 2")

	if got := sqls(open(t, path).Entries()); got != "SELECT 1|SELECT 2" {
		t.Errorf("entries = %q, want the two good ones", got)
	}
}

func TestForSourceFiltersAndKeepsOrder(t *testing.T) {
	s := open(t, tempPath(t))
	for _, e := range [][2]string{{"a", "1"}, {"b", "2"}, {"a", "3"}} {
		s.Add(e[0], e[1])
	}
	if got := sqls(s.ForSource("a")); got != "1|3" {
		t.Errorf("ForSource(a) = %q", got)
	}
	if got := s.ForSource("none"); len(got) != 0 {
		t.Errorf("ForSource(none) = %v", got)
	}
}

func TestTwoStoresAppendingToTheSameFileDoNotCorruptIt(t *testing.T) {
	path := tempPath(t)
	a, b := open(t, path), open(t, path)
	long := strings.Repeat("x", 3000)
	var wg sync.WaitGroup
	for _, s := range []*history.Store{a, b} {
		wg.Add(1)
		go func(s *history.Store) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if err := s.Add("s", fmt.Sprintf("%p-%d-%s", s, i, long)); err != nil {
					t.Error(err)
					return
				}
			}
		}(s)
	}
	wg.Wait()
	if n := len(open(t, path).Entries()); n != 200 {
		t.Errorf("a fresh load sees %d entries, want all 200 (a line was torn)", n)
	}
}

func TestOpenFailsWhereTheDirectoryCannotBeMade(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := history.Open(filepath.Join(blocker, "lode", "history")); err == nil {
		t.Error("expected an error when a parent is a file")
	}
}

// writeFile puts text at path, as an earlier version of the program left it.
func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLoadingTakesASecretOutOfTheFileAndKeepsTheEntry(t *testing.T) {
	path := tempPath(t)
	writeFile(t, path, `{"time":"2026-01-02T03:04:05Z","source":"postgres://u@db.example.com/app?password=hunter2","sql":"SELECT 1"}`+"\n"+
		`{"time":"2026-01-02T03:04:06Z","source":"prod","sql":"SELECT 2"}`+"\n")

	s := open(t, path)

	if got := readFile(t, path); strings.Contains(got, "hunter2") {
		t.Errorf("the file still holds the password:\n%s", got)
	}
	const key = "postgres://u@db.example.com/app?…"
	for label, got := range map[string][]history.Entry{"held": s.ForSource(key), "reloaded": open(t, path).ForSource(key)} {
		if len(got) != 1 || got[0].SQL != "SELECT 1" || got[0].Time.Format(time.RFC3339) != "2026-01-02T03:04:05Z" {
			t.Errorf("%s: entries for the masked Source = %+v, want the query and its time", label, got)
		}
	}
	if got := s.ForSource("prod"); len(got) != 1 || got[0].SQL != "SELECT 2" {
		t.Errorf("entries for a named Source = %+v", got)
	}
}

// An old Label of a DSN with no host, path or user had lost its "//", as net/url prints one.
func TestLoadingFindsADSNThatWasRecordedWithoutItsSlashes(t *testing.T) {
	for _, tc := range []struct{ recorded, key string }{
		{"postgres:?password=hunter2", "postgres://?…"},
		{"postgres:#hunter2", "postgres://"},
		{"postgres:", "postgres://"},
	} {
		path := tempPath(t)
		writeFile(t, path, fmt.Sprintf(`{"time":"2026-01-02T03:04:05Z","source":%q,"sql":"SELECT 1"}`+"\n", tc.recorded))

		s := open(t, path)

		if got := readFile(t, path); strings.Contains(got, "hunter2") {
			t.Errorf("%q: the file still holds the secret:\n%s", tc.recorded, got)
		}
		if got := s.ForSource(tc.key); len(got) != 1 || got[0].SQL != "SELECT 1" {
			t.Errorf("%q: entries under %q = %+v, want the query", tc.recorded, tc.key, got)
		}
		if got := open(t, path).ForSource(tc.key); len(got) != 1 {
			t.Errorf("%q: entries under %q after a second load = %+v, want the query", tc.recorded, tc.key, got)
		}
	}
}

func TestLoadingTakesOutASecretThatWasRecordedWhereAPathGoes(t *testing.T) {
	path := tempPath(t)
	writeFile(t, path, `{"time":"2026-01-02T03:04:05Z","source":"postgres://u@h/a;password=hunter2","sql":"SELECT 1"}`+"\n")

	s := open(t, path)

	if got := readFile(t, path); strings.Contains(got, "hunter2") {
		t.Errorf("the file still holds the secret:\n%s", got)
	}
	if got := s.ForSource("(unparseable DSN)"); len(got) != 1 || got[0].SQL != "SELECT 1" {
		t.Errorf("entries under the Label = %+v, want the query", got)
	}
}

func TestLoadingTakesOutEverySecretEvenInAnEntryThatIsDropped(t *testing.T) {
	path := tempPath(t)
	var b strings.Builder
	fmt.Fprintf(&b, `{"source":"postgres://h/d?password=hunter2","sql":"SELECT old"}`+"\n")
	for i := 0; i < history.Max; i++ {
		fmt.Fprintf(&b, `{"source":"s","sql":"SELECT %d"}`+"\n", i)
	}
	writeFile(t, path, b.String())

	open(t, path)

	if strings.Contains(readFile(t, path), "hunter2") {
		t.Error("an entry past the cap kept its password on disk")
	}
}

func TestEntriesThatMaskToTheSameSourceCollapseAsAnyRepeatDoes(t *testing.T) {
	path := tempPath(t)
	writeFile(t, path, `{"source":"postgres://h/d?a=1","sql":"SELECT 1"}`+"\n"+
		`{"source":"postgres://h/d?a=2","sql":"SELECT 1"}`+"\n")

	got := open(t, path).Entries()

	if len(got) != 1 || got[0].Source != "postgres://h/d?…" {
		t.Errorf("entries = %+v, want one, under the masked Source", got)
	}
	if lines := strings.Count(readFile(t, path), "\n"); lines != 1 {
		t.Errorf("file has %d lines, want 1", lines)
	}
}

// Written by hand in a form the program never writes, so a rewrite would show.
func TestAFileWithNothingToChangeIsNotRewritten(t *testing.T) {
	for name, source := range map[string]string{
		"a configured name":           "prod_1",
		"a path":                      `C:\\data\\x.csv`,
		"a path with a query":         "data/x?y.csv",
		"a forward-slash path":        "C:/data/x.csv",
		"a path with a colon":         "dir/with:colon.csv",
		"a masked DSN":                "postgres://u:xxxxx@h/d?…",
		"a masked DSN with no host":   "postgres://?…",
		"a DSN that is only a scheme": "postgres://",
		"a DSN with no query":         "postgres://u@h/d",
		"an unparseable DSN":          "(unparseable DSN)",
	} {
		t.Run(name, func(t *testing.T) {
			path := tempPath(t)
			text := fmt.Sprintf(`{ "sql": "SELECT 1", "source": "%s", "time": "2026-01-02T03:04:05Z" }`+"\n", source)
			writeFile(t, path, text)

			open(t, path)

			if got := readFile(t, path); got != text {
				t.Errorf("the file was rewritten:\n got %s\nwant %s", got, text)
			}
		})
	}
}

func TestAddFailsCleanlyWhenTheFileCannotBeWritten(t *testing.T) {
	path := tempPath(t)
	s := open(t, path)
	if err := os.Mkdir(path, 0o700); err != nil {
		if err := os.Remove(path); err == nil {
			err = os.Mkdir(path, 0o700)
		}
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Skip("could not put a directory in the file's place on this platform")
	}
	if err := s.Add("s", "SELECT 1"); err == nil {
		t.Error("expected an error")
	}
}
