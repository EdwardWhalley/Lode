// Reads a whole number typed as text.

package sourcetext

import (
	"errors"
	"strconv"
)

var (
	ErrNotNumber = errors.New("not a whole number")
	ErrTooLarge  = errors.New("number too large")
)

// Number reads a whole number typed as text: digits only, with no range check.
func Number(s string) (int, error) {
	if s == "" {
		return 0, ErrNotNumber
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, ErrNotNumber
		}
	}
	n, err := strconv.ParseInt(s, 10, 0)
	if err != nil {
		return 0, ErrTooLarge
	}
	return int(n), nil
}
