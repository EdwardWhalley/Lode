// Several cursors: runs the single-cursor code at each cursor and maps the others through
// each edit.

package editor

import (
	"slices"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
)

// wantedCol is the column ctrl+alt+↑/↓ keeps across short lines.
type wantedCol struct {
	col int
	set bool
}

// caret is a cursor held as offsets into the text: head is where it is and anchor the other
// end of its Selection.
type caret struct {
	anchor, head int
	hasAnchor    bool
	wanted       int
	addCol       wantedCol
}

// orderedCaret is a caret in the order of the text, and whether it is the main cursor or
// has merged into an edit.
type orderedCaret struct {
	caret
	main, gone bool
}

// extent is the part of the text the caret covers: its head if nothing is selected.
func (c caret) extent() (lo, hi int) {
	if c.hasAnchor {
		return min(c.anchor, c.head), max(c.anchor, c.head)
	}
	return c.head, c.head
}

// through is the caret after an edit at a that took r characters out and changed the length
// by delta; swallowed says the edit took out the caret's head or anchor.
func (c caret) through(a, r, delta int) (moved caret, swallowed bool) {
	move := func(o int) int {
		switch {
		case o <= a:
			return o
		case o >= a+r:
			return o + delta
		default:
			swallowed = true
			return a
		}
	}
	c.head = move(c.head)
	if c.hasAnchor {
		c.anchor = move(c.anchor)
		c.hasAnchor = c.anchor != c.head
	}
	return c, swallowed
}

// offset is where p is in the text.
func (m Model) offset(p position) int {
	n := 0
	for r := 0; r < p.row; r++ {
		n += len(m.value[r]) + 1
	}
	return n + p.col
}

// positionAt is the line and column of an offset, clamped to the text.
func (m Model) positionAt(o int) position {
	for r, line := range m.value {
		if o <= len(line) {
			return position{r, max(o, 0)}
		}
		o -= len(line) + 1
	}
	last := len(m.value) - 1
	return position{last, len(m.value[last])}
}

// total is the length of the text.
func (m Model) total() int {
	n := len(m.value) - 1
	for _, line := range m.value {
		n += len(line)
	}
	return n
}

func (m Model) mainCaret() caret {
	c := caret{head: m.offset(m.cursor()), wanted: m.lastCharOffset, addCol: m.addCol}
	if m.hasAnchor {
		c.anchor = m.offset(m.anchor)
		c.hasAnchor = c.anchor != c.head
	}
	return c
}

// loadCaret makes c the main cursor.
func (m *Model) loadCaret(c caret) {
	p := m.positionAt(c.head)
	m.row, m.col = p.row, p.col
	m.hasAnchor = c.hasAnchor
	if c.hasAnchor {
		m.anchor = m.positionAt(c.anchor)
	}
	m.lastCharOffset, m.addCol = c.wanted, c.addCol
}

// allCarets is every cursor, the main one included, in the order of the text.
func (m Model) allCarets() []orderedCaret {
	all := []orderedCaret{{caret: m.mainCaret(), main: true}}
	for _, c := range m.extras {
		all = append(all, orderedCaret{caret: c})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].head < all[j].head })
	return all
}

// cursors is where every cursor is, in the order of the text.
func (m Model) cursors() []position {
	var out []position
	for _, p := range m.allCarets() {
		out = append(out, m.positionAt(p.head))
	}
	return out
}

func (m Model) CursorCount() int { return 1 + len(m.extras) }

// CollapseCursors removes every cursor but the main one, and says whether there were any.
func (m *Model) CollapseCursors() bool {
	had := len(m.extras) > 0
	m.extras = nil
	return had
}

// addCursor adds a cursor on the line above (dir -1) or below (dir 1) every cursor and
// Selection, as the new main one.
func (m *Model) addCursor(dir int) {
	all := m.allCarets()
	base, edge := all[0], 0
	for i, c := range all {
		lo, hi := c.extent()
		if i == 0 || (dir > 0 && hi > edge) || (dir < 0 && lo < edge) {
			base, edge = c, hi
			if dir < 0 {
				edge = lo
			}
		}
	}
	at := m.positionAt(base.head)
	row := m.positionAt(edge).row + dir
	if row < 0 || row >= len(m.value) {
		return
	}
	want := base.addCol
	if !want.set {
		want = wantedCol{col: at.col, set: true}
	}
	added := caret{
		head:   m.offset(position{row, min(want.col, len(m.value[row]))}),
		wanted: max(base.wanted, m.charOffsetAt(at)),
		addCol: want,
	}

	old := m.mainCaret()
	old.addCol = want
	m.extras = append(m.extras, old)
	m.loadCaret(added)
}

// charOffsetAt is the display column of p in its wrapped row: where up and down
// would try to come back to.
func (m Model) charOffsetAt(p position) int {
	m.row, m.col = p.row, p.col
	return m.LineInfo().CharOffset
}

