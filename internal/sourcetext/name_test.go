package sourcetext_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/EdwardWhalley/lode/internal/sourcetext"
)

func TestSourceNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"a", true},
		{"A-b_9", true},
		{"prod", true},
		{"-", true},
		{"_", true},
		{"my db", false},
		{"a.b", false},
		{"", false},
		{"é", false},
		{"a/b", false},
		{"a]", false},
		{`a"b`, false},
		{"a\n", false},
	} {
		err := sourcetext.CheckName(tc.name)
		if tc.ok && err != nil {
			t.Errorf("CheckName(%q) = %v, want nil", tc.name, err)
		}
		if !tc.ok && !errors.Is(err, sourcetext.ErrBadName) {
			t.Errorf("CheckName(%q) = %v, want ErrBadName", tc.name, err)
		}
	}
}

func TestTheNameErrorDoesNotRepeatTheName(t *testing.T) {
	err := sourcetext.CheckName("secret value")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("err = %v, want an error that does not repeat the name", err)
	}
}
