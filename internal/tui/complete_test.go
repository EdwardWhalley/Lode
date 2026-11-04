package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

var altSlash = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}, Alt: true}

func TestTheDropdownOpensWhileTypingAndNeverTouchesTheDatabase(t *testing.T) {
	m, a := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT * FROM ord")
	if got := labels(m); !strings.HasPrefix(got, "orders") {
		t.Fatalf("suggestions after typing ord = %s, want the dropdown open without any key to ask for it", got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got, want := m.editor.Value(), `SELECT * FROM "orders"`; got != want {
		t.Errorf("editor = %q, want %q", got, want)
	}
	if a.queries != 0 || a.schemas != 0 {
		t.Errorf("completion called the database: %d queries, %d schema loads", a.queries, a.schemas)
	}
}

func TestPrefixMatchingIsCaseInsensitiveAndFollowsTyping(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "SELECT * FROM ", -1)
	m = typeText(t, m, "PEO")
	m, _ = send(t, m, altSlash)
	if got := labels(m); got != "people" {
		t.Errorf("suggestions for PEO = %s, want people", got)
	}

	m, _ = completeModel(t, schemaFixture(), "SELECT * FROM o", -1)
	m, _ = send(t, m, altSlash)
	if got := labels(m); !strings.HasPrefix(got, "orders") {
		t.Fatalf("suggestions for o = %s", got)
	}
	m = typeText(t, m, "x")
	if m.complete.active() {
		t.Errorf("the list should close when nothing matches, still %s", labels(m))
	}

	m, _ = completeModel(t, schemaFixture(), "SELECT * FROM or", -1)
	m, _ = send(t, m, altSlash)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if got := labels(m); !strings.HasPrefix(got, "orders") {
		t.Errorf("after backspace = %s", got)
	}
	m = typeText(t, m, " ")
	if m.complete.active() {
		t.Error("a space should close the list")
	}
}

func TestAcceptingATableInsertsExactlyItsRef(t *testing.T) {
	for name, keys := range map[string][]tea.KeyMsg{
		"tab":                   {{Type: tea.KeyTab}},
		"arrow then enter":      {{Type: tea.KeyDown}, {Type: tea.KeyEnter}},
		"arrows round then tab": {{Type: tea.KeyDown}, {Type: tea.KeyUp}, {Type: tea.KeyTab}},
	} {
		m, _ := completeModel(t, schemaFixture(), "SELECT * FROM sal", -1)
		m, _ = send(t, m, altSlash)
		if got := labels(m); got != "sales.Order Items" {
			t.Fatalf("suggestions = %s", got)
		}
		for _, k := range keys {
			m, _ = send(t, m, k)
		}
		if got, want := m.editor.Value(), `SELECT * FROM "sales"."Order Items"`; got != want {
			t.Errorf("%s: %q, want %q (the Ref, not a name built from the display name)", name, got, want)
		}
		if m.complete.active() {
			t.Errorf("%s: accepting should close the list", name)
		}
		if m.focus != focusEditor {
			t.Errorf("%s: accepting also moved the focus", name)
		}
	}
}

func TestADotOffersThatTablesColumnsAndAcceptingInsertsTheColumnRef(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "SELECT orders.", -1)
	m, _ = send(t, m, altSlash)
	if got := labels(m); got != "id,customer,order" {
		t.Errorf("columns of orders = %s", got)
	}
	m = typeText(t, m, "ord")
	if got := labels(m); got != "order" {
		t.Fatalf("after typing ord = %s", got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got, want := m.editor.Value(), `SELECT orders."order"`; got != want {
		t.Errorf("editor = %q, want %q: a keyword-named column must be quoted by its Ref", got, want)
	}
}

func TestColumnsWithSpacesAreInsertedThroughTheirRef(t *testing.T) {
	text := `SELECT fir FROM sales."Order Items"`
	m, _ := completeModel(t, schemaFixture(), text, len("SELECT fir"))
	m, _ = send(t, m, altSlash)
	if got := labels(m); got != "First Name" {
		t.Fatalf("suggestions = %s", got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got, want := m.editor.Value(), `SELECT "First Name" FROM sales."Order Items"`; got != want {
		t.Errorf("editor = %q, want %q", got, want)
	}
}

func TestColumnsAreOfferedOnlyForTablesInTheText(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "SELECT cu", -1)
	m, _ = send(t, m, altSlash)
	if strings.Contains(labels(m), "customer") {
		t.Errorf("customer belongs to orders, which the query does not mention: %s", labels(m))
	}

	m, _ = completeModel(t, schemaFixture(), "SELECT cu FROM orders", len("SELECT cu"))
	m, _ = send(t, m, altSlash)
	if got := labels(m); !strings.HasPrefix(got, "customer") {
		t.Errorf("with orders in the query: %s", got)
	}
}

func TestKeywordsAreInsertedInUpperCase(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "sel", -1)
	m, _ = send(t, m, altSlash)
	if got := labels(m); got != "SELECT" {
		t.Fatalf("suggestions = %s", got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got := m.editor.Value(); got != "SELECT" {
		t.Errorf("editor = %q", got)
	}
}

func TestOnlyTheWordBeforeTheCursorIsReplaced(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "SELECT * FROM peo WHERE x = 1", len("SELECT * FROM peo"))
	m, _ = send(t, m, altSlash)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got, want := m.editor.Value(), `SELECT * FROM "people" WHERE x = 1`; got != want {
		t.Errorf("editor = %q, want %q", got, want)
	}
}

func TestCompletionWorksOnALaterLineOfAMultiLineQuery(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "SELECT *\nFROM peo", -1)
	m, _ = send(t, m, altSlash)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got, want := m.editor.Value(), "SELECT *\nFROM \"people\""; got != want {
		t.Errorf("editor = %q, want %q", got, want)
	}
}

func TestCompletionWorksOnASoftWrappedLine(t *testing.T) {
	long := "SELECT " + strings.Repeat("a, ", 30) + "* FROM peo"
	m, _ := completeModel(t, schemaFixture(), long, -1)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 40, Height: 30})
	m, _ = send(t, m, altSlash)
	if got := labels(m); got != "people" {
		t.Fatalf("suggestions = %s", got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got, want := m.editor.Value(), strings.TrimSuffix(long, "peo")+`"people"`; got != want {
		t.Errorf("editor = %q, want %q", got, want)
	}
}

func TestArrowsMoveEscClosesAndOtherKeysCloseTheListThenActNormally(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "SELECT * FROM ", -1)
	m, _ = send(t, m, altSlash)
	first := m.complete.items[0].label
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.complete.cursor != 1 {
		t.Errorf("down: cursor %d", m.complete.cursor)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyUp})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.complete.cursor != 0 || m.complete.items[0].label != first {
		t.Errorf("up: cursor %d", m.complete.cursor)
	}
	m, _ = send(t, m, esc)
	if m.complete.active() || m.editor.Value() != "SELECT * FROM " {
		t.Error("esc should close the list and change nothing")
	}

	m, _ = send(t, m, altSlash)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.complete.active() {
		t.Error("moving the cursor away should close the list")
	}
}

