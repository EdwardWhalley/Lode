// The results grid: a window over a result's display strings, with the Highlight.

package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	maxColWidth = 40
	columnGap   = 2
)

// grid is a scrollable table of already-formatted cells, drawing only the window; the
// Highlight is one cell, which the window follows.
type grid struct {
	cols   []string
	rows   [][]string
	widths []int
	rowOff int
	colOff int
	hlRow  int
	hlCol  int
	width  int
	height int
}

func (g *grid) setSize(w, h int) {
	inView := g.highlightInView()
	g.width, g.height = w, h
	g.clamp()
	if inView {
		g.follow()
	}
}

func (g *grid) setData(cols []string, rows [][]string) {
	g.cols, g.rows = cols, rows
	g.rowOff, g.colOff = 0, 0
	g.hlRow, g.hlCol = 0, 0
	g.widths = make([]int, len(cols))
	for i, c := range cols {
		g.widths[i] = ansi.StringWidth(c)
	}
	for _, row := range g.rows[:min(len(g.rows), 200)] {
		for i, cell := range row {
			g.widths[i] = max(g.widths[i], ansi.StringWidth(cell))
		}
	}
	for i := range g.widths {
		g.widths[i] = min(g.widths[i], maxColWidth)
	}
}

func (g *grid) bodyHeight() int { return max(g.height-1, 0) }

func (g *grid) clamp() {
	g.rowOff = max(0, min(g.rowOff, len(g.rows)-g.bodyHeight()))
	g.colOff = max(0, min(g.colOff, len(g.cols)-1))
}

// scroll moves the window down by rows, or up when negative.
func (g *grid) scroll(rows int) {
	g.rowOff += rows
	g.clamp()
}

func (g *grid) update(msg tea.KeyMsg) {
	if !g.hasHighlight() {
		switch msg.String() {
		case "left", "h":
			g.colOff--
		case "right", "l":
			g.colOff++
		}
		g.clamp()
		return
	}
	page := max(g.bodyHeight()-1, 1)
	switch msg.String() {
	case "up", "k":
		g.hlRow--
	case "down", "j":
		g.hlRow++
	case "pgup":
		g.hlRow -= page
	case "pgdown":
		g.hlRow += page
	case "home":
		g.hlRow = 0
	case "end":
		g.hlRow = len(g.rows) - 1
	case "left", "h":
		g.hlCol--
	case "right", "l":
		g.hlCol++
	default:
		return
	}
	g.hlRow = max(0, min(g.hlRow, len(g.rows)-1))
	g.hlCol = max(0, min(g.hlCol, len(g.cols)-1))
	g.follow()
}

// hasHighlight is false when there are no rows, so nothing suggests a value to copy.
func (g *grid) hasHighlight() bool { return len(g.rows) > 0 && len(g.cols) > 0 }

// highlightInView says the Highlight is in the window.
func (g *grid) highlightInView() bool {
	return g.hasHighlight() && g.hlRow >= g.rowOff && g.hlRow < g.rowOff+g.bodyHeight() &&
		slices.Contains(g.visibleCols(), g.hlCol)
}

// follow moves the window just far enough to show the Highlight.
func (g *grid) follow() {
	g.rowOff = max(min(g.rowOff, g.hlRow), g.hlRow-max(g.bodyHeight(), 1)+1)
	g.colOff = min(g.colOff, g.hlCol)
	for !slices.Contains(g.visibleCols(), g.hlCol) {
		g.colOff++
	}
	g.clamp()
}

// click moves the Highlight to the cell at (x, y), counted from the grid's corner; a column
// owns the gap on its right.
func (g *grid) click(x, y int) {
	row := g.rowOff + y - 1
	if !g.hasHighlight() || y < 1 || row >= len(g.rows) {
		return
	}
	right := 0
	for _, i := range g.visibleCols() {
		right += g.widths[i] + columnGap
		if x < right {
			g.hlRow, g.hlCol = row, i
			return
		}
	}
}

// visibleCols are the columns that fit, starting at colOff; the first always does.
func (g *grid) visibleCols() []int {
	var visible []int
	used := 0
	for i := g.colOff; i < len(g.cols); i++ {
		w := g.widths[i]
		if len(visible) > 0 && used+w+columnGap > g.width {
			break
		}
		visible = append(visible, i)
		used += w + columnGap
	}
	return visible
}

// view draws the window, with the Highlight only while the grid has the focus.
func (g *grid) view(focused bool) string {
	if len(g.cols) == 0 {
		return strings.Repeat("\n", max(g.height-1, 0))
	}
	visible := g.visibleCols()

	line := func(cell func(i int) string, style func(i int, s, pad string) string) string {
		parts := make([]string, len(visible))
		for n, i := range visible {
			s := ansi.Truncate(cell(i), g.widths[i], "…")
			pad := strings.Repeat(" ", max(g.widths[i]-ansi.StringWidth(s), 0))
			parts[n] = style(i, s, pad)
		}
		return ansi.Truncate(strings.Join(parts, strings.Repeat(" ", columnGap)), g.width, "")
	}

	header := func(_ int, s, pad string) string { return headerStyle.Render(s) + pad }
	lines := []string{line(func(i int) string { return g.cols[i] }, header)}
	end := min(g.rowOff+g.bodyHeight(), len(g.rows))
	for r := g.rowOff; r < end; r++ {
		row := g.rows[r]
		lines = append(lines, line(
			func(i int) string { return row[i] },
			func(i int, s, pad string) string {
				switch {
				case focused && r == g.hlRow && i == g.hlCol:
					return highlightStyle.Render(s + pad)
				case s == nullText:
					return nullStyle.Render(s) + pad
				}
				return s + pad
			},
		))
	}
	for len(lines) < g.height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// position describes the visible window, e.g. "rows 1-20 of 3000".
func (g *grid) position() string {
	if len(g.rows) == 0 {
		return "no rows"
	}
	last := min(g.rowOff+g.bodyHeight(), len(g.rows))
	return "rows " + itoa(g.rowOff+1) + "-" + itoa(last) + " of " + itoa(len(g.rows))
}
