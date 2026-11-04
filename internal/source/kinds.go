// The list of Kinds, and the only file here that imports an Adapter package.

package source

import (
	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/adapter/file"
	"github.com/EdwardWhalley/lode/internal/adapter/mysql"
	"github.com/EdwardWhalley/lode/internal/adapter/postgres"
	"github.com/EdwardWhalley/lode/internal/adapter/sqlite"
)

// Kinds lists every Adapter, and is the only place that names them.
var Kinds = []adapter.Kind{
	file.Kind,
	sqlite.Kind,
	postgres.Kind,
	mysql.Kind,
}
