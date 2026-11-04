// The form that creates and edits a Source.

package tui

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/rowcap"
	"github.com/EdwardWhalley/lode/internal/sourcetext"
)

// fieldKind says what a form row is for.
type fieldKind int

const (
	fkName fieldKind = iota
	fkValue
	fkWrite
	fkMaxRows
	fkRaw
)

type formField struct {
	kind  fieldKind
	field adapter.Field
	label string
	help  string
	input textinput.Model
	on    bool
	err   string
}

// editing is what a form that edits a stored Source knows beyond the fields.
type editing struct {
	name string
	open bool
	keep bool
}

// editsOpen says the form edits the Source the session has open.
func (f sourceForm) editsOpen() bool { return f.edit != nil && f.edit.open }

// formState is where the form is in test-then-save.
type formState int

const (
	stEditing formState = iota
	stTesting
	stFailed
	stSaving
	stSaved
)

// sourceForm is the Overlay for entering a Source: one row per field of the Kind, plus the
// name, write option and row cap.
type sourceForm struct {
	sw     Switcher
	kinds  []KindInfo
	kind   int
	taken  []string
	fields []formField
	focus  int

	edit *editing
	note string

	state  formState
	notice string
	id     int
	cancel context.CancelFunc
	saved  Choice
}

// formIDs numbers every test and save of every form, so a late answer cannot be taken by a
// form opened since.
var formIDs atomic.Int64

func nextFormID() int { return int(formIDs.Add(1)) }

func newInput(value string) textinput.Model {
	in := newTextInput()
	in.Prompt = ""
	in.Cursor.SetMode(cursor.CursorStatic)
	in.SetValue(value)
	in.CursorEnd()
	return in
}

func newSourceForm(sw Switcher, kinds []KindInfo, kind int, taken []string) sourceForm {
	f := sourceForm{sw: sw, kinds: kinds, kind: kind, taken: taken}
	f.fields = append(f.fields, formField{
		kind: fkName, label: "Name", input: newInput(""),
		help: "Letters, digits, - and _; how the Source is listed and chosen",
	})
	info := kinds[kind]
	for _, field := range info.Fields {
		label := field.Label
		if field.Type == adapter.EnvVar {
			label += " (variable name)"
		}
		f.fields = append(f.fields, formField{
			kind: fkValue, field: field, label: label, help: field.Help, input: newInput(field.Default),
		})
	}
	if !info.ReadOnly {
		f.fields = append(f.fields, formField{
			kind: fkWrite, label: "Write", input: newInput(""), help: "space ticks it; off, the Source can only be read",
		})
	}
	f.fields = append(f.fields, formField{
		kind: fkMaxRows, label: "Max rows", input: newInput(""),
		help: "Optional, 1 to 1,000,000; rows shown for this Source",
	})
	f.fields[0].input.Focus()
	return f
}

// newEditForm is the form for a Source in sources.toml, filled in from ef; taken is the
// other Sources' names.
func newEditForm(sw Switcher, kinds []KindInfo, ef EditForm, taken []string, open bool) sourceForm {
	f := newSourceForm(sw, kinds, ef.Kind, taken)
	f.edit = &editing{name: ef.Name, open: open, keep: ef.Placement == Hidden}

	var fields []formField
	rawPlaced := false
	for _, field := range f.fields {
		switch field.kind {
		case fkName:
			field.input.SetValue(ef.Name)
			field.input.CursorEnd()
		case fkValue:
			if ef.Placement == ShownAsText || ef.Placement == Hidden {
				if !rawPlaced && ef.Placement == ShownAsText {
					rawPlaced = true
					fields = append(fields, formField{
						kind: fkRaw, label: "Location", input: newInput(ef.Location),
						help: "The location as stored; this Kind's fields cannot show it",
					})
				}
				continue
			}
			field.input.SetValue(ef.Values[field.field.Key])
			field.input.CursorEnd()
		case fkWrite:
			field.on = ef.Write
		case fkMaxRows:
			if ef.MaxRows > 0 {
				field.input.SetValue(strconv.Itoa(ef.MaxRows))
				field.input.CursorEnd()
			}
		}
		fields = append(fields, field)
	}
	f.fields = fields

	switch ef.Placement {
	case Hidden:
		f.note = "The location holds a password, so it is not shown. It is kept as it is; the rest can be changed."
	case ShownAsText:
		f.note = "This Kind's fields cannot show the location, so it is one field, as stored."
	case PlacedKeepingPassword:
		f.note = "A password is stored in this Source. It is kept unless you name a variable for it below."
	}
	return f
}

