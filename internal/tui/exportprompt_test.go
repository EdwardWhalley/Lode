package tui

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

var ctrlS = tea.KeyMsg{Type: tea.KeyCtrlS}

// valuesAdapter returns fixed rows of raw values; with reuse set it hands out the same
// buffer for every []byte cell.
type valuesAdapter struct {
	cols  []string
	rows  [][]any
	reuse bool
}

func (valuesAdapter) Close() error                                    { return nil }
func (valuesAdapter) Schema(context.Context) ([]adapter.Table, error) { return nil, nil }

func (a valuesAdapter) Query(context.Context, string) (adapter.Rows, error) {
	return &valueRows{a: a, i: -1}, nil
}

type valueRows struct {
	a   valuesAdapter
	i   int
	buf []byte
}

func (r *valueRows) RowsAffected() (int64, bool) { return 0, false }

func (r *valueRows) Columns() []adapter.Column {
	cols := make([]adapter.Column, len(r.a.cols))
	for i, c := range r.a.cols {
		cols[i] = adapter.Column{Name: c}
	}
	return cols
}
func (r *valueRows) Next() bool { r.i++; return r.i < len(r.a.rows) }
func (r *valueRows) Err() error { return nil }
func (r *valueRows) Close() error {
	return nil
}
func (r *valueRows) Values() []any {
	row := r.a.rows[r.i]
	if !r.a.reuse {
		return row
	}
	out := make([]any, len(row))
	for i, v := range row {
		if b, ok := v.([]byte); ok {
			if r.buf == nil {
				r.buf = make([]byte, len(b))
			}
			copy(r.buf, b)
			v = r.buf
		}
		out[i] = v
	}
	return out
}

// withResult returns a Model that has run a query against a and holds its result.
func withResult(t *testing.T, a valuesAdapter) Model {
	t.Helper()
	m := New(a, "values", WithMaxRows(100))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetQuery("SELECT 1")
	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)
	if !m.held.exportable() {
		t.Fatalf("the query did not produce a result: %q", m.status)
	}
	return m
}

var sample = valuesAdapter{
	cols: []string{"id", "note"},
	rows: [][]any{{int64(1), nil}, {int64(2), "NULL"}, {int64(3), "a, \"b\"\nc"}},
}

// enterPath types path into the open export prompt and presses enter.
func enterPath(t *testing.T, m Model, path string) (Model, tea.Cmd) {
	t.Helper()
	p, ok := m.overlay.(exportPrompt)
	if !ok {
		t.Fatalf("the export prompt is not open (overlay %T, status %q)", m.overlay, m.status)
	}
	p.input.SetValue(path)
	m.overlay = p
	return send(t, m, enter)
}

func TestExportBeforeAnyResultSaysSo(t *testing.T) {
	m := New(valuesAdapter{}, "values")
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = send(t, m, ctrlS)
	if m.overlay != nil || !m.isError || !strings.Contains(m.status, "nothing to export") {
		t.Errorf("status = %q, overlay %v", m.status, m.overlay)
	}
}

