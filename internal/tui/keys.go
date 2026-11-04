// The key registry, from which the hint row, the help and the README table come.

package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// scope is where a key hint is shown: a focused pane, or an open Overlay.
type scope int

const (
	scopeEditor scope = iota
	scopeGrid
	scopeSchema
	scopeHelp
	scopeSelector
	scopeExport
	scopeHistory
	scopeComplete
	scopePicker
	scopeForm
	scopeFormTesting
	scopeFormFailed
	scopeFormSaved
	scopeFormSavedOpen
	scopeConfirm
	scopeHome
	scopeValue
)

// hint is the short form of a binding shown in the hint row while scope is active.
type hint struct {
	scope scope
	key   string
	text  string
}

// binding is one entry in the key registry, the source of truth for the hint row, the F1
// help and the README key table.
type binding struct {
	keys  string
	desc  string
	group string
	hints []hint
	late  bool
}

const (
	groupGlobal = "Global"
	groupPanes  = "Results grid and schema tree"
	groupEditor = "Editor"
	groupMouse  = "Mouse"
)

// bindings lists every key in README table order, F1 first so the hint row never drops it.
var bindings = []binding{
	{
		keys: "F1", desc: "Show every key (press again, or esc, to close)", group: groupGlobal,
		hints: []hint{
			{scopeEditor, "F1", "help"}, {scopeGrid, "F1", "help"}, {scopeSchema, "F1", "help"},
			{scopeHelp, "F1/esc", "close"},
		},
	},
	{
		keys: "ctrl+r / f5", desc: "Run the Selection, else the Statement under the cursor", group: groupGlobal,
		hints: []hint{{scopeEditor, "ctrl+r", "run"}, {scopeGrid, "ctrl+r", "run"}, {scopeSchema, "ctrl+r", "run"}},
	},
	{keys: "esc / ctrl+c", desc: "Cancel a query; ctrl+c copies a Selection or grid cell, else quits", group: groupGlobal},
	{
		keys: "tab", desc: "Cycle focus: editor → results grid → schema tree", group: groupGlobal,
		hints: []hint{{scopeEditor, "tab", "focus"}, {scopeGrid, "tab", "focus"}, {scopeSchema, "tab", "focus"}},
	},
	{
		keys: "c / r", desc: "Grid: copy the highlighted cell / its row", group: groupPanes, late: true,
		hints: []hint{{scopeGrid, "c", "copy"}, {scopeGrid, "r", "row"}, {scopeValue, "c", "copy"}},
	},
	{
		keys: "enter", desc: "Grid: show the whole value of the highlighted cell", group: groupPanes, late: true,
		hints: []hint{{scopeGrid, "enter", "view"}, {scopeValue, "esc", "close"}},
	},
	{
		keys: "arrows, pgup/pgdn, home/end", desc: "Grid: move the Highlight (h j k l too); tree: move", group: groupPanes, late: true,
		hints: []hint{{scopeGrid, "↑↓←→", "move"}, {scopeSchema, "↑↓", "move"}, {scopeHelp, "↑↓", "scroll"}, {scopeValue, "↑↓", "scroll"}},
	},
	{
		keys: "enter / space", desc: "Expand or collapse a table in the schema tree", group: groupPanes, late: true,
		hints: []hint{{scopeSchema, "enter", "expand"}},
	},
	{
		keys: "ctrl+o", desc: "Switch Source: ↑/↓ and home/end move, enter switches, esc cancels", group: groupGlobal,
		hints: []hint{
			{scopeEditor, "ctrl+o", "source"}, {scopeGrid, "ctrl+o", "source"}, {scopeSchema, "ctrl+o", "source"},
			{scopeSelector, "↑↓", "move"}, {scopeSelector, "enter", "switch"}, {scopeSelector, "esc", "cancel"},
		},
	},
	{
		keys: "n", desc: "Selector: create a Source (type, form, test, save)", group: groupGlobal,
		hints: []hint{
			{scopeSelector, "n", "new"},
			{scopePicker, "↑↓", "move"}, {scopePicker, "enter", "choose"}, {scopePicker, "esc", "back"},
			{scopeForm, "tab", "next field"}, {scopeForm, "enter", "next / test and save"}, {scopeForm, "esc", "back"},
			{scopeFormTesting, "esc", "cancel the test"},
			{scopeFormFailed, "esc", "back"},
			{scopeFormSaved, "enter", "switch to it"}, {scopeFormSaved, "esc", "stay"},
		},
	},
	{
		keys: "e", desc: "Selector: edit the highlighted Source (if in sources.toml)", group: groupGlobal,
		hints: []hint{
			{scopeSelector, "e", "edit"},
			{scopeFormSavedOpen, "enter", "back"}, {scopeFormSavedOpen, "esc", "back"},
		},
	},
	{
		keys: "d", desc: "Selector: delete the highlighted Source, after asking", group: groupGlobal,
		hints: []hint{
			{scopeSelector, "d", "delete"},
			{scopeConfirm, "enter", "delete"}, {scopeConfirm, "esc", "cancel"},
		},
	},
	{
		keys: "ctrl+s in the Source form", desc: "After a failed test: save the Source anyway", group: groupGlobal,
		hints: []hint{{scopeFormFailed, "ctrl+s", "save anyway"}},
	},
	{
		keys: "ctrl+s", desc: "Export the rows shown to .csv or .json; never overwrites", group: groupGlobal, late: true,
		hints: []hint{
			{scopeEditor, "ctrl+s", "export"}, {scopeGrid, "ctrl+s", "export"}, {scopeSchema, "ctrl+s", "export"},
			{scopeExport, "enter", "save"}, {scopeExport, "esc", "cancel"},
		},
	},
	{
		keys: "ctrl+↑ / ctrl+↓", desc: "Editor: step through this Source's history (alt+↑/↓ too)", group: groupGlobal, late: true,
		hints: []hint{{scopeEditor, "ctrl+↑↓", "history"}},
	},
	{
		keys: "ctrl+g", desc: "Search history of all Sources; enter loads a query, never runs it", group: groupGlobal, late: true,
		hints: []hint{
			{scopeEditor, "ctrl+g", "find"}, {scopeGrid, "ctrl+g", "find"}, {scopeSchema, "ctrl+g", "find"},
			{scopeHistory, "type", "filter"}, {scopeHistory, "↑↓", "move"}, {scopeHistory, "enter", "load"}, {scopeHistory, "esc", "cancel"},
		},
	},
	{
		keys: "alt+/", desc: "Editor: open suggestions now (they also open as you type)", group: groupGlobal, late: true,
		hints: []hint{
			{scopeEditor, "alt+/", "complete"},
			{scopeComplete, "tab", "accept"}, {scopeComplete, "↑↓", "move"}, {scopeComplete, "esc", "close"},
		},
	},
	{
		keys: "F2", desc: "Next colour theme (keep one with theme = in sources.toml or --theme)", group: groupGlobal, late: true,
		hints: []hint{{scopeEditor, "F2", "theme"}, {scopeGrid, "F2", "theme"}, {scopeSchema, "F2", "theme"}},
	},
	{
		keys: "shift+arrows, shift+home/end", desc: "Editor: extend the Selection by character, row, or to the line ends", group: groupEditor, late: true,
		hints: []hint{{scopeEditor, "shift+arrows", "select"}},
	},
	{
		keys: "ctrl+shift+←/→", desc: "Editor: extend the Selection by word", group: groupEditor, late: true,
	},
	{
		keys: "ctrl+shift+home/end", desc: "Editor: extend the Selection to the start or end of the text", group: groupEditor, late: true,
	},
	{
		keys: "ctrl+←/→", desc: "Editor: move by word (alt+←/→ too)", group: groupEditor, late: true,
	},
	{
		keys: "ctrl+alt+↑/↓", desc: "Editor: add a cursor above / below (esc removes the extras)", group: groupEditor, late: true,
		hints: []hint{{scopeEditor, "ctrl+alt+↑↓", "cursors"}},
	},
	{
		keys: "alt+a", desc: "Editor: select all", group: groupEditor, late: true,
		hints: []hint{{scopeEditor, "alt+a", "select all"}},
	},
	{
		keys: "ctrl+z / ctrl+y", desc: "Editor: undo / redo; a run of typing is one step", group: groupEditor, late: true,
		hints: []hint{{scopeEditor, "ctrl+z", "undo"}},
	},
	{
		keys: "ctrl+x", desc: "Editor: cut the Selection (nothing happens with none)", group: groupEditor, late: true,
	},
	{
		keys: "ctrl+v", desc: "Editor: paste, replacing the Selection", group: groupEditor, late: true,
	},
	{
		keys: "click", desc: "Editor: place the cursor; grid: move the Highlight; tree: focus it", group: groupMouse, late: true,
	},
	{
		keys: "drag", desc: "Editor: select; past an edge it scrolls and keeps selecting", group: groupMouse, late: true,
	},
	{
		keys: "double-click / triple-click", desc: "Editor: select the word / the line", group: groupMouse, late: true,
	},
	{
		keys: "shift+click", desc: "Editor: extend the Selection from where it began", group: groupMouse, late: true,
	},
	{
		keys: "drag a border", desc: "Resize the panes by dragging the border between them", group: groupMouse, late: true,
	},
	{
		keys: "wheel", desc: "Scroll the pane under the pointer; the cursor stays put", group: groupMouse, late: true,
	},
	{
		keys: "any key", desc: "Home page: continue to the Source selector (ctrl+c quits)", group: groupGlobal,
		hints: []hint{{scopeHome, "any key", "continue"}},
	},
	{
		keys: "ctrl+q", desc: "Quit", group: groupGlobal,
		hints: []hint{
			{scopeEditor, "ctrl+q", "quit"}, {scopeGrid, "ctrl+q", "quit"}, {scopeSchema, "ctrl+q", "quit"},
			{scopeHome, "ctrl+q", "quit"},
		},
	},
}