func (f sourceForm) scope() scope {
	switch f.state {
	case stTesting, stSaving:
		return scopeFormTesting
	case stFailed:
		return scopeFormFailed
	case stSaved:
		if f.editsOpen() {
			return scopeFormSavedOpen
		}
		return scopeFormSaved
	}
	return scopeForm
}

func (f sourceForm) update(msg tea.KeyMsg, _ int) (overlay, tea.Cmd) {
	f.fields = slices.Clone(f.fields)
	key := msg.String()
	switch f.state {
	case stTesting:
		if key == "esc" || key == "ctrl+c" {
			f.cancel()
			f.id = nextFormID()
			f.state, f.notice = stEditing, "test cancelled; nothing was saved"
		}
		return f, nil
	case stSaving:
		return f, nil
	case stSaved:
		switch key {
		case "enter":
			if f.editsOpen() {
				return nil, func() tea.Msg { return openSelectorMsg{} }
			}
			c := f.saved
			return nil, func() tea.Msg { return switchMsg{choice: c} }
		case "esc", "ctrl+c":
			return nil, func() tea.Msg { return openSelectorMsg{} }
		}
		return f, nil
	}

	switch key {
	case "esc", "ctrl+c":
		return nil, func() tea.Msg { return openSelectorMsg{} }
	case "tab", "down":
		return f.moveFocus(1), nil
	case "shift+tab", "up":
		return f.moveFocus(-1), nil
	case "enter":
		if f.focus < len(f.fields)-1 {
			return f.moveFocus(1), nil
		}
		return f.submit(false)
	case "ctrl+s":
		if f.state == stFailed {
			return f.submit(true)
		}
		return f, nil
	}

	f.state, f.notice = stEditing, ""
	cur := &f.fields[f.focus]
	if cur.kind == fkWrite {
		if key == " " {
			cur.on = !cur.on
		}
		return f, nil
	}
	cur.err = ""
	var cmd tea.Cmd
	cur.input, cmd = cur.input.Update(msg)
	return f, cmd
}

// moveFocus moves the focus by delta rows, wrapping round, checking the field it
// leaves.
func (f sourceForm) moveFocus(delta int) sourceForm {
	f.fields[f.focus].err = f.check(f.focus)
	f.focusOn((f.focus + delta + len(f.fields)) % len(f.fields))
	return f
}

// check says what is wrong with field i, or "". Messages never repeat a value.
func (f sourceForm) check(i int) string {
	field := f.fields[i]
	value := field.input.Value()
	switch field.kind {
	case fkName:
		switch {
		case strings.TrimSpace(value) == "":
			return "required"
		case sourcetext.CheckName(value) != nil:
			return sourcetext.ErrBadName.Error()
		case f.editsOpen() && value != f.edit.name:
			return "this Source is open; switch to another Source to rename it"
		case slices.Contains(f.taken, value):
			return ErrNameTaken.Error()
		}
	case fkValue:
		if err := adapter.CheckField(field.field, value); err != nil {
			return err.Error()
		}
	case fkRaw:
		if strings.TrimSpace(value) == "" {
			return "required"
		}
	case fkMaxRows:
		if strings.TrimSpace(value) == "" {
			return ""
		}
		n, err := sourcetext.Number(value)
		if errors.Is(err, sourcetext.ErrNotNumber) {
			return "must be a whole number"
		}
		if err != nil || rowcap.Check(n) != nil {
			return "must be between 1 and " + strconv.Itoa(rowcap.Limit)
		}
	}
	return ""
}

