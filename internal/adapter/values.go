// The types Rows.Values may hold, written down once so every Adapter and every reader of a
// value agree on them.

package adapter

import (
	"fmt"
	"math/big"
	"time"
)

// Decimal is a driver's exact number. The UI and export tell one by these methods alone,
// because they import no driver, and write it with String.
type Decimal interface {
	String() string
	Float64() float64
}

// CheckValue says why v is not a value Rows.Values may hold, or nil if it is. The allowed
// values are nil, bool, int64, float64, string, []byte, time.Time, a []any and a
// map[string]any whose values are allowed in turn, and four that Adapters need: uint64 for
// an unsigned integer past int64 (MySQL, DuckDB), float32 for a single-precision column,
// whose digits widening would change (MySQL, DuckDB), *big.Int for DuckDB's HUGEINT, and a
// Decimal (DuckDB). Narrower integers are int64 by the time they leave an Adapter.
func CheckValue(v any) error {
	switch v := v.(type) {
	case nil, bool, int64, uint64, float32, float64, string, []byte, time.Time:
		return nil
	case *big.Int:
		return nil
	case []any:
		for _, e := range v {
			if err := CheckValue(e); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		for _, e := range v {
			if err := CheckValue(e); err != nil {
				return err
			}
		}
		return nil
	case Decimal:
		return nil
	}
	return fmt.Errorf("a value of type %T is not one Rows.Values may hold (see adapter.CheckValue)", v)
}
