package tui

import (
	"context"
	"fmt"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

// events is a shared, ordered log of what the fakes did.
type events struct {
	mu  sync.Mutex
	log []string
}

func (e *events) add(format string, args ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.log = append(e.log, fmt.Sprintf(format, args...))
}

func (e *events) all() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.log...)
}

func (e *events) count(s string) int {
	n := 0
	for _, l := range e.all() {
		if l == s {
			n++
		}
	}
	return n
}

type fakeAdapter struct {
	name        string
	ev          *events
	tables      []adapter.Table
	block       bool
	sent        []string
	err         error
	write       *writeAnswer
	answerCount int64
	answerKnown bool
}

// writeAnswer is what a statement with no columns says.
type writeAnswer struct {
	affected int64
	known    bool
	err      error
}

func (w *writeAnswer) Columns() []adapter.Column { return nil }

func (w *writeAnswer) Next() bool { return false }

func (w *writeAnswer) Values() []any { return nil }

func (w *writeAnswer) Err() error { return w.err }

func (w *writeAnswer) Close() error { return nil }

func (w *writeAnswer) RowsAffected() (int64, bool) { return w.affected, w.known }

func (f *fakeAdapter) Close() error { f.ev.add("close %s", f.name); return nil }

func (f *fakeAdapter) Schema(context.Context) ([]adapter.Table, error) { return f.tables, nil }

func (f *fakeAdapter) Query(ctx context.Context, sql string) (adapter.Rows, error) {
	f.sent = append(f.sent, sql)
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.write != nil {
		return f.write, nil
	}
	return &oneRow{from: f.name, affected: f.answerCount, known: f.answerKnown}, nil
}

type oneRow struct {
	from     string
	done     bool
	affected int64
	known    bool
}

func (r *oneRow) RowsAffected() (int64, bool) { return r.affected, r.known }

func (r *oneRow) Columns() []adapter.Column { return []adapter.Column{{Name: "src"}} }

func (r *oneRow) Next() bool { d := r.done; r.done = true; return !d }

func (r *oneRow) Values() []any { return []any{r.from} }

func (r *oneRow) Err() error { return nil }

func (r *oneRow) Close() error { return nil }

type fakeSwitcher struct {
	ev        *events
	adapters  map[string]*fakeAdapter
	choices   map[string]Choice
	maxRows   map[string]int
	errs      map[string]error
	sources   func() ([]Choice, error)
	kinds     []KindInfo
	testFn    func(ctx context.Context, n NewSource) error
	addFn     func(n NewSource) (Choice, error)
	tested    []NewSource
	added     []NewSource
	editFn    func(id string) (EditForm, error)
	replaceFn func(n NewSource) (Choice, error)
	removeFn  func(id string) error
	edited    []string
	replaced  []NewSource
	removed   []string
}

func (s *fakeSwitcher) Sources() ([]Choice, error) {
	if s.sources != nil {
		return s.sources()
	}
	return []Choice{s.choices["a"], s.choices["b"]}, nil
}

func (s *fakeSwitcher) Open(id string) (Opened, error) {
	s.ev.add("open %s", id)
	if err := s.errs[id]; err != nil {
		return Opened{}, err
	}
	return Opened{Adapter: s.adapters[id], Choice: s.choices[id], MaxRows: s.maxRows[id]}, nil
}

// world is two Sources, "a" (current at the start) and "b".
type world struct {
	ev *events
	sw *fakeSwitcher
	a  *fakeAdapter
	b  *fakeAdapter
}

func newWorld() *world {
	ev := &events{}
	a := &fakeAdapter{name: "a", ev: ev, tables: []adapter.Table{{Name: "from_a", Ref: `"from_a"`}}}
	b := &fakeAdapter{name: "b", ev: ev, tables: []adapter.Table{{Name: "from_b", Ref: `"from_b"`}}}
	return &world{ev: ev, a: a, b: b, sw: &fakeSwitcher{
		ev:       ev,
		adapters: map[string]*fakeAdapter{"a": a, "b": b},
		choices: map[string]Choice{
			"a": {ID: "a", Name: "a", Label: "source-a"},
			"b": {ID: "b", Name: "b", Label: "source-b", Write: true},
		},
		maxRows: map[string]int{"a": 100, "b": 7},
		errs:    map[string]error{},
	}}
}

func (w *world) model(t *testing.T) Model {
	t.Helper()
	m := New(w.a, "source-a", WithMaxRows(100), WithSwitcher(w.sw, w.sw.choices["a"]))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return updated.(Model)
}

// countingAdapter counts calls, to show completion never reaches the database.
type countingAdapter struct {
	queries, schemas int
}

func (a *countingAdapter) Close() error { return nil }

func (a *countingAdapter) Schema(context.Context) ([]adapter.Table, error) {
	a.schemas++
	return nil, nil
}

func (a *countingAdapter) Query(context.Context, string) (adapter.Rows, error) {
	a.queries++
	return nil, fmt.Errorf("unexpected query")
}

// fakeSys is the operating system's clipboard, which the tests never touch.
type fakeSys struct {
	text              string
	readErr, writeErr error
}

func (f *fakeSys) Read() (string, error) {
	if f.readErr != nil {
		return "", f.readErr
	}
	return f.text, nil
}

func (f *fakeSys) Write(s string) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.text = s
	return nil
}