// validate checks every field, focusing the first at fault; ok is false if there is one.
func (f *sourceForm) validate() (ok bool) {
	bad := -1
	for i := range f.fields {
		f.fields[i].err = f.check(i)
		if bad < 0 && f.fields[i].err != "" {
			bad = i
		}
	}
	if bad >= 0 {
		f.focusOn(bad)
	}
	return bad < 0
}

func (f *sourceForm) focusOn(i int) {
	f.fields[f.focus].input.Blur()
	f.focus = i
	f.fields[i].input.Focus()
}

// source is what the form holds, as the Switcher takes it.
func (f sourceForm) source() NewSource {
	n := NewSource{Kind: f.kind, Values: adapter.Values{}}
	if f.edit != nil {
		n.Replaces = f.edit.name
		if f.edit.keep {
			n.From = FromStored
		}
	}
	for _, field := range f.fields {
		switch field.kind {
		case fkName:
			n.Name = field.input.Value()
		case fkValue:
			n.Values[field.field.Key] = field.input.Value()
		case fkWrite:
			n.Write = field.on
		case fkMaxRows:
			n.MaxRows, _ = sourcetext.Number(field.input.Value())
		case fkRaw:
			n.From, n.Location = FromText, field.input.Value()
		}
	}
	return n
}

// submit checks the fields, then tests the Source or (anyway) saves it without a passing
// test.
func (f sourceForm) submit(anyway bool) (overlay, tea.Cmd) {
	if !f.validate() {
		f.state, f.notice = stEditing, ""
		return f, nil
	}
	f.id = nextFormID()
	f.notice = ""
	if anyway {
		f.state = stSaving
		return f, addSource(f.sw, f.id, f.source())
	}
	var ctx context.Context
	ctx, f.cancel = context.WithCancel(context.Background())
	f.state = stTesting
	return f, testSource(f.sw, ctx, f.id, f.source())
}

// testedMsg is the answer to Switcher.TestSource.
type testedMsg struct {
	id  int
	err error
}

// addedMsg is the answer to Switcher.AddSource.
type addedMsg struct {
	id     int
	choice Choice
	err    error
}

// testSource tries the connection off the UI goroutine.
func testSource(sw Switcher, ctx context.Context, id int, n NewSource) tea.Cmd {
	return func() tea.Msg { return testedMsg{id: id, err: sw.TestSource(ctx, n)} }
}

func addSource(sw Switcher, id int, n NewSource) tea.Cmd {
	return func() tea.Msg {
		save := sw.AddSource
		if n.Replaces != "" {
			save = sw.ReplaceSource
		}
		c, err := save(n)
		return addedMsg{id: id, choice: c, err: err}
	}
}

// tested takes the answer of a test, dropping one for a test cancelled or replaced.
func (f sourceForm) tested(msg testedMsg) (overlay, tea.Cmd) {
	if f.state != stTesting || msg.id != f.id {
		return f, nil
	}
	f.cancel()
	switch {
	case msg.err == nil:
		f.state = stSaving
		return f, addSource(f.sw, f.id, f.source())
	case errors.Is(msg.err, ErrVariableUnset):
		return f.unset(), nil
	}
	f.state, f.notice = stFailed, sanitize("the test failed: "+msg.err.Error())
	return f, nil
}

