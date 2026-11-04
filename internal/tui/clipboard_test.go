package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/clipboard"
)

var (
	ctrlC = tea.KeyMsg{Type: tea.KeyCtrlC}
	ctrlX = tea.KeyMsg{Type: tea.KeyCtrlX}
	ctrlV = tea.KeyMsg{Type: tea.KeyCtrlV}
)

// clipModel is a Model holding text whose clipboard is the fake, with no Source.
func clipModel(t *testing.T, text string, sys *fakeSys) (Model, *countingAdapter) {
	t.Helper()
	a := &countingAdapter{}
	m := New(a, "x", WithClipboard(clipboard.New(sys, nil)))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetQuery(text)
	return m, a
}

// firstWordSelected has the first word of the query selected.
func firstWordSelected(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlHome})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlShiftRight})
	if !m.editor.HasSelection() {
		t.Fatal("set up no Selection")
	}
	return m
}

func TestCtrlCWithASelectionCopiesItAndDoesNotQuitOrCancel(t *testing.T) {
	sys := &fakeSys{}
	m, _ := clipModel(t, "select name", sys)
	m = firstWordSelected(t, m)
	cancelled := 0
	m.query.cancel = func() { cancelled++ }

	m = sendAndFinish(t, m, ctrlC)

	if sys.text != "select" {
		t.Errorf("system clipboard holds %q, want %q", sys.text, "select")
	}
	if !strings.Contains(m.status, "copied 6") {
		t.Errorf("status = %q, want it to say 6 characters were copied", m.status)
	}
	if cancelled != 0 {
		t.Error("ctrl+c with a Selection cancelled the running query; esc does that")
	}
	if got := m.editor.Value(); got != "select name" {
		t.Errorf("copying changed the text to %q", got)
	}
}

func TestCtrlCWithNoSelectionIsUnchanged(t *testing.T) {
	sys := &fakeSys{}
	m, _ := clipModel(t, "select name", sys)

	_, cmd := send(t, m, ctrlC)
	if cmd == nil {
		t.Fatal("ctrl+c with nothing selected and no query did nothing")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Error("ctrl+c with nothing selected did not quit when idle")
	}

	cancelled := 0
	m.query.cancel = func() { cancelled++ }
	m, _ = send(t, m, ctrlC)
	if cancelled != 1 {
		t.Errorf("ctrl+c cancelled the query %d times, want once", cancelled)
	}
	if sys.text != "" {
		t.Errorf("ctrl+c with nothing selected copied %q", sys.text)
	}
}

func TestEscStillCancelsAQueryWhileThereIsASelection(t *testing.T) {
	m, _ := clipModel(t, "select name", &fakeSys{})
	m = firstWordSelected(t, m)
	cancelled := 0
	m.query.cancel = func() { cancelled++ }

	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if cancelled != 1 {
		t.Errorf("esc cancelled the query %d times, want once", cancelled)
	}
}

func TestCtrlXCutsTheSelection(t *testing.T) {
	sys := &fakeSys{}
	m, _ := clipModel(t, "select name", sys)
	m = firstWordSelected(t, m)

	m = sendAndFinish(t, m, ctrlX)

	if got := m.editor.Value(); got != " name" {
		t.Errorf("text = %q, want %q", got, " name")
	}
	if sys.text != "select" {
		t.Errorf("system clipboard holds %q, want %q", sys.text, "select")
	}
	if !strings.Contains(m.status, "cut 6") {
		t.Errorf("status = %q, want it to say 6 characters were cut", m.status)
	}
}

func TestCtrlXWithNoSelectionDoesNothing(t *testing.T) {
	sys := &fakeSys{text: "untouched"}
	m, _ := clipModel(t, "select name", sys)

	m, cmd := send(t, m, ctrlX)

	if cmd != nil {
		t.Error("ctrl+x with nothing selected started something")
	}
	if got := m.editor.Value(); got != "select name" || sys.text != "untouched" {
		t.Errorf("ctrl+x with nothing selected changed the text to %q or the clipboard to %q", got, sys.text)
	}
}

func TestCtrlVPastesTheClipboardOverASelection(t *testing.T) {
	sys := &fakeSys{text: "people"}
	m, a := clipModel(t, "select name", sys)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlEnd})
	for range 4 {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyShiftLeft})
	}

	m = sendAndFinish(t, m, ctrlV)

	if got := m.editor.Value(); got != "select people" {
		t.Errorf("text = %q, want %q", got, "select people")
	}
	if a.queries != 0 {
		t.Errorf("pasting ran %d queries", a.queries)
	}
}

func TestACopyThenAPasteWorksEvenWithNoSystemClipboard(t *testing.T) {
	m := New(&countingAdapter{}, "x", WithClipboard(clipboard.New(nil, nil)))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetQuery("select name")
	m = firstWordSelected(t, m)

	m = sendAndFinish(t, m, ctrlC)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlEnd})
	m = sendAndFinish(t, m, ctrlV)

	if got := m.editor.Value(); got != "select nameselect" {
		t.Errorf("text = %q, want %q (the copy made inside lode)", got, "select nameselect")
	}
}

