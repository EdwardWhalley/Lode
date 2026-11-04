package guard_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot finds the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above " + dir)
		}
		dir = parent
	}
}

// files returns every file under the repository that keep accepts, relative to the root
// with forward slashes.
func files(t *testing.T, keep func(rel string) bool) []string {
	t.Helper()
	root := repoRoot(t)
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if keep(rel) {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func read(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func isTestFile(rel string) bool { return strings.HasSuffix(rel, "_test.go") }

// hugeNumber finds a number of nine or more digits in a test, usually a query asking an
// engine for an enormous result. DuckDB holds a whole result in memory, so one once crashed
// a 16 GB machine.
var hugeNumber = regexp.MustCompile(`\b[0-9]{9,}\b`)

func TestLargeNumbersInTestsSayWhyTheyAreBounded(t *testing.T) {
	for _, rel := range files(t, isTestFile) {
		lines := strings.Split(string(read(t, rel)), "\n")
		for i, line := range lines {
			if !hugeNumber.MatchString(line) {
				continue
			}
			previous := ""
			if i > 0 {
				previous = lines[i-1]
			}
			if !strings.Contains(line, "bounded:") && !strings.Contains(previous, "bounded:") {
				t.Errorf("%s:%d has a number of 9+ digits with no `bounded:` comment explaining why it cannot exhaust memory:\n\t%s",
					rel, i+1, strings.TrimSpace(line))
			}
		}
	}
}

func TestNoByteOrderMarks(t *testing.T) {
	text := map[string]bool{".go": true, ".md": true, ".yml": true, ".yaml": true, ".toml": true, ".sh": true, ".mod": true}
	bom := []byte{0xEF, 0xBB, 0xBF}
	for _, rel := range files(t, func(rel string) bool {
		return text[filepath.Ext(rel)] || strings.HasPrefix(rel, ".githooks/") || rel == ".gitattributes"
	}) {
		if bytes.HasPrefix(read(t, rel), bom) {
			t.Errorf("%s starts with a UTF-8 byte-order mark; save it without one", rel)
		}
	}
}

func TestNoTabsInMarkdownProse(t *testing.T) {
	for _, rel := range files(t, func(rel string) bool { return strings.HasSuffix(rel, ".md") }) {
		fenced := false
		for n, line := range strings.Split(string(read(t, rel)), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				fenced = !fenced
				continue
			}
			if !fenced && strings.Contains(line, "\t") {
				t.Errorf("%s:%d has a tab outside a code fence; a script probably turned a \\t into one", rel, n+1)
			}
		}
	}
}

// mutationMarker makes a forgotten mutation check findable; it is built from pieces so this
// file does not contain it.
var mutationMarker = "MUTA" + "TION"

func TestNoMutationMarkersLeftBehind(t *testing.T) {
	for _, rel := range files(t, func(rel string) bool {
		return (strings.HasSuffix(rel, ".go") && !strings.HasPrefix(rel, "internal/guard/")) ||
			strings.HasPrefix(rel, ".github/workflows/")
	}) {
		if bytes.Contains(read(t, rel), []byte(mutationMarker)) {
			t.Errorf("%s contains %q: a mutation check was left in place", rel, mutationMarker)
		}
	}
}

func TestOnlyTheKindsListImportsAdapterPackages(t *testing.T) {
	adapterPackage := regexp.MustCompile(`"github\.com/[^"]+/internal/adapter/[^"]+"`)
	for _, rel := range files(t, func(rel string) bool {
		return strings.HasPrefix(rel, "internal/source/") && strings.HasSuffix(rel, ".go") && !isTestFile(rel)
	}) {
		if rel == "internal/source/kinds.go" {
			continue
		}
		if m := adapterPackage.Find(read(t, rel)); m != nil {
			t.Errorf("%s imports %s; Adapters are named only in internal/source/kinds.go", rel, m)
		}
	}
}

func TestNothingImportsTheBubblesTextarea(t *testing.T) {
	textarea := []byte(`"github.com/charmbracelet/bubbles/textarea"`)
	for _, rel := range files(t, func(rel string) bool {
		return strings.HasSuffix(rel, ".go") && !strings.HasPrefix(rel, "internal/guard/")
	}) {
		if bytes.Contains(read(t, rel), textarea) {
			t.Errorf("%s imports bubbles/textarea; use internal/editor", rel)
		}
	}
}

func TestTheEditorImportsNoClipboardFilesOrProcesses(t *testing.T) {
	forbidden := []string{
		`"os"`, `"os/exec"`, `"github.com/atotto/clipboard"`, `"github.com/aymanbagabas/go-osc52/v2"`,
	}
	for _, rel := range files(t, func(rel string) bool {
		return strings.HasPrefix(rel, "internal/editor/") && strings.HasSuffix(rel, ".go") && !isTestFile(rel)
	}) {
		src := read(t, rel)
		for _, imp := range forbidden {
			if bytes.Contains(src, []byte(imp)) {
				t.Errorf("%s imports %s; the editor leaves that to the TUI", rel, imp)
			}
		}
	}
}

func TestSourcetextImportsNothingOfOurs(t *testing.T) {
	ours := regexp.MustCompile(`"github\.com/[^"]+/(internal|cmd)/[^"]*"`)
	for _, rel := range files(t, func(rel string) bool {
		return strings.HasPrefix(rel, "internal/sourcetext/") && strings.HasSuffix(rel, ".go") && !isTestFile(rel)
	}) {
		if m := ours.Find(read(t, rel)); m != nil {
			t.Errorf("%s imports %s; sourcetext must stay a leaf", rel, m)
		}
	}
}

func TestSqltextImportsNothingOfOurs(t *testing.T) {
	ours := regexp.MustCompile(`"github\.com/[^"]+/(internal|cmd)/[^"]*"`)
	for _, rel := range files(t, func(rel string) bool {
		return strings.HasPrefix(rel, "internal/sqltext/") && strings.HasSuffix(rel, ".go") && !isTestFile(rel)
	}) {
		if m := ours.Find(read(t, rel)); m != nil {
			t.Errorf("%s imports %s; sqltext must stay a leaf", rel, m)
		}
	}
}

func TestHookScriptsUseUnixLineEndings(t *testing.T) {
	for _, rel := range files(t, func(rel string) bool { return strings.HasPrefix(rel, ".githooks/") }) {
		if bytes.Contains(read(t, rel), []byte("\r")) {
			t.Errorf("%s contains a carriage return; hook scripts must use LF line endings", rel)
		}
	}
}

func TestNoColourIsNamedInTheTUIOutsideTheThemeFile(t *testing.T) {
	colour := regexp.MustCompile(`lipgloss\.(Color|AdaptiveColor|CompleteColor)\b|\.(Foreground|Background)\([^)]|\x1b\[|\033\[`)
	for _, rel := range files(t, func(rel string) bool {
		return strings.HasPrefix(rel, "internal/tui/") && strings.HasSuffix(rel, ".go") && !isTestFile(rel)
	}) {
		if rel == "internal/tui/theme.go" {
			continue
		}
		if m := colour.Find(read(t, rel)); m != nil {
			t.Errorf("%s names a colour (%s); colours belong to a theme (internal/theme, applied in internal/tui/theme.go)", rel, m)
		}
	}
}