func TestNoSchemaSaysSoButKeywordsStillComplete(t *testing.T) {
	m, _ := completeModel(t, nil, "SEL", -1)
	m, _ = send(t, m, altSlash)
	if !strings.Contains(m.status, "no schema loaded") {
		t.Errorf("status = %q, want it to say the schema is not loaded", m.status)
	}
	if got := labels(m); got != "SELECT" {
		t.Errorf("keywords should still be offered, got %s", got)
	}
}

func TestNothingMatchingSaysSo(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "SELECT zzz", -1)
	m, _ = send(t, m, altSlash)
	if m.complete.active() || m.status != "no suggestions" {
		t.Errorf("status %q, list %v", m.status, m.complete)
	}
}

func TestAfterASwitchTheSuggestionsComeFromTheNewSource(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	m = drain(t, m, loadSchema(w.a, m.adapterGen))
	m, cmd := send(t, m, switchMsg{w.sw.choices["b"]})
	m = drain(t, m, cmd)

	m.SetQuery("SELECT * FROM fro")
	m, _ = send(t, m, altSlash)
	if got := labels(m); !strings.Contains(got, "from_b") || strings.Contains(got, "from_a") {
		t.Errorf("suggestions = %s, want the new Source's from_b and not from_a", got)
	}
}

func TestALargeSchemaIsFastAndShowsAtMostEightRows(t *testing.T) {
	var tables []adapter.Table
	for i := 0; i < 5000; i++ {
		name := fmt.Sprintf("table_%04d", i)
		tables = append(tables, adapter.Table{Name: name, Ref: adapter.QuoteIdent(name), Columns: []adapter.Column{col("id")}})
	}
	m, _ := completeModel(t, tables, "SELECT * FROM t", -1)
	start := time.Now()
	m, _ = send(t, m, altSlash)
	if d := time.Since(start); d > time.Second {
		t.Errorf("opening took %v", d)
	}
	if n := len(m.complete.items); n > maxSuggestions {
		t.Errorf("%d candidates, want at most %d", n, maxSuggestions)
	}
	if rows, _ := m.complete.box(70, 30); len(rows) > maxVisible {
		t.Errorf("%d suggestions shown at once, want at most %d", len(rows), maxVisible)
	}
	m = typeText(t, m, "able_499")
	if n := len(m.complete.items); n != 10 {
		t.Errorf("typing narrows to %d suggestions, want 10", n)
	}
	m = typeText(t, m, "9")
	if m.complete.active() {
		t.Errorf("the dropdown should close once the word is complete, still %s", labels(m))
	}
}