func TestCtrlSOpensAPromptWithADefaultCSVName(t *testing.T) {
	m := withResult(t, sample)
	m, _ = send(t, m, ctrlS)
	view := plain(m)
	p, ok := m.overlay.(exportPrompt)
	if !ok {
		t.Fatal("ctrl+s did not open the export prompt")
	}
	if v := p.input.Value(); !strings.HasPrefix(v, "results-") || !strings.HasSuffix(v, ".csv") {
		t.Errorf("default name = %q, want results-<timestamp>.csv", v)
	}
	for _, want := range []string{"Export results", "3 rows", "Path:", ".csv or .json"} {
		if !strings.Contains(view, want) {
			t.Errorf("prompt is missing %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "enter save") {
		t.Errorf("hint row should say how to save:\n%s", view)
	}
}

func TestExportWritesWhatTheAdapterReturnedNotWhatTheGridShows(t *testing.T) {
	m := withResult(t, sample)
	if m.grid.rows[0][1] != "NULL" || m.grid.rows[1][1] != "NULL" {
		t.Fatalf("grid cells = %q, %q; both display as NULL", m.grid.rows[0][1], m.grid.rows[1][1])
	}

	path := filepath.Join(t.TempDir(), "out.csv")
	m, _ = send(t, m, ctrlS)
	m, cmd := enterPath(t, m, path)
	if m.overlay != nil {
		t.Errorf("the prompt should close once the file is created, error %q", m.overlay)
	}
	m = drain(t, m, cmd)

	if m.isError || !strings.Contains(m.status, "exported 3 rows to") {
		t.Errorf("status = %q", m.status)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 || recs[0][0] != "id" || recs[1][1] != "" || recs[2][1] != "NULL" || recs[3][1] != "a, \"b\"\nc" {
		t.Errorf("exported records = %q: a NULL is empty, the text NULL is not, nothing is sanitized", recs)
	}
}

func TestExportKeepsTheRealColumnNamesNotTheSanitizedOnes(t *testing.T) {
	m := withResult(t, valuesAdapter{cols: []string{"a\tb"}, rows: [][]any{{int64(1)}}})
	path := filepath.Join(t.TempDir(), "out.json")
	m, _ = send(t, m, ctrlS)
	m, cmd := enterPath(t, m, path)
	m = drain(t, m, cmd)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"a\tb"`) {
		t.Errorf("the file should hold the real column name:\n%s", b)
	}
}

func TestAnExistingFileStaysInThePromptAndIsUntouched(t *testing.T) {
	m := withResult(t, sample)
	path := filepath.Join(t.TempDir(), "keep.csv")
	if err := os.WriteFile(path, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, _ = send(t, m, ctrlS)
	m, cmd := enterPath(t, m, path)
	if cmd != nil {
		t.Error("nothing should be written for an existing file")
	}
	if _, ok := m.overlay.(exportPrompt); !ok || !strings.Contains(plain(m), "already exists") {
		t.Errorf("the prompt should stay open and explain:\n%s", plain(m))
	}
	if b, _ := os.ReadFile(path); string(b) != "precious" {
		t.Errorf("the existing file was changed: %q", b)
	}
}

func TestBadPathsStayInThePromptAndCreateNothing(t *testing.T) {
	dir := t.TempDir()
	for name, path := range map[string]string{
		"unknown extension": filepath.Join(dir, "out.parquet"),
		"no extension":      filepath.Join(dir, "out"),
		"missing directory": filepath.Join(dir, "nope", "out.csv"),
		"empty":             "   ",
	} {
		m := withResult(t, sample)
		m, _ = send(t, m, ctrlS)
		m, cmd := enterPath(t, m, path)
		if _, ok := m.overlay.(exportPrompt); !ok || cmd != nil {
			t.Errorf("%s: the prompt should stay open and write nothing", name)
		}
		if m.overlay.(exportPrompt).err == "" {
			t.Errorf("%s: no error shown", name)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("files were created for bad paths: %v", entries)
	}
}

func TestEscCancelsTheExportPrompt(t *testing.T) {
	m := withResult(t, sample)
	m, _ = send(t, m, ctrlS)
	m, cmd := send(t, m, esc)
	if m.overlay != nil || cmd != nil {
		t.Error("esc should close the prompt and write nothing")
	}
}

func TestExportingATruncatedResultSaysSo(t *testing.T) {
	m := withResult(t, sample)
	m.held.truncated, m.held.truncatedAt = true, 3
	path := filepath.Join(t.TempDir(), "out.csv")
	m, _ = send(t, m, ctrlS)
	if !strings.Contains(plain(m), "truncated at 3 rows") {
		t.Errorf("the prompt should warn that the result was cut:\n%s", plain(m))
	}
	m, cmd := enterPath(t, m, path)
	m = drain(t, m, cmd)
	if !strings.Contains(m.status, "exported 3 rows") || !strings.Contains(m.status, "result was truncated at 3 rows") {
		t.Errorf("status = %q", m.status)
	}
}

func TestAFailedExportIsReportedAndDoesNotStopTheSession(t *testing.T) {
	m := withResult(t, sample)
	m, _ = send(t, m, exportedMsg{path: "x.csv", err: os.ErrClosed})
	if !m.isError || !strings.Contains(m.status, "export failed") {
		t.Errorf("status = %q", m.status)
	}
	m.SetQuery("SELECT 1")
	m, run := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlR})
	m = drain(t, m, run)
	if m.isError {
		t.Errorf("the session stopped working after a failed export: %q", m.status)
	}
}

func TestRetainedRowsAreNotCorruptedByBufferReuse(t *testing.T) {
	m := withResult(t, valuesAdapter{
		cols:  []string{"b"},
		rows:  [][]any{{[]byte("aaa")}, {[]byte("bbb")}, {[]byte("ccc")}},
		reuse: true,
	})
	var got []string
	for _, row := range m.held.raw {
		got = append(got, string(row[0].([]byte)))
	}
	if strings.Join(got, ",") != "aaa,bbb,ccc" {
		t.Errorf("held values = %v, want aaa,bbb,ccc: they alias the driver's buffer", got)
	}
}

func TestRetainingRawValuesDoesNotChangeWhatTheGridShows(t *testing.T) {
	m := withResult(t, sample)
	if got := m.grid.rows[2][1]; got != "a, \"b\"↵c" {
		t.Errorf("grid cell = %q, want the sanitized display form", got)
	}
	if len(m.held.raw) != len(m.grid.rows) || len(m.held.rawCols) != len(m.grid.cols) {
		t.Errorf("raw %dx%d, grid %dx%d", len(m.held.raw), len(m.held.rawCols), len(m.grid.rows), len(m.grid.cols))
	}
}

func TestExportIsInTheLegend(t *testing.T) {
	m := withResult(t, sample)
	m, _ = send(t, m, f1)
	if !strings.Contains(plain(m), "ctrl+s") {
		t.Errorf("F1 help does not list ctrl+s:\n%s", plain(m))
	}
}
