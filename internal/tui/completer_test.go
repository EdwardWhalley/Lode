package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/adapter/mysql"
	"github.com/EdwardWhalley/lode/internal/adapter/postgres"
	"github.com/EdwardWhalley/lode/internal/editor"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

func TestACompleterIsActiveOnlyWhileItsListIsOpen(t *testing.T) {
	var c completer
	if c.active() {
		t.Fatal("a new completer is active")
	}
	c.completion = &completion{items: []suggestion{{label: "users"}}}
	if !c.active() {
		t.Error("a completer with a list is not active")
	}
	if c.close().active() {
		t.Error("a closed completer is active")
	}
}

func TestClosingTheListKeepsTheDismissal(t *testing.T) {
	c := completer{}.dismiss(wordKey{row: 1, col: 4}, "se")
	c.completion = &completion{items: []suggestion{{label: "select"}}}

	c = c.close()
	if _, silenced := c.silenced(wordKey{row: 1, col: 4}, "se"); !silenced {
		t.Error("closing the list forgot the dismissal")
	}
}

func TestDismissingRecordsTheWordInLowerCase(t *testing.T) {
	c := completer{}.dismiss(wordKey{row: 0, col: 7}, "SeL")
	if !c.hasDismissed || c.dismissedWord != "sel" || c.dismissed != (wordKey{row: 0, col: 7}) {
		t.Errorf("dismissal = %+v", c)
	}
}

func TestADismissedWordStaysSilencedWhileItIsTheSameWord(t *testing.T) {
	c := completer{}.dismiss(wordKey{row: 0, col: 7}, "se")
	for _, word := range []string{"se", "sel", "SELECT"} {
		if _, silenced := c.silenced(wordKey{row: 0, col: 7}, word); !silenced {
			t.Errorf("silenced(%q) = false at the dismissed place", word)
		}
	}
}

func TestADifferentWordForgetsTheDismissal(t *testing.T) {
	for name, tc := range map[string]struct {
		key  wordKey
		word string
	}{
		"another place": {wordKey{row: 0, col: 9}, "se"},
		"another line":  {wordKey{row: 1, col: 7}, "se"},
		"other text":    {wordKey{row: 0, col: 7}, "fr"},
		"shorter text":  {wordKey{row: 0, col: 7}, "s"},
	} {
		c := completer{}.dismiss(wordKey{row: 0, col: 7}, "se")
		c, silenced := c.silenced(tc.key, tc.word)
		if silenced || c.hasDismissed {
			t.Errorf("%s: silenced = %v, still dismissed = %v; want false, false", name, silenced, c.hasDismissed)
		}
		if _, again := c.silenced(wordKey{row: 0, col: 7}, "se"); again {
			t.Errorf("%s: the first word is silenced again after the dismissal was dropped", name)
		}
	}
}

func TestNothingIsSilencedBeforeAnythingIsDismissed(t *testing.T) {
	if _, silenced := (completer{}).silenced(wordKey{}, ""); silenced {
		t.Error("a new completer silences a word")
	}
}

// editorWith is an editor holding text with the cursor at its end, set up as the
// Model sets one up.
func editorWith(text string) editor.Model {
	ed := editor.New()
	ed.SetHeight(defaultEditorHeight)
	ed.SetWidth(80)
	ed.Focus()
	ed.SetValue(text)
	return ed
}

// opened is a completer opened for the editor's text, as typing or a request would.
func opened(t *testing.T, how int, text string) (completer, editor.Model) {
	t.Helper()
	ed := editorWith(text)
	c, _ := completer{}.refresh(how, ed, schemaFixture(), sqltext.Dialect{})
	if !c.active() {
		t.Fatalf("no dropdown for %q", text)
	}
	return c, ed
}

func TestRefreshByTypingOpensForTwoLettersAndNotForOne(t *testing.T) {
	c, _ := completer{}.refresh(byTyping, editorWith("SELECT * FROM pe"), schemaFixture(), sqltext.Dialect{})
	if !c.active() || len(c.items) != 1 || c.items[0].label != "people" || c.items[0].insert != `"people"` {
		t.Errorf("two letters: active = %v, items = %+v; want the people table", c.active(), c.items)
	}
	if c, _ := (completer{}).refresh(byTyping, editorWith("SELECT * FROM p"), schemaFixture(), sqltext.Dialect{}); c.active() {
		t.Error("one letter opened the dropdown")
	}
}

