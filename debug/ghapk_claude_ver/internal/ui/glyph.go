package ui

import "os"

// Glyphs is the set of drawing characters in use.
type Glyphs struct {
	Name                      string
	TL, TR, BL, BR, H, V      string // frame corners and lines (rounded when unicode)
	TeeL, TeeR                string
	Dot, Check, Cross, Warn   string
	Run, Queue, Skip, Unknown string
	Right, Down, Up, Left     string
	Ellipsis, Sep, Bullet     string
	Full, Empty               string
	Eighths                   []string // partial bar cells, 1/8 .. 7/8
	Spin                      []string
	Scroll, ScrollTrack       string
	Mark, Unmark              string
	Lock, Branch, Pkg, Phone  string
}

var unicodeGlyphs = Glyphs{
	Name: "unicode",
	TL:   "╭", TR: "╮", BL: "╰", BR: "╯", H: "─", V: "│", TeeL: "├", TeeR: "┤",
	Dot: "•", Check: "✓", Cross: "✗", Warn: "!", Run: "●", Queue: "○", Skip: "–", Unknown: "?",
	Right: "›", Down: "▾", Up: "▴", Left: "‹",
	Ellipsis: "…", Sep: "│", Bullet: "·",
	Full: "█", Empty: "░",
	Eighths: []string{"▏", "▎", "▍", "▌", "▋", "▊", "▉"},
	Spin:    []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	Scroll:  "┃", ScrollTrack: "│",
	Mark: "◉", Unmark: "○",
	Lock: "⚿", Branch: "⎇", Pkg: "▣", Phone: "▯",
}

var asciiGlyphs = Glyphs{
	Name: "ascii",
	TL:   "+", TR: "+", BL: "+", BR: "+", H: "-", V: "|", TeeL: "+", TeeR: "+",
	Dot: "*", Check: "v", Cross: "x", Warn: "!", Run: "*", Queue: "o", Skip: "-", Unknown: "?",
	Right: ">", Down: "v", Up: "^", Left: "<",
	Ellipsis: "...", Sep: "|", Bullet: ".",
	Full: "#", Empty: "-",
	Eighths: nil,
	Spin:    []string{"|", "/", "-", "\\"},
	Scroll:  "#", ScrollTrack: "|",
	Mark: "[x]", Unmark: "[ ]",
	Lock: "@", Branch: "~", Pkg: "#", Phone: "=",
}

// NewGlyphs picks a set. mode is "unicode", "ascii" or "auto" (unicode unless
// the locale or terminal says otherwise).
func NewGlyphs(mode string) *Glyphs {
	switch mode {
	case "ascii":
		g := asciiGlyphs
		return &g
	case "unicode":
		g := unicodeGlyphs
		return &g
	}
	if os.Getenv("TERM") == "linux" || os.Getenv("TERM") == "dumb" {
		g := asciiGlyphs
		return &g
	}
	g := unicodeGlyphs
	return &g
}
