// Kind: what an Adapter opens and how, and the checks on a list of Kinds.

package adapter

import (
	"errors"
	"fmt"
	"strings"

	"github.com/EdwardWhalley/lode/internal/sqltext"
)

// Request is what opening a Source asks of an Adapter, in terms no Adapter owns.
type Request struct {
	Location string
	Write    bool
	RowCap   int
}

// Kind describes one kind of Source: what it is called, what it opens and how, and the
// Dialect sqltext reads its engine's SQL with.
type Kind struct {
	Name       string
	Schemes    []string
	Extensions []string
	Form
	ReadOnly bool
	Dialect  sqltext.Dialect
	Open     func(Request) (Adapter, error)
}

// CheckKinds reports why a list of Kinds cannot be used to choose between them.
func CheckKinds(kinds []Kind) error {
	var problems []string
	schemes := map[string]string{}
	extensions := map[string]string{}

	claims := func(owner, what string, claimed []string, seen map[string]string) {
		for _, c := range claimed {
			switch {
			case c == "":
				problems = append(problems, fmt.Sprintf("%s has an empty %s", owner, what))
			case c != strings.ToLower(c):
				problems = append(problems, fmt.Sprintf("%s claims %s %q, which is not lower case", owner, what, c))
			case strings.ContainsAny(c, ".:/"):
				problems = append(problems, fmt.Sprintf("%s claims %s %q: write it without dots, colons or slashes", owner, what, c))
			}
			if first, dup := seen[c]; dup {
				problems = append(problems, fmt.Sprintf("%s and %s both claim %s %q", first, owner, what, c))
				continue
			}
			seen[c] = owner
		}
	}

	for i, k := range kinds {
		owner := fmt.Sprintf("kind %q", k.Name)
		if k.Name == "" {
			owner = fmt.Sprintf("kind #%d", i)
			problems = append(problems, owner+" has no name")
		}
		if k.Open == nil {
			problems = append(problems, owner+" has no open function")
		}
		claims(owner, "scheme", k.Schemes, schemes)
		claims(owner, "extension", k.Extensions, extensions)
		problems = append(problems, k.Form.problems(owner)...)
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}
