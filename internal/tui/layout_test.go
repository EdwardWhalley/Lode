package tui

import (
	"testing"

	"github.com/EdwardWhalley/lode/internal/editor"
)

// oldLayout is the formulas WindowSizeMsg used before there was a layout, written out as
// the oracle.
func oldLayout(width, height int) (schemaW, mainW, editorH, gridH int) {
	schemaW = min(30, width/3)
	mainW = max(width-schemaW-1, 1)
	gridH = max(height-(1+5+1+1+1), 2)
	return schemaW, mainW, 5, gridH
}

func TestTheDefaultLayoutIsWhatTheOldFormulasGave(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 40}, {40, 10}, {100, 30}, {60, 14}, {200, 30}, {90, 24}, {91, 24}, {89, 24}, {100, 11}, {100, 12}, {2, 11}}
	for _, s := range sizes {
		l := newLayout(s[0], s[1], sizePrefs{})
		schemaW, mainW, editorH, gridH := oldLayout(s[0], s[1])
		if l.schemaW != schemaW || l.mainW != mainW || l.editorH != editorH || l.gridH != gridH {
			t.Errorf("%dx%d: schema %d main %d editor %d grid %d; the old code gave %d, %d, %d, %d",
				s[0], s[1], l.schemaW, l.mainW, l.editorH, l.gridH, schemaW, mainW, editorH, gridH)
		}
		if l.bodyH != editorH+1+gridH {
			t.Errorf("%dx%d: body %d, want %d", s[0], s[1], l.bodyH, editorH+1+gridH)
		}
	}
}

func TestEveryOldWindowSizeGivesTheOldNumbers(t *testing.T) {
	for w := 0; w <= 200; w++ {
		for h := 0; h <= 80; h++ {
			l := newLayout(w, h, sizePrefs{})
			schemaW, mainW, editorH, gridH := oldLayout(w, h)
			if l.schemaW != schemaW || l.mainW != mainW || l.editorH != editorH || l.gridH != gridH {
				t.Fatalf("%dx%d: schema %d main %d editor %d grid %d; the old code gave %d, %d, %d, %d",
					w, h, l.schemaW, l.mainW, l.editorH, l.gridH, schemaW, mainW, editorH, gridH)
			}
		}
	}
}

func TestThePanesTileTheWindow(t *testing.T) {
	for w := minWindowWidth; w <= 200; w++ {
		for h := minWindowHeight; h <= 80; h++ {
			for _, p := range []sizePrefs{{}, {schemaW: 40, editorH: 12}, {schemaW: 1, editorH: 1}, {schemaW: 500, editorH: 500}} {
				l := newLayout(w, h, p)
				if got := 1 + l.bodyH + 1 + 1; got != h {
					t.Fatalf("%dx%d %+v: title, body %d, status and hints are %d rows", w, h, p, l.bodyH, got)
				}
				if got := l.schemaW + 1 + l.mainW; got != w {
					t.Fatalf("%dx%d %+v: schema %d, separator and main %d are %d columns", w, h, p, l.schemaW, l.mainW, got)
				}
				if l.editorH+1+l.gridH != l.bodyH {
					t.Fatalf("%dx%d %+v: editor %d, divider and grid %d do not fill the body %d", w, h, p, l.editorH, l.gridH, l.bodyH)
				}
			}
		}
	}
}

func TestPaneAtAgreesWithTheLayoutForEveryCell(t *testing.T) {
	for w := minWindowWidth; w <= 200; w += 3 {
		for h := minWindowHeight; h <= 80; h += 3 {
			m := newBare(t, w, h)
			m.prefs = sizePrefs{schemaW: 17, editorH: 7}
			m = m.relayout()
			l := m.layout
			count := map[pane]int{}
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					count[m.paneAt(x, y)]++
				}
			}
			want := map[pane]int{
				paneSchema: l.schemaW * l.bodyH,
				paneEditor: l.mainW * l.editorH,
				paneGrid:   l.mainW * l.gridH,
			}
			want[paneNone] = w*h - want[paneSchema] - want[paneEditor] - want[paneGrid]
			for _, p := range []pane{paneNone, paneSchema, paneEditor, paneGrid} {
				if count[p] != want[p] {
					t.Fatalf("%dx%d: %d cells in pane %d, want %d", w, h, count[p], p, want[p])
				}
			}
			if l.schemaW > 0 && m.paneAt(l.schemaW-1, 1) != paneSchema {
				t.Fatalf("%dx%d: the last schema column is not the schema", w, h)
			}
			if m.paneAt(l.schemaW, 1) != paneNone || m.paneAt(l.schemaW+1, 1) != paneEditor {
				t.Fatalf("%dx%d: the separator or the first editor column is wrong", w, h)
			}
			if m.paneAt(l.schemaW+1, l.editorH) != paneEditor || m.paneAt(l.schemaW+1, l.editorH+1) != paneNone || m.paneAt(l.schemaW+1, l.editorH+2) != paneGrid {
				t.Fatalf("%dx%d: the divider or the rows either side of it are wrong", w, h)
			}
		}
	}
}

