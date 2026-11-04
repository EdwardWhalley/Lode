package rowcap_test

import (
	"testing"

	"github.com/EdwardWhalley/lode/internal/rowcap"
)

func TestCheck(t *testing.T) {
	for _, n := range []int{1, 10, rowcap.Default, rowcap.Limit} {
		if err := rowcap.Check(n); err != nil {
			t.Errorf("Check(%d) = %v, want it accepted", n, err)
		}
	}
	for _, n := range []int{0, -1, rowcap.Limit + 1} {
		if err := rowcap.Check(n); err == nil {
			t.Errorf("Check(%d) accepted, want an error", n)
		}
	}
}

func TestDefaultIsUsable(t *testing.T) {
	if err := rowcap.Check(rowcap.Default); err != nil {
		t.Fatal(err)
	}
}
