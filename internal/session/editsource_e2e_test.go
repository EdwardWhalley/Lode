package session_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/config"
	"github.com/EdwardWhalley/lode/internal/session"
	"github.com/EdwardWhalley/lode/internal/source"
)

// e2eFile is read in the selector's order; nothing listens on port 1, so a test of a server
// Source fails fast.
const e2eFile = `# my databases

[sources.a_prod]  # primary
dsn = "postgres://reader:${E2E_PASSWORD}@127.0.0.1:1/app?sslmode=require"
max_rows = 500

# the shop
[sources.b_shop]
dsn = "mysql://root:${E2E_PASSWORD}@127.0.0.1:1/shop?tls=skip-verify"
write = true

[sources.c_legacy]
dsn = "postgres://admin:hunter2-sekrit@127.0.0.1:1/old?sslmode=disable"

[sources.d_file]
path = "x.csv"
`

func e2eScreen(t *testing.T, content string) (*screen, string) {
	t.Helper()
	t.Setenv("E2E_PASSWORD", "pw-from-env")
	path := filepath.Join(t.TempDir(), "lode", "sources.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s := session.New(session.Options{
		ConfigPath: path, Lookup: os.LookupEnv,
		Load: func() (config.Config, error) { return config.Load(path, os.LookupEnv) },
	})
	sc := newScreen(t, s)
	return sc, path
}

func fileText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// down moves the selector n Sources down.
func (sc *screen) down(n int) {
	for range n {
		sc.key(tea.KeyDown)
	}
}

// enters presses enter n times: through the form's rows, the last one testing.
func (sc *screen) enters(n int) {
	for range n {
		sc.key(tea.KeyEnter)
	}
}

// name, host, port, database, user, password variable, options, write, max rows
const serverRows = 9

func TestEditingAPostgresSourceShowsItsFieldsAndAnUnchangedSaveLeavesTheFileByteIdentical(t *testing.T) {
	sc, path := e2eScreen(t, e2eFile)
	sc.expect("a_prod")
	sc.typeText("e")
	sc.expect("Edit Source: a_prod")
	for _, want := range []string{"127.0.0.1", "app", "reader", "E2E_PASSWORD", "sslmode=require", "500"} {
		sc.expect(want)
	}
	sc.noSecret("pw-from-env")
	sc.enters(serverRows)
	sc.expect("ctrl+s saves it anyway")
	sc.noSecret("pw-from-env")
	sc.key(tea.KeyCtrlS)
	sc.expect("saved")
	if got := fileText(t, path); got != e2eFile {
		t.Errorf("an unchanged save changed the file:\n%s", got)
	}
}

func TestEditingAHandWrittenMySQLSourceRoundTripsTheSameWay(t *testing.T) {
	sc, path := e2eScreen(t, e2eFile)
	sc.down(1)
	sc.typeText("e")
	sc.expect("Edit Source: b_shop")
	sc.expect("tls=skip-verify")
	sc.enters(serverRows)
	sc.key(tea.KeyCtrlS)
	sc.expect("saved")
	if got := fileText(t, path); got != e2eFile {
		t.Errorf("an unchanged save changed the file:\n%s", got)
	}
}

func TestALiteralPasswordIsNeverOnTheScreenAndAnUnchangedSaveKeepsIt(t *testing.T) {
	sc, path := e2eScreen(t, e2eFile)
	sc.down(2)
	sc.typeText("e")
	sc.expect("Edit Source: c_legacy")
	sc.expect("password is stored")
	sc.noSecret("hunter2-sekrit")
	sc.enters(serverRows)
	sc.expect("ctrl+s saves it anyway")
	sc.noSecret("hunter2-sekrit")
	sc.key(tea.KeyCtrlS)
	sc.expect("saved")
	sc.noSecret("hunter2-sekrit")
	if got := fileText(t, path); got != e2eFile {
		t.Errorf("an unchanged save changed the file:\n%s", got)
	}
}

func TestNamingAVariableForALiteralPasswordReplacesItInTheFile(t *testing.T) {
	t.Setenv("E2E_NEW", "x")
	sc, path := e2eScreen(t, e2eFile)
	sc.down(2)
	sc.typeText("e")
	for range 5 {
		sc.key(tea.KeyTab)
	}
	sc.typeText("E2E_NEW")
	sc.enters(serverRows - 5)
	sc.expect("ctrl+s saves it anyway")
	sc.key(tea.KeyCtrlS)
	sc.expect("saved")
	want := strings.Replace(e2eFile, "admin:hunter2-sekrit@", "admin:${E2E_NEW}@", 1)
	if got := fileText(t, path); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	sc.noSecret("hunter2-sekrit")
}

