package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

func (s *fakeSwitcher) EditForm(id string) (EditForm, error) {
	s.edited = append(s.edited, id)
	if s.editFn != nil {
		return s.editFn(id)
	}
	return EditForm{Kind: 0, Name: id, Values: adapter.Values{"host": "h.example", "port": "5432", "password_var": "PGPASS"}}, nil
}

func (s *fakeSwitcher) ReplaceSource(n NewSource) (Choice, error) {
	s.replaced = append(s.replaced, n)
	if s.replaceFn != nil {
		return s.replaceFn(n)
	}
	return Choice{ID: n.Name, Name: n.Name, Label: "edited-" + n.Name}, nil
}

func (s *fakeSwitcher) RemoveSource(id string) error {
	s.removed = append(s.removed, id)
	if s.removeFn != nil {
		return s.removeFn(id)
	}
	return nil
}

var (
	keyE = keyRunes("e")
	keyD = keyRunes("d")
)

// withAdHoc lists the ad-hoc Source first, then a and b: the order Sources gives.
func (w *world) withAdHoc() {
	adhoc := Choice{ID: "adhoc", Label: "adhoc.csv"}
	w.sw.sources = func() ([]Choice, error) {
		return []Choice{adhoc, w.sw.choices["a"], w.sw.choices["b"]}, nil
	}
}

// selectorAt opens the selector with the cursor on the Source with the given ID.
func (w *world) selectorAt(t *testing.T, id string) Model {
	t.Helper()
	w.sw.kinds = []KindInfo{serverKind, pathKind}
	m := w.model(t)
	m = pressKey(t, m, ctrlO)
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyHome})
	for i := 0; i < 5 && m.overlay.(selector).choices[m.overlay.(selector).cursor].ID != id; i++ {
		m = pressKey(t, m, down)
	}
	if got := m.overlay.(selector).choices[m.overlay.(selector).cursor].ID; got != id {
		t.Fatalf("could not put the selector on %q", id)
	}
	return m
}

func (w *world) editing(t *testing.T, id string) Model {
	t.Helper()
	return pressKey(t, w.selectorAt(t, id), keyE)
}

// inputs is the text in each row of the form, by label.
func inputs(t *testing.T, m Model) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, f := range m.overlay.(sourceForm).fields {
		out[f.label] = f.input.Value()
	}
	return out
}

// pressEnter sends enter n times, running what each returns.
func pressEnter(t *testing.T, m Model, n int) Model {
	t.Helper()
	for range n {
		m = pressKey(t, m, enter)
	}
	return m
}

// submitUnchanged walks to the last row of the Zork edit form and presses enter.
func submitUnchanged(t *testing.T, m Model) Model {
	t.Helper()
	return pressEnter(t, m, len(m.overlay.(sourceForm).fields))
}

func TestEOpensTheFormForTheHighlightedSourceFilledInFromWhatIsStored(t *testing.T) {
	w := newWorld()
	w.sw.editFn = func(id string) (EditForm, error) {
		return EditForm{
			Kind: 0, Name: "b", Write: true, MaxRows: 250,
			Values: adapter.Values{"host": "h.example", "port": "5432", "password_var": "PGPASS"},
		}, nil
	}
	m := w.editing(t, "b")
	if got := w.sw.edited; len(got) != 1 || got[0] != "b" {
		t.Errorf("EditForm asked for %v, want b", got)
	}
	if _, ok := m.overlay.(sourceForm); !ok || !strings.Contains(plain(m), "Edit Source") {
		t.Fatalf("no edit form:\n%s", plain(m))
	}
	in := inputs(t, m)
	if in["Name"] != "b" || in["Host"] != "h.example" || in["Port"] != "5432" || in["Max rows"] != "250" ||
		!m.overlay.(sourceForm).fields[4].on {
		t.Errorf("inputs = %v, write ticked %v", in, m.overlay.(sourceForm).fields[4].on)
	}
}

