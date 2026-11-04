// The schema tree pane.

package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

// tree is the schema pane: tables that expand to show their columns.
type tree struct {
	tables   []adapter.Table
	expanded map[string]bool
	loadErr  string

	cursor int
	off    int
	width  int
	height int
}

// item is one visible line: a table (col == -1) or one of its columns.
type item struct {
	table int
	col   int
}

func (t *tree) setSize(w, h int) {
	t.width, t.height = w, h
	t.clamp()
}

func (t *tree) setTables(tables []adapter.Table) {
	t.tables = tables
	t.expanded = map[string]bool{}
	t.loadErr = ""
	t.cursor, t.off = 0, 0
}

// items is the rows of the tree: every table, with the columns of the expanded ones.
func (t *tree) items() []item {
	var out []item
	for i, tb := range t.tables {
		out = append(out, item{table: i, col: -1})
		if t.expanded[tb.Name] {
			for j := range tb.Columns {
				out = append(out, item{table: i, col: j})
			}
		}
	}
	return out
}

func (t *tree) clamp() {
	n := len(t.items())
	t.cursor = max(0, min(t.cursor, n-1))
	if t.cursor < t.off {
		t.off = t.cursor
	}
	if h := max(t.height, 1); t.cursor >= t.off+h {
		t.off = t.cursor - h + 1
	}
	t.off = max(0, min(t.off, max(n-t.height, 0)))
}

// selectedTable returns the table the cursor is on or inside, if any.
func (t *tree) selectedTable() (adapter.Table, bool) {
	items := t.items()
	if t.cursor >= len(items) {
		return adapter.Table{}, false
	}
	return t.tables[items[t.cursor].table], true
}

// scroll moves the window down by rows, or up when negative, keeping the cursor in it.
func (t *tree) scroll(rows int) {
	t.off = max(0, min(t.off+rows, len(t.items())-t.height))
	t.cursor = max(t.off, min(t.cursor, t.off+t.height-1))
	t.clamp()
}

func (t *tree) update(msg tea.KeyMsg) {
	switch msg.String() {
	case "up", "k":
		t.cursor--
	case "down", "j":
		t.cursor++
	case "home":
		t.cursor = 0
	case "end":
		t.cursor = len(t.items())
	case "enter", " ", "right", "l":
		t.toggle(msg.String() != "right" && msg.String() != "l")
	case "left", "h":
		t.collapse()
	}
	t.clamp()
}

// toggle expands the table under the cursor; with flip it collapses an open one.
func (t *tree) toggle(flip bool) {
	tb, ok := t.selectedTable()
	if !ok {
		return
	}
	if t.expanded[tb.Name] && flip {
		t.collapse()
		return
	}
	t.expanded[tb.Name] = true
}

// collapse closes the table under the cursor and moves the cursor onto it.
func (t *tree) collapse() {
	items := t.items()
	if t.cursor >= len(items) {
		return
	}
	cur := items[t.cursor]
	t.expanded[t.tables[cur.table].Name] = false
	for i, it := range t.items() {
		if it.table == cur.table && it.col == -1 {
			t.cursor = i
		}
	}
}

func (t *tree) view(focused bool) string {
	lines := make([]string, 0, t.height)

	switch {
	case t.loadErr != "":
		lines = append(lines, errStyle.Render(ansi.Truncate("schema: "+t.loadErr, t.width, "…")))
	case len(t.tables) == 0:
		lines = append(lines, dimStyle.Render("no tables"))
	}

	items := t.items()
	for i := t.off; i < len(items) && len(lines) < t.height; i++ {
		it := items[i]
		tb := t.tables[it.table]

		var text string
		if it.col == -1 {
			marker := "▸ "
			if t.expanded[tb.Name] {
				marker = "▾ "
			}
			text = marker + sanitize(tb.Name)
		} else {
			c := tb.Columns[it.col]
			text = "    " + sanitize(c.Name)
			if c.Type != "" {
				text += " " + typeStyle.Render(sanitize(strings.ToLower(c.Type)))
			}
		}
		text = ansi.Truncate(text, t.width, "…")
		if pad := t.width - ansi.StringWidth(text); pad > 0 {
			text += strings.Repeat(" ", pad)
		}
		if focused && i == t.cursor {
			text = highlightStyle.Render(ansi.Strip(text))
		}
		lines = append(lines, text)
	}
	for len(lines) < t.height {
		lines = append(lines, strings.Repeat(" ", t.width))
	}
	return strings.Join(lines, "\n")
}
