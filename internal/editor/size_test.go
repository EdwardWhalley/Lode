package editor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func twentyLines() string {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "line" + string(rune('a'+i))
	}
	return strings.Join(lines, "\n")
}

func TestSetHeightKeepsTheCursorInView(t *testing.T) {
	ed := newEditor(40)
	ed.SetHeight(12)
	ed.SetValue(twentyLines())
	_ = ed.View()
	ed, _ = ed.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !strings.Contains(ed.View(), "linet") {
		t.Fatalf("test needs the cursor in view to begin with:\n%s", ed.View())
	}
	ed.SetHeight(3)
	if view := ed.View(); !strings.Contains(view, "linet") || len(strings.Split(view, "\n")) != 3 {
		t.Errorf("after SetHeight(3) the view is:\n%s", view)
	}
}

func TestSetHeightLeavesAViewTheWheelScrolledAway(t *testing.T) {
	ed := newEditor(40)
	ed.SetHeight(5)
	ed.SetValue(twentyLines())
	_ = ed.View()
	ed.ScrollView(-100)
	ed.SetHeight(4)
	if view := ed.View(); !strings.Contains(view, "linea") {
		t.Errorf("SetHeight scrolled the view back to the cursor:\n%s", view)
	}
}