func TestSchemaNamesCannotDriveTheTerminal(t *testing.T) {
	tables := []adapter.Table{{Name: "ev\x1b[2Jil", Ref: `"evil"`, Columns: []adapter.Column{col("c\x1b[2J")}}}
	m, _ := completeModel(t, tables, "SELECT ", -1)
	m, _ = send(t, m, altSlash)
	if strings.Contains(m.View(), "\x1b[2J") {
		t.Errorf("a schema name reached the terminal unsanitized: %q", m.View())
	}
}

func lastLine(m Model) string {
	parts := strings.Split(m.View(), "\n")
	return ansi.Strip(parts[len(parts)-1])
}

func TestAutocompleteKeyIsInTheLegend(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)

	m, _ = send(t, m, tea.WindowSizeMsg{Width: 160, Height: 30})
	if got := lastLine(m); !strings.Contains(got, "alt+/ complete") {
		t.Errorf("hint row = %q", got)
	}

	m, _ = send(t, m, altSlash)
	if got := lastLine(m); !strings.Contains(got, "tab accept") || strings.Contains(got, "ctrl+r") {
		t.Errorf("hint row while the list is open = %q", got)
	}
}

func TestNarrowTerminalsKeepTheCoreHints(t *testing.T) {
	for _, width := range []int{70, 80, 100} {
		m, _ := completeModel(t, schemaFixture(), "", -1)
		m, _ = send(t, m, tea.WindowSizeMsg{Width: width, Height: 30})
		for _, pane := range []string{"editor", "grid", "tree"} {
			got := lastLine(m)
			for _, core := range []string{"F1 help", "ctrl+r run", "tab focus", "ctrl+o source", "ctrl+q quit"} {
				if !strings.Contains(got, core) {
					t.Errorf("width %d, %s: hint row %q is missing the core hint %q", width, pane, got, core)
				}
			}
			m, _ = send(t, m, tab)
		}
	}
}

func TestTheDropdownNeedsTwoCharactersButADotOpensItAtOnce(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "S")
	if m.complete.active() {
		t.Errorf("one character opened the dropdown: %s", labels(m))
	}
	m = typeText(t, m, "E")
	if got := labels(m); got != "SELECT,SET" {
		t.Errorf("two characters: %s, want SELECT,SET", got)
	}

	m, _ = completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "orders.")
	if got := labels(m); got != "id,customer,order" {
		t.Errorf("right after the dot: %s, want the columns of orders", got)
	}
}

func TestItStaysClosedInsideStringsAndComments(t *testing.T) {
	for name, text := range map[string]string{
		"string":        "SELECT 'ord",
		"after a quote": "SELECT 'it''s' || 'ord",
		"line comment":  "SELECT 1 -- ord",
		"block comment": "SELECT 1 /* ord",
	} {
		m, _ := completeModel(t, schemaFixture(), "", -1)
		m = typeText(t, m, text)
		if m.complete.active() {
			t.Errorf("%s: the dropdown opened inside it: %s", name, labels(m))
		}
	}
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT 'x' || /* c */ ord")
	if !m.complete.active() {
		t.Error("it should open again once the string and the comment are closed")
	}
	m, _ = completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT 1 -- note\nord")
	if !m.complete.active() {
		t.Error("a line comment ends at the end of its line")
	}
}

