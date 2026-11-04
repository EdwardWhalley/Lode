// The Kind that describes SQLite Sources to the rest of the program.

package sqlite

import "github.com/EdwardWhalley/lode/internal/adapter"

// Kind describes SQLite Sources.
var Kind = adapter.Kind{
	Name:       "SQLite",
	Extensions: []string{"sqlite", "sqlite3", "db", "db3"},
	Form:       adapter.PathForm(),
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