func TestSavingAnEditFormThatWasNotChangedSendsTheStoredSourceBackAsAReplace(t *testing.T) {
	w := newWorld()
	m := w.editing(t, "b")
	m = submitUnchanged(t, m)
	if len(w.sw.tested) != 1 || len(w.sw.replaced) != 1 || len(w.sw.added) != 0 {
		t.Fatalf("tested %d, replaced %d, added %d; want a test then a replace", len(w.sw.tested), len(w.sw.replaced), len(w.sw.added))
	}
	got := w.sw.replaced[0]
	if got.Replaces != "b" || got.Name != "b" || got.From != FromFields || got.Write || got.MaxRows != 0 ||
		got.Values["host"] != "h.example" || got.Values["port"] != "5432" || got.Values["password_var"] != "PGPASS" {
		t.Errorf("replaced %+v", got)
	}
	if w.sw.tested[0].Replaces != "b" {
		t.Errorf("the test did not say which Source it replaces: %+v", w.sw.tested[0])
	}
	if !strings.Contains(plain(m), "enter switches") {
		t.Errorf("no offer to switch to the edited Source:\n%s", plain(m))
	}
}

func TestARenameIsSentWithTheOldNameAndMayNotTakeAnotherSourcesName(t *testing.T) {
	w := newWorld()
	m := w.editing(t, "b")
	m = typeText(t, m, "2")
	m = submitUnchanged(t, m)
	if got := w.sw.replaced[0]; got.Name != "b2" || got.Replaces != "b" {
		t.Errorf("replaced %+v, want b2 replacing b", got)
	}

	w = newWorld()
	m = w.editing(t, "b")
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = typeText(t, m, "a")
	m = pressKey(t, m, tab)
	if got := fieldError(t, m, "Name"); !strings.Contains(got, "already") {
		t.Errorf("name message = %q, want it refused as taken", got)
	}
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	m = pressEnter(t, m, len(m.overlay.(sourceForm).fields))
	if len(w.sw.tested) != 0 {
		t.Error("a Source with another's name was tested")
	}
}

func TestTheNameOfTheSourceBeingEditedIsNotTakenByItself(t *testing.T) {
	w := newWorld()
	m := w.editing(t, "b")
	m = pressKey(t, m, tab)
	if got := fieldError(t, m, "Name"); got != "" {
		t.Errorf("name message = %q for the Source's own name", got)
	}
}

func TestTheOpenSourceCanBeEditedButNotRenamed(t *testing.T) {
	w := newWorld()
	m := w.editing(t, "a")
	m = typeText(t, m, "2")
	m = pressKey(t, m, tab)
	if got := fieldError(t, m, "Name"); got == "" {
		t.Error("the open Source was allowed a new name")
	}
}

func TestEditingTheOpenSourceSavesWithoutTouchingTheConnectionAndSaysWhenItApplies(t *testing.T) {
	w := newWorld()
	w.sw.kinds = []KindInfo{serverKind, pathKind}
	m := w.model(t)
	m.SetQuery("SELECT 1")
	m.status = ""
	m = pressKey(t, m, ctrlO)
	m = pressKey(t, m, keyE)
	m = submitUnchanged(t, m)
	if len(w.sw.replaced) != 1 || w.sw.replaced[0].Replaces != "a" {
		t.Fatalf("replaced %+v", w.sw.replaced)
	}
	if !strings.Contains(m.status, "next switched to or opened") {
		t.Errorf("status = %q, want it to say when the change applies", m.status)
	}
	m = pressKey(t, m, enter)
	if m.adapter != w.a || m.current.ID != "a" || m.editor.Value() != "SELECT 1" {
		t.Error("editing the open Source disturbed the session")
	}
	for _, ev := range w.ev.all() {
		if strings.HasPrefix(ev, "open") || strings.Contains(ev, "close") {
			t.Errorf("event %q: the open connection must not be touched", ev)
		}
	}
}

