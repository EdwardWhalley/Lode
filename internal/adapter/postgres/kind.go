// The Kind that describes PostgreSQL Sources to the rest of the program.

package postgres

import (
	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

var schemes = []string{"postgres", "postgresql"}

// Kind describes PostgreSQL Sources.
var Kind = adapter.Kind{
	Name:    "PostgreSQL",
	Schemes: schemes,
	Form:    adapter.ServerForm(5432, schemes),
	Dialect: sqltext.Dialect{NestedComments: true},
	Open: func(r adapter.Request) (adapter.Adapter, error) {
		var opts []Option
		if r.Write {
			opts = append(opts, WithWrite())
		}
		a, err := Open(r.Location, opts...)
		if err != nil {
			return nil, err
		}
		return a, nil
	},
}
