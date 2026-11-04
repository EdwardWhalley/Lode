package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

func (s *fakeSwitcher) Kinds() []KindInfo { return s.kinds }

func (s *fakeSwitcher) TestSource(ctx context.Context, n NewSource) error {
	s.tested = append(s.tested, n)
	if s.testFn != nil {
		return s.testFn(ctx, n)
	}
	return nil
}

func (s *fakeSwitcher) AddSource(n NewSource) (Choice, error) {
	s.added = append(s.added, n)
	if s.addFn != nil {
		return s.addFn(n)
	}
	return Choice{ID: "named:" + n.Name, Name: n.Name, Label: "new-" + n.Name}, nil
}

// Kinds the real code has never heard of.
var (
	pathKind = KindInfo{Name: "Quux files", ReadOnly: true, Fields: []adapter.Field{
		{Key: "path", Label: "Path", Type: adapter.Text, Required: true, Help: "Where the quux is"},
	}}
	serverKind = KindInfo{Name: "Zork server", Fields: []adapter.Field{
		{Key: "host", Label: "Host", Type: adapter.Text, Required: true, Help: "The zork host"},
		{Key: "port", Label: "Port", Type: adapter.Number, Min: 1, Max: 65535, Default: "1234", Help: "The zork port"},
		{Key: "password_var", Label: "Password variable", Type: adapter.EnvVar, Help: "Name of the variable"},
	}}
)

func (w *world) pickerModel(t *testing.T) Model {
	t.Helper()
	w.sw.kinds = []KindInfo{serverKind, pathKind}
	m := w.model(t)
	m = pressKey(t, m, ctrlO)
	return pressKey(t, m, keyRunes("n"))
}

func TestNOpensThePickerListingTheKindsInListOrder(t *testing.T) {
	w := newWorld()
	m := w.pickerModel(t)

	if _, ok := m.overlay.(kindPicker); !ok {
		t.Fatalf("n in the selector opened %T, want the type picker", m.overlay)
	}
	view := plain(m)
	zork, quux := strings.Index(view, "Zork server"), strings.Index(view, "Quux files")
	if zork < 0 || quux < 0 || zork > quux {
		t.Errorf("the picker should list Zork server then Quux files, in the order given:\n%s", view)
	}
}

func TestChoosingAKindShowsItsFieldsAndTheCommonOnes(t *testing.T) {
	for _, tc := range []struct {
		pick       int
		want       []string
		wantAbsent []string
	}{
		{0, []string{"Name", "Host", "Port", "Password variable", "Write", "Max rows"}, nil},
		{1, []string{"Name", "Path", "Max rows"}, []string{"Write", "Host"}},
	} {
		w := newWorld()
		m := w.pickerModel(t)
		for i := 0; i < tc.pick; i++ {
			m = pressKey(t, m, down)
		}
		m = pressKey(t, m, enter)
		if _, ok := m.overlay.(sourceForm); !ok {
			t.Fatalf("enter in the picker opened %T, want the form", m.overlay)
		}
		view := plain(m)
		last := -1
		for _, want := range tc.want {
			i := strings.Index(view, want)
			if i < 0 || i < last {
				t.Errorf("form for pick %d: %q missing or out of order:\n%s", tc.pick, want, view)
			}
			last = i
		}
		for _, absent := range tc.wantAbsent {
			if strings.Contains(view, absent) {
				t.Errorf("form for pick %d should not show %q:\n%s", tc.pick, absent, view)
			}
		}
	}
}

func TestAFormStartsWithTheFieldDefaults(t *testing.T) {
	w := newWorld()
	m := w.pickerModel(t)
	m = pressKey(t, m, enter)
	if view := plain(m); !strings.Contains(view, "1234") {
		t.Errorf("the port should start at its default:\n%s", view)
	}
}

func TestEscGoesBackToTheSelectorFromEachStep(t *testing.T) {
	w := newWorld()
	m := w.pickerModel(t)
	m = pressKey(t, m, esc)
	if _, ok := m.overlay.(selector); !ok {
		t.Fatalf("esc in the picker left %T, want the selector", m.overlay)
	}
	m = pressKey(t, m, keyRunes("n"))
	m = pressKey(t, m, enter)
	m = typeText(t, m, "abc")
	m = pressKey(t, m, esc)
	if _, ok := m.overlay.(selector); !ok {
		t.Fatalf("esc in the form left %T, want the selector", m.overlay)
	}
	m = pressKey(t, m, keyRunes("n"))
	m = pressKey(t, m, enter)
	if strings.Contains(plain(m), "abc") {
		t.Error("a new form should not remember what an earlier one held")
	}
}

func TestTabAndShiftTabMoveBetweenFieldsAndTypingGoesToTheFocusedOne(t *testing.T) {
	w := newWorld()
	m := w.pickerModel(t)
	m = pressKey(t, m, enter)
	m = typeText(t, m, "mine")
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(t, m, "db1")
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	m = typeText(t, m, "2")
	f := m.overlay.(sourceForm)
	if got := f.fields[0].input.Value(); got != "mine2" {
		t.Errorf("name = %q, want mine2", got)
	}
	if got := f.fields[1].input.Value(); got != "db1" {
		t.Errorf("host = %q, want db1", got)
	}
}

var tab = tea.KeyMsg{Type: tea.KeyTab}