// edit does what msg asks at every cursor. skipTail is as for editOne.
func (m *Model) edit(msg tea.Msg) (skipTail bool) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "alt+ctrl+down":
			m.addCursor(1)
			return false
		case "alt+ctrl+up":
			m.addCursor(-1)
			return false
		case "alt+a":
			m.extras = nil
		}
	}
	switch msg.(type) {
	case tea.KeyMsg, PasteMsg:
		m.forgetWantedColumns()
	}
	if len(m.extras) == 0 {
		return m.editOne(msg)
	}
	return m.editEach(msg)
}

// forgetWantedColumns ends a run of ctrl+alt+↑/↓: the next one starts from where
// the cursor is.
func (m *Model) forgetWantedColumns() {
	m.addCol = wantedCol{}
	for i := range m.extras {
		m.extras[i].addCol = wantedCol{}
	}
}

// editEach runs the single-cursor code at each cursor, first in the text first, mapping the
// others through each edit.
func (m *Model) editEach(msg tea.Msg) (skipTail bool) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+v":
			return true
		case "enter", "ctrl+m":
			if !m.enterFits(m.allCarets()) {
				return true
			}
		}
	}

	all := m.allCarets()
	for i := range all {
		if all[i].gone {
			continue
		}
		m.loadCaret(all[i].caret)
		before := m.total()
		start, _ := all[i].extent()

		m.editOne(msg)

		now := m.mainCaret()
		all[i].caret = now
		if delta := m.total() - before; delta != 0 {
			a := min(start, now.head)
			n := now.head - a
			for j := range all {
				if j == i || all[j].gone {
					continue
				}
				var swallowed bool
				if all[j].caret, swallowed = all[j].caret.through(a, n-delta, delta); swallowed {
					all[j].gone = true
					all[i].main = all[i].main || all[j].main
				}
			}
		}
	}
	m.settle(all)
	return false
}

// enterFits reports whether enter can run at every cursor, in the order editEach runs them.
func (m Model) enterFits(all []orderedCaret) bool {
	lines := len(m.value)
	for _, c := range all {
		if lines >= maxHeight {
			return false
		}
		lo, hi := c.extent()
		lines += 1 - (m.positionAt(hi).row - m.positionAt(lo).row)
	}
	return true
}

// settle merges cursors that meet and Selections that touch, and makes the main cursor and
// the extras from what is left.
func (m *Model) settle(all []orderedCaret) {
	all = slices.DeleteFunc(all, func(c orderedCaret) bool { return c.gone })
	for i := range all {
		all[i].hasAnchor = all[i].hasAnchor && all[i].anchor != all[i].head
	}
	slices.SortStableFunc(all, func(a, b orderedCaret) int {
		alo, ahi := a.extent()
		blo, bhi := b.extent()
		if alo != blo {
			return alo - blo
		}
		return ahi - bhi
	})

	merged := []orderedCaret{all[0]}
	for _, next := range all[1:] {
		cur := &merged[len(merged)-1]
		clo, chi := cur.extent()
		nlo, nhi := next.extent()
		if nlo > chi {
			merged = append(merged, next)
			continue
		}
		lo, hi := clo, max(chi, nhi)
		forward := cur.hasAnchor && cur.anchor <= cur.head
		if next.hasAnchor {
			forward = next.anchor <= next.head
		}
		keep := cur.caret
		if next.main {
			keep = next.caret
		}
		keep.hasAnchor = lo < hi
		if forward {
			keep.anchor, keep.head = lo, hi
		} else {
			keep.anchor, keep.head = hi, lo
		}
		cur.caret = keep
		cur.main = cur.main || next.main
	}

	mainAt := 0
	for i, p := range merged {
		if p.main {
			mainAt = i
		}
	}
	m.extras = nil
	for i, p := range merged {
		if i != mainAt {
			m.extras = append(m.extras, p.caret)
		}
	}
	m.loadCaret(merged[mainAt].caret)
}

// rowGeom is where one wrapped row of a line sits.
type rowGeom struct {
	line    int
	lineLen int
	from    int
	rowLen  int
	drawLen int
}

// marksOnRow is what is drawn specially in one wrapped row: the selected stretches and the
// cursor cells.
func (m Model) marksOnRow(g rowGeom, mainPastEnd bool, heads []orderedCaret, sels []selRange) (ranges [][2]int, cursors []int, pastEnd bool) {
	for _, sp := range sels {
		if lo, hi, ok := coveredColumns(sp.start, sp.end, g.line, g.lineLen); ok {
			if a, b := max(lo-g.from, 0), min(hi-g.from, g.drawLen); a < b {
				ranges = append(ranges, [2]int{a, b})
			}
		}
	}
	for _, h := range heads {
		at := m.positionAt(h.head)
		if at.row != g.line || at.col < g.from || at.col >= g.from+g.rowLen {
			continue
		}
		switch idx := at.col - g.from; {
		case h.main && mainPastEnd:
			pastEnd = true
		case idx < g.drawLen:
			cursors = append(cursors, idx)
		}
	}
	return ranges, cursors, pastEnd
}