func TestPastedTextDoesNotOpenIt(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("SELECT * FROM ord"), Paste: true})
	if m.complete.active() {
		t.Errorf("a paste opened the dropdown: %s", labels(m))
	}
	if got := m.editor.Value(); got != "SELECT * FROM ord" {
		t.Errorf("the paste itself failed: %q", got)
	}
}

func TestAWordThatIsAlreadyCompleteOffersNothing(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT * FROM orders")
	if m.complete.active() {
		t.Errorf("the dropdown is still open for a finished word: %s", labels(m))
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.editor.Value(); got != "SELECT * FROM orders\n" {
		t.Errorf("editor = %q", got)
	}
}

func TestEnterIsANewlineUnlessYouChoseFromTheList(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT * FROM or")
	if !m.complete.active() {
		t.Fatal("the dropdown should be open")
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.complete.active() {
		t.Error("enter should close the dropdown")
	}
	if got := m.editor.Value(); got != "SELECT * FROM or\n" {
		t.Errorf("editor = %q: enter without a choice must be a newline and change nothing else", got)
	}

	m, _ = completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT * FROM or")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyUp})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.editor.Value(); got != `SELECT * FROM "orders"` {
		t.Errorf("editor = %q: enter after choosing should accept", got)
	}
}

func TestEscClosesItForThatWordOnly(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT * FROM or")
	m, _ = send(t, m, esc)
	if m.complete.active() {
		t.Fatal("esc should close the dropdown")
	}
	m = typeText(t, m, "d")
	if m.complete.active() {
		t.Errorf("typing more of the same word reopened it: %s", labels(m))
	}
	m = typeText(t, m, "ers JOIN pe")
	if got := labels(m); got != "people" {
		t.Errorf("the next word should get its dropdown again: %s", got)
	}
}

func TestBackspaceNarrowsAndClosesWhenNoWordIsLeft(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SEL")
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if !m.complete.active() {
		t.Error("the dropdown should stay open while a word remains")
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.complete.active() {
		t.Errorf("the dropdown should close when the word is gone: %s", labels(m))
	}
}

func TestTheDropdownIsDrawnOverTheGridUnderTheWord(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT * FROM or")
	rows := strings.Split(ansi.Strip(m.View()), "\n")

	firstGridRow := rows[1+m.layout.editorH+1]
	idx := strings.Index(firstGridRow, "orders")
	if idx < 0 || !strings.Contains(firstGridRow, "table") {
		t.Fatalf("the first suggestion is not on the first grid row:\n%s", strings.Join(rows, "\n"))
	}
	wantCol := 30 + 1 + 2 + 14 + 1
	if got := ansi.StringWidth(firstGridRow[:idx]); got != wantCol {
		t.Errorf("suggestion starts at column %d, want %d (under the word)", got, wantCol)
	}
	if n := len(rows); n != 30 {
		t.Errorf("the view is %d lines, want 30: the dropdown must not push anything down", n)
	}
}

func TestOverlayAtKeepsWhatIsOnEitherSide(t *testing.T) {
	if got := ansi.Strip(overlayAt("abcdefghij", 2, "XYZ", 3)); got != "abXYZfghij" {
		t.Errorf("got %q", got)
	}
	coloured := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("abcdefghij")
	if got := ansi.Strip(overlayAt(coloured, 2, "XYZ", 3)); got != "abXYZfghij" {
		t.Errorf("with colour: got %q", got)
	}
	if got := ansi.Strip(overlayAt("ab", 4, "Z", 1)); got != "ab  Z" {
		t.Errorf("past the end: got %q", got)
	}
	if got := ansi.Strip(overlayAt("abcdef", 4, "XYZ", 3)); got != "abcdXYZ" {
		t.Errorf("hanging off the end: got %q", got)
	}
}

func TestASwitchClosesTheDropdown(t *testing.T) {
	w := newWorld()
	m := w.model(t)
	m.SetQuery("")
	m = typeText(t, m, "SELECT * FROM fr")
	if !m.complete.active() {
		t.Fatal("the dropdown should be open")
	}
	m, cmd := send(t, m, switchMsg{w.sw.choices["b"]})
	m = drain(t, m, cmd)
	if m.complete.active() {
		t.Error("a Source switch left suggestions from the old Source open")
	}
}

func TestTypingInTheGridDoesNotOpenIt(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(t, m, "ord")
	if m.complete.active() {
		t.Errorf("the dropdown opened while the grid had the focus: %s", labels(m))
	}
}

func TestTheHighlightedSuggestionIsAlwaysOnScreen(t *testing.T) {
	for height := 11; height <= 40; height++ {
		m, _ := completeModel(t, schemaFixture(), "", -1)
		m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: height})
		m, _ = send(t, m, altSlash)
		total := len(m.complete.items)
		if total < 20 {
			t.Fatalf("test needs a long list, got %d", total)
		}
		for i := 0; i < total; i++ {
			rows := strings.Split(ansi.Strip(m.View()), "\n")
			if len(rows) != height {
				t.Fatalf("height %d, item %d: the view is %d lines", height, i, len(rows))
			}
			grid := rows[1+m.layout.editorH+1 : height-2]
			want := m.complete.items[m.complete.cursor].label
			found := false
			for _, line := range grid {
				if len(line) > m.layout.schemaW+1 && strings.Contains(line[m.layout.schemaW+1:], want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("height %d, item %d (%q): the highlighted row is not on screen:\n%s", height, i, want, strings.Join(grid, "\n"))
			}
			m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyDown})
		}
	}
}

