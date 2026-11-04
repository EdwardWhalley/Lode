// Mouse calls on the editor: cells of the screen to cursor positions, Selections and
// scrolling.

package editor

import (
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	rw "github.com/mattn/go-runewidth"
)

// leftFrame is the columns the style draws before the prompt.
func (m Model) leftFrame() int {
	b := m.style().Base
	return b.GetMarginLeft() + b.GetBorderLeftSize() + b.GetPaddingLeft()
}

// cellPosition is the place in the text that the cell (x, y) shows, the nearest end if past
// the text or a line.
func (m Model) cellPosition(x, y int) position {
	drawn := max(y, 0) + m.viewport.YOffset
	cell := max(x-m.leftFrame()-m.promptWidth, 0)

	for r, line := range m.value {
		rows := wrap(line, m.width)
		if drawn >= len(rows) {
			drawn -= len(rows)
			continue
		}
		start := 0
		for _, earlier := range rows[:drawn] {
			start += len(earlier)
		}
		row := rows[drawn]
		used := 0
		for i, c := range row {
			used += rw.RuneWidth(c)
			if cell < used {
				return position{r, start + i}
			}
		}
		if drawn == len(rows)-1 {
			return position{r, len(line)}
		}
		return position{r, start + len(row) - 1}
	}
	last := len(m.value) - 1
	return position{last, len(m.value[last])}
}

// ClickAt puts the cursor at the cell (x, y), dropping the Selection and every other
// cursor.
func (m *Model) ClickAt(x, y int) {
	m.extras, m.addCol = nil, wantedCol{}
	m.collapseTo(m.cellPosition(x, y))
}

// ExtendTo moves the cursor to the cell (x, y), keeping the other end of the Selection:
// shift+click and each step of a drag.
func (m *Model) ExtendTo(x, y int) {
	m.extras, m.addCol = nil, wantedCol{}
	p := m.cellPosition(x, y)
	m.extendSelection(func() {
		m.row = p.row
		m.SetCursor(p.col)
	})
}

// SelectWordAt selects the word at the cell (x, y): a run of letters, digits and
// underscores, of spaces, or of anything else.
func (m *Model) SelectWordAt(x, y int) {
	m.extras, m.addCol = nil, wantedCol{}
	p := m.cellPosition(x, y)
	line := m.value[p.row]
	if len(line) == 0 {
		m.collapseTo(p)
		return
	}
	at := min(p.col, len(line)-1)
	lo, hi := at, at+1
	for lo > 0 && wordClass(line[lo-1]) == wordClass(line[at]) {
		lo--
	}
	for hi < len(line) && wordClass(line[hi]) == wordClass(line[at]) {
		hi++
	}
	m.selectRange(position{p.row, lo}, position{p.row, hi})
}

// SelectLineAt selects the whole line at the cell (x, y), without its line break.
func (m *Model) SelectLineAt(x, y int) {
	m.extras, m.addCol = nil, wantedCol{}
	p := m.cellPosition(x, y)
	m.selectRange(position{p.row, 0}, position{p.row, len(m.value[p.row])})
}

// selectRange selects from start to end, with the cursor at end.
func (m *Model) selectRange(start, end position) {
	m.collapseTo(start)
	m.extendSelection(func() {
		m.row = end.row
		m.SetCursor(end.col)
	})
}

// wordClass says which kind of run r is part of: space, word or anything else.
func wordClass(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return 1
	}
	return 2
}

// scrollsToCursor reports whether Update should scroll the view to the cursor after
// msg: a key, a paste, or a cut.
func scrollsToCursor(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.KeyMsg, PasteMsg, deleteSelectionMsg:
		return true
	}
	return false
}

// maxEdgeScroll is the most rows one step of a drag scrolls.
const maxEdgeScroll = 3

// drawnRows is how many rows the whole text takes, wrapped.
func (m Model) drawnRows() int {
	n := 0
	for _, line := range m.value {
		n += len(wrap(line, m.width))
	}
	return n
}

// ScrollView scrolls the view by rows, leaving the cursor and the Selection where they are.
func (m *Model) ScrollView(rows int) {
	m.viewport.YOffset = clamp(m.viewport.YOffset+rows, 0, max(m.drawnRows()-m.height, 0))
	m.detached = true
}

// DragTo is ExtendTo for a drag: a pointer past the drawn rows scrolls the view toward it.
func (m *Model) DragTo(x, y int) {
	switch {
	case y < 0:
		m.ScrollView(-min(-y, maxEdgeScroll))
	case y >= m.height:
		m.ScrollView(min(y-m.height+1, maxEdgeScroll))
	}
	m.ExtendTo(x, clamp(y, 0, m.height-1))
}
