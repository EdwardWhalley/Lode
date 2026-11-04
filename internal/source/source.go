// Package source defines Sources and opens them with the right Adapter.
package source

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/EdwardWhalley/lode/internal/adapter"
	"github.com/EdwardWhalley/lode/internal/sourcetext"
)

// Source is something the user can query, a database or a file: exactly one of Path and DSN
// is set.
type Source struct {
	Name    string
	Path    string
	DSN     string
	Write   bool
	MaxRows int
}

// Label describes where the Source lives, safe to show on screen: a DSN is masked, as
// sourcetext.MaskDSN says.
func (s Source) Label() string {
	text, isDSN := sourcetext.Of(s.Path, s.DSN)
	if isDSN {
		return sourcetext.MaskDSN(text)
	}
	return text
}

type OpenOption func(*openOptions)

type openOptions struct{ rowCap int }

// WithRowCap tells Open that at most n rows will be shown, so an Adapter can bound the
// work.
func WithRowCap(n int) OpenOption { return func(o *openOptions) { o.rowCap = n } }

// Open opens the Source with the Adapter that claims its DSN scheme or file
// extension, from Kinds.
func Open(s Source, opts ...OpenOption) (adapter.Adapter, error) {
	return open(Kinds, s, opts...)
}

func open(kinds []adapter.Kind, s Source, opts ...OpenOption) (adapter.Adapter, error) {
	var o openOptions
	for _, opt := range opts {
		opt(&o)
	}
	k, what, ok := kindOf(kinds, s)
	if !ok {
		return nil, unsupported(kinds, what)
	}

	return k.Open(adapter.Request{Location: sourcetext.Text(s.Path, s.DSN), Write: s.Write, RowCap: o.rowCap})
}

// KindOf returns the Kind that opens s, taking a DSN as stored, so a ${NAME} does not hide
// the scheme.
func KindOf(s Source) (adapter.Kind, bool) {
	if scheme, _, ok := sourcetext.SplitScheme(s.DSN); ok && scheme != "" {
		s.DSN = scheme + "://"
	}
	k, _, ok := kindOf(Kinds, s)
	return k, ok
}

// kindOf finds the Kind for s. what describes s for an error when none claims it.
func kindOf(kinds []adapter.Kind, s Source) (k adapter.Kind, what string, ok bool) {
	if text, isDSN := sourcetext.Of(s.Path, s.DSN); isDSN {
		scheme := ""
		if u, err := url.Parse(text); err == nil {
			scheme = strings.ToLower(u.Scheme)
		}
		k, ok = find(kinds, scheme, func(k adapter.Kind) []string { return k.Schemes })
		what = "unrecognised DSNs"
		if scheme != "" {
			what = scheme + " DSNs"
		}
		return k, what, ok
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(s.Path)), ".")
	k, ok = find(kinds, ext, func(k adapter.Kind) []string { return k.Extensions })
	what = "files with no extension"
	if ext != "" {
		what = "." + ext + " files"
	}
	return k, what, ok
}

// find returns the Kind whose claims, as chosen by claims, include name.
func find(kinds []adapter.Kind, name string, claims func(adapter.Kind) []string) (adapter.Kind, bool) {
	if name == "" {
		return adapter.Kind{}, false
	}
	for _, k := range kinds {
		for _, c := range claims(k) {
			if c == name {
				return k, true
			}
		}
	}
	return adapter.Kind{}, false
}

// unsupported is the error for a Source no Adapter claims, naming only the scheme or
// extension.
func unsupported(kinds []adapter.Kind, what string) error {
	var schemes, extensions []string
	for _, k := range kinds {
		schemes = append(schemes, k.Schemes...)
		extensions = append(extensions, k.Extensions...)
	}
	return fmt.Errorf("no Adapter for %s; supported DSN schemes: %s; supported file extensions: %s",
		what, strings.Join(schemes, ", "), strings.Join(extensions, ", "))
}

// Resolve turns a command-line argument into a Source: a configured name, else an ad-hoc
// path or DSN.
func Resolve(arg string, named []Source) Source {
	for _, s := range named {
		if s.Name == arg {
			return s
		}
	}
	if sourcetext.LooksLikeDSN(arg) {
		return Source{DSN: arg}
	}
	return Source{Path: arg}
}
