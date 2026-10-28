// Package ui holds ghapk's look and its widgets: colour schemes, glyph sets,
// and the small building blocks (boxes, lists, inputs, progress bars) the
// screens in package tui are assembled from. Nothing here knows about GitHub.
package ui

import (
	"strconv"
	"strings"

	"ghapk/internal/term"
)

// Theme is one colour scheme. Every colour carries a 16-colour fallback so the
// meaning (green = good, red = bad) survives on a plain console.
type Theme struct {
	Name   string
	Accent term.Color // titles, focus, the current tab
	OK     term.Color
	OKB    term.Color // brighter ok: "installed", "done"
	Warn   term.Color
	Err    term.Color
	Muted  term.Color // secondary text
	Text   term.Color // primary text
	Border term.Color // unfocused frame
	Bar    term.Color // progress fill
	BarH   term.Color // progress head / emphasis
	Track  term.Color // empty progress, selected-row background
	// Plain themes carry meaning with weight and reversal rather than hue.
	Plain bool
}

// roles, in the order the original script's tables list them.
var themeRGB = map[string]string{
	"default": "86,204,255 120,230,150 140,255,170 255,190,90 255,120,130 130,140,155 225,232,240 72,86,105 86,204,255 225,245,255 58,68,84",
	"nord":    "136,192,208 163,190,140 179,208,150 235,203,139 191,97,106 96,108,128 216,222,233 67,76,94 136,192,208 236,239,244 59,66,82",
	"tokyo":   "125,207,255 158,206,106 178,226,126 224,175,104 247,118,142 108,118,168 192,202,245 65,72,104 122,162,247 192,202,245 41,46,66",
	"gruvbox": "131,165,152 184,187,38 204,207,66 250,189,47 251,73,52 146,131,116 235,219,178 80,73,69 215,153,33 250,189,47 60,56,54",
	"dracula": "189,147,249 80,250,123 120,255,155 241,250,140 255,85,85 118,134,184 248,248,242 68,71,90 139,233,253 248,248,242 52,55,70",
	// colourblind-safe: blue is good, orange is bad; warning vs error differ in brightness
	"cb":   "86,180,233 86,180,233 150,210,245 240,190,60 225,110,40 125,140,155 225,232,240 70,88,110 86,180,233 225,245,255 55,68,88",
	"mono": "190,190,190 225,225,225 255,255,255 200,200,200 245,245,245 115,115,115 235,235,235 85,85,85 210,210,210 255,255,255 70,70,70",
}

// 16-colour fallbacks per role: accent cyan, ok green, okb bright green, warn
// yellow, err red, muted grey, text white, border grey, bar cyan, bar head
// bright white, track dark grey.
var fb16 = [11]uint8{6, 2, 10, 3, 1, 8, 7, 8, 6, 15, 8}

// Names lists the built-in themes.
func Names() []string {
	return []string{"default", "nord", "tokyo", "gruvbox", "dracula", "mono", "cb", "ansi"}
}

// NewTheme builds a theme by name; an unknown name gives the default.
func NewTheme(name string) *Theme {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "ansi" {
		return &Theme{Name: "ansi",
			Accent: term.ANSI(6), OK: term.ANSI(2), OKB: term.ANSI(10), Warn: term.ANSI(3), Err: term.ANSI(1),
			Muted: term.ANSI(8), Text: term.Default, Border: term.ANSI(8), Bar: term.ANSI(6), BarH: term.ANSI(14), Track: term.ANSI(8)}
	}
	tab, ok := themeRGB[name]
	if !ok {
		name, tab = "default", themeRGB["default"]
	}
	f := strings.Fields(tab)
	c := make([]term.Color, 11)
	for i := range c {
		p := strings.Split(f[i], ",")
		r, _ := strconv.Atoi(p[0])
		g, _ := strconv.Atoi(p[1])
		b, _ := strconv.Atoi(p[2])
		c[i] = term.RGB(uint8(r), uint8(g), uint8(b), fb16[i])
	}
	return &Theme{Name: name, Accent: c[0], OK: c[1], OKB: c[2], Warn: c[3], Err: c[4], Muted: c[5], Text: c[6],
		Border: c[7], Bar: c[8], BarH: c[9], Track: c[10], Plain: name == "mono"}
}

// ---- styles -----------------------------------------------------------------

func (t *Theme) fg(c term.Color) term.Style { return term.Style{FG: c} }

// Base is ordinary text.
func (t *Theme) Base() term.Style { return t.fg(t.Text) }

// Dim is secondary text.
func (t *Theme) Dim() term.Style { return t.fg(t.Muted) }

// Acc is accent text.
func (t *Theme) Acc() term.Style { return t.fg(t.Accent) }

// Good, Bad and Caution are status text.
func (t *Theme) Good() term.Style {
	if t.Plain {
		return t.fg(t.OKB).With(term.Bold)
	}
	return t.fg(t.OK)
}
func (t *Theme) Bad() term.Style {
	if t.Plain {
		return t.fg(t.Err).With(term.Bold | term.Underline)
	}
	return t.fg(t.Err)
}
func (t *Theme) Caution() term.Style {
	if t.Plain {
		return t.fg(t.Warn).With(term.Italic)
	}
	return t.fg(t.Warn)
}

// Title is a bold heading in the accent colour.
func (t *Theme) Title() term.Style { return t.fg(t.Accent).With(term.Bold) }

// Frame is a border; focused frames use the accent.
func (t *Theme) Frame(focused bool) term.Style {
	if focused {
		return t.fg(t.Accent)
	}
	return t.fg(t.Border)
}

// Sel is a selected row: a tinted background for colour themes, reversal for
// plain ones. focused rows are bolder than rows in an unfocused pane.
func (t *Theme) Sel(focused bool) term.Style {
	if t.Plain || t.Name == "ansi" {
		if focused {
			return term.Style{Attr: term.Reverse}
		}
		return term.Style{Attr: term.Bold}
	}
	if focused {
		return term.Style{FG: t.BarH, BG: t.Track, Attr: term.Bold}
	}
	return term.Style{FG: t.Text, BG: t.Track}
}

// Chip is a label on a solid accent background (the active tab, key caps).
func (t *Theme) Chip() term.Style {
	if t.Plain || t.Name == "ansi" {
		return term.Style{Attr: term.Reverse | term.Bold}
	}
	return term.Style{FG: term.RGB(12, 16, 24, 0), BG: t.Accent, Attr: term.Bold}
}

// Key is a key-cap in a hint bar.
func (t *Theme) Key() term.Style { return t.fg(t.Accent).With(term.Bold) }

// Status maps a run conclusion/state word to a style.
func (t *Theme) Status(s string) term.Style {
	switch s {
	case "success", "ok", "done":
		return t.Good()
	case "failure", "failed", "error", "timed_out", "startup_failure":
		return t.Bad()
	case "cancelled", "skipped", "stale", "neutral":
		return t.Dim()
	case "in_progress", "running", "queued", "waiting", "pending", "requested":
		return t.Caution()
	}
	return t.Base()
}
