package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// filledForm is the Zork form with a valid Source typed in, the cursor on Max rows.
func (w *world) filledForm(t *testing.T) Model {
	t.Helper()
	m := w.pickerModel(t)
	m = pressKey(t, m, enter)
	return fillForm(t, m, "mine", "db.example", "", "PGPASS", "")
}

func (w *world) submitted(t *testing.T) (Model, tea.Cmd) {
	t.Helper()
	return send(t, w.filledForm(t), enter)
}

func TestEnterOnTheLastFieldTestsTheSourceAndEscCancelsTheTest(t *testing.T) {
	w := newWorld()
	started := make(chan struct{})
	var ctx context.Context
	w.sw.testFn = func(c context.Context, _ NewSource) error {
		ctx = c
		close(started)
		<-c.Done()
		return c.Err()
	}
	m, cmd := w.submitted(t)
	if !strings.Contains(plain(m), "testing") {
		t.Errorf("no testing state shown:\n%s", plain(m))
	}
	done := make(chan tea.Msg)
	go func() { done <- cmd() }()
	<-started

	m, _ = send(t, m, esc)
	late := <-done
	if ctx.Err() == nil {
		t.Error("esc did not cancel the test's context")
	}
	if _, ok := m.overlay.(sourceForm); !ok || strings.Contains(plain(m), "testing") {
		t.Errorf("esc while testing should return to the editable form:\n%s", plain(m))
	}
	m, cmd = send(t, m, late)
	if cmd != nil || len(w.sw.added) != 0 {
		t.Error("a cancelled test must not save")
	}

	got := w.sw.tested[0]
	if got.Kind != 0 || got.Name != "mine" || got.Write || got.MaxRows != 0 || len(got.Values) != 3 ||
		got.Values["host"] != "db.example" || got.Values["port"] != "1234" || got.Values["password_var"] != "PGPASS" {
		t.Errorf("tested %+v, want the Zork Source as typed", got)
	}
}

func TestTheWriteBoxAndMaxRowsReachTheSwitcher(t *testing.T) {
	w := newWorld()
	m := w.pickerModel(t)
	m = pressKey(t, m, enter)
	m = fillForm(t, m, "mine", "db.example", "", "PGPASS")
	m = pressKey(t, m, keyRunes(" "))
	m = pressKey(t, m, tab)
	m = typeText(t, m, "250")
	m, cmd := send(t, m, enter)
	drain(t, m, cmd)
	got := w.sw.tested[0]
	if !got.Write || got.MaxRows != 250 {
		t.Errorf("tested %+v, want write on and 250 rows", got)
	}
}

func TestASuccessfulTestSavesAndOffersTheSwitch(t *testing.T) {
	w := newWorld()
	w.sw.adapters["new"] = &fakeAdapter{name: "new", ev: w.ev}
	w.sw.choices["new"] = Choice{ID: "new", Name: "mine", Label: "zork://db.example"}
	w.sw.addFn = func(NewSource) (Choice, error) { return w.sw.choices["new"], nil }
	m, cmd := w.submitted(t)
	m = drain(t, m, cmd)

	if len(w.sw.added) != 1 || w.sw.added[0].Name != "mine" {
		t.Fatalf("added %+v, want the Source once", w.sw.added)
	}
	if !strings.Contains(plain(m), "enter switches") {
		t.Errorf("no offer to switch:\n%s", plain(m))
	}
	if m.adapter != w.a {
		t.Fatal("nothing should switch before the user says so")
	}
	m = pressKey(t, m, enter)
	if m.adapter == w.a || m.current.ID != "new" || m.overlay != nil {
		t.Errorf("enter should switch to the new Source and close the form (current %q)", m.current.ID)
	}
}

