package sourcetext_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/sourcetext"
)

func TestNumberReadsDigitsOnly(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"0", 0},
		{"7", 7},
		{"007", 7},
		{"65535", 65535},
		{strings.Repeat("1", 12), 111111111111}, // bounded: a plain number, no query
	} {
		n, err := sourcetext.Number(tc.in)
		if err != nil || n != tc.want {
			t.Errorf("Number(%q) = %d, %v; want %d", tc.in, n, err, tc.want)
		}
	}
}

func TestNumberRefusesAnythingElse(t *testing.T) {
	for _, in := range []string{"", "+5", "-1", " 5", "5 ", "1.5", "1e3", "0x10", "1_000", "٣", "5\n"} {
		if _, err := sourcetext.Number(in); !errors.Is(err, sourcetext.ErrNotNumber) {
			t.Errorf("Number(%q) err = %v, want ErrNotNumber", in, err)
		}
	}
}

func TestNumberTooLargeIsStillANumber(t *testing.T) {
	_, err := sourcetext.Number(strings.Repeat("9", 40))
	if !errors.Is(err, sourcetext.ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
	if errors.Is(err, sourcetext.ErrNotNumber) {
		t.Errorf("err = %v: too many digits is not 'not a number'", err)
	}
}

func TestNumberErrorsDoNotRepeatTheText(t *testing.T) {
	for _, in := range []string{"secret", strings.Repeat("7", 40)} {
		if _, err := sourcetext.Number(in); err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "777") {
			t.Errorf("Number(%q) err = %v, want one that does not repeat the text", in, err)
		}
	}
}
