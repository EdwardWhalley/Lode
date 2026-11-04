package tui_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/source"
	"github.com/EdwardWhalley/lode/internal/tui"
)

// writeParquet makes a real Parquet file holding the query's result, through the driver.
func writeParquet(t *testing.T, name, query string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	literal := "'" + strings.ReplaceAll(path, "'", "''") + "'"
	if _, err := db.Exec(`COPY (` + query + `) TO ` + literal + ` (FORMAT parquet)`); err != nil {
		t.Fatal(err)
	}
	return path
}

// awkward is a row of a list, a struct, a decimal, a timestamp and a NULL.
func awkward(t *testing.T) string {
	t.Helper()
	return writeParquet(t, "data.parquet", `
		SELECT [1, NULL, 3] AS list, {'x': 1, 'y': 'two'} AS pair, 12.50::DECIMAL(10, 2) AS price,
			TIMESTAMP '2026-11-01 18:30:05' AS seen, NULL::INTEGER AS nothing`)
}

// openParquet is the real wiring, as main does it, in a window wide enough for every
// column to show.
func openParquet(t *testing.T, path string) *harness {
	t.Helper()
	a, err := source.Open(source.Source{Path: path}, source.WithRowCap(5))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	h := &harness{t: t, m: tui.New(a, "data.parquet", tui.WithMaxRows(5))}
	h.send(tea.WindowSizeMsg{Width: 200, Height: 24})
	return h
}

func TestAParquetFilesAwkwardColumnsDisplay(t *testing.T) {
	h := openParquet(t, awkward(t))
	h.typeQuery("SELECT * FROM data")
	h.runToCompletion()
	view := ansi.Strip(h.view())
	if !strings.Contains(view, "1 rows in") {
		t.Fatalf("the query did not succeed:\n%s", view)
	}
	for name, want := range map[string]string{
		"a list":      `\[1,null,3\]`,
		"a struct":    `\{"x":1,"y":"two"\}`,
		"a decimal":   `\b12\.50\b`,
		"a timestamp": `2026-11-01 18:30:05`,
		"a NULL":      `\bNULL\b`,
	} {
		if !regexp.MustCompile(want).MatchString(view) {
			t.Errorf("%s does not show as %s:\n%s", name, want, view)
		}
	}
}

