// Package term is a small, dependency-free terminal toolkit: raw mode and the
// alternate screen, an input parser (keys, mouse, paste), a cell buffer and a
// renderer that only sends what changed.
package term

import (
	"strings"
	"unicode"
)

// Color is a terminal colour: the terminal default, a true-colour RGB value
// with an optional 16-colour fallback, or a plain ANSI index.
type Color struct {
	kind    uint8 // 0 default, 1 rgb, 2 ansi index
	R, G, B uint8
	A16     uint8 // ANSI index (0-15) used when the terminal is limited to 16 colours; 255 = derive
	idx     uint8
}

// Default is the terminal's own colour.
var Default = Color{}

// RGB is a true-colour value. fallback16 is the ANSI colour to use on a
// 16-colour terminal, so green still means "ok" there; pass 255 to derive one.
func RGB(r, g, b uint8, fallback16 uint8) Color {
	return Color{kind: 1, R: r, G: g, B: b, A16: fallback16}
}

// ANSI is one of the terminal's 16 colours.
func ANSI(i uint8) Color { return Color{kind: 2, idx: i} }

// IsDefault reports whether c is the terminal default.
func (c Color) IsDefault() bool { return c.kind == 0 }

// Attr is a set of text attributes.
type Attr uint8

// Attributes.
const (
	Bold Attr = 1 << iota
	Dim
	Italic
	Underline
	Reverse
	Strike
)

// Style is the look of one cell.
type Style struct {
	FG, BG Color
	Attr   Attr
}

// With returns s with the given attributes added.
func (s Style) With(a Attr) Style { s.Attr |= a; return s }

// Fg returns s with a new foreground.
func (s Style) Fg(c Color) Style { s.FG = c; return s }

// Bg returns s with a new background.
func (s Style) Bg(c Color) Style { s.BG = c; return s }

// Cell is one character cell. W is the display width of the glyph: 1, 2, or 0
// for the right half of a wide glyph.
type Cell struct {
	R     rune
	W     uint8
	Style Style
}

// Buffer is a W×H grid of cells.
type Buffer struct {
	W, H  int
	Cells []Cell
}

// NewBuffer allocates a blank buffer.
func NewBuffer(w, h int) *Buffer {
	b := &Buffer{}
	b.Resize(w, h)
	return b
}

// Resize reallocates the buffer (contents are cleared).
func (b *Buffer) Resize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	b.W, b.H = w, h
	b.Cells = make([]Cell, w*h)
	b.Clear(Style{})
}

// Clear fills the whole buffer with spaces in style st.
func (b *Buffer) Clear(st Style) {
	for i := range b.Cells {
		b.Cells[i] = Cell{R: ' ', W: 1, Style: st}
	}
}

// At returns the cell at x,y (a blank cell outside the buffer).
func (b *Buffer) At(x, y int) Cell {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return Cell{R: ' ', W: 1}
	}
	return b.Cells[y*b.W+x]
}

// Set draws one glyph, handling wide glyphs and clipping. It returns the width
// consumed (0 when clipped or zero-width).
func (b *Buffer) Set(x, y int, r rune, st Style) int {
	w := RuneWidth(r)
	if w == 0 || x < 0 || y < 0 || y >= b.H || x >= b.W || (w == 2 && x+1 >= b.W) {
		return 0
	}
	// Overwriting half of a wide glyph blanks the other half.
	if c := b.Cells[y*b.W+x]; c.W == 2 && x+1 < b.W {
		b.Cells[y*b.W+x+1] = Cell{R: ' ', W: 1, Style: c.Style}
	} else if c.W == 0 && x > 0 {
		b.Cells[y*b.W+x-1] = Cell{R: ' ', W: 1, Style: c.Style}
	}
	b.Cells[y*b.W+x] = Cell{R: r, W: uint8(w), Style: st}
	if w == 2 {
		if c := b.Cells[y*b.W+x+1]; c.W == 2 && x+2 < b.W {
			b.Cells[y*b.W+x+2] = Cell{R: ' ', W: 1, Style: c.Style}
		}
		b.Cells[y*b.W+x+1] = Cell{R: 0, W: 0, Style: st}
	}
	return w
}

// Text draws s starting at x,y, clipped to maxW columns (0 = to the edge). It
// returns the number of columns written.
func (b *Buffer) Text(x, y int, s string, st Style, maxW int) int {
	if maxW <= 0 || x+maxW > b.W {
		maxW = b.W - x
	}
	col := 0
	for _, r := range s {
		w := RuneWidth(r)
		if w == 0 {
			continue
		}
		if col+w > maxW {
			break
		}
		b.Set(x+col, y, r, st)
		col += w
	}
	return col
}

// Fill paints a rectangle with r in style st.
func (b *Buffer) Fill(x, y, w, h int, r rune, st Style) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			b.Set(i, j, r, st)
		}
	}
}

// Restyle changes the style of a rectangle without touching its text.
func (b *Buffer) Restyle(x, y, w, h int, f func(Style) Style) {
	for j := y; j < y+h && j < b.H; j++ {
		for i := x; i < x+w && i < b.W; i++ {
			if i < 0 || j < 0 {
				continue
			}
			c := &b.Cells[j*b.W+i]
			c.Style = f(c.Style)
		}
	}
}

