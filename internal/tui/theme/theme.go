// Package theme defines the visual language of the ghapk TUI: palettes,
// glyph sets and the lipgloss styles derived from them.
package theme

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Palette is the raw color set of a theme. Colors degrade gracefully:
// lipgloss maps them to whatever the terminal supports.
type Palette struct {
	Name        string
	Description string

	Bg          lipgloss.Color // app background
	BgPanel     lipgloss.Color // panel background
	BgActive    lipgloss.Color // focused panel background
	Border      lipgloss.Color // unfocused panel border
	BorderFocus lipgloss.Color // focused panel border

	Text      lipgloss.Color
	Muted     lipgloss.Color
	Dim       lipgloss.Color
	Accent    lipgloss.Color // primary accent (android green in the default theme)
	AccentAlt lipgloss.Color // secondary accent (github actions purple)
	OnAccent  lipgloss.Color // text drawn on top of Accent

	Success   lipgloss.Color
	Warning   lipgloss.Color
	Error     lipgloss.Color
	Info      lipgloss.Color
	Selection lipgloss.Color // selected row background
}

// Themes keyed by config name.
var Themes = map[string]Palette{
	"action": {
		Name: "action", Description: "GitHub-dark canvas, Android-green accent (default)",
		Bg: "#0D1117", BgPanel: "#0D1117", BgActive: "#111820",
		Border: "#30363D", BorderFocus: "#3DDC84",
		Text: "#E6EDF3", Muted: "#8B949E", Dim: "#6E7681",
		Accent: "#3DDC84", AccentAlt: "#A371F7", OnAccent: "#06281A",
		Success: "#3FB950", Warning: "#D29922", Error: "#F85149", Info: "#58A6FF",
		Selection: "#163325",
	},
	"mocha": {
		Name: "mocha", Description: "Catppuccin Mocha — soft latte-dark, peach accent",
		Bg: "#1E1E2E", BgPanel: "#1E1E2E", BgActive: "#232336",
		Border: "#45475A", BorderFocus: "#FAB387",
		Text: "#CDD6F4", Muted: "#A6ADC8", Dim: "#6C7086",
		Accent: "#FAB387", AccentAlt: "#CBA6F7", OnAccent: "#2A1D12",
		Success: "#A6E3A1", Warning: "#F9E2AF", Error: "#F38BA8", Info: "#89B4FA",
		Selection: "#3A2E2A",
	},
	"emerald": {
		Name: "emerald", Description: "Termux homage — phosphor green on near-black",
		Bg: "#000000", BgPanel: "#000000", BgActive: "#04140A",
		Border: "#1F3D2B", BorderFocus: "#00E676",
		Text: "#D7FFE7", Muted: "#7FBF97", Dim: "#3F7A57",
		Accent: "#00E676", AccentAlt: "#26C6DA", OnAccent: "#00150A",
		Success: "#00E676", Warning: "#FFD54F", Error: "#FF5252", Info: "#40C4FF",
		Selection: "#062B18",
	},
	"paper": {
		Name: "paper", Description: "GitHub light — for bright rooms",
		Bg: "#FFFFFF", BgPanel: "#FFFFFF", BgActive: "#F3F7EE",
		Border: "#D0D7DE", BorderFocus: "#1A7F37",
		Text: "#1F2328", Muted: "#59636E", Dim: "#8C959F",
		Accent: "#1A7F37", AccentAlt: "#8250DF", OnAccent: "#FFFFFF",
		Success: "#1A7F37", Warning: "#9A6700", Error: "#D1242F", Info: "#0969DA",
		Selection: "#DAFBE1",
	},
}

// Order is the cycle order for the 't' key.
var Order = []string{"action", "mocha", "emerald", "paper"}

// Get returns a palette by name, falling back to the default.
func Get(name string) Palette {
	if p, ok := Themes[name]; ok {
		return p
	}
	return Themes["action"]
}

// Next follows the cycle order.
func Next(name string) string {
	for i, n := range Order {
		if n == name {
			return Order[(i+1)%len(Order)]
		}
	}
	return Order[0]
}

// Glyphs are the icons used across the UI.
type Glyphs struct {
	OK        string
	Fail      string
	Running   string
	Queued    string
	Skipped   string
	Cancelled string
	Neutral   string
	Star      string
	StarEmpty string
	Dot       string
	Arrow     string
	Package   string
	Rocket    string
	Gear      string
	Branch    string
	Ellipsis  string
}

// UnicodeGlyphs is the default set.
var UnicodeGlyphs = Glyphs{
	OK: "✓", Fail: "✗", Running: "◐", Queued: "◌", Skipped: "⊘",
	Cancelled: "⊗", Neutral: "·", Star: "★", StarEmpty: "☆",
	Dot: "•", Arrow: "▶", Package: "", Rocket: "",
	Gear: "⚙", Branch: "", Ellipsis: "…",
}

// ASCIIGlyphs is the fallback for terminals that cannot draw icons.
var ASCIIGlyphs = Glyphs{
	OK: "+", Fail: "x", Running: "~", Queued: "o", Skipped: "-",
	Cancelled: "x", Neutral: ".", Star: "*", StarEmpty: ".",
	Dot: "-", Arrow: ">", Package: "A", Rocket: ">",
	Gear: "*", Branch: "@", Ellipsis: "...",
}