// hintRow renders the hints for scope on one line of at most width columns, dropping the
// extra ones first.
func hintRow(s scope, width int) string {
	const sep = " · "
	var line string
	for _, wantLate := range []bool{false, true} {
		for _, b := range bindings {
			for _, h := range b.hints {
				if h.scope != s || b.late != wantLate {
					continue
				}
				item := h.key + " " + h.text
				next := item
				if line != "" {
					next = line + sep + item
				}
				if ansi.StringWidth(next) > width {
					return dimStyle.Render(line)
				}
				line = next
			}
		}
	}
	return dimStyle.Render(line)
}

// groupedBindings returns the bindings by group, in first-appearance order.
func groupedBindings() (names []string, byGroup map[string][]binding) {
	byGroup = map[string][]binding{}
	for _, b := range bindings {
		if _, seen := byGroup[b.group]; !seen {
			names = append(names, b.group)
		}
		byGroup[b.group] = append(byGroup[b.group], b)
	}
	return names, byGroup
}

// helpLines is the text of the help overlay, one entry per line.
func helpLines(width int) []string {
	keyWidth := 0
	for _, b := range bindings {
		keyWidth = max(keyWidth, ansi.StringWidth(b.keys))
	}
	names, byGroup := groupedBindings()
	var lines []string
	for i, name := range names {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, titleStyle.Render(name))
		for _, b := range byGroup[name] {
			pad := strings.Repeat(" ", keyWidth-ansi.StringWidth(b.keys))
			lines = append(lines, ansi.Truncate("  "+b.keys+pad+"  "+b.desc, width, "…"))
		}
	}
	return lines
}