func TestEscOnlySilencesThatWordNotWhateverLandsInItsPlace(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT * FROM or")
	m, _ = send(t, m, esc)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = typeText(t, m, "pe")
	if got := labels(m); got != "people" {
		t.Errorf("a different word at the same position: %s, want people", got)
	}

	m, _ = completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT * FROM or")
	m, _ = send(t, m, esc)
	m = typeText(t, m, "DER")
	if m.complete.active() {
		t.Errorf("more of the dismissed word reopened it: %s", labels(m))
	}

	m, _ = completeModel(t, schemaFixture(), "SELECT * FROM p", -1)
	m.complete = m.complete.dismiss(wordKey{row: 0, col: 14}, "or")
	m = typeText(t, m, "e")
	if got := labels(m); got != "people" {
		t.Errorf("another word at the dismissed coordinate: %s, want people", got)
	}
	if m.complete.hasDismissed {
		t.Error("the marker should be cleared once it no longer matches the word")
	}
}

func TestEscThenASpaceAndANewWordOpensAgain(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT * FROM or")
	m, _ = send(t, m, esc)
	m = typeText(t, m, "ders JOIN pe")
	if got := labels(m); got != "people" {
		t.Errorf("the next word after a dismissed one: %s", got)
	}
}

func promptWidth(m Model) int { return ansi.StringWidth(m.editor.Prompt) }

func TestTheDropdownAnchorOnAWordThatWrapsOntoTheNextRow(t *testing.T) {
	long := strings.Repeat("a", 45)
	tables := []adapter.Table{{Name: long, Ref: adapter.QuoteIdent(long)}}
	m, _ := completeModel(t, tables, "", -1)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 40, Height: 30})
	m = typeText(t, m, strings.Repeat("a", 40))
	if !m.complete.active() {
		t.Fatal("the dropdown should be open")
	}
	info := m.editor.LineInfo()
	if info.StartColumn == 0 {
		t.Fatalf("test needs the cursor on a wrapped row, StartColumn = %d", info.StartColumn)
	}
	if got, want := m.complete.anchor, promptWidth(m); got != want {
		t.Errorf("anchor = %d, want %d: the start of the text on the row the cursor is on", got, want)
	}
}

func TestTheDropdownAnchorAfterWideCharacters(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "", -1)
	m = typeText(t, m, "SELECT 日本語 or")
	if !m.complete.active() {
		t.Fatal("the dropdown should be open")
	}
	if got, want := m.complete.anchor, promptWidth(m)+7+6+1; got != want {
		t.Errorf("anchor = %d, want %d (cells, not characters)", got, want)
	}
}

func TestTheDropdownAnchorIsNeverPastTheCursorOrBeforeTheText(t *testing.T) {
	for name, text := range map[string]string{
		"tab":      "SELECT\tor",
		"two tabs": "\t\tor",
		"emoji":    "SELECT 🙂🙂 or",
	} {
		m, _ := completeModel(t, schemaFixture(), "", -1)
		m = typeText(t, m, text)
		if !m.complete.active() {
			t.Errorf("%s: the dropdown should be open", name)
			continue
		}
		lo, hi := promptWidth(m), promptWidth(m)+m.editor.LineInfo().CharOffset
		if a := m.complete.anchor; a < lo || a > hi {
			t.Errorf("%s: anchor %d is outside the text [%d, %d]", name, a, lo, hi)
		}
	}
}

