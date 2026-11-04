// Turns a Palette into the styles every view draws with.

package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/EdwardWhalley/lode/internal/editor"
	"github.com/EdwardWhalley/lode/internal/theme"
)

// The styles every part of the UI draws with, replaced as a set by ApplyTheme.
var (
	titleStyle     lipgloss.Style
	dimStyle       lipgloss.Style
	errStyle       lipgloss.Style
	warnStyle      lipgloss.Style
	activeStyle    lipgloss.Style
	headerStyle    lipgloss.Style
	nullStyle      lipgloss.Style
	typeStyle      lipgloss.Style
	highlightStyle lipgloss.Style
	selectionStyle lipgloss.Style
	boxStyle       lipgloss.Style
	boxDimStyle    lipgloss.Style
	boxPickStyle   lipgloss.Style

	logoStyle, logoGlintStyle, logoGleamStyle lipgloss.Style
	logoShadowStyle, logoRuleStyle            lipgloss.Style
	promptStyle                               lipgloss.Style
)

func init() {
	p, _ := theme.Get(theme.Default)
	applyPalette(p)
}

// ApplyTheme switches the package's styles to the named theme.
func ApplyTheme(name string) error {
	p, ok := theme.Get(name)
	if !ok {
		return theme.Check(name)
	}
	applyPalette(p)
	return nil
}

func fg(c string) lipgloss.Style {
	if c == "" {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c))
}

// withBg adds a background colour, if there is one.
func withBg(s lipgloss.Style, c string) lipgloss.Style {
	if c == "" {
		return s
	}
	return s.Background(lipgloss.Color(c))
}

// editorMode is how the current theme wants the editor widget drawn.
var editorMode theme.EditorStyle

// The editor's own styles, kept so a theme can give them back.
var defaultFocused, defaultBlurred = editor.DefaultStyles()

func applyPalette(p theme.Palette) {
	editorMode = p.Editor
	titleStyle = withBg(fg(p.TitleFg), p.TitleBg).Bold(true)
	if p.TitleBg != "" {
		titleStyle = titleStyle.Padding(0, 1)
	}
	dimStyle = orElse(p.Dim, fg(p.Dim), func(s lipgloss.Style) lipgloss.Style { return s.Faint(true) })
	errStyle = orElse(p.Error, fg(p.Error), func(s lipgloss.Style) lipgloss.Style { return s.Reverse(true) })
	warnStyle = orElse(p.Warn, fg(p.Warn), func(s lipgloss.Style) lipgloss.Style { return s.Italic(true) })
	activeStyle = orElse(p.Active, fg(p.Active), func(s lipgloss.Style) lipgloss.Style { return s.Bold(true) })
	headerStyle = fg(p.Header).Bold(true)
	nullStyle = orElse(p.Null, fg(p.Null), func(s lipgloss.Style) lipgloss.Style { return s.Italic(true) })
	typeStyle = orElse(p.Type, fg(p.Type), func(s lipgloss.Style) lipgloss.Style { return s.Faint(true) })

	highlightStyle = withBg(fg(p.HighlightFg), p.HighlightBg)
	if p.HighlightFg == "" && p.HighlightBg == "" {
		highlightStyle = highlightStyle.Reverse(true)
	}

	selectionStyle = withBg(fg(p.SelectionFg), p.SelectionBg)
	if p.SelectionFg == "" && p.SelectionBg == "" {
		selectionStyle = selectionStyle.Underline(true)
	}

	boxStyle = withBg(lipgloss.NewStyle(), p.BoxBg)
	boxDimStyle = withBg(orElse(p.BoxDim, fg(p.BoxDim), func(s lipgloss.Style) lipgloss.Style { return s.Faint(true) }), p.BoxBg)
	logoStyle = orElse(p.LogoFg, fg(p.LogoFg), func(s lipgloss.Style) lipgloss.Style { return s.Bold(true) })
	logoGlintStyle = orElse(p.LogoGlint, fg(p.LogoGlint), func(s lipgloss.Style) lipgloss.Style { return s.Faint(true) })
	logoGleamStyle = orElse(p.LogoGleam, fg(p.LogoGleam), func(s lipgloss.Style) lipgloss.Style { return s.Faint(true) })
	logoShadowStyle = orElse(p.LogoShadow, fg(p.LogoShadow), func(s lipgloss.Style) lipgloss.Style { return s.Faint(true) })
	logoRuleStyle = orElse(p.LogoRule, fg(p.LogoRule), func(s lipgloss.Style) lipgloss.Style { return s.Faint(true) })
	promptStyle = orElse(p.PromptFg, fg(p.PromptFg), func(s lipgloss.Style) lipgloss.Style { return s.Bold(true) })
	if p.LogoFg != "" {
		logoStyle = logoStyle.Bold(true)
	}

	boxPickStyle = withBg(fg(p.BoxPickFg), p.BoxPickBg)
	if p.BoxPickFg == "" && p.BoxPickBg == "" {
		boxPickStyle = boxPickStyle.Reverse(true)
	}
}

// WithTheme starts the Model in the named theme; an unknown name leaves the default.
func WithTheme(name string) Option {
	return func(m *Model) {
		if p, ok := theme.Get(name); ok {
			m.themeName = p.Name
		}
	}
}

// newTextInput is a text field in the current theme.
func newTextInput() textinput.Model {
	in := textinput.New()
	in.PlaceholderStyle = dimStyle
	return in
}

// WithNoColour holds the Model to mono, for a terminal that must show no colour; it wins
// over WithTheme.
func WithNoColour() Option {
	return func(m *Model) { m.noColour = true }
}

// nextTheme is F2: the next colour theme, until the program ends.
func (m Model) nextTheme() (Model, tea.Cmd) {
	if m.noColour {
		m.isError = false
		m.status = "no colour to show here, so the theme stays " + theme.Mono
		return m, nil
	}
	m.themeName = theme.Next(m.themeName)
	ApplyTheme(m.themeName)
	m.restyleEditor()
	m.isError = false
	m.status = "theme: " + m.themeName
	return m, nil
}

func (m *Model) restyleEditor() {
	plain := lipgloss.NewStyle()
	switch editorMode {
	case theme.EditorThemed:
		themed := editor.Style{
			Base: plain, CursorLine: plain, EndOfBuffer: plain,
			Placeholder: dimStyle, Prompt: activeStyle, Text: plain, Selection: selectionStyle,
		}
		m.editor.FocusedStyle = themed
		themed.Prompt = dimStyle
		m.editor.BlurredStyle = themed
	case theme.EditorPlain:
		none := editor.Style{
			Base: plain, CursorLine: plain, EndOfBuffer: plain,
			Placeholder: dimStyle, Prompt: plain, Text: plain, Selection: selectionStyle,
		}
		m.editor.BlurredStyle = none
		none.Prompt = activeStyle
		m.editor.FocusedStyle = none
	default:
		m.editor.FocusedStyle, m.editor.BlurredStyle = defaultFocused, defaultBlurred
		m.editor.FocusedStyle.Selection, m.editor.BlurredStyle.Selection = selectionStyle, selectionStyle
	}
}

// orElse is the style for a colour or, for one the theme leaves empty, the style with an
// attribute applied instead.
func orElse(colour string, styled lipgloss.Style, attribute func(lipgloss.Style) lipgloss.Style) lipgloss.Style {
	if colour != "" {
		return styled
	}
	return attribute(styled)
}
