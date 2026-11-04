package tui

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	ewResize   = "\x1b]22;ew-resize\x1b\\"
	nsResize   = "\x1b]22;ns-resize\x1b\\"
	shapeReset = "\x1b]22;\x1b\\"
)

func TestAPointerShapeIsWrittenOnlyWhenItChanges(t *testing.T) {
	var out bytes.Buffer
	p := NewPointerShapes(&out)

	runCmd(p.want(shapeColumnResize))
	if out.String() != ewResize {
		t.Fatalf("wrote %q, want %q", out.String(), ewResize)
	}
	if cmd := p.want(shapeColumnResize); cmd != nil {
		t.Error("asking again for the shape it has gave a command")
	}
	out.Reset()
	runCmd(p.want(shapeRowResize))
	runCmd(p.want(shapeDefault))
	if out.String() != nsResize+shapeReset {
		t.Errorf("wrote %q, want %q", out.String(), nsResize+shapeReset)
	}
}

func TestAnOvertakenShapeIsNotWritten(t *testing.T) {
	var out bytes.Buffer
	p := NewPointerShapes(&out)
	enter := p.want(shapeColumnResize)
	leave := p.want(shapeDefault)
	runCmd(leave)
	runCmd(enter)
	if out.String() != shapeReset {
		t.Errorf("wrote %q, want only the reset", out.String())
	}
}

func TestRestoreResetsThePointerOnlyIfItWasChanged(t *testing.T) {
	var out bytes.Buffer
	p := NewPointerShapes(&out)
	p.Restore()
	if out.Len() != 0 {
		t.Errorf("Restore with nothing changed wrote %q", out.String())
	}
	runCmd(p.want(shapeRowResize))
	out.Reset()
	p.Restore()
	if out.String() != shapeReset {
		t.Errorf("Restore wrote %q, want %q", out.String(), shapeReset)
	}
	late := p.want(shapeColumnResize)
	p.Restore()
	out.Reset()
	runCmd(late)
	if out.Len() != 0 {
		t.Errorf("a late command wrote %q after Restore", out.String())
	}
}

func TestANilPointerShapesDoesNothing(t *testing.T) {
	var p *PointerShapes
	if cmd := p.want(shapeColumnResize); cmd != nil {
		t.Error("a nil PointerShapes gave a command")
	}
	p.Restore()
}

// shapesModel is a Model with a pointer writer.
func shapesModel(t *testing.T) (Model, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	m := mouseModel(t, "select 1")
	m.shapes = NewPointerShapes(&out)
	return m, &out
}

func moveAndRun(t *testing.T, m Model, msgs ...tea.MouseMsg) Model {
	t.Helper()
	for _, msg := range msgs {
		var cmd tea.Cmd
		m, cmd = send(t, m, msg)
		runCmd(cmd)
	}
	return m
}

func TestHoveringABorderAsksForTheResizePointer(t *testing.T) {
	m, out := shapesModel(t)
	m = moveAndRun(t, m, hover(50, 10), hover(separatorCol, 10), hover(separatorCol, 12))
	if out.String() != ewResize {
		t.Errorf("over the separator wrote %q, want %q once", out.String(), ewResize)
	}
	out.Reset()
	m = moveAndRun(t, m, hover(50, dividerRow), hover(60, dividerRow))
	if out.String() != nsResize {
		t.Errorf("over the divider wrote %q, want %q once", out.String(), nsResize)
	}
	out.Reset()
	m = moveAndRun(t, m, hover(separatorCol, dividerRow))
	m = moveAndRun(t, m, hover(50, 10))
	if out.String() != shapeReset {
		t.Errorf("leaving the borders wrote %q, want one reset", out.String())
	}
}

func TestTheResizePointerStaysForTheWholeDrag(t *testing.T) {
	m, out := shapesModel(t)
	m = moveAndRun(t, m, hover(separatorCol, 10), press(separatorCol, 10), motion(95, 10), motion(97, 20))
	if out.String() != ewResize {
		t.Errorf("during the drag wrote %q, want %q once", out.String(), ewResize)
	}
	out.Reset()
	m = moveAndRun(t, m, release(97, 20))
	if out.String() != shapeReset {
		t.Errorf("a release away from the border wrote %q, want a reset", out.String())
	}
}

func TestOpeningAnOverlayGivesThePointerBack(t *testing.T) {
	m, out := shapesModel(t)
	m = moveAndRun(t, m, hover(separatorCol, 10))
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyF1})
	out.Reset()
	m = moveAndRun(t, m, hover(separatorCol, 11))
	if out.String() != shapeReset {
		t.Errorf("a motion over an overlay wrote %q, want a reset", out.String())
	}
}

