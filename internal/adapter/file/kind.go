// The Kind that describes file Sources to the rest of the program.

package file

import (
	"fmt"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

// Kind describes file Sources.
var Kind = adapter.Kind{
	Name:       "file",
	Extensions: []string{"csv", "tsv", "txt", "json", "ndjson", "jsonl", "parquet"},
	Form:       adapter.PathForm(),
	ReadOnly:   true,
	Dialect:    sqltext.Dialect{NestedComments: true},
	Open: func(r adapter.Request) (adapter.Adapter, error) {
		if r.Write {
			return nil, fmt.Errorf("%s: writes are not supported for file Sources", r.Location)
		}
		var opts []Option
		if r.RowCap > 0 {
			opts = append(opts, WithRowLimit(r.RowCap+1))
		}
		a, err := Open(r.Location, opts...)
		if err != nil {
			return nil, err
		}
		return a, nil
	},
}
