// The Kind that describes MySQL and MariaDB Sources to the rest of the program.

package mysql

import (
	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/sqltext"
)

var schemes = []string{"mysql", "mariadb"}

// Kind describes MySQL and MariaDB Sources.
var Kind = adapter.Kind{
	Name:    "MySQL",
	Schemes: schemes,
	Form:    adapter.ServerForm(3306, schemes),
	Dialect: sqltext.Dialect{BackslashEscapes: true, HashComments: true},
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