func TestRefreshByRequestOpensForNothingAndSaysWhenNothingMatches(t *testing.T) {
	c, noMatch := completer{}.refresh(byRequest, editorWith("SELECT * FROM "), schemaFixture(), sqltext.Dialect{})
	if !c.active() || noMatch {
		t.Errorf("a request with nothing typed: active = %v, noMatch = %v; want true, false", c.active(), noMatch)
	}
	c, noMatch = completer{}.refresh(byRequest, editorWith("SELECT zzzz"), schemaFixture(), sqltext.Dialect{})
	if c.active() || !noMatch {
		t.Errorf("a request nothing matches: active = %v, noMatch = %v; want false, true", c.active(), noMatch)
	}
	if c, noMatch := (completer{}).refresh(byTyping, editorWith("SELECT zzzz"), schemaFixture(), sqltext.Dialect{}); c.active() || noMatch {
		t.Errorf("typing: active = %v, noMatch = %v; want false, false", c.active(), noMatch)
	}
}

func TestRefreshKeepsADismissedWordClosed(t *testing.T) {
	c := completer{}.dismiss(wordKey{row: 0, col: 14}, "pe")
	c, _ = c.refresh(byTyping, editorWith("SELECT * FROM pe"), schemaFixture(), sqltext.Dialect{})
	if c.active() {
		t.Error("the dismissed word opened the dropdown")
	}
	c, _ = c.refresh(byTyping, editorWith("SELECT * FROM or"), schemaFixture(), sqltext.Dialect{})
	if !c.active() {
		t.Error("another word stayed closed after a dismissal")
	}
}

func TestRefreshStaysClosedInsideAStringOrAComment(t *testing.T) {
	for _, text := range []string{"SELECT 'pe", "SELECT * -- pe", "SELECT /* pe"} {
		if c, _ := (completer{}).refresh(byTyping, editorWith(text), schemaFixture(), sqltext.Dialect{}); c.active() {
			t.Errorf("%q opened the dropdown", text)
		}
	}
}

func TestRefreshReadsStringsAsTheDialectDoes(t *testing.T) {
	text := `SELECT * FROM people WHERE name = 'a\' OR pe`
	if c, _ := (completer{}).refresh(byTyping, editorWith(text), schemaFixture(), mysql.Kind.Dialect); c.active() {
		t.Error(`with backslash escapes, \' does not end the string, so the dropdown stays closed`)
	}
	if c, _ := (completer{}).refresh(byTyping, editorWith(text), schemaFixture(), sqltext.Dialect{}); !c.active() {
		t.Error(`without them, \' ends the string and the word after it is offered`)
	}
}

func TestRefreshOnPostgreSQLStaysClosedInsideANestedComment(t *testing.T) {
	text := "SELECT * FROM /* a /* b */ still a comment; FROM pe"
	if c, _ := (completer{}).refresh(byTyping, editorWith(text), schemaFixture(), postgres.Kind.Dialect); c.active() {
		t.Error("with PostgreSQL's Dialect the comment is still open, so the dropdown stays closed")
	}
	if c, _ := (completer{}).refresh(byTyping, editorWith(text), schemaFixture(), sqltext.Dialect{}); !c.active() {
		t.Error("with the zero Dialect the comment has ended and the word is offered")
	}
}

func TestRefreshClosesTheDropdownForSeveralCursors(t *testing.T) {
	c, _ := opened(t, byTyping, "SELECT * FROM pe")
	ed := editorWith("a1\nb2\nc3")
	ed, _ = ed.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	ed, _ = ed.Update(ctrlAltDown)
	if ed.CursorCount() != 2 {
		t.Fatalf("%d cursors, want 2", ed.CursorCount())
	}
	if c, _ := c.refresh(whileOpen, ed, schemaFixture(), sqltext.Dialect{}); c.active() {
		t.Error("the dropdown stayed open with two cursors")
	}
}

func TestTheArrowsMoveTheHighlightAndEnterNeedsAMoveFirst(t *testing.T) {
	c, ed := opened(t, byRequest, "SELECT * FROM ")
	if c.moved || c.cursor != 0 {
		t.Fatalf("a new list: moved = %v, cursor = %d", c.moved, c.cursor)
	}
	next, _, _, handled := c.key(tea.KeyMsg{Type: tea.KeyDown}, ed, schemaFixture(), sqltext.Dialect{})
	if !handled || !next.moved || next.cursor != 1 || c.cursor != 0 {
		t.Errorf("down: handled = %v, moved = %v, cursor = %d (old cursor %d); want true, true, 1, 0", handled, next.moved, next.cursor, c.cursor)
	}
	next, _, _, _ = next.key(tea.KeyMsg{Type: tea.KeyUp}, ed, schemaFixture(), sqltext.Dialect{})
	if next.cursor != 0 {
		t.Errorf("up: cursor = %d, want 0", next.cursor)
	}

	before := ed.Value()
	closed, ed2, _, handled := c.key(tea.KeyMsg{Type: tea.KeyEnter}, ed, schemaFixture(), sqltext.Dialect{})
	if handled || closed.active() || ed2.Value() != before {
		t.Errorf("enter before a move: handled = %v, active = %v, value changed = %v; want false, false, false", handled, closed.active(), ed2.Value() != before)
	}
	moved, _, _, _ := c.key(tea.KeyMsg{Type: tea.KeyDown}, ed, schemaFixture(), sqltext.Dialect{})
	want := moved.items[moved.cursor].insert
	closed, ed2, _, handled = moved.key(tea.KeyMsg{Type: tea.KeyEnter}, ed, schemaFixture(), sqltext.Dialect{})
	if !handled || closed.active() || !strings.HasSuffix(ed2.Value(), want) {
		t.Errorf("enter after a move: handled = %v, active = %v, value = %q; want true, false, ending in %q", handled, closed.active(), ed2.Value(), want)
	}
}