func TestAFailingClipboardSaysSoAndEditingCarriesOn(t *testing.T) {
	sys := &fakeSys{writeErr: errors.New("no xclip\x1b[31m")}
	m, _ := clipModel(t, "select name", sys)
	m = firstWordSelected(t, m)

	m = sendAndFinish(t, m, ctrlC)

	if !strings.Contains(m.status, "system clipboard") {
		t.Errorf("status = %q, want it to say the system clipboard did not take it", m.status)
	}
	if strings.ContainsRune(m.status, 0x1b) {
		t.Errorf("status carries an escape character: %q", m.status)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlEnd})
	m = sendAndFinish(t, m, ctrlV)
	if got := m.editor.Value(); got != "select nameselect" {
		t.Errorf("text = %q, want the copy pasted", got)
	}
}

func TestAnUnreadableClipboardPastesTheLastCopyAndSaysSo(t *testing.T) {
	sys := &fakeSys{}
	m, _ := clipModel(t, "select name", sys)
	m = firstWordSelected(t, m)
	m = sendAndFinish(t, m, ctrlC)
	sys.readErr = errors.New("clipboard is locked")

	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlEnd})
	m = sendAndFinish(t, m, ctrlV)

	if got := m.editor.Value(); got != "select nameselect" {
		t.Errorf("text = %q, want the last copy pasted", got)
	}
	if !strings.Contains(m.status, "could not be read") {
		t.Errorf("status = %q, want it to say the clipboard could not be read", m.status)
	}
}

func TestSeveralCursorsCopyEverySelectionCutThemAllAndPasteAtEach(t *testing.T) {
	sys := &fakeSys{}
	m, _ := clipModel(t, "one two\nthree four", sys)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlHome})
	m, _ = send(t, m, ctrlAltDown)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlShiftRight})

	m = sendAndFinish(t, m, ctrlC)
	if sys.text != "one\nthree" {
		t.Errorf("copied %q, want every Selection joined by a newline", sys.text)
	}

	m = sendAndFinish(t, m, ctrlX)
	if got := m.editor.Value(); got != " two\n four" {
		t.Errorf("after the cut text = %q, want both Selections gone", got)
	}

	sys.text = "Z"
	m = sendAndFinish(t, m, ctrlV)
	if got := m.editor.Value(); got != "Z two\nZ four" {
		t.Errorf("after the paste text = %q, want it at each cursor", got)
	}
}

func TestCutAndPasteOnlyActWhileTheEditorHasTheFocus(t *testing.T) {
	sys := &fakeSys{text: "x"}
	m, _ := clipModel(t, "select name", sys)
	m = firstWordSelected(t, m)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})

	m, cmd := send(t, m, ctrlX)
	if cmd != nil || m.editor.Value() != "select name" {
		t.Error("ctrl+x acted while the grid had the focus")
	}
	m, cmd = send(t, m, ctrlV)
	if cmd != nil || m.editor.Value() != "select name" {
		t.Error("ctrl+v acted while the grid had the focus")
	}
}

func TestTheClipboardKeysAreInTheLegend(t *testing.T) {
	help := strings.Join(helpLines(140), "\n")
	for _, want := range []string{"ctrl+x", "ctrl+v", "copies"} {
		if !strings.Contains(help, want) {
			t.Errorf("F1 help does not mention %q", want)
		}
	}
}

func TestAPasteRightAfterACopyGivesTheCopyNotTheOldClipboard(t *testing.T) {
	sys := &fakeSys{text: "OLD"}
	m, _ := clipModel(t, "select name", sys)
	m = firstWordSelected(t, m)
	m, _ = send(t, m, ctrlC)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlEnd})

	m = sendAndFinish(t, m, ctrlV)

	if got := m.editor.Value(); got != "select nameselect" {
		t.Errorf("text = %q, want the copy pasted, not %q", got, "OLD")
	}
}

func TestALateCopyFailureAddsToTheStatusInsteadOfReplacingIt(t *testing.T) {
	sys := &fakeSys{writeErr: errors.New("no xclip")}
	m, _ := clipModel(t, "select name", sys)
	m = firstWordSelected(t, m)
	m, cmd := send(t, m, ctrlC)
	answer := cmd()

	m.status = "42 rows in 1ms"
	m, _ = send(t, m, answer)

	if !strings.HasPrefix(m.status, "42 rows in 1ms") {
		t.Errorf("status = %q, want the newer status kept", m.status)
	}
	if !strings.Contains(m.status, "system clipboard") {
		t.Errorf("status = %q, want the copy failure said too", m.status)
	}
}
