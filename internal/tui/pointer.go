// Asks the terminal for a mouse pointer shape over the borders, with OSC 22.

package tui

import (
	"io"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

type shape string

const (
	shapeDefault      shape = ""
	shapeColumnResize shape = "ew-resize"
	shapeRowResize    shape = "ns-resize"
)

// PointerShapes writes pointer shapes to the terminal, shared by every copy of the Model;
// an overtaken request writes nothing.
type PointerShapes struct {
	w io.Writer

	mu     sync.Mutex
	latest int
	wanted shape
	sent   shape
}

// NewPointerShapes writes shapes to w, the terminal.
func NewPointerShapes(w io.Writer) *PointerShapes { return &PointerShapes{w: w} }

// WithPointerShapes lets the Model ask the terminal for a resize pointer over a border.
func WithPointerShapes(p *PointerShapes) Option { return func(m *Model) { m.shapes = p } }

// want asks for s, returning the command that writes it, or nil if that is already what was
// asked for.
func (p *PointerShapes) want(s shape) tea.Cmd {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if s == p.wanted {
		return nil
	}
	p.latest++
	n := p.latest
	p.wanted = s
	return func() tea.Msg {
		p.mu.Lock()
		defer p.mu.Unlock()
		if n == p.latest {
			p.write(s)
		}
		return nil
	}
}

// Restore gives the terminal its own pointer back, if lode changed it; call it once the
// program has ended.
func (p *PointerShapes) Restore() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.latest++
	p.wanted = shapeDefault
	if p.sent != shapeDefault {
		p.write(shapeDefault)
	}
}

// write sends s. The caller holds mu.
func (p *PointerShapes) write(s shape) {
	io.WriteString(p.w, "\x1b]22;"+string(s)+"\x1b\\")
	p.sent = s
}
