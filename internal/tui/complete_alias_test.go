package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

func aliasSchema() []adapter.Table {
	return []adapter.Table{
		{Name: "orders", Ref: `"orders"`, Columns: []adapter.Column{col("id"), col("total")}},
		{Name: "customers", Ref: `"customers"`, Columns: []adapter.Column{col("id"), col("name")}},
	}
}

func TestTypingAnAliasAndADotOffersThatTablesColumns(t *testing.T) {
	m, a := completeModel(t, aliasSchema(), "SELECT  FROM orders o", len("SELECT "))
	m = typeText(t, m, "o.")
	if got := labels(m); got != "id,total" {
		t.Fatalf("after typing o. = %s, want the columns of orders", got)
	}
	m = typeText(t, m, "to")
	if got := labels(m); got != "total" {
		t.Fatalf("after typing to = %s", got)
	}
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if got, want := m.editor.Value(), `SELECT o."total" FROM orders o`; got != want {
		t.Errorf("editor = %q, want %q", got, want)
	}
	if a.queries != 0 || a.schemas != 0 {
		t.Errorf("completion called the database: %d queries, %d schema loads", a.queries, a.schemas)
	}
}

// details is the label and detail of every suggestion, to show whose column each is.
func details(m Model) string {
	if !m.complete.active() {
		return "<closed>"
	}
	var out []string
	for _, s := range m.complete.items {
		out = append(out, s.label+" ("+s.detail+")")
	}
	return strings.Join(out, ", ")
}

// suggestedAt opens the dropdown by request with the cursor at the first "|" of
// text, which is not part of the text.
func suggestedAt(t *testing.T, text string) Model {
	t.Helper()
	before, after, _ := strings.Cut(text, "|")
	m, _ := completeModel(t, aliasSchema(), before+after, -1)
	m = cursorTo(m, strings.Count(before, "\n"), len([]rune(before[strings.LastIndex(before, "\n")+1:])))
	m, _ = send(t, m, altSlash)
	return m
}

func TestAnAliasListsTheColumnsOfItsTable(t *testing.T) {
	const orders = "id (column of orders), total (column of orders)"
	const customers = "id (column of customers), name (column of customers)"
	for name, tc := range map[string]struct{ text, want string }{
		"defined after the cursor":     {"SELECT o.| FROM orders o", orders},
		"with AS, before the cursor":   {"SELECT * FROM orders AS o WHERE o.|", orders},
		"a joined table":               {"SELECT * FROM orders o JOIN customers c ON c.|", customers},
		"the first table of a join":    {"SELECT * FROM orders o JOIN customers c ON o.|", orders},
		"narrowed by what is typed":    {"SELECT o.to| FROM orders o", "total (column of orders)"},
		"a schema-qualified table":     {"SELECT o.| FROM sales.orders o", orders},
		"an alias in capitals":         {"SELECT O.| FROM orders o", orders},
		"no alias, a table's own name": {"SELECT orders.| FROM orders", orders},
		"a table named by no alias":    {"SELECT customers.| FROM orders o", customers},
	} {
		if got := details(suggestedAt(t, tc.text)); got != tc.want {
			t.Errorf("%s: %q gives %s, want %s", name, tc.text, got, tc.want)
		}
	}
}

func TestAnAliasWinsOverATableOfTheSameName(t *testing.T) {
	const orders = "id (column of orders), total (column of orders)"
	if got := details(suggestedAt(t, "SELECT customers.| FROM orders customers")); got != orders {
		t.Errorf("customers is an alias for orders: %s", got)
	}
	if got := details(suggestedAt(t, "SELECT orders.| FROM customers orders")); !strings.HasPrefix(got, "id (column of customers)") {
		t.Errorf("orders is an alias for customers: %s", got)
	}
}

func TestAnAliasForSeveralTablesListsTheColumnsOfEach(t *testing.T) {
	want := "id (column of orders), total (column of orders), id (column of customers), name (column of customers)"
	if got := details(suggestedAt(t, "SELECT x.| FROM orders x, customers x")); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestOnlyTheStatementTheCursorIsInIsRead(t *testing.T) {
	for name, text := range map[string]string{
		"defined before":  "SELECT * FROM orders o; SELECT o.|",
		"defined after":   "SELECT o.|; SELECT * FROM orders o",
		"between, before": "SELECT * FROM orders o; SELECT o.| FROM customers c; SELECT 1",
	} {
		if got := details(suggestedAt(t, text)); got != "<closed>" {
			t.Errorf("%s: %q offered %s, want nothing: o is not defined in this statement", name, text, got)
		}
	}
	if got := details(suggestedAt(t, "SELECT * FROM customers c; SELECT o.| FROM orders o; SELECT 1")); !strings.HasPrefix(got, "id (column of orders)") {
		t.Errorf("an alias in the cursor's own statement, among others: %s", got)
	}
}

func TestAnAliasForATableNotInTheSchemaOffersNothing(t *testing.T) {
	if got := details(suggestedAt(t, "SELECT z.| FROM nosuch z")); got != "<closed>" {
		t.Errorf("got %s, want nothing", got)
	}
	if got := details(suggestedAt(t, "SELECT orders.| FROM nosuch orders")); got != "<closed>" {
		t.Errorf("an alias for an unknown table, named like a known one: %s", got)
	}
}

func TestAnAliasDoesNotOpenTheListInsideAStringOrAComment(t *testing.T) {
	for _, text := range []string{
		"SELECT * FROM orders o WHERE x = 'o",
		"SELECT * FROM orders o -- o",
		"SELECT * FROM orders o /* o",
	} {
		m, _ := completeModel(t, aliasSchema(), text, -1)
		m = typeText(t, m, ".")
		if m.complete.active() {
			t.Errorf("%q opened %s", text, labels(m))
		}
	}
}

func TestAnAliasIsFoundNearTheCursorOnly(t *testing.T) {
	padding := strings.Repeat("-- padding line\n", 600)

	below := "SELECT o.|\n" + padding + "FROM orders o"
	if got := details(suggestedAt(t, below)); got != "<closed>" {
		t.Errorf("an alias 10 KB below the cursor was read: %s", got)
	}
	above := "SELECT * FROM orders o\n" + padding + "WHERE o.|"
	if got := details(suggestedAt(t, above)); got != "<closed>" {
		t.Errorf("an alias 10 KB above the cursor was read: %s", got)
	}

	near := "SELECT o.|\n-- a few lines\nFROM orders o"
	if got := details(suggestedAt(t, near)); !strings.HasPrefix(got, "id (column of orders)") {
		t.Errorf("an alias a few lines below the cursor: %s", got)
	}
}

func TestAHugeScriptGivesTheAliasFinderOnlyTheWindow(t *testing.T) {
	filler := strings.Repeat("SELECT 1 FROM people p WHERE x = 2 AND y = 3;\n", 2300)
	text := filler + "SELECT * FROM orders o WHERE o."
	m, _ := completeModel(t, aliasSchema(), text, -1)
	ctx := cursorContextOf(m.editor)
	if len(text) < 100_000 || len(ctx.around) > contextBefore+contextAfter {
		t.Errorf("a text of %d bytes gave a window of %d, want at most %d", len(text), len(ctx.around), contextBefore+contextAfter)
	}
	m, _ = send(t, m, altSlash)
	if got := details(m); !strings.HasPrefix(got, "id (column of orders)") {
		t.Errorf("the alias at the cursor in a huge script: %s", got)
	}
}
