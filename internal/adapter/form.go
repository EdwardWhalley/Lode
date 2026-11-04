// What a Kind's form asks for: its fields, the check on each, and the form for a single
// path.

package adapter

import (
	"errors"
	"fmt"
	"strings"

	"github.com/EdwardWhalley/lode/internal/sourcetext"
)

// FieldType says how a form field is entered and checked: free text, a whole number or an
// environment variable's name.
type FieldType int

const (
	Text FieldType = iota
	Number
	EnvVar
)

// Field describes one input of a Source form.
type Field struct {
	Key      string
	Label    string
	Type     FieldType
	Required bool
	Default  string
	Help     string
	Min, Max int
}

// Values holds what a form's fields contain, by Field.Key.
type Values map[string]string

// Form is what a Kind needs for the user to describe a Source in fields; Compose and Parse
// turn them into the stored location and back.
type Form struct {
	Fields  []Field
	Compose func(values Values, keep string) (string, error)
	Parse   func(location string) (values Values, literalPassword bool, err error)
}

// problems lists what is inconsistent about the form of the Kind named by owner.
func (f Form) problems(owner string) []string {
	var out []string
	if len(f.Fields) == 0 {
		if f.Compose != nil || f.Parse != nil {
			out = append(out, owner+" has a compose or parse function but no fields")
		}
		return out
	}
	seen := map[string]bool{}
	for _, field := range f.Fields {
		switch {
		case field.Key == "":
			out = append(out, owner+" has a form field with an empty key")
		case seen[field.Key]:
			out = append(out, fmt.Sprintf("%s has the form field %q twice", owner, field.Key))
		}
		seen[field.Key] = true
	}
	if f.Compose == nil {
		out = append(out, owner+" has form fields but no compose function")
	}
	if f.Parse == nil {
		out = append(out, owner+" has form fields but no parse function")
	}
	return out
}

// PathForm is the form of a Kind whose Source is a single path.
func PathForm() Form {
	return Form{
		Fields: []Field{{
			Key: "path", Label: "Path", Type: Text, Required: true,
			Help: "Where the file is; ~/ is the home folder",
		}},
		Compose: func(v Values, _ string) (string, error) {
			if strings.TrimSpace(v["path"]) == "" {
				return "", errors.New("path is required")
			}
			return v["path"], nil
		},
		Parse: func(location string) (Values, bool, error) {
			if strings.TrimSpace(location) == "" {
				return nil, false, errUnplaceable
			}
			return Values{"path": location}, false, nil
		},
	}
}

// CheckField reports what is wrong with a value typed into field f, or nil, never repeating
// the value.
func CheckField(f Field, value string) error {
	if strings.TrimSpace(value) == "" {
		if f.Required {
			return errors.New("required")
		}
		return nil
	}
	switch f.Type {
	case Number:
		n, err := sourcetext.Number(value)
		if errors.Is(err, sourcetext.ErrNotNumber) {
			return errors.New("must be a whole number")
		}
		if err != nil || n < f.Min || n > f.Max {
			return fmt.Errorf("must be between %d and %d", f.Min, f.Max)
		}
	case EnvVar:
		if !sourcetext.ValidVarName(value) {
			return errors.New("must be a variable name: letters, digits and _, not starting with a digit")
		}
	}
	return nil
}
