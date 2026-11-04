package tui

import (
	"strings"
	"testing"
)

// counting is a cancel function that says how often it was called.
func counting(n *int) func() { return func() { *n++ } }

func TestBeginMarksAQueryRunning(t *testing.T) {
	var q queryRun
	if q.running() {
		t.Fatal("a new queryRun is running")
	}
	q, cmd := q.begin(nil, "select 1", 10)
	if !q.running() || cmd == nil {
		t.Errorf("running = %v, cmd = %v; want a running query and a command", q.running(), cmd)
	}
}

func TestASecondBeginCancelsTheFirstOnceAndMakesItsResultStale(t *testing.T) {
	q, _ := queryRun{}.begin(nil, "select 1", 10)
	first := q.id
	cancelled := 0
	q.cancel = counting(&cancelled)

	q, _ = q.begin(nil, "select 2", 10)
	if cancelled != 1 {
		t.Errorf("the first query was cancelled %d times, want 1", cancelled)
	}
	if _, fresh := q.finish(resultMsg{id: first}); fresh {
		t.Error("the first query's result is fresh after a second began")
	}
	if _, fresh := q.finish(resultMsg{id: q.id}); !fresh {
		t.Error("the second query's result is not fresh")
	}
}

func TestAbandonCancelsOnceAndMakesALateResultStale(t *testing.T) {
	q, _ := queryRun{}.begin(nil, "select 1", 10)
	late := q.id
	cancelled := 0
	q.cancel = counting(&cancelled)

	q, was := q.abandon()
	if !was || cancelled != 1 || q.running() {
		t.Errorf("wasRunning = %v, cancelled %d times, running = %v; want true, once, false", was, cancelled, q.running())
	}
	if _, fresh := q.finish(resultMsg{id: late}); fresh {
		t.Error("a result that arrives after abandon is fresh")
	}
}

func TestAbandonWithNothingRunningCancelsNothingAndSaysSo(t *testing.T) {
	var q queryRun
	q, was := q.abandon()
	if was || q.running() {
		t.Errorf("wasRunning = %v, running = %v; want false, false", was, q.running())
	}
}

func TestFinishOfTheCurrentResultEndsTheRunAndOfAStaleOneDoesNot(t *testing.T) {
	q, _ := queryRun{}.begin(nil, "select 1", 10)

	if after, fresh := q.finish(resultMsg{id: q.id - 1}); fresh || !after.running() {
		t.Errorf("a stale result: fresh = %v, running = %v; want false, true", fresh, after.running())
	}
	after, fresh := q.finish(resultMsg{id: q.id})
	if !fresh || after.running() {
		t.Errorf("the current result: fresh = %v, running = %v; want true, false", fresh, after.running())
	}
}

func TestAResultSaysWhatCapItWasCutAt(t *testing.T) {
	m := mouseModel(t, "select 1")
	m.maxRows = 5
	m, _ = send(t, m, resultMsg{id: m.query.id, cols: []string{"n"}, rows: [][]string{{"1"}}, truncated: true, maxRows: 7})
	if m.held.truncatedAt != 7 || !strings.Contains(plain(m), "truncated at 7 rows") {
		t.Errorf("truncatedAt = %d; want 7, and the divider to say so:\n%s", m.held.truncatedAt, plain(m))
	}
}
