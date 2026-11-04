package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestReadmeKeyTableMatchesTheRegistry(t *testing.T) {
	readme := readmeKeyTable(t)

	registry := map[string]string{}
	for _, b := range bindings {
		registry[b.keys] = b.desc
	}
	for keys, desc := range registry {
		got, ok := readme[keys]
		if !ok {
			t.Errorf("README key table has no row for %q (registry: %q)", keys, desc)
		} else if got != desc {
			t.Errorf("README says %q for %q, the registry says %q", got, keys, desc)
		}
	}
	for keys := range readme {
		if _, ok := registry[keys]; !ok {
			t.Errorf("README key table has a row for %q that is not in the key registry", keys)
		}
	}
}

func readmeText(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above " + dir)
		}
		dir = parent
	}
	raw, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// readmeKeyTable returns the README's key table as keys → description, with the
// backticks removed.
func readmeKeyTable(t *testing.T) map[string]string {
	t.Helper()
	raw := readmeText(t)

	rows := map[string]string{}
	inTable := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "| Key | Action |"):
			inTable = true
		case inTable && strings.HasPrefix(line, "| ---"):
		case inTable && strings.HasPrefix(line, "|"):
			cells := strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(line, "| "), " |"), " | ", 2)
			if len(cells) != 2 {
				t.Fatalf("README key table row %q does not have two cells", line)
			}
			rows[strings.ReplaceAll(cells[0], "`", "")] = strings.ReplaceAll(cells[1], "`", "")
		case inTable:
			return rows
		}
	}
	if !inTable {
		t.Fatal("README has no key table starting `| Key | Action |`")
	}
	return rows
}

var f1 = tea.KeyMsg{Type: tea.KeyF1}

