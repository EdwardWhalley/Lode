// Undo and redo: the steps, and how typing and deleting merge into runs.

package editor

import (
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// state is everything an undo has to put back.
type state struct {
	text   string
	main   caret
	extras []caret
}

// editKind is what a run of edits is made of: only plain typing and the delete keys merge.
type editKind int

const (
	ownStep editKind = iota
	typing
	deleting
)

// editRun is the run the last edit began or joined: its kind, when, and where the cursors
// were left.
type editRun struct {
	kind editKind
	at   time.Time
	end  state
}

// undoPause is how long a pause ends a run of typing.
const undoPause = time.Second

func (m Model) currentTime() time.Time {
	if m.now == nil {
		return time.Now()
	}
	return m.now()
}

// SetClock sets where the editor gets the time from, which decides how long a pause ends a
// run of typing.
func (m *Model) SetClock(now func() time.Time) { m.now = now }

// capture is the editor's state as it is.
func (m Model) capture() state {
	return state{text: m.Value(), main: m.mainCaret(), extras: append([]caret(nil), m.extras...)}
}

// restore puts s back: the text, the cursors and the Selections.
func (m *Model) restore(s state) {
	lines := strings.Split(s.text, "\n")
	m.value = make([][]rune, len(lines), maxLines)
	for i, l := range lines {
		m.value[i] = []rune(l)
	}
	m.extras = append([]caret(nil), s.extras...)
	m.loadCaret(s.main)
	m.repositionView()
}

// selecting says whether the state has a Selection at any cursor.
func (s state) selecting() bool {
	return s.main.hasAnchor || slices.ContainsFunc(s.extras, func(c caret) bool { return c.hasAnchor })
}

// sameCursors says whether the two states have their cursors and Selections in the
// same places.
func (s state) sameCursors(o state) bool {
	return s.main == o.main && slices.Equal(s.extras, o.extras)
}

// editKindOf is the kind of edit msg is, for deciding whether it joins a run.
func editKindOf(msg tea.Msg, before state) editKind {
	key, ok := msg.(tea.KeyMsg)
	if !ok || before.selecting() {
		return ownStep
	}
	letter := key.Type == tea.KeyRunes || key.Type == tea.KeySpace
	if letter && len(key.Runes) == 1 && !key.Alt && !key.Paste {
		return typing
	}
	switch key.String() {
	case "backspace", "ctrl+h", "delete", "ctrl+d":
		return deleting
	}
	return ownStep
}

// undoKey says whether msg is ctrl+z (undo) or ctrl+y (redo).
func undoKey(msg tea.Msg) (undo, redo bool) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, false
	}
	return key.String() == "ctrl+z", key.String() == "ctrl+y"
}

// undoLimit is how many steps are kept; past it the oldest is dropped.
const undoLimit = 200

// pushed is list with s added and the oldest dropped to stay within undoLimit, never
// writing into list's array.
func pushed(list []state, s state) []state {
	if len(list) >= undoLimit {
		list = list[len(list)-undoLimit+1:]
	}
	out := make([]state, len(list)+1)
	copy(out, list)
	out[len(list)] = s
	return out
}

// popped is the last of list and the rest of it.
func popped(list []state) (state, []state) {
	last := len(list) - 1
	return list[last], list[:last:last]
}

// undo puts the state before the last step back; the state it leaves can be redone.
func (m *Model) undo() {
	if len(m.undoStack) == 0 {
		return
	}
	var s state
	s, m.undoStack = popped(m.undoStack)
	m.redoStack = pushed(m.redoStack, m.capture())
	m.run = editRun{}
	m.restore(s)
}

// redo puts back the state the last undo left.
func (m *Model) redo() {
	if len(m.redoStack) == 0 {
		return
	}
	var s state
	s, m.redoStack = popped(m.redoStack)
	m.undoStack = pushed(m.undoStack, m.capture())
	m.run = editRun{}
	m.restore(s)
}

// note records an edit just made from the state before, or ends the run if it only moved
// the cursors.
func (m *Model) note(before state, kind editKind) {
	switch after := m.capture(); {
	case after.text != before.text:
		m.record(before, kind)
	case !after.sameCursors(before):
		m.run = editRun{}
	}
}

// step makes whatever edit changes one step, or part of the step it is inside.
func (m *Model) step(edit func()) {
	m.detached = false
	if m.grouped {
		edit()
		return
	}
	before := m.capture()
	m.grouped = true
	edit()
	m.grouped = false
	m.note(before, ownStep)
}

// Batch makes everything fn does to the editor one step to undo.
func (m *Model) Batch(fn func(*Model)) { m.step(func() { fn(m) }) }

// SetInitialValue replaces the text as SetValue does, but as the state to start from rather
// than a step.
func (m *Model) SetInitialValue(s string) {
	m.grouped = true
	m.SetValue(s)
	m.grouped = false
	m.undoStack, m.redoStack = nil, nil
	m.run = editRun{}
}

// record notes an edit from before to now, which joins the run before it or is a step of
// its own.
func (m *Model) record(before state, kind editKind) {
	now := m.currentTime()
	if kind != ownStep && m.run.kind == kind && now.Sub(m.run.at) < undoPause && before.sameCursors(m.run.end) {
		m.run.at, m.run.end = now, m.capture()
		return
	}
	m.undoStack = pushed(m.undoStack, before)
	m.redoStack = nil
	m.run = editRun{}
	if kind != ownStep {
		m.run = editRun{kind: kind, at: now, end: m.capture()}
	}
}
