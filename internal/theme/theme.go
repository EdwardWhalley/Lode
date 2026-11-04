// Package theme holds the colour palettes of the terminal UI, as ANSI 256 colour numbers;
// an empty colour leaves the terminal's own.
package theme

import (
	"fmt"
	"strings"
)

// Default is the theme used when none is chosen.
const Default = "bubbletea"

// Mono is the theme that names no colour.
const Mono = "mono"

// EditorStyle says how the text editor is coloured: its own colours, the theme's, or none.
type EditorStyle int

const (
	EditorDefault EditorStyle = iota
	EditorThemed
	EditorPlain
)

// Palette is every colour the UI uses, as ANSI 256 numbers; a Highlight or Selection with
// none is drawn by an attribute.
type Palette struct {
	Name   string
	Editor EditorStyle

	TitleFg, TitleBg                                             string
	Dim                                                          string
	Error, Warn                                                  string
	Active, Header                                               string
	Null, Type                                                   string
	HighlightFg, HighlightBg                                     string
	SelectionFg, SelectionBg                                     string
	BoxBg, BoxDim, BoxPickFg, BoxPickBg                          string
	LogoFg, LogoGlint, LogoGleam, LogoShadow, LogoRule, PromptFg string
}

// themes is in the order F2 cycles through them.
var themes = []Palette{
	{
		Name:    "bubbletea",
		Editor:  EditorThemed,
		TitleFg: "230", TitleBg: "62",
		Dim:   "241",
		Error: "203", Warn: "214",
		Active: "205", Header: "212",
		Null: "241", Type: "99",
		HighlightFg: "229", HighlightBg: "57",
		SelectionFg: "255", SelectionBg: "25",
		BoxBg: "236", BoxDim: "245", BoxPickFg: "229", BoxPickBg: "57",
		LogoFg: "33", LogoGlint: "75", LogoGleam: "159", LogoShadow: "24", LogoRule: "25", PromptFg: "111",
	},
	{
		Name:  "classic",
		Dim:   "241",
		Error: "203", Warn: "214",
		Active: "39", Header: "39",
		Null: "241", Type: "241",
		SelectionFg: "255", SelectionBg: "24",
		BoxBg: "236", BoxDim: "245", BoxPickFg: "255", BoxPickBg: "24",
		LogoFg: "33", LogoGlint: "75", LogoGleam: "159", LogoShadow: "24", LogoRule: "25", PromptFg: "111",
	},
	{
		Name:    "light",
		Editor:  EditorThemed,
		TitleFg: "255", TitleBg: "54",
		Dim:   "243",
		Error: "160", Warn: "130",
		Active: "162", Header: "90",
		Null: "243", Type: "55",
		HighlightFg: "255", HighlightBg: "62",
		SelectionFg: "16", SelectionBg: "153",
		BoxBg: "254", BoxDim: "243", BoxPickFg: "255", BoxPickBg: "62",
		LogoFg: "25", LogoGlint: "33", LogoGleam: "75", LogoShadow: "153", LogoRule: "67", PromptFg: "25",
	},
	{
		Name:   Mono,
		Editor: EditorPlain,
	},
}

// Names lists the themes in the order F2 cycles through them.
func Names() []string {
	out := make([]string, len(themes))
	for i, p := range themes {
		out[i] = p.Name
	}
	return out
}

// Get returns the theme with that name, ignoring case.
func Get(name string) (Palette, bool) {
	for _, p := range themes {
		if strings.EqualFold(p.Name, strings.TrimSpace(name)) {
			return p, true
		}
	}
	return Palette{}, false
}

// Check reports whether name is a theme, with an error listing the real ones if not.
func Check(name string) error {
	if _, ok := Get(name); ok {
		return nil
	}
	return fmt.Errorf("unknown theme %q (available: %s)", name, strings.Join(Names(), ", "))
}

// Next returns the theme after name, wrapping round. An unknown name gives the first.
func Next(name string) string {
	for i, p := range themes {
		if strings.EqualFold(p.Name, name) {
			return themes[(i+1)%len(themes)].Name
		}
	}
	return themes[0].Name
}