func TestTheLayoutIsTotal(t *testing.T) {
	for w := 0; w <= 12; w++ {
		for h := 0; h <= 14; h++ {
			for _, p := range []sizePrefs{{}, {schemaW: 40, editorH: 12}, {schemaW: -3, editorH: -3}} {
				l := newLayout(w, h, p)
				for name, v := range map[string]int{"schema": l.schemaW, "main": l.mainW, "editor": l.editorH, "grid": l.gridH, "body": l.bodyH} {
					if v < 0 {
						t.Fatalf("%dx%d %+v: %s is %d", w, h, p, name, v)
					}
				}
			}
		}
	}
}

func TestAPreferenceIsKeptWithinTheLimits(t *testing.T) {
	cases := []struct {
		name          string
		w, h          int
		p             sizePrefs
		schemaW, edit int
	}{
		{"wide pane", 120, 40, sizePrefs{schemaW: 40}, 40, 5},
		{"too narrow", 120, 40, sizePrefs{schemaW: 3}, minSchemaWidth, 5},
		{"too wide leaves 20 columns for the main area", 120, 40, sizePrefs{schemaW: 119}, 120 - 1 - minMainWidth, 5},
		{"tall editor", 120, 40, sizePrefs{editorH: 12}, 30, 12},
		{"too short", 120, 40, sizePrefs{editorH: 1}, 30, minEditorHeight},
		{"too tall leaves 3 grid rows", 120, 40, sizePrefs{editorH: 99}, 30, 40 - 3 - 1 - minGridHeight},
		{"a window too narrow to keep the limits ignores the preference", 25, 40, sizePrefs{schemaW: 15}, 8, 5},
		{"a window too short to keep the limits ignores the preference", 120, 9, sizePrefs{editorH: 7}, 30, 5},
	}
	for _, c := range cases {
		l := newLayout(c.w, c.h, c.p)
		if l.schemaW != c.schemaW || l.editorH != c.edit {
			t.Errorf("%s: schema %d editor %d, want %d and %d", c.name, l.schemaW, l.editorH, c.schemaW, c.edit)
		}
		if c.h >= minWindowHeight && l.gridH < minGridHeight && c.p.editorH > 0 {
			t.Errorf("%s: grid %d rows, below the minimum", c.name, l.gridH)
		}
	}
}

func TestTheLayoutHasTheRowsAndColumnsOfEachBorder(t *testing.T) {
	l := newLayout(100, 30, sizePrefs{})
	if l.gridH != 21 || l.bodyH != 27 {
		t.Fatalf("grid %d body %d, want 21 and 27", l.gridH, l.bodyH)
	}
	if l.separatorX() != 30 || l.mainLeft() != 31 || l.dividerY() != 6 {
		t.Errorf("separator %d, main left %d, divider %d; want 30, 31, 6", l.separatorX(), l.mainLeft(), l.dividerY())
	}
}

func TestTheEditorIsNeverGivenMoreRowsThanItCanShow(t *testing.T) {
	l := newLayout(100, 200, sizePrefs{editorH: 190})
	if l.editorH != editor.MaxHeight {
		t.Errorf("the editor is %d rows, want its limit %d", l.editorH, editor.MaxHeight)
	}
	if l.bodyH != l.editorH+1+l.gridH || 1+l.bodyH+2 != 200 {
		t.Errorf("the panes do not fill the window: %+v", l)
	}
}