func TestTabAcceptsAndTheWordIsReplacedInOneUndoStep(t *testing.T) {
	c, ed := opened(t, byTyping, "SELECT * FROM pe")
	closed, ed, _, handled := c.key(tea.KeyMsg{Type: tea.KeyTab}, ed, schemaFixture(), sqltext.Dialect{})
	if !handled || closed.active() || ed.Value() != `SELECT * FROM "people"` {
		t.Fatalf("tab: handled = %v, active = %v, value = %q", handled, closed.active(), ed.Value())
	}
	ed, _ = ed.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	if ed.Value() != "SELECT * FROM pe" {
		t.Errorf("after one undo: %q, want the typed word back", ed.Value())
	}
}

func TestEscInTheCompleterDismissesThatWord(t *testing.T) {
	c, ed := opened(t, byTyping, "SELECT * FROM pe")
	closed, ed2, _, handled := c.key(tea.KeyMsg{Type: tea.KeyEsc}, ed, schemaFixture(), sqltext.Dialect{})
	if !handled || closed.active() || ed2.Value() != ed.Value() {
		t.Fatalf("esc: handled = %v, active = %v, value changed = %v", handled, closed.active(), ed2.Value() != ed.Value())
	}
	if again, _ := closed.refresh(byTyping, ed, schemaFixture(), sqltext.Dialect{}); again.active() {
		t.Error("the dismissed word opened the dropdown again")
	}
}

func TestAWordCharacterKeepsItOpenAndASpaceClosesIt(t *testing.T) {
	c, ed := opened(t, byTyping, "SELECT * FROM pe")
	kept, ed, _, handled := c.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")}, ed, schemaFixture(), sqltext.Dialect{})
	if !handled || !kept.active() || !strings.HasSuffix(ed.Value(), "peo") {
		t.Errorf("a word character: handled = %v, active = %v, value = %q; want true, true, ending in peo", handled, kept.active(), ed.Value())
	}
	closed, ed, _, handled := c.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")}, ed, schemaFixture(), sqltext.Dialect{})
	if !handled || closed.active() {
		t.Errorf("a space: handled = %v, active = %v; want true, false", handled, closed.active())
	}
}

func TestAKeyItDoesNotKnowClosesTheListAndIsNotHandled(t *testing.T) {
	c, ed := opened(t, byTyping, "SELECT * FROM pe")
	closed, _, _, handled := c.key(tea.KeyMsg{Type: tea.KeyLeft}, ed, schemaFixture(), sqltext.Dialect{})
	if handled || closed.active() {
		t.Errorf("left: handled = %v, active = %v; want false, false", handled, closed.active())
	}
}

func TestDrawFloatsTheListOverTheBodyAndLeavesAClosedOneAlone(t *testing.T) {
	body := strings.Repeat(strings.Repeat(".", 40)+"\n", 9) + strings.Repeat(".", 40)
	if got := (completer{}).draw(body, 40); got != body {
		t.Error("a closed completer changed the body")
	}
	c, _ := opened(t, byTyping, "SELECT * FROM pe")
	if got := c.draw(body, 40); got == body || !strings.Contains(got, "people") {
		t.Errorf("an open completer did not draw its list:\n%s", got)
	}
}

func TestTheModelTellsTheCompleterTheDialectOfTheOpenSource(t *testing.T) {
	for _, c := range []struct {
		name     string
		dialect  sqltext.Dialect
		wantOpen bool
	}{
		{"MySQL's", mysql.Kind.Dialect, false},
		{"the zero", sqltext.Dialect{}, true},
	} {
		m, _ := completeModel(t, schemaFixture(), `SELECT * FROM people WHERE name = 'a\' OR p`, -1)
		m.current.Dialect = c.dialect
		m = typeText(t, m, "e")
		if m.complete.active() != c.wantOpen {
			t.Errorf("with %s Dialect: dropdown open = %v, want %v", c.name, m.complete.active(), c.wantOpen)
		}
	}
}
