// Where the panes are: newLayout works out every size from the window and what was dragged.

package tui

import "github.com/EdwardWhalley/lode/internal/editor"

// The sizes with nothing dragged, and the limits of a drag.
const (
	defaultEditorHeight  = 5
	maxSchemaWidth       = 30
	minDefaultGridHeight = 2

	minSchemaWidth  = 10
	minMainWidth    = 20
	minEditorHeight = 3
	minGridHeight   = 3

	fixedRows = 3
	bodyTop   = 1

	minWindowWidth  = 2
	minWindowHeight = fixedRows + defaultEditorHeight + 1 + minDefaultGridHeight
)

// sizePrefs are the sizes the user has dragged to; zero means not dragged.
type sizePrefs struct {
	schemaW, editorH int
}

// layout is the size of every pane: the title is row 0, the body rows 1 to bodyH, then the
// status and hint rows.
type layout struct {
	schemaW int
	mainW   int
	editorH int
	gridH   int
	bodyH   int
}

// newLayout works out the layout for a window and the sizes dragged to.
func newLayout(width, height int, p sizePrefs) layout {
	width, height = max(width, 0), max(height, 0)

	schemaW := min(maxSchemaWidth, width/3)
	mainW := max(width-schemaW-1, 1)
	if hi := width - 1 - minMainWidth; p.schemaW > 0 && hi >= minSchemaWidth {
		schemaW = min(max(p.schemaW, minSchemaWidth), hi)
		mainW = width - schemaW - 1
	}

	editorH := defaultEditorHeight
	gridH := max(height-fixedRows-editorH-1, minDefaultGridHeight)
	if hi := min(height-fixedRows-1-minGridHeight, editor.MaxHeight); p.editorH > 0 && hi >= minEditorHeight {
		editorH = min(max(p.editorH, minEditorHeight), hi)
		gridH = height - fixedRows - 1 - editorH
	}

	return layout{schemaW: schemaW, mainW: mainW, editorH: editorH, gridH: gridH, bodyH: editorH + 1 + gridH}
}

// treeH is the rows of the schema tree: the body less its heading.
func (l layout) treeH() int { return l.bodyH - 1 }

func (l layout) separatorX() int { return l.schemaW }
func (l layout) mainLeft() int   { return l.schemaW + 1 }
func (l layout) dividerY() int   { return bodyTop + l.editorH }
func (l layout) gridTop() int    { return l.dividerY() + 1 }
