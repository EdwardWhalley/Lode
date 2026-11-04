package adapter_test

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

// exact is a decimal as a driver hands one over: it has String and Float64.
type exact string

func (e exact) String() string   { return string(e) }
func (e exact) Float64() float64 { return 0 }

type onlyStringer struct{}

func (onlyStringer) String() string { return "x" }

func TestEveryAllowedValueIsAllowed(t *testing.T) {
	for _, v := range []any{
		nil, true, int64(-1), uint64(18446744073709551615), float32(0.1), float64(0.1), // bounded: a value, not a count
		big.NewInt(1), "text", []byte{0xff}, time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC), exact("1.50"),
		[]any{int64(1), nil, []any{"nested"}},
		map[string]any{"a": exact("2.50"), "b": map[string]any{"c": []byte("x")}},
	} {
		if err := adapter.CheckValue(v); err != nil {
			t.Errorf("%#v: %v", v, err)
		}
	}
}

func TestAValueOfAnyOtherTypeIsRefusedAndNamed(t *testing.T) {
	for _, c := range []struct {
		v    any
		says string
	}{
		{int32(1), "int32"},
		{uint8(1), "uint8"},
		{struct{}{}, "struct {}"},
		{[]string{"a"}, "[]string"},
		{map[int]any{1: "a"}, "map[int]interface {}"},
		{onlyStringer{}, "onlyStringer"},
		{[]any{int64(1), int32(2)}, "int32"},
		{map[string]any{"a": []any{int16(1)}}, "int16"},
		{(*big.Float)(nil), "*big.Float"},
	} {
		err := adapter.CheckValue(c.v)
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%#v: got %v, want it refused naming %s", c.v, err, c.says)
		}
	}
}
