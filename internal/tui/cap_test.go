package tui_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/source"
	"github.com/EdwardWhalley/lode/internal/tui"
)

// fakeAdapter serves numbered rows without limit (or up to total, if set) and
// records how it was used.
type fakeAdapter struct {
	total int

	nextCalls           atomic.Int64
	closedWithLiveCtx   atomic.Bool
	closedWithCancelCtx atomic.Bool
	closed              atomic.Bool
}

func (f *fakeAdapter) Close() error                                    { return nil }
func (f *fakeAdapter) Schema(context.Context) ([]adapter.Table, error) { return nil, nil }
func (f *fakeAdapter) Query(ctx context.Context, _ string) (adapter.Rows, error) {
	return &fakeRows{f: f, ctx: ctx}, nil
}

type fakeRows struct {
	f   *fakeAdapter
	ctx context.Context
	n   int
	err error
}

func (r *fakeRows) Columns() []adapter.Column { return []adapter.Column{{Name: "n", Type: "INT"}} }
func (r *fakeRows) Values() []any             { return []any{r.n} }
func (r *fakeRows) Err() error                { return r.err }

func (r *fakeRows) RowsAffected() (int64, bool) { return 0, false }

func (r *fakeRows) Next() bool {
	r.f.nextCalls.Add(1)
	if err := r.ctx.Err(); err != nil {
		r.err = err
		return false
	}
	if r.f.total > 0 && r.n >= r.f.total {
		return false
	}
	r.n++
	return true
}

func (r *fakeRows) Close() error {
	r.f.closed.Store(true)
	if r.ctx.Err() != nil {
		r.f.closedWithCancelCtx.Store(true)
	} else {
		r.f.closedWithLiveCtx.Store(true)
	}
	return nil
}

func runWith(t *testing.T, f *fakeAdapter, maxRows int) *harness {
	t.Helper()
	m := tui.New(f, "fake", tui.WithMaxRows(maxRows))
	h := &harness{t: t, m: m}
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.typeQuery("SELECT n FROM endless")
	h.runToCompletion()
	return h
}

func TestCapStopsReadingAnEndlessResult(t *testing.T) {
	f := &fakeAdapter{}
	h := runWith(t, f, 5)

	view := h.view()
	if !strings.Contains(view, "truncated at 5 rows") {
		t.Errorf("expected the truncation indicator:\n%s", view)
	}
	if !strings.Contains(view, "5 rows in") {
		t.Errorf("expected exactly 5 rows reported:\n%s", view)
	}
	if calls := f.nextCalls.Load(); calls != 6 {
		t.Errorf("Next was called %d times, want 6 (the cap plus one)", calls)
	}
}

func TestCapCancelsTheQueryBeforeClosingTheRows(t *testing.T) {
	f := &fakeAdapter{}
	runWith(t, f, 3)

	if !f.closed.Load() {
		t.Fatal("the rows were never closed")
	}
	if f.closedWithLiveCtx.Load() {
		t.Error("the rows were closed while the query was still running; a real driver would read out the rest first")
	}
	if !f.closedWithCancelCtx.Load() {
		t.Error("the query was not cancelled before the rows were closed")
	}
}

func TestCapDoesNotReportItsOwnCancellationAsAnError(t *testing.T) {
	h := runWith(t, &fakeAdapter{}, 3)
	view := h.view()
	for _, bad := range []string{"cancelled", "canceled", "context"} {
		if strings.Contains(view, bad) {
			t.Errorf("the deliberate stop at the cap was reported as %q:\n%s", bad, view)
		}
	}
}

func TestResultExactlyAtTheCapIsNotTruncated(t *testing.T) {
	f := &fakeAdapter{total: 5}
	h := runWith(t, f, 5)

	if view := h.view(); strings.Contains(view, "truncated") {
		t.Errorf("a result of exactly the cap must not be marked truncated:\n%s", view)
	}
	if f.closedWithCancelCtx.Load() {
		t.Error("a complete result needs no cancellation before it is closed")
	}
}