func TestEAndDOnTheAdHocSourceSayThereIsNothingToChangeAndDoNothingElse(t *testing.T) {
	for _, key := range []tea.KeyMsg{keyE, keyD} {
		w := newWorld()
		w.withAdHoc()
		m := w.selectorAt(t, "adhoc")
		m = pressKey(t, m, key)
		if _, ok := m.overlay.(selector); !ok {
			t.Errorf("%s: the selector should stay open, got %T", key, m.overlay)
		}
		if !m.isError || !strings.Contains(m.status, "command line") {
			t.Errorf("%s: status = %q, want it to say the Source is not in sources.toml", key, m.status)
		}
		if len(w.sw.edited)+len(w.sw.removed) != 0 {
			t.Errorf("%s: the Switcher was asked to change something", key)
		}
	}
}

func TestDOnTheOpenSourceIsRefused(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	m = pressKey(t, m, ctrlO)
	m = pressKey(t, m, keyD)
	if _, ok := m.overlay.(selector); !ok || !m.isError || !strings.Contains(m.status, "open") {
		t.Errorf("overlay %T, status %q; want the selector and a refusal", m.overlay, m.status)
	}
	if len(w.sw.removed) != 0 {
		t.Error("the open Source was removed")
	}
}

func TestEFailureShowsItsMessageAndGoesBackToTheSelector(t *testing.T) {
	w := newWorld()
	w.sw.editFn = func(string) (EditForm, error) {
		return EditForm{}, errors.New("change this Source in sources.toml by hand \x1b[2J")
	}
	m := w.editing(t, "b")
	if _, ok := m.overlay.(selector); !ok || !m.isError || !strings.Contains(m.status, "by hand") {
		t.Errorf("overlay %T, status %q", m.overlay, m.status)
	}
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Error("the message reached the screen unsanitized")
	}
}

func TestALiteralPasswordIsKeptAndTheFormSaysSoWithoutAnInputForIt(t *testing.T) {
	w := newWorld()
	w.sw.editFn = func(string) (EditForm, error) {
		return EditForm{Kind: 0, Name: "b", Placement: PlacedKeepingPassword, Values: adapter.Values{"host": "h", "port": "5432"}}, nil
	}
	m := w.editing(t, "b")
	if !strings.Contains(plain(m), "password") || !strings.Contains(plain(m), "kept") {
		t.Errorf("the form should say a stored password is kept:\n%s", plain(m))
	}
	if got := inputs(t, m)["Password variable (variable name)"]; got != "" {
		t.Errorf("the password variable field holds %q", got)
	}
	m = submitUnchanged(t, m)
	if got := w.sw.replaced[0]; got.Values["password_var"] != "" || got.Replaces != "b" {
		t.Errorf("replaced %+v", got)
	}
}

func TestALocationTheKindCannotPlaceIsOneRawFieldThatSavesWhatIsTyped(t *testing.T) {
	w := newWorld()
	w.sw.editFn = func(string) (EditForm, error) {
		return EditForm{Kind: 0, Name: "b", Placement: ShownAsText, Location: "zork://h:notaport/d"}, nil
	}
	m := w.editing(t, "b")
	in := inputs(t, m)
	if in["Location"] != "zork://h:notaport/d" {
		t.Errorf("inputs = %v, want the stored location in one Location field", in)
	}
	if _, has := in["Host"]; has {
		t.Errorf("the Kind's fields are shown beside the raw one: %v", in)
	}
	m = pressEnter(t, m, 1)
	m = typeText(t, m, "x")
	m = pressEnter(t, m, len(m.overlay.(sourceForm).fields))
	got := w.sw.replaced[0]
	if got.From != FromText || got.Location != "zork://h:notaport/dx" || got.Replaces != "b" {
		t.Errorf("replaced %+v", got)
	}
}

func TestAnEmptyRawLocationIsRefusedOnTheField(t *testing.T) {
	w := newWorld()
	w.sw.editFn = func(string) (EditForm, error) {
		return EditForm{Kind: 0, Name: "b", Placement: ShownAsText, Location: "z"}, nil
	}
	m := w.editing(t, "b")
	m = pressEnter(t, m, 1)
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = pressKey(t, m, tab)
	if got := fieldError(t, m, "Location"); got != "required" {
		t.Errorf("Location message = %q", got)
	}
}

