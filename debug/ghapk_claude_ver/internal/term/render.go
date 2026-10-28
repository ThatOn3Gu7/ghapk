package term

import (
	"io"
	"strconv"
	"strings"
)

// Depth is how many colours the terminal can show.
type Depth int

// Colour depths.
const (
	DepthNone Depth = iota // no colour at all (NO_COLOR, dumb terminals)
	Depth16                // the 16 ANSI colours
	Depth256               // the xterm 256-colour palette
	DepthTrue              // 24-bit
)

// Renderer turns buffers into the smallest escape sequence stream that updates
// the screen, remembering what it last drew.
type Renderer struct {
	out   io.Writer
	Depth Depth
	prev  *Buffer
	sb    strings.Builder
}

// NewRenderer writes to out.
func NewRenderer(out io.Writer, d Depth) *Renderer { return &Renderer{out: out, Depth: d} }

// Invalidate forgets the previous frame so the next Flush repaints everything.
func (r *Renderer) Invalidate() { r.prev = nil }

// Flush draws b. Only cells that differ from the previous frame are sent.
func (r *Renderer) Flush(b *Buffer) error {
	full := r.prev == nil || r.prev.W != b.W || r.prev.H != b.H
	sb := &r.sb
	sb.Reset()
	sb.WriteString("\x1b[?2026h") // synchronized update: no tearing where supported, ignored elsewhere
	if full {
		sb.WriteString("\x1b[0m\x1b[2J")
	}
	var cur Style
	curKnown := false
	cx, cy := -1, -1
	for y := 0; y < b.H; y++ {
		x := 0
		for x < b.W {
			c := b.Cells[y*b.W+x]
			if c.W == 0 { // right half of a wide glyph: drawn with its left half
				x++
				continue
			}
			if !full {
				p := r.prev.Cells[y*b.W+x]
				if p == c {
					x++
					continue
				}
			}
			if cx != x || cy != y {
				sb.WriteString("\x1b[" + strconv.Itoa(y+1) + ";" + strconv.Itoa(x+1) + "H")
			}
			if !curKnown || cur != c.Style {
				r.sgr(sb, c.Style)
				cur, curKnown = c.Style, true
			}
			if c.R == 0 {
				sb.WriteByte(' ')
			} else {
				sb.WriteRune(c.R)
			}
			x += int(c.W)
			cx, cy = x, y
		}
	}
	sb.WriteString("\x1b[0m\x1b[?2026l")
	if r.prev == nil || r.prev.W != b.W || r.prev.H != b.H {
		r.prev = NewBuffer(b.W, b.H)
	}
	copy(r.prev.Cells, b.Cells)
	if sb.Len() <= len("\x1b[?2026h\x1b[0m\x1b[?2026l") {
		return nil // nothing changed
	}
	_, err := io.WriteString(r.out, sb.String())
	return err
}

// sgr writes a complete, absolute SGR for st (reset first, then everything it
// needs), which is simpler and safe against any terminal's attribute quirks.
func (r *Renderer) sgr(sb *strings.Builder, st Style) {
	sb.WriteString("\x1b[0")
	if r.Depth == DepthNone {
		st.FG, st.BG = Default, Default
	}
	if st.Attr&Bold != 0 {
		sb.WriteString(";1")
	}
	if st.Attr&Dim != 0 {
		sb.WriteString(";2")
	}
	if st.Attr&Italic != 0 {
		sb.WriteString(";3")
	}
	if st.Attr&Underline != 0 {
		sb.WriteString(";4")
	}
	if st.Attr&Reverse != 0 {
		sb.WriteString(";7")
	}
	if st.Attr&Strike != 0 {
		sb.WriteString(";9")
	}
	r.color(sb, st.FG, false)
	r.color(sb, st.BG, true)
	sb.WriteByte('m')
}

func (r *Renderer) color(sb *strings.Builder, c Color, bg bool) {
	if c.kind == 0 {
		return
	}
	base := 30
	if bg {
		base = 40
	}
	if c.kind == 2 {
		i := int(c.idx)
		if i < 8 {
			sb.WriteString(";" + strconv.Itoa(base+i))
		} else {
			sb.WriteString(";" + strconv.Itoa(base+60+i-8))
		}
		return
	}
	switch r.Depth {
	case DepthTrue:
		sb.WriteString(";" + strconv.Itoa(base+8) + ";2;" + strconv.Itoa(int(c.R)) + ";" + strconv.Itoa(int(c.G)) + ";" + strconv.Itoa(int(c.B)))
	case Depth256:
		sb.WriteString(";" + strconv.Itoa(base+8) + ";5;" + strconv.Itoa(rgb256(c.R, c.G, c.B)))
	default:
		i := int(c.A16)
		if c.A16 == 255 {
			i = nearest16(c.R, c.G, c.B)
		}
		if i < 8 {
			sb.WriteString(";" + strconv.Itoa(base+i))
		} else {
			sb.WriteString(";" + strconv.Itoa(base+60+i-8))
		}
	}
}

// cube maps 0..255 to the xterm cube level 0..5 (levels 0,95,135,175,215,255).
func cube(v uint8) int {
	switch {
	case v < 48:
		return 0
	case v < 115:
		return 1
	case v < 155:
		return 2
	case v < 195:
		return 3
	case v < 235:
		return 4
	}
	return 5
}

// rgb256 is the nearest xterm-256 index. Greys use the 24-step ramp: a grey
// sent through the colour cube comes out tinted.
func rgb256(r, g, b uint8) int {
	mx, mn := r, r
	for _, v := range []uint8{g, b} {
		if v > mx {
			mx = v
		}
		if v < mn {
			mn = v
		}
	}
	if mx-mn < 12 {
		switch {
		case mn < 4:
			return 16
		case mn > 243:
			return 231
		}
		return 232 + (int(mn)-3)/10
	}
	return 16 + 36*cube(r) + 6*cube(g) + cube(b)
}

var pal = [16][3]int{{0, 0, 0}, {205, 49, 49}, {13, 188, 121}, {229, 229, 16}, {36, 114, 200}, {188, 63, 188}, {17, 168, 205}, {229, 229, 229},
	{102, 102, 102}, {241, 76, 76}, {35, 209, 139}, {245, 245, 67}, {59, 142, 234}, {214, 112, 214}, {41, 184, 219}, {255, 255, 255}}

// RGB8 resolves a colour to concrete RGB for screenshots and tests. ok is false
// for the terminal default.
func (c Color) RGB8() (r, g, b uint8, ok bool) {
	switch c.kind {
	case 1:
		return c.R, c.G, c.B, true
	case 2:
		p := pal[c.idx&15]
		return uint8(p[0]), uint8(p[1]), uint8(p[2]), true
	}
	return 0, 0, 0, false
}

func nearest16(r, g, b uint8) int {
	best, bd := 7, 1<<30
	for i, p := range pal {
		dr, dg, db := int(r)-p[0], int(g)-p[1], int(b)-p[2]
		if d := dr*dr + dg*dg + db*db; d < bd {
			best, bd = i, d
		}
	}
	return best
}