func TestCompletionInTheMiddleOfALineAfterWideCharacters(t *testing.T) {
	text := "SELECT 日本語 peo WHERE 1"
	runes := []rune(text)
	at := len([]rune("SELECT 日本語 peo"))
	if string(runes[at-3:at]) != "peo" {
		t.Fatal("test setup is wrong")
	}
	m, _ := completeModel(t, schemaFixture(), text, at)
	m, _ = send(t, m, altSlash)
	if got := labels(m); got != "people" {
		t.Fatalf("suggestions = %s, want people", got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got, want := m.editor.Value(), `SELECT 日本語 "people" WHERE 1`; got != want {
		t.Errorf("editor = %q, want %q", got, want)
	}
}

func TestTheContextWindowKeepsWholeLinesAndNormalQueriesWhole(t *testing.T) {
	m, _ := completeModel(t, schemaFixture(), "SELECT cu\nFROM orders\nWHERE 1", len("SELECT cu"))
	m.editor.CursorStart()
	ctx := cursorContextOf(m.editor)
	if !strings.Contains(ctx.around, "FROM orders") || !strings.Contains(ctx.around, "WHERE 1") {
		t.Errorf("a short query must be seen whole, around = %q", ctx.around)
	}

	line := strings.Repeat("日本語", 40) + "\n"
	long := strings.Repeat(line, 400) + "SELECT or"
	m, _ = completeModel(t, schemaFixture(), long, -1)
	ctx = cursorContextOf(m.editor)
	if !utf8.ValidString(ctx.before) || !utf8.ValidString(ctx.around) {
		t.Error("a window cut a character in half")
	}
	if len(ctx.before) > contextBefore+len("SELECT or") {
		t.Errorf("before is %d bytes, want at most about %d", len(ctx.before), contextBefore)
	}
	if !strings.HasSuffix(ctx.before, "SELECT or") || string(ctx.line) != "SELECT or" || ctx.col != len("SELECT or") {
		t.Errorf("the cursor line and column are wrong: %q at %d", string(ctx.line), ctx.col)
	}
}

func TestAWordIsCompletedCorrectlyInAHugeScript(t *testing.T) {
	filler := strings.Repeat("SELECT 1 FROM people WHERE x = 2;\n", 1500)
	m, _ := completeModel(t, schemaFixture(), filler+"SELECT * FROM or", -1)
	m, _ = send(t, m, altSlash)
	if got := labels(m); !strings.HasPrefix(got, "orders") {
		t.Fatalf("suggestions = %s", got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if !strings.HasSuffix(m.editor.Value(), `SELECT * FROM "orders"`) || !strings.HasPrefix(m.editor.Value(), "SELECT 1 FROM people") {
		t.Errorf("the completion damaged the script: ...%q", m.editor.Value()[len(m.editor.Value())-40:])
	}
}

func TestColumnsComeFromTablesNearTheCursorOnly(t *testing.T) {
	near := "SELECT cu\nFROM orders"
	m, _ := completeModel(t, schemaFixture(), near, -1)
	m = cursorTo(m, 0, len("SELECT cu"))
	m, _ = send(t, m, altSlash)
	if !strings.HasPrefix(labels(m), "customer") {
		t.Errorf("a table named on the next line should count: %s", labels(m))
	}

	far := "SELECT cu\n" + strings.Repeat("-- padding line\n", 600) + "FROM orders"
	m, _ = completeModel(t, schemaFixture(), far, -1)
	m = cursorTo(m, 0, len("SELECT cu"))
	m, _ = send(t, m, altSlash)
	if strings.Contains(labels(m), "customer") {
		t.Errorf("a table named 10 KB below the cursor should not be searched for: %s", labels(m))
	}
}

// cursorTo puts the editor cursor on a row and a column (in runes).
func cursorTo(m Model, row, col int) Model {
	for m.editor.Line() > row {
		m.editor, _ = m.editor.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	m.editor.SetCursor(col)
	return m
}
