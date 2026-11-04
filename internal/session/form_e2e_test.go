package session_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/config"
	"github.com/EdwardWhalley/lode/internal/session"
	"github.com/EdwardWhalley/lode/internal/tui"
)

type screen struct {
	t *testing.T
	m tui.Model
}

func newScreen(t *testing.T, s *session.Session) *screen {
	t.Helper()
	m := tui.New(nil, "", tui.WithSwitcher(s, tui.Choice{}), tui.WithSelectorAtStart())
	sc := &screen{t: t, m: m}
	sc.send(tea.WindowSizeMsg{Width: 110, Height: 40})
	return sc
}

// send feeds a message to the model and then everything its commands say, as Bubble Tea
// would.
func (sc *screen) send(msg tea.Msg) {
	sc.t.Helper()
	var run func(tea.Msg)
	run = func(msg tea.Msg) {
		switch msg := msg.(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range msg {
				if c != nil {
					run(c())
				}
			}
		default:
			next, cmd := sc.m.Update(msg)
			sc.m = next.(tui.Model)
			if cmd != nil {
				run(cmd())
			}
		}
	}
	run(msg)
}

func (sc *screen) key(k tea.KeyType) { sc.t.Helper(); sc.send(tea.KeyMsg{Type: k}) }

func (sc *screen) typeText(s string) {
	sc.t.Helper()
	for _, r := range s {
		sc.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func (sc *screen) view() string { return ansi.Strip(sc.m.View()) }

func (sc *screen) expect(want string) {
	sc.t.Helper()
	if v := sc.view(); !strings.Contains(v, want) {
		sc.t.Fatalf("the screen should show %q:\n%s", want, v)
	}
}

func (sc *screen) noSecret(secret string) {
	sc.t.Helper()
	if strings.Contains(sc.view(), secret) || strings.Contains(sc.m.View(), secret) {
		sc.t.Fatalf("the password is on the screen:\n%s", sc.view())
	}
}

func TestCreatingASQLiteSourceThroughTheRealFormTestsSavesAndSwitches(t *testing.T) {
	db := makeSQLite(t)
	path := filepath.Join(t.TempDir(), "lode", "sources.toml")
	lookup := os.LookupEnv
	s := session.New(session.Options{
		ConfigPath: path, Lookup: lookup,
		Load: func() (config.Config, error) {
			cfg, err := config.Load(path, lookup)
			if os.IsNotExist(err) {
				return config.Config{}, nil
			}
			return cfg, err
		},
	})
	sc := newScreen(t, s)
	defer func() { sc.m.Close() }()
	sc.expect("no Sources are configured")

	sc.typeText("n")
	sc.expect("New Source: what kind?")
	sc.key(tea.KeyDown)
	sc.key(tea.KeyEnter)
	sc.expect("New Source: SQLite")
	if strings.Contains(sc.view(), "Write ") && strings.Contains(sc.view(), "[ ]") {
	} else {
		t.Fatalf("a SQLite form has a write box:\n%s", sc.view())
	}
	sc.typeText("mine")
	sc.key(tea.KeyTab)
	sc.typeText(db)
	sc.key(tea.KeyEnter)
	sc.key(tea.KeyEnter)
	sc.key(tea.KeyEnter)
	sc.expect("saved")
	sc.expect("enter switches")

	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "[sources.mine]") {
		t.Fatalf("sources.toml = %q, %v", data, err)
	}
	sc.key(tea.KeyEnter)
	sc.expect(filepath.Base(db))
	if strings.Contains(sc.view(), "no Source open") {
		t.Errorf("the new Source should be open:\n%s", sc.view())
	}
}

func TestCreatingAServerSourceThatCannotBeReachedKeepsThePasswordOffTheScreenAndOutOfTheFile(t *testing.T) {
	const pw = "hunter2-sekrit"
	t.Setenv("LODE_E2E_PW", pw)
	path := filepath.Join(t.TempDir(), "sources.toml")
	lookup := os.LookupEnv
	s := session.New(session.Options{
		ConfigPath: path, Lookup: lookup,
		Load: func() (config.Config, error) {
			cfg, err := config.Load(path, lookup)
			if os.IsNotExist(err) {
				return config.Config{}, nil
			}
			return cfg, err
		},
	})
	sc := newScreen(t, s)
	sc.typeText("n")
	sc.key(tea.KeyDown)
	sc.key(tea.KeyDown)
	sc.key(tea.KeyEnter)
	sc.expect("New Source: PostgreSQL")
	sc.expect("5432")

	sc.typeText("prod")
	sc.key(tea.KeyTab)
	sc.typeText("127.0.0.1")
	sc.key(tea.KeyTab)
	for range "5432" {
		sc.key(tea.KeyBackspace)
	}
	sc.typeText("1")
	sc.key(tea.KeyTab)
	sc.typeText("d")
	sc.key(tea.KeyTab)
	sc.typeText("u")
	sc.key(tea.KeyTab)
	sc.typeText("LODE_E2E_PW")
	sc.key(tea.KeyTab)
	sc.typeText("sslmode=disable")
	sc.key(tea.KeyEnter)
	sc.key(tea.KeyEnter)
	sc.key(tea.KeyEnter)
	sc.expect("the test failed")
	sc.expect("ctrl+s saves it anyway")
	sc.noSecret(pw)
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a failed test must not write the file")
	}

	sc.key(tea.KeyCtrlS)
	sc.expect("saved")
	sc.noSecret(pw)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "${LODE_E2E_PW}") || strings.Contains(string(data), pw) {
		t.Errorf("sources.toml should hold the variable and not its value:\n%s", data)
	}
	cfg, err := config.Load(path, lookup)
	if err != nil || len(cfg.Sources) != 1 || cfg.Sources[0].Name != "prod" {
		t.Errorf("the section should load: %v %+v", err, cfg.Sources)
	}
}

func TestAnUnsetPasswordVariableCannotBeSavedThroughTheRealForm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.toml")
	s := session.New(session.Options{
		ConfigPath: path, Lookup: func(string) (string, bool) { return "", false },
		Load: func() (config.Config, error) { return config.Config{}, nil },
	})
	sc := newScreen(t, s)
	sc.typeText("n")
	sc.key(tea.KeyDown)
	sc.key(tea.KeyDown)
	sc.key(tea.KeyEnter)
	sc.typeText("prod")
	sc.key(tea.KeyTab)
	sc.typeText("127.0.0.1")
	for i := 0; i < 3; i++ {
		sc.key(tea.KeyTab)
	}
	sc.key(tea.KeyTab)
	sc.typeText("NOT_SET_ANYWHERE")
	for i := 0; i < 3; i++ {
		sc.key(tea.KeyEnter)
	}
	sc.key(tea.KeyEnter)
	sc.expect("not set")
	if strings.Contains(sc.view(), "anyway") {
		t.Errorf("save anyway must not be offered:\n%s", sc.view())
	}
	sc.key(tea.KeyCtrlS)
	if _, err := os.Stat(path); err == nil {
		t.Error("ctrl+s wrote the file")
	}
	if strings.Count(sc.view(), "NOT_SET_ANYWHERE") > 1 {
		t.Errorf("the name is repeated outside its field:\n%s", sc.view())
	}
}