func TestEscAfterSavingStaysAndTheSelectorListsTheNewSource(t *testing.T) {
	w := newWorld()
	listed := []Choice{w.sw.choices["a"], w.sw.choices["b"]}
	w.sw.sources = func() ([]Choice, error) { return listed, nil }
	w.sw.addFn = func(NewSource) (Choice, error) {
		c := Choice{ID: "new", Name: "mine", Label: "zork://db.example"}
		listed = append(listed, c)
		return c, nil
	}
	m, cmd := w.submitted(t)
	m = drain(t, m, cmd)
	m = pressKey(t, m, esc)
	if m.adapter != w.a {
		t.Error("esc should stay on the current Source")
	}
	if _, ok := m.overlay.(selector); !ok || !strings.Contains(plain(m), "zork://db.example") {
		t.Errorf("the selector should list the new Source:\n%s", plain(m))
	}
}

func TestAFailedTestKeepsTheFormShowsASafeErrorAndSavesOnlyOnPurpose(t *testing.T) {
	w := newWorld()
	w.sw.testFn = func(context.Context, NewSource) error { return errors.New("dial \x1b[31mfailed\x1b[0m: refused") }
	m, cmd := w.submitted(t)
	m = drain(t, m, cmd)

	view := plain(m)
	if _, ok := m.overlay.(sourceForm); !ok || !strings.Contains(view, "refused") || strings.Contains(m.View(), "\x1b[31mfailed") {
		t.Errorf("the form should stay open with the (sanitized) error:\n%s", view)
	}
	if len(w.sw.added) != 0 {
		t.Fatal("a failed test must not save by itself")
	}
	if !strings.Contains(view, "ctrl+s") {
		t.Errorf("save anyway is not offered:\n%s", view)
	}

	m = pressKey(t, m, ctrlS)
	if len(w.sw.added) != 1 || len(w.sw.tested) != 1 {
		t.Errorf("save anyway: %d saves, %d tests, want 1 and 1", len(w.sw.added), len(w.sw.tested))
	}
	if !strings.Contains(plain(m), "enter switches") {
		t.Errorf("no offer to switch after saving anyway:\n%s", plain(m))
	}
}

func TestSaveAnywayDoesNothingBeforeATestHasFailed(t *testing.T) {
	w := newWorld()
	m := w.filledForm(t)
	pressKey(t, m, ctrlS)
	if len(w.sw.added) != 0 {
		t.Error("ctrl+s saved a form that has not been tested")
	}
}

func TestAnUnsetPasswordVariableBlocksTheSaveWithNoWayAround(t *testing.T) {
	w := newWorld()
	w.sw.testFn = func(context.Context, NewSource) error { return ErrVariableUnset }
	m, cmd := w.submitted(t)
	m = drain(t, m, cmd)

	msg := fieldError(t, m, "Password variable")
	if msg == "" {
		t.Error("the message belongs on the password variable field")
	}
	if strings.Contains(msg, "PGPASS") || strings.Contains(m.status, "PGPASS") {
		t.Error("the message repeats the variable's name")
	}
	view := plain(m)
	if strings.Contains(view, "anyway") || strings.Contains(hintRow(m.currentScope(), 200), "anyway") {
		t.Errorf("save anyway must not be offered:\n%s", view)
	}
	pressKey(t, m, ctrlS)
	if len(w.sw.added) != 0 {
		t.Error("ctrl+s saved a Source whose password variable is not set")
	}
}

func TestASaveRefusedForAnUnsetVariableOffersNoSwitchAndNoWayAround(t *testing.T) {
	w := newWorld()
	w.sw.addFn = func(NewSource) (Choice, error) { return Choice{}, ErrVariableUnset }
	m, cmd := w.submitted(t)
	m = drain(t, m, cmd)
	if strings.Contains(plain(m), "enter switches") || strings.Contains(plain(m), "anyway") {
		t.Errorf("a refused save must not offer the switch or a way around:\n%s", plain(m))
	}
	if fieldError(t, m, "Password variable") == "" {
		t.Error("the message belongs on the password variable field")
	}
}

func TestANameTakenAtSaveTimeIsShownOnTheNameField(t *testing.T) {
	w := newWorld()
	w.sw.addFn = func(NewSource) (Choice, error) { return Choice{}, ErrNameTaken }
	m, cmd := w.submitted(t)
	m = drain(t, m, cmd)
	if got := fieldError(t, m, "Name"); !strings.Contains(got, "already") {
		t.Errorf("name field message = %q", got)
	}
	if got := m.overlay.(sourceForm).focus; got != 0 {
		t.Errorf("focus = %d, want the name field", got)
	}
}

