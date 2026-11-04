// Package sourcetext holds the rules for the text a Source is stored as. This file has the
// rule for a Source's name.
package sourcetext

import (
	"errors"
	"regexp"
)

// ErrBadName: a name that is not letters, digits, - and _; it never holds the name.
var ErrBadName = errors.New("a Source name may use letters, digits, - and _ only")

var nameRule = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// CheckName returns ErrBadName unless name can be the name of a stored Source.
func CheckName(name string) error {
	if !nameRule.MatchString(name) {
		return ErrBadName
	}
	return nil
}
