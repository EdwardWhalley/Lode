package tui

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestNothingIsHeldBeforeTheFirstSuccess(t *testing.T) {
	m := New(nil, "values")
	if m.held.exportable() {
		t.Fatal("a new Model holds a result")
	}
	m, _ = send(t, m, resultMsg{id: m.query.id, err: errors.New("boom")})
	if m.held.exportable() {
		t.Error("a failed first query left a held result")
	}
}

func TestASuccessReplacesTheHeldResultWithItsOwnCapAndSource(t *testing.T) {
	m := withResult(t, sample)
	m, _ = send(t, m, resultMsg{
		id: m.query.id, cols: []string{"x"}, rows: [][]string{{"1"}},
		rawCols: []string{"x"}, raw: [][]any{{int64(1)}}, truncated: true, maxRows: 9,
	})
	h := m.held
	if !h.exportable() || !reflect.DeepEqual(h.rawCols, []string{"x"}) || !reflect.DeepEqual(h.raw, [][]any{{int64(1)}}) ||
		!h.truncated || h.truncatedAt != 9 || h.from != "values" {
		t.Errorf("held = %+v; want the new result, cut at 9 rows, from %q", h, "values")
	}
}

func TestAFailedOrCancelledQueryKeepsTheHeldResult(t *testing.T) {
	for name, err := range map[string]error{
		"failed":    errors.New("boom"),
		"cancelled": context.Canceled,
	} {
		m := withResult(t, sample)
		before := m.held
		m, _ = send(t, m, resultMsg{id: m.query.id, err: err})
		if !reflect.DeepEqual(m.held, before) {
			t.Errorf("a %s query changed the held result: %+v, was %+v", name, m.held, before)
		}
	}
}

func TestAHeldResultKnowsWhenItCameFromAnotherSource(t *testing.T) {
	h := heldResult{ok: true, from: "label", fromKey: "a"}
	if h.fromElsewhere("label", "a") {
		t.Error("the same Label and key are the same Source")
	}
	if !h.fromElsewhere("label", "b") {
		t.Error("two Sources can share a Label")
	}
	if !h.fromElsewhere("moved", "a") {
		t.Error("a Source whose location was edited no longer points where the rows came from")
	}
	if (heldResult{}).fromElsewhere("label", "b") {
		t.Error("nothing held is not from another Source")
	}
}