// String returns the buffer as plain text, one line per row (for tests).
func (b *Buffer) String() string {
	var sb strings.Builder
	for y := 0; y < b.H; y++ {
		line := make([]rune, 0, b.W)
		for x := 0; x < b.W; x++ {
			c := b.Cells[y*b.W+x]
			if c.W == 0 {
				continue
			}
			line = append(line, c.R)
		}
		sb.WriteString(strings.TrimRight(string(line), " "))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ---- display width ----------------------------------------------------------

type rng struct{ lo, hi rune }

var wide = []rng{
	{0x1100, 0x115F}, {0x231A, 0x231B}, {0x2329, 0x232A}, {0x23E9, 0x23EC}, {0x23F0, 0x23F0}, {0x23F3, 0x23F3},
	{0x25FD, 0x25FE}, {0x2614, 0x2615}, {0x2648, 0x2653}, {0x267F, 0x267F}, {0x2693, 0x2693}, {0x26A1, 0x26A1},
	{0x26AA, 0x26AB}, {0x26BD, 0x26BE}, {0x26C4, 0x26C5}, {0x26CE, 0x26CE}, {0x26D4, 0x26D4}, {0x26EA, 0x26EA},
	{0x26F2, 0x26F3}, {0x26F5, 0x26F5}, {0x26FA, 0x26FA}, {0x26FD, 0x26FD}, {0x2705, 0x2705}, {0x270A, 0x270B},
	{0x2728, 0x2728}, {0x274C, 0x274C}, {0x274E, 0x274E}, {0x2753, 0x2755}, {0x2757, 0x2757}, {0x2795, 0x2797},
	{0x27B0, 0x27B0}, {0x27BF, 0x27BF}, {0x2B1B, 0x2B1C}, {0x2B50, 0x2B50}, {0x2B55, 0x2B55},
	{0x2E80, 0x303E}, {0x3041, 0x33FF}, {0x3400, 0x4DBF}, {0x4E00, 0x9FFF}, {0xA000, 0xA4CF}, {0xA960, 0xA97F},
	{0xAC00, 0xD7A3}, {0xF900, 0xFAFF}, {0xFE10, 0xFE19}, {0xFE30, 0xFE6F}, {0xFF00, 0xFF60}, {0xFFE0, 0xFFE6},
	{0x1F004, 0x1F004}, {0x1F0CF, 0x1F0CF}, {0x1F18E, 0x1F18E}, {0x1F191, 0x1F19A}, {0x1F200, 0x1F320},
	{0x1F32D, 0x1F335}, {0x1F337, 0x1F37C}, {0x1F37E, 0x1F393}, {0x1F3A0, 0x1F3CA}, {0x1F3CF, 0x1F3D3},
	{0x1F3E0, 0x1F3F0}, {0x1F3F4, 0x1F3F4}, {0x1F3F8, 0x1F43E}, {0x1F440, 0x1F440}, {0x1F442, 0x1F4FC},
	{0x1F4FF, 0x1F53D}, {0x1F54B, 0x1F54E}, {0x1F550, 0x1F567}, {0x1F57A, 0x1F57A}, {0x1F595, 0x1F596},
	{0x1F5A4, 0x1F5A4}, {0x1F5FB, 0x1F64F}, {0x1F680, 0x1F6C5}, {0x1F6CC, 0x1F6CC}, {0x1F6D0, 0x1F6D2},
	{0x1F6D5, 0x1F6D7}, {0x1F6EB, 0x1F6EC}, {0x1F6F4, 0x1F6FC}, {0x1F7E0, 0x1F7EB}, {0x1F90C, 0x1F93A},
	{0x1F93C, 0x1F945}, {0x1F947, 0x1F9FF}, {0x1FA70, 0x1FAFF}, {0x20000, 0x2FFFD}, {0x30000, 0x3FFFD},
}

// RuneWidth is the number of terminal columns r occupies: 0 for combining
// marks and control characters, 2 for East Asian wide and emoji glyphs, else 1.
func RuneWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 0x20 || (r >= 0x7f && r < 0xa0):
		return 0
	case r < 0x300:
		return 1
	case unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf):
		return 0
	}
	if r >= 0x1100 {
		lo, hi := 0, len(wide)-1
		for lo <= hi {
			m := (lo + hi) / 2
			switch {
			case r < wide[m].lo:
				hi = m - 1
			case r > wide[m].hi:
				lo = m + 1
			default:
				return 2
			}
		}
	}
	return 1
}

// StringWidth is the display width of s.
func StringWidth(s string) int {
	n := 0
	for _, r := range s {
		n += RuneWidth(r)
	}
	return n
}

// Truncate shortens s to at most w columns, ending with an ellipsis when it
// had to cut. ellipsis is what to append ("…" or "...").
func Truncate(s string, w int, ellipsis string) string {
	if w <= 0 {
		return ""
	}
	if StringWidth(s) <= w {
		return s
	}
	ew := StringWidth(ellipsis)
	if w <= ew {
		return Clip(s, w)
	}
	return Clip(s, w-ew) + ellipsis
}

// Clip cuts s to at most w columns without an ellipsis.
func Clip(s string, w int) string {
	col := 0
	var sb strings.Builder
	for _, r := range s {
		rw := RuneWidth(r)
		if col+rw > w {
			break
		}
		sb.WriteRune(r)
		col += rw
	}
	return sb.String()
}

// PadRight pads s with spaces to w columns (or truncates it).
func PadRight(s string, w int) string {
	n := StringWidth(s)
	if n >= w {
		return Clip(s, w)
	}
	return s + strings.Repeat(" ", w-n)
}