func TestRenamingASourceInTheRealFormMovesOnlyItsHeader(t *testing.T) {
	sc, path := e2eScreen(t, e2eFile)
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "x.csv"), []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sc.down(3)
	sc.typeText("e")
	sc.expect("Edit Source: d_file")
	sc.typeText("2")
	sc.enters(3)
	sc.expect("saved")
	want := strings.Replace(e2eFile, "[sources.d_file]", "[sources.d_file2]", 1)
	if got := fileText(t, path); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestRenamingOverAnotherSourceIsRefusedInTheRealForm(t *testing.T) {
	sc, path := e2eScreen(t, e2eFile)
	sc.down(3)
	sc.typeText("e")
	for range len("d_file") {
		sc.key(tea.KeyBackspace)
	}
	sc.typeText("a_prod")
	sc.key(tea.KeyTab)
	sc.expect("already exists")
	sc.enters(4)
	if got := fileText(t, path); got != e2eFile {
		t.Errorf("the file changed:\n%s", got)
	}
}

func TestALocationTheKindCannotPlaceIsEditableAsOneRawField(t *testing.T) {
	const odd = "[sources.odd]\ndsn = \"postgres://127.0.0.1:notaport/d\"\n"
	sc, path := e2eScreen(t, odd)
	sc.typeText("e")
	sc.expect("Location")
	sc.expect("postgres://127.0.0.1:notaport/d")
	sc.key(tea.KeyTab)
	for range len("notaport/d") {
		sc.key(tea.KeyBackspace)
	}
	sc.typeText("1/d")
	sc.enters(3)
	sc.key(tea.KeyCtrlS)
	sc.expect("saved")
	if got, want := fileText(t, path), strings.Replace(odd, "notaport/d", "1/d", 1); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestARawLocationWithAPasswordIsNeverShownAndIsKept(t *testing.T) {
	const odd = "[sources.odd]\ndsn = \"postgres://u:hunter2-sekrit@127.0.0.1:notaport/d\"\n"
	sc, path := e2eScreen(t, odd)
	sc.typeText("e")
	sc.expect("Edit Source: odd")
	sc.expect("kept")
	sc.noSecret("hunter2-sekrit")
	sc.noSecret("notaport")
	sc.enters(3)
	sc.key(tea.KeyCtrlS)
	sc.expect("saved")
	sc.noSecret("hunter2-sekrit")
	if got := fileText(t, path); got != odd {
		t.Errorf("an unchanged save changed the file:\n%s", got)
	}
}

func TestDeletingASourceAsksFirstAndRemovesOnlyItsSection(t *testing.T) {
	sc, path := e2eScreen(t, e2eFile)
	sc.down(1)
	sc.typeText("d")
	sc.expect(`Delete the Source "b_shop"`)
	sc.key(tea.KeyEsc)
	sc.expect("Choose a Source")
	if got := fileText(t, path); got != e2eFile {
		t.Fatalf("esc deleted something:\n%s", got)
	}

	sc.down(1)
	sc.typeText("d")
	sc.expect(`Delete the Source "b_shop"`)
	sc.key(tea.KeyEnter)
	sc.expect("deleted")
	want := strings.Replace(e2eFile, "# the shop\n[sources.b_shop]\ndsn = \"mysql://root:${E2E_PASSWORD}@127.0.0.1:1/shop?tls=skip-verify\"\nwrite = true\n\n", "", 1)
	if got := fileText(t, path); got != want || want == e2eFile {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(sc.view(), "b_shop") && !strings.Contains(sc.view(), "deleted") {
		t.Errorf("the removed Source is still listed:\n%s", sc.view())
	}
	sc.noSecret("hunter2-sekrit")
}

func TestAFileChangedByAnotherProgramKeepsTheConfirmationOpenWithItsMessage(t *testing.T) {
	sc, path := e2eScreen(t, e2eFile)
	sc.typeText("d")
	other := e2eFile + "\n[sources.z_new]\npath = \"y.csv\"\n"
	if err := os.WriteFile(path, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	sc.key(tea.KeyEnter)
	sc.expect("changed on disk")
	sc.expect(`Delete the Source "a_prod"`)
	if got := fileText(t, path); got != other {
		t.Fatalf("a refused delete wrote anyway:\n%s", got)
	}
	sc.key(tea.KeyEnter)
	sc.expect("deleted")
	if got := fileText(t, path); strings.Contains(got, "a_prod") || !strings.Contains(got, "z_new") {
		t.Errorf("file:\n%s", got)
	}
}

func TestAnAdHocSourceCannotBeEditedOrDeleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.toml")
	if err := os.WriteFile(path, []byte("[sources.a]\npath = \"x.csv\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	launch := filepath.Join(t.TempDir(), "adhoc.csv")
	if err := os.WriteFile(launch, []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := session.New(session.Options{
		ConfigPath: path, Lookup: os.LookupEnv, Launch: source.Source{Path: launch},
		Load: func() (config.Config, error) { return config.Load(path, os.LookupEnv) },
	})
	sc := newScreen(t, s)
	sc.expect("(ad-hoc)")
	sc.typeText("e")
	sc.expect("not in sources.toml")
	sc.typeText("d")
	sc.expect("not in sources.toml")
	sc.expect("Choose a Source")
}
