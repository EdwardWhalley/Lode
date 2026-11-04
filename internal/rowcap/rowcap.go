// Package rowcap defines the limit on how many rows one query may show.
package rowcap

import "fmt"

// Default is the row cap when none is set, and Limit the largest that can be configured, as
// the grid holds every row in memory.
const (
	Default = 10000
	Limit   = 1_000_000
)

// Check returns an error if n is not a usable row cap.
func Check(n int) error {
	if n < 1 || n > Limit {
		return fmt.Errorf("max rows must be between 1 and %d, got %d", Limit, n)
	}
	return nil
}