// added takes the answer of a save.
func (f sourceForm) added(msg addedMsg) sourceForm {
	if f.state != stSaving || msg.id != f.id {
		return f
	}
	f.fields = slices.Clone(f.fields)
	switch {
	case msg.err == nil && f.editsOpen():
		f.state, f.saved = stSaved, msg.choice
		f.notice = "saved. The open connection is unchanged; enter or esc goes back"
	case msg.err == nil:
		f.state, f.saved = stSaved, msg.choice
		f.notice = "saved. enter switches to it, esc stays where you are"
	case errors.Is(msg.err, ErrVariableUnset):
		return f.unset()
	case errors.Is(msg.err, ErrNameTaken):
		f.state, f.notice = stEditing, ""
		f.fields[0].err = ErrNameTaken.Error()
		f.focusOn(0)
	default:
		f.state, f.notice = stEditing, sanitize("not saved: "+msg.err.Error())
	}
	return f
}

// unset says on the password variable's field that an environment variable is not set,
// without saying which.
func (f sourceForm) unset() sourceForm {
	f.fields = slices.Clone(f.fields)
	f.state, f.notice = stEditing, "an environment variable the Source needs is not set; nothing was saved"
	for i := range f.fields {
		field := &f.fields[i]
		if field.kind == fkValue && field.field.Type == adapter.EnvVar && field.input.Value() != "" {
			field.err = "not set in the environment lode is running in"
			f.focusOn(i)
			break
		}
	}
	return f
}

func (f sourceForm) view(width, height int) string {
	title := "New Source: " + sanitize(f.kinds[f.kind].Name)
	if f.edit != nil {
		title = "Edit Source: " + sanitize(f.edit.name) + " (" + sanitize(f.kinds[f.kind].Name) + ")"
	}
	labelWidth := 0
	for _, field := range f.fields {
		labelWidth = max(labelWidth, ansi.StringWidth(field.label))
	}
	var body []string
	focusLine := 0
	for i, field := range f.fields {
		if i == f.focus {
			focusLine = len(body)
		}
		pad := strings.Repeat(" ", labelWidth-ansi.StringWidth(field.label))
		var value string
		if field.kind == fkWrite {
			value = "[ ]"
			if field.on {
				value = "[x]"
			}
		} else {
			in := field.input
			in.Width = max(width-labelWidth-6, 10)
			value = in.View()
		}
		body = append(body, ansi.Truncate("  "+field.label+pad+"  "+value, width, "…"))
		if field.err != "" {
			body = append(body, "  "+errStyle.Render(sanitize(field.err)))
		} else {
			body = append(body, dimStyle.Render(ansi.Truncate("  "+field.help, width, "…")))
		}
	}

	lines := []string{titleStyle.Render(title)}
	switch f.state {
	case stTesting:
		lines = append(lines, dimStyle.Render("  testing…  esc cancels"))
	case stSaving:
		lines = append(lines, dimStyle.Render("  saving…"))
	case stSaved:
		lines = append(lines, activeStyle.Render("  "+f.notice))
	case stFailed:
		lines = append(lines, errorLines(f.notice, width)...)
		lines = append(lines, dimStyle.Render("  ctrl+s saves it anyway; or fix the fields and press enter on the last one"))
	default:
		switch {
		case f.notice != "":
			lines = append(lines, errorLines(f.notice, width)...)
		case f.note != "":
			lines = append(lines, noteLines(f.note, width)...)
		default:
			lines = append(lines, "")
		}
	}
	lines = append(lines, "")
	visible := max(height-len(lines), 1)
	start := max(0, min(focusLine-visible/2, len(body)-visible))
	lines = append(lines, body[start:min(start+visible, len(body))]...)
	return fit(lines, height)
}

// noteLines wraps a note, drawn dim, to the width of the overlay.
func noteLines(text string, width int) []string {
	var lines []string
	for _, l := range strings.Split(ansi.Wrap(text, max(width-2, 10), ""), "\n") {
		lines = append(lines, dimStyle.Render("  "+l))
	}
	return lines
}

// errorLines wraps an error message to the width of the overlay.
func errorLines(text string, width int) []string {
	var lines []string
	for _, l := range strings.Split(ansi.Wrap(text, max(width-2, 10), ""), "\n") {
		lines = append(lines, "  "+errStyle.Render(l))
	}
	return lines
}
