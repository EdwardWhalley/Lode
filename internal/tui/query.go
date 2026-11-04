// Running a query: what ctrl+r sends, the running query, and the result held.

package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/editor"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

// toRun is what ctrl+r decided on.
type toRun struct {
	query    string
	position string
	refusal  string
}

// whatToRun decides what ctrl+r sends: the Selection if there is one, otherwise the
// Statement the cursor means.
func whatToRun(ed editor.Model, d sqltext.Dialect) toRun {
	nothing := toRun{refusal: "nothing to run"}
	if ed.CursorCount() > 1 {
		return toRun{refusal: "several cursors: press esc first"}
	}
	if ed.HasSelection() {
		text := strings.TrimSpace(ed.SelectedText())
		if sqltext.IsBlank(text, d) {
			return nothing
		}
		return toRun{query: text}
	}
	chosen, ok := sqltext.StatementToRun(ed.Value(), ed.Offset(), d)
	if !ok {
		return nothing
	}
	run := toRun{query: chosen.Text}
	if chosen.Of > 1 {
		run.position = fmt.Sprintf("statement %d of %d", chosen.Position, chosen.Of)
	}
	return run
}

// queryRun is the query that is running, if one is; numbered results let a stale one be
// dropped.
type queryRun struct {
	cancel context.CancelFunc
	id     int
}

// running reports whether a query has begun and not yet finished or been abandoned.
func (q queryRun) running() bool { return q.cancel != nil }

// begin starts sql on a, first cancelling any query still running; the command answers with
// a resultMsg.
func (q queryRun) begin(a adapter.Adapter, sql string, maxRows int) (queryRun, tea.Cmd) {
	if q.cancel != nil {
		q.cancel()
	}
	q.id++
	ctx, cancel := context.WithCancel(context.Background())
	q.cancel = cancel
	return q, runQuery(ctx, cancel, a, sql, maxRows, q.id)
}

// abandon cancels the running query, if there is one, and says whether there was.
func (q queryRun) abandon() (next queryRun, wasRunning bool) {
	wasRunning = q.cancel != nil
	if wasRunning {
		q.cancel()
		q.cancel = nil
	}
	q.id++
	return q, wasRunning
}

// finish says whether msg answers the current query; a stale result changes nothing.
func (q queryRun) finish(msg resultMsg) (next queryRun, fresh bool) {
	if msg.id != q.id {
		return q, false
	}
	q.cancel = nil
	return q, true
}

type resultMsg struct {
	id            int
	cols          []string
	rows          [][]string
	rawCols       []string
	raw           [][]any
	truncated     bool
	maxRows       int
	affected      int64
	affectedKnown bool
	elapsed       time.Duration
	err           error
}

// outcome is what the status line says of a result that succeeded.
func (r resultMsg) outcome() string {
	took := r.elapsed.Round(time.Millisecond)
	switch {
	case len(r.cols) > 0:
		return fmt.Sprintf("%d rows in %s", len(r.rows), took)
	case !r.affectedKnown:
		return fmt.Sprintf("done in %s", took)
	case r.affected == 1:
		return fmt.Sprintf("1 row affected in %s", took)
	}
	return fmt.Sprintf("%d rows affected in %s", r.affected, took)
}

// runQuery runs query and collects at most maxRows rows. cancel ends ctx, the
// context the query runs under.
func runQuery(ctx context.Context, cancel context.CancelFunc, a adapter.Adapter, query string, maxRows, id int) tea.Cmd {
	return func() (msg tea.Msg) {
		start := time.Now()
		res := resultMsg{id: id, maxRows: maxRows}
		defer func() {
			if r := recover(); r != nil {
				cancel()
				res.rows, res.raw = nil, nil
				res.err = fmt.Errorf("could not read a value of this result (%v); select the columns you can read", r)
				msg = res
			}
		}()

		rows, err := a.Query(ctx, query)
		if err != nil {
			cancel()
			res.err = err
			return res
		}
		defer cancel()
		defer rows.Close()

		for _, c := range rows.Columns() {
			res.cols = append(res.cols, sanitize(c.Name))
			res.rawCols = append(res.rawCols, c.Name)
		}
		for rows.Next() {
			if len(res.rows) >= maxRows {
				res.truncated = true
				cancel()
				break
			}
			vals := rows.Values()
			row := make([]string, len(vals))
			for i, v := range vals {
				row[i] = formatValue(v)
			}
			res.rows = append(res.rows, row)
			res.raw = append(res.raw, retain(vals))
		}
		if err := rows.Err(); err != nil && !res.truncated {
			res.err = err
		}
		if res.err == nil && !res.truncated {
			res.affected, res.affectedKnown = rows.RowsAffected()
		}
		res.elapsed = time.Since(start)
		return res
	}
}

// retain copies a row's values, because a driver may reuse the buffer behind a []byte.
func retain(vals []any) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		if b, ok := v.([]byte); ok {
			v = append([]byte(nil), b...)
		}
		out[i] = v
	}
	return out
}

// heldResult is the result of the last query that succeeded, as the Adapter gave it, kept
// for export. from is its Source's Label, to show, and fromKey its historyKey, because two
// Sources can share a Label.
type heldResult struct {
	rawCols     []string
	raw         [][]any
	truncated   bool
	truncatedAt int
	from        string
	fromKey     string
	ok          bool
}

// newHeldResult holds the successful result msg, which came from the Source with this Label
// and historyKey.
func newHeldResult(msg resultMsg, from, fromKey string) heldResult {
	return heldResult{
		rawCols: msg.rawCols, raw: msg.raw,
		truncated: msg.truncated, truncatedAt: msg.maxRows,
		from: from, fromKey: fromKey, ok: true,
	}
}

// exportable is false until a query has succeeded.
func (h heldResult) exportable() bool { return h.ok }

// fromElsewhere reports that the rows came from another Source than the one with this Label and
// historyKey, or from where that Source pointed before it was edited.
func (h heldResult) fromElsewhere(label, key string) bool {
	return h.fromKey != "" && (h.fromKey != key || h.from != label)
}