func TestResultJustOverTheCapIsTruncated(t *testing.T) {
	h := runWith(t, &fakeAdapter{total: 6}, 5)
	if view := h.view(); !strings.Contains(view, "truncated at 5 rows") {
		t.Errorf("one row over the cap must be marked truncated:\n%s", view)
	}
}

func TestTruncationIndicatorClearsWhenTheNextResultFits(t *testing.T) {
	f := &fakeAdapter{}
	h := runWith(t, f, 5)
	if !strings.Contains(h.view(), "truncated") {
		t.Fatal("setup: expected a truncated first result")
	}

	f.total = 2
	h.runToCompletion()
	if view := h.view(); strings.Contains(view, "truncated") {
		t.Errorf("the indicator from the previous result is still showing:\n%s", view)
	}
}

func TestFileSourceIsBoundedEndToEnd(t *testing.T) {
	csv := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(csv, []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := source.Open(source.Source{Path: csv}, source.WithRowCap(5))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	h := &harness{t: t, m: tui.New(a, "a.csv", tui.WithMaxRows(5))}
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.typeQuery("SELECT * FROM range(1000000)")
	h.runToCompletion()

	view := h.view()
	if !strings.Contains(view, "5 rows in") || !strings.Contains(view, "truncated at 5 rows") {
		t.Errorf("want exactly 5 rows and the truncation indicator:\n%s", view)
	}

	h.typeQuery("SELECT * FROM range(5)")
	h.runToCompletion()
	if view := h.view(); strings.Contains(view, "truncated") {
		t.Errorf("a result of exactly the cap was marked truncated:\n%s", view)
	}
}

func TestAStatementBesideAnotherIsBoundedOnAFileSource(t *testing.T) {
	csv := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(csv, []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := source.Open(source.Source{Path: csv}, source.WithRowCap(5))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	h := &harness{t: t, m: tui.New(a, "a.csv", tui.WithMaxRows(5))}
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.typeQuery("SELECT 1; SELECT * FROM range(1000000)")
	h.runToCompletion()
	view := h.view()
	if !strings.Contains(view, "5 rows in") || !strings.Contains(view, "truncated at 5 rows") || !strings.Contains(view, "statement 2 of 2") {
		t.Errorf("want 5 rows of the second Statement and the truncation indicator:\n%s", view)
	}
}

func TestAWriteOnAFileSourceSaysItsCount(t *testing.T) {
	csv := filepath.Join(t.TempDir(), "a.csv")
	if err := os.WriteFile(csv, []byte("n\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := source.Open(source.Source{Path: csv}, source.WithRowCap(5))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })

	h := &harness{t: t, m: tui.New(a, "a.csv", tui.WithMaxRows(5))}
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.typeQuery("CREATE TABLE t (i INT)")
	h.runToCompletion()
	if view := h.view(); !strings.Contains(view, "done in") {
		t.Errorf("want done:\n%s", view)
	}
	h.typeQuery("INSERT INTO t VALUES (1), (2), (3)")
	h.runToCompletion()
	view := h.view()
	if !strings.Contains(view, "3 rows affected in") {
		t.Errorf("want 3 rows affected:\n%s", view)
	}
	if strings.Contains(view, "Count") {
		t.Errorf("a Count row is on screen:\n%s", view)
	}
}

func TestRowCapAppliesPerQueryEvenWhenLarge(t *testing.T) {
	for _, cap := range []int{1, 7, 1000} {
		t.Run(fmt.Sprint(cap), func(t *testing.T) {
			f := &fakeAdapter{}
			h := runWith(t, f, cap)
			if want := fmt.Sprintf("%d rows in", cap); !strings.Contains(h.view(), want) {
				t.Errorf("want %q reported:\n%s", want, h.view())
			}
			if calls := f.nextCalls.Load(); calls != int64(cap)+1 {
				t.Errorf("Next called %d times, want %d", calls, cap+1)
			}
		})
	}
}
