package tui

import (
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestFormatValue(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"null", nil, "NULL"},
		{"number", int64(42), "42"},
		{"text", "hello", "hello"},
		{"newlines collapse", "a\nb\r\nc", "a↵b↵c"},
		{"tabs become spaces", "a\tb", "a b"},
		{"json bytes shown as text", []byte(`{"a":1}`), `{"a":1}`},
		{"binary bytes shown as hex", []byte{0xde, 0xad, 0xbe, 0xef}, `\xdeadbeef`},
		{"bytes with a control char shown as hex", []byte("a\x00b"), `\x610062`},
		{"date drops the midnight time", time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC), "2025-01-31"},
		{"timestamp keeps the time", time.Date(2025, 1, 31, 9, 5, 7, 0, time.UTC), "2025-01-31 09:05:07"},
		{"timestamp keeps fractional seconds", time.Date(2025, 1, 31, 9, 5, 7, 250_000_000, time.UTC), "2025-01-31 09:05:07.25"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatValue(tc.in); got != tc.want {
				t.Errorf("formatValue(%#v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatValueNeutralisesControlCharacters(t *testing.T) {
	hostile := []any{
		"\x1b[2J\x1b[Hgotcha",
		"bell\x07",
		"del\x7f",
		"c1 \u009b31m",
		[]byte("\x1b]0;title\x07"),
	}
	for _, in := range hostile {
		got := formatValue(in)
		if strings.IndexFunc(got, unicode.IsControl) >= 0 {
			t.Errorf("formatValue(%q) = %q still contains a control character", in, got)
		}
	}
}