func TestAParquetFilesColumnsAreInTheTreeAndSuggested(t *testing.T) {
	h := openParquet(t, awkward(t))
	h.start()
	h.typeQuery("")
	for _, r := range "SELECT data.pri" {
		h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if view := ansi.Strip(h.view()); !strings.Contains(view, "column of data") {
		t.Errorf("the column is not suggested:\n%s", view)
	}

	h.press(tea.KeyEsc, tea.KeyTab, tea.KeyTab, tea.KeyEnter)
	view := ansi.Strip(h.view())
	for _, want := range []string{"price", "decimal(10,2)", "seen", "timestamp"} {
		if !strings.Contains(view, want) {
			t.Errorf("the schema tree does not show %q:\n%s", want, view)
		}
	}
}

func TestAParquetFileExportsToCSVAndJSON(t *testing.T) {
	path := writeParquet(t, "data.parquet", `
		SELECT [1, NULL, 3] AS list, {'x': 1, 'y': 'two'} AS pair, 12.50::DECIMAL(10, 2) AS price,
			[1.5, NULL] AS decimals, {'cost': 2.50::DECIMAL(10, 2)} AS nested,
			TIMESTAMP '2026-11-01 18:30:05' AS seen, NULL::INTEGER AS nothing,
			'6ba7b810-9dad-11d1-80b4-00c04fd430c8'::UUID AS id, TIME '18:30:05' AS at`)
	for name, wants := range map[string][]string{
		"out.csv": {
			"list,pair,price,decimals,nested,seen,nothing,id,at",
			",2026-11-01T18:30:05Z,,6ba7b810-9dad-11d1-80b4-00c04fd430c8,18:30:05",
			`"[1,null,3]","{""x"":1,""y"":""two""}",12.50,`,
		},
		"out.json": {
			`"list": [1,null,3]`,
			`"pair": {"x":1,"y":"two"}`,
			`"price": 12.50`,
			`"decimals": [1.5,null]`,
			`"nested": {"cost":2.50}`,
			`"seen": "2026-11-01T18:30:05Z"`,
			`"nothing": null`,
			`"id": "6ba7b810-9dad-11d1-80b4-00c04fd430c8"`,
			`"at": "18:30:05"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := openParquet(t, path)
			h.typeQuery("SELECT * FROM data")
			h.runToCompletion()
			out := filepath.Join(t.TempDir(), name)
			h.send(tea.KeyMsg{Type: tea.KeyCtrlS})
			for range 200 {
				h.send(tea.KeyMsg{Type: tea.KeyBackspace})
			}
			h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(out)})
			for cmd := h.send(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil; {
				cmd = h.send(cmd())
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("%v\n%s", err, ansi.Strip(h.view()))
			}
			for _, want := range wants {
				if !strings.Contains(string(got), want) {
					t.Errorf("%s lacks %q:\n%s", name, want, got)
				}
			}
		})
	}
}

func TestAParquetFileIsBoundedAndSaysSoWhenCut(t *testing.T) {
	h := openParquet(t, writeParquet(t, "many.parquet", `SELECT * FROM range(100) t(i)`))
	h.typeQuery("SELECT * FROM many")
	h.runToCompletion()
	view := ansi.Strip(h.view())
	if !strings.Contains(view, "5 rows in") || !strings.Contains(view, "truncated at 5 rows") {
		t.Errorf("want 5 rows and the truncation indicator:\n%s", view)
	}
}

func TestAValueTheDriverCannotReadIsAnErrorAndNotACrash(t *testing.T) {
	path := writeParquet(t, "odd.parquet", `SELECT 1 AS id, MAP(['k'::BLOB], [1]) AS odd`)
	h := openParquet(t, path)
	h.typeQuery("SELECT * FROM odd")
	h.runToCompletion()
	view := ansi.Strip(h.view())
	if !strings.Contains(view, "could not read a value") {
		t.Errorf("the status does not say so:\n%s", view)
	}

	h.typeQuery("SELECT id FROM odd")
	h.runToCompletion()
	if view := ansi.Strip(h.view()); !strings.Contains(view, "1 rows in") {
		t.Errorf("the Source does not work after it:\n%s", view)
	}
}

// panickingAdapter panics while its rows are read, as a driver can.
type panickingAdapter struct{ closed *bool }

func (panickingAdapter) Close() error                                    { return nil }
func (panickingAdapter) Schema(context.Context) ([]adapter.Table, error) { return nil, nil }
func (a panickingAdapter) Query(context.Context, string) (adapter.Rows, error) {
	return panickingRows{closed: a.closed}, nil
}

type panickingRows struct{ closed *bool }

func (panickingRows) Columns() []adapter.Column   { return []adapter.Column{{Name: "n"}} }
func (panickingRows) Next() bool                  { panic(errors.New("hash of unhashable type")) }
func (panickingRows) Values() []any               { return nil }
func (panickingRows) Err() error                  { return nil }
func (panickingRows) RowsAffected() (int64, bool) { return 0, false }
func (r panickingRows) Close() error              { *r.closed = true; return nil }

func TestAPanicWhileReadingClosesTheRowsAndKeepsTheResultOnScreen(t *testing.T) {
	closed := false
	h := &harness{t: t, m: tui.New(panickingAdapter{closed: &closed}, "x", tui.WithMaxRows(5))}
	h.send(tea.WindowSizeMsg{Width: 200, Height: 24})
	h.typeQuery("SELECT 1")
	h.runToCompletion()
	if !closed {
		t.Error("the rows were left open, holding the connection")
	}
	if view := ansi.Strip(h.view()); !strings.Contains(view, "could not read a value") || !strings.Contains(view, "unhashable") {
		t.Errorf("the status does not say what happened:\n%s", view)
	}
}
