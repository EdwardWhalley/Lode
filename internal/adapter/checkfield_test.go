package adapter_test

import (
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

func TestCheckFieldReadsANumberAsDigitsInRange(t *testing.T) {
	f := adapter.Field{Key: "n", Type: adapter.Number, Min: 1, Max: 65535}
	for _, tc := range []struct {
		typed string
		want  string
	}{
		{"1", ""},
		{"65535", ""},
		{"007", ""},
		{"0", "between 1 and 65535"},
		{"65536", "between 1 and 65535"},
		{strings.Repeat("1", 12), "between 1 and 65535"},
		{strings.Repeat("9", 40), "between 1 and 65535"},
		{"+5", "whole number"},
		{"-1", "whole number"},
		{" 5", "whole number"},
		{"5x", "whole number"},
	} {
		err := adapter.CheckField(f, tc.typed)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("CheckField(%q) = %v, want nil", tc.typed, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("CheckField(%q) = %v, want %q", tc.typed, err, tc.want)
		}
	}
}
