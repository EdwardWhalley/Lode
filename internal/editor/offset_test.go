package editor

import "testing"

func TestOffsetIsTheCursorsPlaceInTheValueInBytes(t *testing.T) {
	m := New()
	if got := m.Offset(); got != 0 {
		t.Errorf("empty: Offset = %d, want 0", got)
	}
	m.SetValue("é世\nab\ncd")
	value := m.Value()
	if got := m.Offset(); got != len(value) {
		t.Errorf("at the end: Offset = %d, want %d", got, len(value))
	}
	m.CursorUp()
	m.SetCursor(1)
	if got, want := m.Offset(), len("é世\na"); got != want {
		t.Errorf("after the a: Offset = %d, want %d", got, want)
	}
	m.CursorUp()
	m.SetCursor(2)
	if got, want := m.Offset(), len("é世"); got != want {
		t.Errorf("after the wide characters: Offset = %d, want %d", got, want)
	}
}
