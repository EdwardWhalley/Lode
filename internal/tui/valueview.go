// The Value view Overlay: one value, whole, in a scrolling window.

package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/EdwardWhalley/lode/internal/export"
)

// valueTabWidth is how many spaces a tab in a value becomes, as in the editor.
const valueTabWidth = 4

// valueView is the Overlay that shows the whole value of the highlighted cell, in its own
// copy.
type valueView struct {
	title  string
	value  any
	text   string
	lines  []string
	width  int
	offset int
}

// newValueView opens value, the cell at row of rows in the named column, in a window of the
// given width.
func newValueView(value any, column string, row, rows, width int) valueView {
	v := valueView{
		title: fmt.Sprintf("%s, row %d of %d", sanitize(column), row+1, rows),
		value: value,
		text:  valueText(value),
	}
	return v.resized(width)
}

// resized wraps the text to a new width; the Model calls it because an Overlay is told the
// width only when drawn.
func (v valueView) resized(width int) valueView {
	if v.lines != nil && width == v.width {
		return v
	}
	v.width = width
	v.lines = wrapValue(v.text, width)
	return v
}

// bodyLines is how many lines of the value show in an Overlay of this height.
func bodyLines(height int) int { return max(height-1, 0) }

func (valueView) scope() scope { return scopeValue }

func (v valueView) update(msg tea.KeyMsg, height int) (overlay, tea.Cmd) {
	body := max(bodyLines(height), 1)
	last := max(len(v.lines)-body, 0)
	v.offset = min(v.offset, last)
	switch msg.String() {
	case "esc", "enter":
		return nil, nil
	case "c", "ctrl+c":
		return v, func() tea.Msg { return copyValueMsg{value: v.value} }
	case "up", "k":
		v.offset--
	case "down", "j":
		v.offset++
	case "pgup":
		v.offset -= body
	case "pgdown":
		v.offset += body
	case "home":
		v.offset = 0
	case "end":
		v.offset = last
	}
	v.offset = max(0, min(v.offset, last))
	return v, nil
}

func (v valueView) view(width, height int) string {
	title := titleStyle.Render(ansi.Truncate(v.title, max(width-lipgloss.Width(titleStyle.Render("")), 0), "…"))
	lines := []string{ansi.Truncate(title, width, "")}
	start := max(0, min(v.offset, len(v.lines)-max(bodyLines(height), 1)))
	for _, line := range v.lines[start:min(start+bodyLines(height), len(v.lines))] {
		lines = append(lines, ansi.Truncate(line, width, ""))
	}
	return fit(lines, height)
}

// copyValueMsg asks the Model to copy what the Value view holds.
type copyValueMsg struct{ value any }

// openValueView opens the Value view on the highlighted cell, if there are rows.
func (m Model) openValueView() Model {
	value, ok := m.highlightedValue()
	if !ok {
		return m
	}
	return m.open(newValueView(value, m.grid.cols[m.grid.hlCol], m.grid.hlRow, len(m.grid.rows), m.width))
}

// valueText is a value as the Value view shows it, safe to print, indented if it is a JSON
// object or array.
func valueText(v any) string {
	switch v := v.(type) {
	case nil:
		return nullText
	case []byte, []any, map[string]any:
		return harmless(indentJSON(export.CellText(v)))
	case string:
		return harmless(indentJSON(v))
	case time.Time:
		return formatValue(v)
	default:
		return harmless(fmt.Sprint(v))
	}
}

// indentJSON indents text that is a JSON object or array, leaving anything else as it is.
func indentJSON(text string) string {
	trimmed := strings.Trim(text, " \t\r\n")
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return text
	}
	if jsonDepth(trimmed) > maxIndentDepth {
		return text
	}
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(trimmed), "", "  "); err != nil {
		return text
	}
	return out.String()
}

// maxIndentDepth is how deep a value is indented, because indenting grows with the square
// of the depth.
const maxIndentDepth = 100

// jsonDepth is how deeply text nests, counting brackets outside its strings.
func jsonDepth(text string) int {
	depth, deepest := 0, 0
	inString, escaped := false, false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case escaped:
			escaped = false
		case inString:
			escaped = c == '\\'
			inString = c != '"'
		case c == '"':
			inString = true
		case c == '{' || c == '[':
			depth++
			deepest = max(deepest, depth)
		case c == '}' || c == ']':
			depth--
		}
	}
	return deepest
}

// harmless makes text safe to print and keeps its line breaks.
func harmless(s string) string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", strings.Repeat(" ", valueTabWidth)).Replace(s)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = sanitize(line)
	}
	return strings.Join(lines, "\n")
}

// wrapValue cuts text into lines of at most width cells, dropping nothing and never cutting
// a wide character.
func wrapValue(text string, width int) []string {
	width = max(width, 1)
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if ansi.StringWidth(line) <= width {
			out = append(out, line)
			continue
		}
		var row strings.Builder
		cells := 0
		state := -1
		for len(line) > 0 {
			var cluster string
			cluster, line, _, state = uniseg.FirstGraphemeClusterInString(line, state)
			w := ansi.StringWidth(cluster)
			if cells+w > width && cells > 0 {
				out = append(out, row.String())
				row.Reset()
				cells = 0
			}
			row.WriteString(cluster)
			cells += w
		}
		out = append(out, row.String())
	}
	return out
}
