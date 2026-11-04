package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/EdwardWhalley/lode/internal/config"
	"github.com/EdwardWhalley/lode/internal/session"
	"github.com/EdwardWhalley/lode/internal/source"
	"github.com/EdwardWhalley/lode/internal/tui"
)

func writeConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sources.toml")
	toml := "[sources.demo]\npath = \"demo.sqlite\"\n\n[sources.people]\npath = \"people.csv\"\n"
	if err := os.WriteFile(path, []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOnlyAColourlessTerminalThatCanDrawAttributesGetsThem(t *testing.T) {
	for _, tt := range []struct {
		name    string
		profile termenv.Profile
		term    string
		want    bool
	}{
		{"NO_COLOR, or a terminal with no colour", termenv.Ascii, "xterm-256color", true},
		{"no TERM at all, as on Windows", termenv.Ascii, "", true},
		{"a real dumb terminal", termenv.Ascii, "dumb", false},
		{"a colour terminal", termenv.ANSI256, "xterm-256color", false},
		{"a true colour terminal", termenv.TrueColor, "xterm-256color", false},
	} {
		if got := attributesWithoutColour(tt.profile, tt.term); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestMistypedSourceNameListsTheConfiguredOnes(t *testing.T) {
	err := run([]string{"--config", writeConfig(t), "demo2"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{`"demo2"`, "demo, people"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}
}

func TestFlagsMayFollowTheSource(t *testing.T) {
	csv := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(csv, []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"--config", writeConfig(t), csv, "--write"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "writes are not supported for file Sources") {
		t.Errorf("err = %v, want the file-Source write refusal (so --write was parsed after the source)", err)
	}
}

func TestMaxRowsFlagIsValidatedBeforeAnythingOpens(t *testing.T) {
	csv := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(csv, []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"0", "-3", "5000000"} {
		err := run([]string{"--config", writeConfig(t), "--max-rows", arg, csv}, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "--max-rows") {
			t.Errorf("--max-rows %s: err = %v, want a --max-rows rejection", arg, err)
		}
	}
}

// startView draws the Model run would start with, on a terminal of 100x30.
func startView(t *testing.T, configPath string, homeOff bool, positional ...string) string {
	t.Helper()
	cfg, err := loadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var s source.Source
	if len(positional) == 1 {
		s = source.Resolve(positional[0], cfg.Sources)
	}
	sess := session.New(session.Options{
		Load:   func() (config.Config, error) { return loadConfig(configPath) },
		Config: cfg,
		Launch: s,
	})
	m, opened, err := startModel(sess, nil, homeOff)
	if err != nil {
		t.Fatal(err)
	}
	if opened != nil {
		defer opened.Adapter.Close()
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return ansi.Strip(updated.(tui.Model).View())
}

func TestNoArgumentStartsOnTheHomePage(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "sources.toml")
	if err := os.WriteFile(empty, []byte("max_rows = 100\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"with Sources": writeConfig(t), "with none": empty} {
		view := startView(t, path, false)
		if !strings.Contains(strings.ToLower(view), "press any key") || strings.Contains(view, "Choose a Source") {
			t.Errorf("%s: want the home page:\n%s", name, view)
		}
	}
}

func TestASourceGivenStartsWithoutTheHomePage(t *testing.T) {
	csv := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(csv, []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	view := startView(t, writeConfig(t), false, csv)
	if strings.Contains(strings.ToLower(view), "press any key") {
		t.Errorf("a file on the command line starts on the editor:\n%s", view)
	}
}

func TestWithTheHomePageSkippedNoArgumentStartsOnTheSelector(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "sources.toml")
	if err := os.WriteFile(empty, []byte("max_rows = 100\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{writeConfig(t): "demo", empty: "no Sources are configured"} {
		view := startView(t, name, true)
		if strings.Contains(strings.ToLower(view), "press any key") || !strings.Contains(view, "Choose a Source") || !strings.Contains(view, want) {
			t.Errorf("want the selector showing %q:\n%s", want, view)
		}
	}
}

func TestTheHomePageIsSkippedByTheSettingOrTheFlag(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     config.Config
		noHome  bool
		wantOff bool
	}{
		{"default", config.Config{}, false, false},
		{"home = false", config.Config{HomeOff: true}, false, true},
		{"--no-home", config.Config{}, true, true},
		{"--no-home wins over home = true", config.Config{HomeOff: false}, true, true},
		{"both", config.Config{HomeOff: true}, true, true},
	} {
		if got := homeSkipped(tc.cfg, tc.noHome); got != tc.wantOff {
			t.Errorf("%s: skipped = %v, want %v", tc.name, got, tc.wantOff)
		}
	}
}

func TestNoHomeIsAFlagAndMayFollowTheSource(t *testing.T) {
	var out strings.Builder
	if err := run([]string{"--no-home", "--version"}, &out, io.Discard); err != nil || !strings.Contains(out.String(), "lode") {
		t.Errorf("--no-home was not accepted: %v %q", err, out.String())
	}
}

func TestUsageListsNoHome(t *testing.T) {
	if !strings.Contains(usage, "--no-home") {
		t.Error("lode --help does not list --no-home")
	}
}

func TestVersionFlagPrintsTheVersionAndExits(t *testing.T) {
	var out strings.Builder
	if err := run([]string{"--version"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "lode "+version+"\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestVersionDefaultsToDevAndCanBeStamped(t *testing.T) {
	if version != "dev" {
		t.Skipf("built with a stamped version (%s)", version)
	}
	var out strings.Builder
	if err := run([]string{"--version"}, &out, io.Discard); err != nil || out.String() != "lode dev\n" {
		t.Errorf("got %q, %v", out.String(), err)
	}
}

func TestMoreThanOneSourceIsAUsageError(t *testing.T) {
	if err := run([]string{"--config", writeConfig(t), "a.csv", "b.csv"}, io.Discard, io.Discard); err != errUsage {
		t.Errorf("err = %v, want errUsage", err)
	}
}

func TestMissingExplicitConfigIsAnError(t *testing.T) {
	err := run([]string{"--config", filepath.Join(t.TempDir(), "nope.toml"), "x.csv"}, io.Discard, io.Discard)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want one satisfying os.ErrNotExist", err)
	}
}

func TestAnUnknownThemeFlagIsReportedBeforeAnythingOpens(t *testing.T) {
	csv := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(csv, []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"--config", writeConfig(t), "--theme", "neon", csv}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--theme") || !strings.Contains(err.Error(), `"neon"`) || !strings.Contains(err.Error(), "bubbletea") {
		t.Errorf("err = %v, want a --theme error naming the bad theme and the real ones", err)
	}
}

func TestAnUnknownThemeInTheConfigIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.toml")
	if err := os.WriteFile(path, []byte("theme = \"neon\"\n[sources.p]\npath = \"a.csv\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"--config", path, "p"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "theme") || !strings.Contains(err.Error(), `"neon"`) {
		t.Errorf("err = %v", err)
	}
}

func TestAnEmptyThemeFlagIsReportedNotIgnored(t *testing.T) {
	csv := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(csv, []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "  "} {
		err := run([]string{"--config", writeConfig(t), "--theme", value, csv}, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "--theme") || !strings.Contains(err.Error(), "bubbletea") {
			t.Errorf("--theme %q: err = %v, want a --theme error listing the available themes", value, err)
		}
	}
}

func TestAThemeFlagStillWinsOverTheConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.toml")
	if err := os.WriteFile(path, []byte("theme = \"neon\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"--config", path, "--theme", "classic", "x.csv"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), `"neon"`) {
		t.Errorf("err = %v, want the config's bad theme reported", err)
	}
}

func TestMouseCaptureIsRequestedOnlyWhenNothingTurnsItOff(t *testing.T) {
	withMouse := len(programOptions(config.Config{}, false))
	for _, tc := range []struct {
		name      string
		cfg       config.Config
		noMouse   bool
		wantMouse bool
	}{
		{"default", config.Config{}, false, true},
		{"mouse = false", config.Config{MouseOff: true}, false, false},
		{"--no-mouse", config.Config{}, true, false},
		{"--no-mouse wins over mouse = true", config.Config{MouseOff: false}, true, false},
		{"both", config.Config{MouseOff: true}, true, false},
	} {
		got := len(programOptions(tc.cfg, tc.noMouse)) == withMouse
		if got != tc.wantMouse {
			t.Errorf("%s: capture requested = %v, want %v", tc.name, got, tc.wantMouse)
		}
	}
	if withMouse != len(programOptions(config.Config{MouseOff: true}, false))+1 {
		t.Errorf("turning the mouse off should remove exactly one option (capture)")
	}
}

func TestNoMouseIsAFlagAndMayFollowTheSource(t *testing.T) {
	var out strings.Builder
	if err := run([]string{"--no-mouse", "--version"}, &out, io.Discard); err != nil || !strings.Contains(out.String(), "lode") {
		t.Errorf("--no-mouse was not accepted: %v %q", err, out.String())
	}
}