func TestARawLocationWithAPasswordIsNeverShownOrHeldAndIsKept(t *testing.T) {
	w := newWorld()
	w.sw.editFn = func(string) (EditForm, error) {
		return EditForm{Kind: 0, Name: "b", Placement: Hidden}, nil
	}
	m := w.editing(t, "b")
	for label, v := range inputs(t, m) {
		if label == "Location" || strings.Contains(v, "://") {
			t.Errorf("field %q = %q: the location must not be in an input", label, v)
		}
	}
	if !strings.Contains(plain(m), "kept") {
		t.Errorf("the form should say the location is kept:\n%s", plain(m))
	}
	m = submitUnchanged(t, m)
	if got := w.sw.replaced[0]; got.From != FromStored || got.Location != "" {
		t.Errorf("replaced %+v, want the stored location kept", got)
	}
}

func TestAStaleFileRefusalOnAnEditShowsAndKeepsTheForm(t *testing.T) {
	w := newWorld()
	w.sw.replaceFn = func(NewSource) (Choice, error) {
		return Choice{}, errors.New("sources.toml changed on disk \x1b[2J")
	}
	m := w.editing(t, "b")
	m = submitUnchanged(t, m)
	if _, ok := m.overlay.(sourceForm); !ok || !strings.Contains(plain(m), "changed on disk") {
		t.Errorf("the refusal should show in the form:\n%s", plain(m))
	}
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Error("the message reached the screen unsanitized")
	}
	w.sw.replaceFn = nil
	m = pressEnter(t, m, 1)
	if !strings.Contains(plain(m), "enter switches") {
		t.Errorf("the retry should have saved:\n%s", plain(m))
	}
}

func TestAnEditedNameClashAtSaveTimeIsShownOnTheNameField(t *testing.T) {
	w := newWorld()
	w.sw.replaceFn = func(NewSource) (Choice, error) { return Choice{}, ErrNameTaken }
	m := w.editing(t, "b")
	m = submitUnchanged(t, m)
	if got := fieldError(t, m, "Name"); !strings.Contains(got, "already") {
		t.Errorf("name message = %q", got)
	}
}

func TestAFailedTestOfAnEditOffersSaveAnywayAndSavesAReplace(t *testing.T) {
	w := newWorld()
	w.sw.testFn = func(_ context.Context, _ NewSource) error { return errors.New("refused") }
	m := w.editing(t, "b")
	m = submitUnchanged(t, m)
	if len(w.sw.replaced) != 0 || !strings.Contains(plain(m), "ctrl+s saves it anyway") {
		t.Fatalf("replaced %d; view:\n%s", len(w.sw.replaced), plain(m))
	}
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if len(w.sw.replaced) != 1 || w.sw.replaced[0].Replaces != "b" {
		t.Errorf("replaced %+v", w.sw.replaced)
	}
}

func TestSavedEditsAppearInTheSelectorsList(t *testing.T) {
	w := newWorld()
	listed := []Choice{w.sw.choices["a"], w.sw.choices["b"]}
	w.sw.sources = func() ([]Choice, error) { return listed, nil }
	w.sw.replaceFn = func(n NewSource) (Choice, error) {
		listed[1] = Choice{ID: "b", Name: n.Name, Label: "renamed-label"}
		return listed[1], nil
	}
	m := w.editing(t, "b")
	m = submitUnchanged(t, m)
	m = pressKey(t, m, esc)
	if _, ok := m.overlay.(selector); !ok || !strings.Contains(plain(m), "renamed-label") {
		t.Errorf("the selector should show the edit:\n%s", plain(m))
	}
}

func TestDAsksForConfirmationNamingTheSourceAndEscCancels(t *testing.T) {
	w := newWorld()
	m := w.selectorAt(t, "b")
	m = pressKey(t, m, keyD)
	view := plain(m)
	if _, ok := m.overlay.(confirmDelete); !ok || !strings.Contains(view, `"b"`) || !strings.Contains(view, "enter") {
		t.Fatalf("no confirmation naming the Source:\n%s", view)
	}
	if len(w.sw.removed) != 0 {
		t.Fatal("removed before the user confirmed")
	}
	m = pressKey(t, m, esc)
	if len(w.sw.removed) != 0 {
		t.Error("esc removed the Source")
	}
	if _, ok := m.overlay.(selector); !ok {
		t.Errorf("esc should go back to the selector, got %T", m.overlay)
	}
}

