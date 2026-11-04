package file

import (
	"errors"
	"testing"

	duckdb "github.com/marcboeker/go-duckdb/v2"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

// cannedRows is a result the engine cannot be made to give.
type cannedRows struct {
	cols   []string
	rows   [][]any
	at     int
	err    error
	closed bool
}

func (r *cannedRows) Columns() []adapter.Column {
	cols := make([]adapter.Column, len(r.cols))
	for i, c := range r.cols {
		cols[i] = adapter.Column{Name: c}
	}
	return cols
}
func (r *cannedRows) Next() bool                  { r.at++; return r.at <= len(r.rows) }
func (r *cannedRows) Values() []any               { return r.rows[r.at-1] }
func (r *cannedRows) Err() error                  { return r.err }
func (r *cannedRows) RowsAffected() (int64, bool) { return 0, false }
func (r *cannedRows) Close() error                { r.closed = true; return nil }

var anUpdate = checkedQuery{stmtType: duckdb.STATEMENT_TYPE_UPDATE, lastStatement: "UPDATE t SET i = 1"}

func TestAWriteWhoseResultIsNotACountIsLeftAsItIs(t *testing.T) {
	for _, cols := range [][]string{{"Count", "other"}, {"count"}, {"n"}} {
		rows := &cannedRows{cols: cols, rows: [][]any{make([]any, len(cols))}}
		if got := anUpdate.answer(rows); got != adapter.Rows(rows) {
			t.Errorf("columns %v were taken for a count", cols)
		}
		if rows.closed {
			t.Errorf("columns %v: the rows were closed, and could not be read", cols)
		}
	}
}

func TestACountThatIsNotANumberIsNotKnown(t *testing.T) {
	got := anUpdate.answer(&cannedRows{cols: []string{"Count"}, rows: [][]any{{"three"}}})
	if n, known := got.RowsAffected(); known {
		t.Errorf("reports %d rows affected", n)
	}
}

func TestAFailureWhileAnsweringIsKept(t *testing.T) {
	failed := errors.New("disk full")
	for name, q := range map[string]checkedQuery{
		"a count":   anUpdate,
		"no result": {stmtType: duckdb.STATEMENT_TYPE_CREATE},
	} {
		rows := &cannedRows{cols: []string{"Count"}, err: failed}
		if got := q.answer(rows); !errors.Is(got.Err(), failed) {
			t.Errorf("%s: Err = %v, want the failure", name, got.Err())
		}
		if !rows.closed {
			t.Errorf("%s: the rows were left open, holding the one connection", name)
		}
	}
}

func TestANoResultStatementIsReadOutAndClosed(t *testing.T) {
	rows := &cannedRows{cols: []string{"Count"}, rows: [][]any{{int64(7)}}}
	got := checkedQuery{stmtType: duckdb.STATEMENT_TYPE_CREATE}.answer(rows)
	if rows.at != 2 || !rows.closed {
		t.Errorf("read to %d, closed %v; want it read out and closed", rows.at, rows.closed)
	}
	if _, known := got.RowsAffected(); known || len(got.Columns()) != 0 {
		t.Errorf("reports %+v, want no columns and no count", got)
	}
}

func TestACallThatReturnsRowsStaysAQuery(t *testing.T) {
	rows := &cannedRows{cols: []string{"name"}, rows: [][]any{{"x"}}}
	if got := (checkedQuery{stmtType: duckdb.STATEMENT_TYPE_CALL}).answer(rows); got != adapter.Rows(rows) {
		t.Error("a CALL with rows of its own was taken for no result")
	}
}