// cellAt is what the screen shows at (x, y), for rows of characters one cell wide.
func cellAt(m Model, x, y int) string {
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	r := []rune(rows[y])
	if x >= len(r) {
		return ""
	}
	return string(r[x])
}

// arrows counts the resize arrows on the screen.
func arrows(m Model) int {
	v := ansi.Strip(m.View())
	return strings.Count(v, "↔") + strings.Count(v, "↕")
}

func TestHoveringTheSeparatorDrawsAnArrowAtThePointer(t *testing.T) {
	m := mouseModel(t, "select 1")
	plain := m.View()
	m = mouse(t, m, hover(separatorCol, 10))
	if got := cellAt(m, separatorCol, 10); got != "↔" {
		t.Errorf("the separator at the pointer shows %q, want ↔", got)
	}
	if got := cellAt(m, separatorCol, 11); got != "│" {
		t.Errorf("the separator below the pointer shows %q, want │", got)
	}
	if arrows(m) != 1 {
		t.Errorf("%d arrows on screen, want 1", arrows(m))
	}
	if ansi.Strip(m.View()) == ansi.Strip(plain) || m.View() == plain {
		t.Error("hovering changed nothing on screen")
	}
	m = mouse(t, m, hover(50, 10))
	if arrows(m) != 0 || m.View() != plain {
		t.Error("moving off the separator left it lit")
	}
}

func TestHoveringTheDividerDrawsAnArrowAtThePointer(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, hover(60, dividerRow))
	if got := cellAt(m, 60, dividerRow); got != "↕" {
		t.Errorf("the divider at the pointer shows %q, want ↕", got)
	}
	if got := cellAt(m, 61, dividerRow); got != "─" {
		t.Errorf("the divider beside the pointer shows %q, want ─", got)
	}
	m = mouse(t, m, hover(editorLeft+3, dividerRow))
	if got := cellAt(m, editorLeft+3, dividerRow); got != "↕" || arrows(m) != 1 {
		t.Errorf("over the label: %q and %d arrows", got, arrows(m))
	}
	fits(t, m)
}

func TestTheLitBorderIsDrawnInTheActiveStyle(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, hover(separatorCol, 10))
	rows := strings.Split(m.View(), "\n")
	ApplyTheme(m.themeName)
	if want := activeStyle.Render("│"); !strings.Contains(rows[11], want) {
		t.Errorf("row 11 does not draw the separator as %q: %q", want, rows[11])
	}
}

func TestNothingIsLitOverTheCornerAPaneOrAnOverlay(t *testing.T) {
	m := mouseModel(t, "select 1")
	for _, at := range [][2]int{{separatorCol, dividerRow}, {50, 3}, {50, 15}, {5, 5}, {separatorCol, 0}, {separatorCol, 28}} {
		if n := arrows(mouse(t, m, hover(at[0], at[1]))); n != 0 {
			t.Errorf("hovering %v drew %d arrows", at, n)
		}
	}
	m = mouse(t, m, hover(separatorCol, 10))
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyF1})
	if arrows(m) != 0 {
		t.Error("an overlay showed the arrow of the border under it")
	}
}

func TestTheArrowFollowsADrag(t *testing.T) {
	m := mouseModel(t, "select 1")
	m = mouse(t, m, hover(separatorCol, 10), press(separatorCol, 10), motion(40, 12))
	if got := cellAt(m, 40, 12); got != "↔" || arrows(m) != 1 {
		t.Errorf("during the drag (40, 12) shows %q and there are %d arrows", got, arrows(m))
	}
	m = mouse(t, m, motion(97, 14))
	if got := cellAt(m, m.layout.separatorX(), 14); got != "↔" || arrows(m) != 1 {
		t.Errorf("past the limit the separator shows %q and there are %d arrows", got, arrows(m))
	}
	m = mouse(t, m, motion(40, 29))
	if got := cellAt(m, 40, m.layout.bodyH); got != "↔" || arrows(m) != 1 {
		t.Errorf("below the body the last body row shows %q and there are %d arrows", got, arrows(m))
	}
	m = mouse(t, m, release(40, 29))
	if arrows(m) != 0 {
		t.Error("the arrow stayed after a release off the border")
	}

	m = mouse(t, m, press(50, dividerRow), motion(70, dividerAtRow(10)))
	if got := cellAt(m, 70, dividerAtRow(10)); got != "↕" || arrows(m) != 1 {
		t.Errorf("during a divider drag the divider shows %q and there are %d arrows", got, arrows(m))
	}
	fits(t, m)
}
