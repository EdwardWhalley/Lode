// A Location is where a Source lives, a path or a DSN: the questions asked of the pair.

package sourcetext

import (
	"errors"
	"strings"
)

var (
	ErrNoLocation    = errors.New("needs a path or a dsn")
	ErrBothLocations = errors.New("has both a path and a dsn; use one")
)

// CheckOne reports ErrNoLocation or ErrBothLocations unless exactly one of path and
// dsn is set.
func CheckOne(path, dsn string) error {
	switch {
	case path == "" && dsn == "":
		return ErrNoLocation
	case path != "" && dsn != "":
		return ErrBothLocations
	}
	return nil
}

// Of returns the text a Source or Section holds, the DSN if it has one, and whether that is
// the DSN.
func Of(path, dsn string) (text string, isDSN bool) {
	if dsn != "" {
		return dsn, true
	}
	return path, false
}

// Text is the text of Of, for a caller that does not care which it is.
func Text(path, dsn string) string {
	text, _ := Of(path, dsn)
	return text
}

// SplitScheme cuts text at its first "://"; ok is false, and both parts empty, if there is
// none.
func SplitScheme(text string) (scheme, rest string, ok bool) {
	scheme, rest, ok = strings.Cut(text, "://")
	if !ok {
		return "", "", false
	}
	return scheme, rest, true
}

// LooksLikeDSN reports whether text typed by a user is a DSN and not a path.
func LooksLikeDSN(text string) bool {
	_, _, ok := SplitScheme(text)
	return ok
}