func TestEnterOnTheConfirmationRemovesTheSourceAndRereadsTheList(t *testing.T) {
	w := newWorld()
	listed := []Choice{w.sw.choices["a"], w.sw.choices["b"]}
	w.sw.sources = func() ([]Choice, error) { return listed, nil }
	w.sw.removeFn = func(string) error { listed = listed[:1]; return nil }
	m := w.selectorAt(t, "b")
	m = pressKey(t, m, keyD)
	m = pressKey(t, m, enter)
	if got := w.sw.removed; len(got) != 1 || got[0] != "b" {
		t.Fatalf("removed %v", got)
	}
	if _, ok := m.overlay.(selector); !ok || strings.Contains(plain(m), "source-b") {
		t.Errorf("the selector should be back without the Source:\n%s", plain(m))
	}
	if !strings.Contains(m.status, "deleted") || m.isError {
		t.Errorf("status = %q", m.status)
	}
	if m.adapter != w.a {
		t.Error("deleting another Source touched the open one")
	}
}

func TestARefusedDeleteShowsItsMessageAndKeepsTheConfirmation(t *testing.T) {
	w := newWorld()
	w.sw.removeFn = func(string) error { return errors.New("sources.toml changed on disk \x1b[2J") }
	m := w.selectorAt(t, "b")
	m.SetQuery("SELECT 1")
	m = pressKey(t, m, keyD)
	m = pressKey(t, m, enter)
	if _, ok := m.overlay.(confirmDelete); !ok || !strings.Contains(plain(m), "changed on disk") {
		t.Fatalf("the refusal should show in the confirmation:\n%s", plain(m))
	}
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Error("the message reached the screen unsanitized")
	}
	if m.adapter != w.a || m.editor.Value() != "SELECT 1" {
		t.Error("a refused delete disturbed the session")
	}
	w.sw.removeFn = nil
	m = pressKey(t, m, enter)
	if len(w.sw.removed) != 2 {
		t.Errorf("removed %v, want a second try", w.sw.removed)
	}
	if _, ok := m.overlay.(selector); !ok {
		t.Errorf("after the retry the selector should be back, got %T", m.overlay)
	}
}

func TestAConfirmationTakesNoKeyButEnterAndEsc(t *testing.T) {
	w := newWorld()
	m := w.selectorAt(t, "b")
	m = pressKey(t, m, keyD)
	for _, k := range []tea.KeyMsg{keyRunes("y"), keyD, keyE, tab, down} {
		m = pressKey(t, m, k)
	}
	if _, ok := m.overlay.(confirmDelete); !ok || len(w.sw.removed) != 0 {
		t.Errorf("overlay %T, removed %v: only enter confirms", m.overlay, w.sw.removed)
	}
}

func TestTheConfirmationNeverDrawsControlCharactersFromAName(t *testing.T) {
	w := newWorld()
	w.sw.choices["b"] = Choice{ID: "b", Name: "b\x1b[2J", Label: "lbl"}
	m := w.selectorAt(t, "b")
	m = pressKey(t, m, keyD)
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Error("a name reached the screen unsanitized")
	}
}

func TestEditAndDeleteAreInTheLegend(t *testing.T) {
	row := ansi.Strip(hintRow(scopeSelector, 200))
	for _, want := range []string{"e edit", "d delete"} {
		if !strings.Contains(row, want) {
			t.Errorf("selector hints = %q, want %q", row, want)
		}
	}
	row = ansi.Strip(hintRow(scopeConfirm, 200))
	for _, want := range []string{"enter delete", "esc cancel"} {
		if !strings.Contains(row, want) {
			t.Errorf("confirmation hints = %q, want %q", row, want)
		}
	}
}
