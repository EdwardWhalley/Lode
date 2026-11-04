// The Selection: the text between an anchor and the cursor, and the keys that make,
// collapse or remove one.

package editor

import (
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// position is a place in the text: a line and a column in it.
type position struct{ row, col int }

func (p position) before(q position) bool {
	return p.row < q.row || (p.row == q.row && p.col < q.col)
}

func (m Model) cursor() position { return position{m.row, m.col} }

// selection returns the ends of the Selection, front first.
func (m Model) selection() (start, end position, ok bool) {
	cur := m.cursor()
	if !m.hasAnchor || m.anchor == cur {
		return position{}, position{}, false
	}
	if cur.before(m.anchor) {
		return cur, m.anchor, true
	}
	return m.anchor, cur, true
}

func (m Model) HasSelection() bool {
	if len(m.extras) > 0 {
		return len(m.selections()) > 0
	}
	_, _, ok := m.selection()
	return ok
}

// selRange is a stretch of the text from start up to but not including end.
type selRange struct{ start, end position }

// selections is every Selection, in the order of the text.
func (m Model) selections() []selRange {
	var out []selRange
	for _, c := range m.allCarets() {
		if c.hasAnchor && c.anchor != c.head {
			lo, hi := c.extent()
			out = append(out, selRange{m.positionAt(lo), m.positionAt(hi)})
		}
	}
	return out
}

// ClearSelection leaves the text and the cursor as they are and selects nothing.
func (m *Model) ClearSelection() { m.hasAnchor = false }

// SelectedText is the text of every Selection, in the order of the text, joined by a
// newline.
func (m Model) SelectedText() string {
	if len(m.extras) > 0 {
		var parts []string
		for _, sp := range m.selections() {
			parts = append(parts, m.textBetween(sp.start, sp.end))
		}
		return strings.Join(parts, "\n")
	}
	start, end, ok := m.selection()
	if !ok {
		return ""
	}
	return m.textBetween(start, end)
}

func (m Model) textBetween(start, end position) string {
	if start.row == end.row {
		return string(m.value[start.row][start.col:end.col])
	}
	parts := []string{string(m.value[start.row][start.col:])}
	for r := start.row + 1; r < end.row; r++ {
		parts = append(parts, string(m.value[r]))
	}
	parts = append(parts, string(m.value[end.row][:end.col]))
	return strings.Join(parts, "\n")
}

// deleteSelection removes the Selection, if there is one, and puts the cursor
// where it began.
func (m *Model) deleteSelection() {
	start, end, ok := m.selection()
	m.hasAnchor = false
	if !ok {
		return
	}
	if start.row == end.row {
		m.value[start.row] = append(m.value[start.row][:start.col], m.value[start.row][end.col:]...)
	} else {
		head := append([]rune(nil), m.value[start.row][:start.col]...)
		m.value[start.row] = append(head, m.value[end.row][end.col:]...)
		m.value = append(m.value[:start.row+1], m.value[end.row+1:]...)
	}
	m.row = start.row
	m.SetCursor(start.col)
}

// deleteSelectionMsg asks Update to remove every Selection and do nothing else.
type deleteSelectionMsg struct{}

// DeleteSelection removes every Selection, at every cursor; it is what cut does once the
// text is copied.
func (m *Model) DeleteSelection() { *m, _ = m.Update(deleteSelectionMsg{}) }

// collapseTo drops the Selection and puts the cursor at p.
func (m *Model) collapseTo(p position) {
	m.hasAnchor = false
	m.row = p.row
	m.SetCursor(p.col)
}

// selectedColumns is the part of line l that the Selection covers, as columns lo up to but
// not including hi.
func (m Model) selectedColumns(l, lineLen int) (lo, hi int, ok bool) {
	start, end, has := m.selection()
	if !has {
		return 0, 0, false
	}
	return coveredColumns(start, end, l, lineLen)
}

// coveredColumns is the part of line l between start and end, with a line break passed
// counted as one cell.
func coveredColumns(start, end position, l, lineLen int) (lo, hi int, ok bool) {
	if l < start.row || l > end.row {
		return 0, 0, false
	}
	lo, hi = 0, lineLen+1
	if l == start.row {
		lo = start.col
	}
	if l == end.row {
		hi = end.col
	}
	return lo, hi, lo < hi
}

// selectedRow draws one wrapped row, with ranges drawn as selected and each cell in cursors
// as a cursor.
func (m Model) selectedRow(row []rune, ranges [][2]int, cursors []int, pastEnd bool, rowStyle lipgloss.Style) string {
	sel := m.style().computedSelection()
	var out strings.Builder
	var run []rune
	var runSelected bool
	flush := func() {
		if len(run) > 0 {
			style := rowStyle
			if runSelected {
				style = sel
			}
			out.WriteString(style.Render(string(run)))
			run = run[:0]
		}
	}
	for i, r := range row {
		if slices.Contains(cursors, i) {
			flush()
			m.Cursor.SetChar(string(r))
			out.WriteString(rowStyle.Render(m.Cursor.View()))
			continue
		}
		selected := slices.ContainsFunc(ranges, func(rg [2]int) bool { return i >= rg[0] && i < rg[1] })
		if len(run) > 0 && selected != runSelected {
			flush()
		}
		runSelected = selected
		run = append(run, r)
	}
	flush()
	if pastEnd {
		m.Cursor.SetChar(" ")
		out.WriteString(m.Cursor.View())
	}
	return out.String()
}

// extendSelection moves the cursor with move and keeps the other end of the Selection where
// it was.
func (m *Model) extendSelection(move func()) {
	if !m.hasAnchor {
		m.anchor, m.hasAnchor = m.cursor(), true
	}
	move()
	if m.anchor == m.cursor() {
		m.hasAnchor = false
	}
}

// selectAll selects the whole text, with the cursor at its end.
func (m *Model) selectAll() {
	m.hasAnchor = false
	m.moveToBegin()
	m.extendSelection(m.moveToEnd)
}

// cursorUpOrToStart is up for a Selection: from the first row it goes to the start.
func (m *Model) cursorUpOrToStart() {
	if m.row == 0 && m.LineInfo().RowOffset == 0 {
		m.CursorStart()
		return
	}
	m.CursorUp()
}

// cursorDownOrToEnd is down for a Selection: on the last row of the text it goes
// to the end.
func (m *Model) cursorDownOrToEnd() {
	if li := m.LineInfo(); m.row == len(m.value)-1 && li.RowOffset+1 >= li.Height {
		m.CursorEnd()
		return
	}
	m.CursorDown()
}

// selectionKey handles the keys that make, collapse or remove a Selection; done says the
// key needs nothing more.
func (m *Model) selectionKey(key string) (done bool) {
	switch key {
	case "shift+left":
		m.extendSelection(func() { m.characterLeft(false) })
	case "shift+right":
		m.extendSelection(m.characterRight)
	case "shift+up":
		m.extendSelection(m.cursorUpOrToStart)
	case "shift+down":
		m.extendSelection(m.cursorDownOrToEnd)
	case "shift+home":
		m.extendSelection(m.CursorStart)
	case "shift+end":
		m.extendSelection(m.CursorEnd)
	case "ctrl+shift+left":
		m.extendSelection(m.wordLeft)
	case "ctrl+shift+right":
		m.extendSelection(m.wordRight)
	case "ctrl+shift+home":
		m.extendSelection(m.moveToBegin)
	case "ctrl+shift+end":
		m.extendSelection(m.moveToEnd)
	case "alt+a":
		m.selectAll()
	default:
		start, end, ok := m.selection()
		if !ok {
			return false
		}
		switch key {
		case "backspace", "ctrl+h", "delete", "ctrl+d", "alt+backspace", "ctrl+w",
			"alt+delete", "alt+d", "ctrl+k", "ctrl+u":
			m.deleteSelection()
		case "left", "ctrl+b":
			m.collapseTo(start)
		case "right", "ctrl+f":
			m.collapseTo(end)
		case "enter", "ctrl+m":
			if len(m.value) >= maxHeight {
				return true
			}
			m.deleteSelection()
			return false
		case "up", "ctrl+p", "down", "ctrl+n", "home", "ctrl+a", "end", "ctrl+e",
			"alt+left", "alt+b", "ctrl+left", "alt+right", "alt+f", "ctrl+right",
			"alt+<", "ctrl+home", "alt+>", "ctrl+end", "alt+l", "alt+u", "alt+c", "ctrl+t":
			m.hasAnchor = false
			return false
		default:
			return false
		}
	}
	return true
}