// fillForm types each value into its field, moving on with tab; an empty string leaves the
// field as it is.
func fillForm(t *testing.T, m Model, values ...string) Model {
	t.Helper()
	for _, v := range values {
		m = typeText(t, m, v)
		m = pressKey(t, m, tab)
	}
	return m
}

func fieldError(t *testing.T, m Model, label string) string {
	t.Helper()
	for _, f := range m.overlay.(sourceForm).fields {
		if strings.HasPrefix(f.label, label) {
			return f.err
		}
	}
	t.Fatalf("no field labelled %q", label)
	return ""
}

func TestLeavingAFieldChecksItAndNamesTheFieldAtFault(t *testing.T) {
	for _, tc := range []struct {
		name  string
		taken []string
		typed []string
		label string
		want  string
	}{
		{"empty name", nil, []string{"", "h", "", ""}, "Name", "required"},
		{"bad characters in the name", nil, []string{"my db", "h", "", ""}, "Name", "letters, digits"},
		{"name already used", []string{"a"}, []string{"a", "h", "", ""}, "Name", "already"},
		{"empty required field", nil, []string{"n", "", "", ""}, "Host", "required"},
		{"port too big", nil, []string{"n", "h", "70000", ""}, "Port", "between 1 and 65535"},
		{"port not a number", nil, []string{"n", "h", "ab", ""}, "Port", "whole number"},
		{"password variable that is not a name", nil, []string{"n", "h", "", "pa ss"}, "Password variable", "variable name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld()
			w.sw.sources = func() ([]Choice, error) {
				var out []Choice
				for _, n := range tc.taken {
					out = append(out, Choice{ID: "named:" + n, Name: n})
				}
				return append(out, w.sw.choices["a"]), nil
			}
			m := w.pickerModel(t)
			m = pressKey(t, m, enter)
			for i, v := range tc.typed {
				if i == 2 && v != "" {
					for range "1234" {
						m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
					}
				}
				m = typeText(t, m, v)
				m = pressKey(t, m, tab)
			}
			got := fieldError(t, m, tc.label)
			if !strings.Contains(got, tc.want) {
				t.Errorf("message on %s = %q, want it to contain %q", tc.label, got, tc.want)
			}
			if !strings.Contains(plain(m), got) {
				t.Errorf("the message is not on the screen:\n%s", plain(m))
			}
		})
	}
}

func TestMaxRowsMustBeAWholeNumberInRange(t *testing.T) {
	for _, bad := range []string{"0", "1000001", "x", "-5"} {
		w := newWorld()
		m := w.pickerModel(t)
		m = pressKey(t, m, enter)
		m = fillForm(t, m, "n", "h", "", "", "")
		m = typeText(t, m, bad)
		m = pressKey(t, m, enter)
		if len(w.sw.tested) != 0 {
			t.Errorf("max rows %q: the Source was tested, want the save blocked", bad)
		}
		if fieldError(t, m, "Max rows") == "" {
			t.Errorf("max rows %q: no message on the field", bad)
		}
	}
}

func TestMaxRowsIsReadLikeEveryOtherNumber(t *testing.T) {
	for _, tc := range []struct {
		typed string
		want  string
	}{
		{"007", ""},
		{"5", ""},
		{"+5", "whole number"},
		{"-1", "whole number"},
		{" 5", "whole number"},
		{"x", "whole number"},
		{"0", "between 1 and"},
		{"1000001", "between 1 and"},
		{strings.Repeat("1", 12), "between 1 and"},
		{strings.Repeat("9", 40), "between 1 and"},
	} {
		w := newWorld()
		m := w.pickerModel(t)
		m = pressKey(t, m, enter)
		m = fillForm(t, m, "n", "h", "", "", "")
		m = typeText(t, m, tc.typed)
		m = pressKey(t, m, enter)
		got := fieldError(t, m, "Max rows")
		if tc.want == "" && (got != "" || len(w.sw.tested) != 1) {
			t.Errorf("max rows %q: message %q, tested %d; want it accepted", tc.typed, got, len(w.sw.tested))
		}
		if tc.want != "" && (!strings.Contains(got, tc.want) || len(w.sw.tested) != 0) {
			t.Errorf("max rows %q: message %q, tested %d; want a message with %q", tc.typed, got, len(w.sw.tested), tc.want)
		}
	}
}

func TestBracketedPasteGoesIntoTheFocusedField(t *testing.T) {
	w := newWorld()
	m := w.pickerModel(t)
	m = pressKey(t, m, enter)
	m = pressKey(t, m, tab)
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("db.example.com"), Paste: true})
	if got := m.overlay.(sourceForm).fields[1].input.Value(); got != "db.example.com" {
		t.Errorf("host = %q after a paste", got)
	}
}

func TestEveryStepSaysWhatItsKeysDo(t *testing.T) {
	for _, tc := range []struct {
		scope scope
		want  []string
	}{
		{scopeSelector, []string{"n new"}},
		{scopePicker, []string{"enter choose", "esc back"}},
		{scopeForm, []string{"tab", "esc back"}},
		{scopeFormTesting, []string{"esc cancel the test"}},
		{scopeFormFailed, []string{"ctrl+s save anyway"}},
		{scopeFormSaved, []string{"enter switch to it", "esc stay"}},
	} {
		row := ansi.Strip(hintRow(tc.scope, 200))
		for _, want := range tc.want {
			if !strings.Contains(row, want) {
				t.Errorf("hints for scope %d = %q, want %q", tc.scope, row, want)
			}
		}
	}
}