func TestHintRowFollowsTheFocusedPane(t *testing.T) {
	m := newBare(t, 100, 24)
	last := func(m Model) string { l := lines(m.View()); return ansi.Strip(l[len(l)-1]) }

	if got := last(m); !strings.Contains(got, "ctrl+r run") || !strings.Contains(got, "F1 help") {
		t.Errorf("editor hints = %q", got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got := last(m); !strings.Contains(got, "c copy") || strings.Contains(got, "expand") {
		t.Errorf("grid hints = %q", got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got := last(m); !strings.Contains(got, "enter expand") {
		t.Errorf("schema hints = %q", got)
	}
	m, _ = send(t, m, f1)
	if got := last(m); !strings.Contains(got, "F1/esc close") || strings.Contains(got, "ctrl+r") {
		t.Errorf("help hints = %q", got)
	}
}

func TestHintRowNeverWrapsOrCutsAHintInHalf(t *testing.T) {
	var whole []string
	for _, b := range bindings {
		for _, h := range b.hints {
			whole = append(whole, h.key+" "+h.text)
		}
	}
	for _, s := range []scope{scopeEditor, scopeGrid, scopeSchema, scopeHelp, scopeValue} {
		for width := 1; width <= 120; width++ {
			row := ansi.Strip(hintRow(s, width))
			if w := ansi.StringWidth(row); w > width {
				t.Fatalf("scope %d width %d: row is %d columns: %q", s, width, w, row)
			}
			if strings.Contains(row, "\n") {
				t.Fatalf("scope %d width %d: row wraps: %q", s, width, row)
			}
			for _, part := range strings.Split(row, " · ") {
				if part == "" {
					continue
				}
				found := false
				for _, w := range whole {
					found = found || part == w
				}
				if !found {
					t.Fatalf("scope %d width %d: %q is not a whole hint (row %q)", s, width, part, row)
				}
			}
		}
	}
}

func TestHelpListsEveryBindingAndClosesOnF1AndEsc(t *testing.T) {
	m := newBare(t, 100, 50)
	m, _ = send(t, m, f1)
	if m.overlay == nil {
		t.Fatal("F1 did not open the help overlay")
	}
	view := ansi.Strip(m.View())
	for _, b := range bindings {
		if !strings.Contains(view, b.keys) || !strings.Contains(view, b.desc) {
			t.Errorf("help is missing %q / %q:\n%s", b.keys, b.desc, view)
		}
	}
	for _, group := range []string{groupGlobal, groupPanes} {
		if !strings.Contains(view, group) {
			t.Errorf("help is missing the group %q", group)
		}
	}

	for _, closer := range []tea.KeyMsg{f1, {Type: tea.KeyEsc}} {
		m = newBare(t, 100, 40)
		m, _ = send(t, m, f1)
		m, _ = send(t, m, closer)
		if m.overlay != nil {
			t.Errorf("%v did not close the help overlay", closer)
		}
	}
}

func TestAnOpenOverlayTakesTheKeyboard(t *testing.T) {
	m := newBare(t, 100, 40)
	m.SetQuery("SELECT 1")
	m, _ = send(t, m, f1)

	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd != nil || m.query.running() {
		t.Error("ctrl+r started a query while the overlay was open")
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != focusEditor || m.editor.Value() != "SELECT 1" {
		t.Errorf("keys reached the panes behind the overlay: focus %v, editor %q", m.focus, m.editor.Value())
	}
	if _, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlQ}); cmd == nil {
		t.Error("ctrl+q must still quit with an overlay open")
	}
}

func TestOpeningASecondOverlayIsIgnored(t *testing.T) {
	m := newBare(t, 100, 40)
	m, _ = send(t, m, f1)
	first := m.overlay
	m = m.open(help{offset: 3})
	if m.overlay != first {
		t.Error("a second overlay replaced the open one")
	}
}

func TestANewBindingShowsInTheHintRowAndHelp(t *testing.T) {
	saved := bindings
	defer func() { bindings = saved }()
	bindings = append(append([]binding{}, bindings...), binding{
		keys: "ctrl+zz", desc: "Do the new thing", group: "Brand new group",
		hints: []hint{{scopeEditor, "ctrl+zz", "newthing"}},
	})

	m := newBare(t, 200, 50)
	l := lines(m.View())
	if got := ansi.Strip(l[len(l)-1]); !strings.Contains(got, "ctrl+zz newthing") {
		t.Errorf("hint row = %q", got)
	}
	m, _ = send(t, m, f1)
	view := ansi.Strip(m.View())
	for _, want := range []string{"ctrl+zz", "Do the new thing", "Brand new group"} {
		if !strings.Contains(view, want) {
			t.Errorf("help is missing %q:\n%s", want, view)
		}
	}
}

func TestHelpScrollsOnAShortTerminal(t *testing.T) {
	m := newBare(t, 100, 10)
	m, _ = send(t, m, f1)
	height := m.overlayHeight()
	all := helpLines(100)
	if height >= len(all) {
		t.Fatalf("test needs a terminal shorter than the help (%d lines), have %d", len(all), height)
	}

	body := func(m Model) []string { l := lines(m.View()); return l[1 : 1+height] }
	if got := ansi.Strip(body(m)[0]); !strings.Contains(got, groupGlobal) {
		t.Errorf("help starts at %q, want the first group", got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	lastLine := strings.TrimSpace(ansi.Strip(all[len(all)-1]))
	if got := ansi.Strip(strings.Join(body(m), "\n")); !strings.Contains(got, lastLine) {
		t.Errorf("end did not reach the last binding %q:\n%s", lastLine, got)
	}
	for i := 0; i < 50; i++ {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if got := m.overlay.(help).offset; got != len(all)-height-1 {
		t.Errorf("offset after overshooting and one up = %d, want %d", got, len(all)-height-1)
	}
}

func TestTheLayoutFitsTheTerminalExactly(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {60, 14}} {
		m := newBare(t, size[0], size[1])
		for name, mm := range map[string]Model{"main": m, "help": func() Model { x, _ := send(t, m, f1); return x }()} {
			l := lines(mm.View())
			if len(l) != size[1] {
				t.Errorf("%dx%d %s view is %d lines, want %d", size[0], size[1], name, len(l), size[1])
			}
			for _, line := range l {
				if w := ansi.StringWidth(line); w > size[0] {
					t.Errorf("%dx%d %s view has a %d-column line", size[0], size[1], name, w)
				}
			}
		}
	}
}
