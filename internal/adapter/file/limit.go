// Bounds a query to the row cap by wrapping it in a LIMIT, because DuckDB holds a whole
// result before returning a row.

package file

import (
	"fmt"
	"strings"

	"github.com/EdwardWhalley/lode/internal/sqltext"
)

// limitQuery bounds query to at most n rows by wrapping it in a LIMIT, if it is sure it is
// one plain query. It reads query as sqltext does with the file Kind's Dialect.
func limitQuery(query string, n int) string {
	var single sqltext.Statement
	statements, open := 0, false
	for _, s := range sqltext.Statements(query, Kind.Dialect) {
		open = open || s.Open
		if !s.Blank {
			single = s
			statements++
		}
	}
	if statements != 1 || open || !isQuery(sqltext.Words(query[single.Code:single.CodeEnd], Kind.Dialect)) {
		return query
	}
	return fmt.Sprintf("SELECT * FROM (\n%s\n) LIMIT %d", query[single.Start:single.CodeEnd], n)
}

// mainKeywords are the words that can start the statement that follows a WITH clause.
var mainKeywords = map[string]bool{
	"select": true, "from": true, "values": true, "table": true,
	"insert": true, "update": true, "delete": true, "merge": true, "copy": true,
}

// queryKeywords are the statements it is safe to wrap.
var queryKeywords = map[string]bool{"select": true, "from": true, "values": true, "table": true}

func isQuery(words []sqltext.Word) bool {
	if len(words) == 0 {
		return false
	}
	switch first := strings.ToLower(words[0].Text); {
	case queryKeywords[first]:
		return true
	case first == "with":
		for _, w := range words[1:] {
			if text := strings.ToLower(w.Text); w.Depth == 0 && mainKeywords[text] {
				return queryKeywords[text]
			}
		}
	}
	return false
}