func TestARefusedSaveShowsItsMessageAndKeepsTheForm(t *testing.T) {
	w := newWorld()
	w.sw.addFn = func(NewSource) (Choice, error) {
		return Choice{}, errors.New("sources.toml changed since it was read \x1b[2J")
	}
	m, cmd := w.submitted(t)
	m = drain(t, m, cmd)
	if _, ok := m.overlay.(sourceForm); !ok || !strings.Contains(plain(m), "changed since it was read") {
		t.Errorf("the refusal should show in the form:\n%s", plain(m))
	}
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Error("the message reached the screen unsanitized")
	}
	w.sw.addFn = nil
	m = pressKey(t, m, enter)
	if len(w.sw.added) != 2 || !strings.Contains(plain(m), "enter switches") {
		t.Errorf("retry: %d saves; view:\n%s", len(w.sw.added), plain(m))
	}
}

func TestAFailedTestLeavesTheEditorAndResultsAlone(t *testing.T) {
	w := newWorld()
	w.sw.kinds = []KindInfo{serverKind, pathKind}
	w.sw.testFn = func(context.Context, NewSource) error { return errors.New("refused") }
	m := w.model(t)
	m.SetQuery("SELECT 1")
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, cmd)
	rows := len(m.grid.rows)

	m = pressKey(t, m, ctrlO)
	m = pressKey(t, m, keyRunes("n"))
	m = pressKey(t, m, enter)
	m = fillForm(t, m, "mine", "db.example", "", "PGPASS", "")
	m = pressKey(t, m, enter)
	m = pressKey(t, m, esc)
	m = pressKey(t, m, esc)

	if m.adapter != w.a || m.editor.Value() != "SELECT 1" || len(m.grid.rows) != rows || m.overlay != nil {
		t.Errorf("a failed test disturbed the session: adapter ok %v, editor %q, rows %d/%d",
			m.adapter == w.a, m.editor.Value(), len(m.grid.rows), rows)
	}
}

func TestALateAnswerFromAnEarlierFormIsIgnored(t *testing.T) {
	w := newWorld()
	block := make(chan struct{})
	defer close(block)
	w.sw.testFn = func(c context.Context, _ NewSource) error {
		select {
		case <-c.Done():
			return c.Err()
		case <-block:
			return nil
		}
	}
	m, cmd := w.submitted(t)
	late := make(chan tea.Msg)
	go func() { late <- cmd() }()
	for len(w.sw.tested) == 0 {
		time.Sleep(time.Millisecond)
	}
	m, _ = send(t, m, esc)
	lateMsg := <-late
	m = pressKey(t, m, esc)
	m = pressKey(t, m, keyRunes("n"))
	m = pressKey(t, m, enter)
	m = fillForm(t, m, "mine", "db.example", "", "PGPASS", "")
	m, _ = send(t, m, enter)

	m, cmd = send(t, m, lateMsg)
	if cmd != nil || strings.Contains(plain(m), "the test failed") || !strings.Contains(plain(m), "testing") {
		t.Errorf("the first form's answer was taken by the second:\n%s", plain(m))
	}
}

func TestAnUnsetVariableElsewhereIsNotBlamedOnAnEmptyPasswordField(t *testing.T) {
	w := newWorld()
	w.sw.testFn = func(context.Context, NewSource) error { return ErrVariableUnset }
	m := w.pickerModel(t)
	m = pressKey(t, m, enter)
	m = fillForm(t, m, "mine", "${DBHOST}", "", "", "")
	m, cmd := send(t, m, enter)
	m = drain(t, m, cmd)
	if got := fieldError(t, m, "Password variable"); got != "" {
		t.Errorf("the empty password field was blamed: %q", got)
	}
	if !strings.Contains(plain(m), "variable the Source needs is not set") || strings.Contains(plain(m), "anyway") {
		t.Errorf("the form should say a variable is not set, with no way round it:\n%s", plain(m))
	}
}
