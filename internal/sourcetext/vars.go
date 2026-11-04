// The ${NAME} reference syntax: finding references, expanding them and writing one.

package sourcetext

import (
	"regexp"
	"strings"
)

var (
	varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	varRef  = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

// ValidVarName reports whether name fits ${NAME}: letters, digits and _, not starting with
// a digit.
func ValidVarName(name string) bool { return varName.MatchString(name) }

// Ref is one ${NAME} reference in a text: s[Start:End] is the whole reference.
type Ref struct {
	Start, End int
	Name       string
}

func References(s string) []Ref {
	var refs []Ref
	for _, m := range varRef.FindAllStringSubmatchIndex(s, -1) {
		refs = append(refs, Ref{Start: m[0], End: m[1], Name: s[m[2]:m[3]]})
	}
	return refs
}

// SoleReference reports whether s is exactly one reference and nothing else, and
// the name in it.
func SoleReference(s string) (name string, ok bool) {
	refs := References(s)
	if len(refs) != 1 || refs[0].Start != 0 || refs[0].End != len(s) {
		return "", false
	}
	return refs[0].Name, true
}

// MapAround returns s with f applied to the text between its references, which are left as
// written.
func MapAround(s string, f func(string) string) string {
	var b strings.Builder
	last := 0
	for _, r := range References(s) {
		if r.Start > last {
			b.WriteString(f(s[last:r.Start]))
		}
		b.WriteString(s[r.Start:r.End])
		last = r.End
	}
	if last < len(s) {
		b.WriteString(f(s[last:]))
	}
	return b.String()
}

// Expand replaces each reference in s with what lookup says, and returns the names it did
// not know.
func Expand(s string, lookup func(name string) (string, bool)) (out string, missing []string) {
	var b strings.Builder
	last := 0
	for _, r := range References(s) {
		b.WriteString(s[last:r.Start])
		v, ok := lookup(r.Name)
		if !ok {
			missing = append(missing, r.Name)
		}
		b.WriteString(v)
		last = r.End
	}
	b.WriteString(s[last:])
	return b.String(), missing
}

// Reference is the text of a reference to the variable called name.
func Reference(name string) string { return "${" + name + "}" }