// Styles are the derived lipgloss styles for a palette.
type Styles struct {
	P Palette
	G Glyphs

	Header        lipgloss.Style
	HeaderAccent  lipgloss.Style
	PanelBorder   lipgloss.Style
	PanelFocus    lipgloss.Style
	PanelTitle    lipgloss.Style
	PanelTitleFoc lipgloss.Style
	PanelBody     lipgloss.Style

	Row        lipgloss.Style
	RowSel     lipgloss.Style
	RowSelBlur lipgloss.Style
	Text       lipgloss.Style
	Bold       lipgloss.Style
	Muted      lipgloss.Style
	Dim        lipgloss.Style
	Accent     lipgloss.Style
	AccentAlt  lipgloss.Style
	Hotkey     lipgloss.Style

	OK      lipgloss.Style
	Warn    lipgloss.Style
	Err     lipgloss.Style
	Info    lipgloss.Style
	Running lipgloss.Style

	StatusBarBg   lipgloss.Style
	StatusKey     lipgloss.Style
	StatusText    lipgloss.Style
	Toast         lipgloss.Style
	ButtonFocus   lipgloss.Style
	ButtonBlur    lipgloss.Style
	FieldFocused  lipgloss.Style
	BadgeStyle    lipgloss.Style
	ProgressFull  lipgloss.Color
	ProgressEmpty lipgloss.Color
}

// New builds the style set for a palette. Terminals without Unicode
// (TERM=dumb) or GHAPK_ASCII=1 get the ASCII glyph set instead.
func New(p Palette) Styles {
	s := Styles{P: p, G: UnicodeGlyphs}
	if asciiMode() {
		s.G = ASCIIGlyphs
	}
	if noColor := os.Getenv("NO_COLOR"); noColor != "" {
		s = noColorStyles(s)
	}
	base := lipgloss.NewStyle().Background(p.Bg)

	s.Header = base.Foreground(p.OnAccent).Background(p.Accent).Bold(true).PaddingLeft(1).PaddingRight(1)
	s.HeaderAccent = base.Foreground(p.Accent).Bold(true)

	s.PanelBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Border).
		Background(p.BgPanel)
	s.PanelFocus = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.BorderFocus).
		Background(p.BgActive)
	s.PanelTitle = lipgloss.NewStyle().Foreground(p.Muted).Bold(true)
	s.PanelTitleFoc = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	s.PanelBody = lipgloss.NewStyle().Foreground(p.Text)

	s.Row = lipgloss.NewStyle().Foreground(p.Text)
	s.RowSel = lipgloss.NewStyle().Foreground(p.Text).Background(p.Selection).Bold(true)
	s.RowSelBlur = lipgloss.NewStyle().Foreground(p.Muted).Background(p.Selection)
	s.Text = lipgloss.NewStyle().Foreground(p.Text)
	s.Bold = lipgloss.NewStyle().Foreground(p.Text).Bold(true)
	s.Muted = lipgloss.NewStyle().Foreground(p.Muted)
	s.Dim = lipgloss.NewStyle().Foreground(p.Dim)
	s.Accent = lipgloss.NewStyle().Foreground(p.Accent)
	s.AccentAlt = lipgloss.NewStyle().Foreground(p.AccentAlt)
	s.Hotkey = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)

	s.OK = lipgloss.NewStyle().Foreground(p.Success)
	s.Warn = lipgloss.NewStyle().Foreground(p.Warning)
	s.Err = lipgloss.NewStyle().Foreground(p.Error)
	s.Info = lipgloss.NewStyle().Foreground(p.Info)
	s.Running = lipgloss.NewStyle().Foreground(p.Warning)

	s.StatusBarBg = lipgloss.NewStyle().Background(p.BgPanel)
	s.StatusKey = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	s.StatusText = lipgloss.NewStyle().Foreground(p.Muted)
	s.Toast = lipgloss.NewStyle().Foreground(p.OnAccent).Background(p.AccentAlt).PaddingLeft(1).PaddingRight(1)
	s.ButtonFocus = lipgloss.NewStyle().Foreground(p.OnAccent).Background(p.Accent).PaddingLeft(2).PaddingRight(2).Bold(true)
	s.ButtonBlur = lipgloss.NewStyle().Foreground(p.Text).Background(p.BgPanel).PaddingLeft(2).PaddingRight(2)
	s.FieldFocused = lipgloss.NewStyle().Foreground(p.Text).Background(p.Selection)
	s.BadgeStyle = lipgloss.NewStyle().Foreground(p.OnAccent).Background(p.AccentAlt).PaddingLeft(1).PaddingRight(1)
	s.ProgressFull = p.Accent
	s.ProgressEmpty = p.Border
	return s
}

// asciiMode reports whether the terminal should get plain-ASCII glyphs.
func asciiMode() bool {
	switch os.Getenv("GHAPK_ASCII") {
	case "1", "true", "on", "yes":
		return true
	}
	t := os.Getenv("TERM")
	return t == "" || t == "dumb"
}

// noColorStyles greys everything out when NO_COLOR is set.
func noColorStyles(s Styles) Styles {
	plain := lipgloss.NewStyle()
	s.Header = plain
	s.HeaderAccent = plain
	s.PanelTitle = plain
	s.PanelTitleFoc = plain
	s.RowSel = plain.Reverse(true)
	s.RowSelBlur = plain.Reverse(true)
	s.Accent = plain
	s.AccentAlt = plain
	s.Hotkey = plain
	s.OK = plain
	s.Warn = plain
	s.Err = plain
	s.Info = plain
	s.Running = plain
	s.StatusKey = plain
	s.Toast = plain.Reverse(true)
	s.ButtonFocus = plain.Reverse(true)
	return s
}
