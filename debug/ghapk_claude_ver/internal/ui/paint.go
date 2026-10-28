package ui

import (
	"ghapk/internal/term"
)

// Rect is a rectangle of cells.
type Rect struct{ X, Y, W, H int }

// Contains reports whether the point is inside r.
func (r Rect) Contains(x, y int) bool { return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H }

// Inset shrinks r by n cells on every side.
func (r Rect) Inset(n int) Rect {
	r = Rect{r.X + n, r.Y + n, r.W - 2*n, r.H - 2*n}
	if r.W < 0 {
		r.W = 0
	}
	if r.H < 0 {
		r.H = 0
	}
	return r
}

// Span is a run of text in one style.
type Span struct {
	S  string
	St term.Style
}

// Sp is shorthand for building a span.
func Sp(s string, st term.Style) Span { return Span{s, st} }

// Painter draws into a buffer with a theme and glyph set.
type Painter struct {
	B    *term.Buffer
	T    *Theme
	G    *Glyphs
	Tick int // animation frame counter

	// FrameOverride/TitleOverride recolour the next boxes (danger dialogs).
	FrameOverride, TitleOverride *term.Style
}

// Text draws one styled string, clipped to maxW columns; returns columns used.
func (p *Painter) Text(x, y int, s string, st term.Style, maxW int) int {
	return p.B.Text(x, y, s, st, maxW)
}

// Spans draws consecutive spans clipped to maxW columns in total.
func (p *Painter) Spans(x, y int, spans []Span, maxW int) int {
	if maxW <= 0 {
		maxW = p.B.W - x
	}
	used := 0
	for _, s := range spans {
		if used >= maxW {
			break
		}
		used += p.B.Text(x+used, y, s.S, s.St, maxW-used)
	}
	return used
}

// SpansWidth is the display width of the spans.
func SpansWidth(spans []Span) int {
	n := 0
	for _, s := range spans {
		n += term.StringWidth(s.S)
	}
	return n
}

// Right draws spans right-aligned so they end at column x+w.
func (p *Painter) Right(x, y, w int, spans []Span) {
	n := SpansWidth(spans)
	if n > w {
		n = w
	}
	p.Spans(x+w-n, y, spans, n)
}

// Clear blanks a rectangle in the base style.
func (p *Painter) Clear(r Rect) { p.B.Fill(r.X, r.Y, r.W, r.H, ' ', term.Style{}) }

// Row paints a background across a whole row segment, for selections.
func (p *Painter) Row(x, y, w int, st term.Style) { p.B.Fill(x, y, w, 1, ' ', st) }

// Box draws a frame with an optional title (left) and a right-hand note. The
// inside is cleared. Frames are rounded in unicode mode.
func (p *Painter) Box(r Rect, title string, note string, focused bool) Rect {
	if r.W < 2 || r.H < 2 {
		return Rect{}
	}
	g, fr := p.G, p.T.Frame(focused)
	if p.FrameOverride != nil {
		fr = *p.FrameOverride
	}
	p.B.Fill(r.X, r.Y, r.W, r.H, ' ', term.Style{})
	p.B.Set(r.X, r.Y, []rune(g.TL)[0], fr)
	p.B.Set(r.X+r.W-1, r.Y, []rune(g.TR)[0], fr)
	p.B.Set(r.X, r.Y+r.H-1, []rune(g.BL)[0], fr)
	p.B.Set(r.X+r.W-1, r.Y+r.H-1, []rune(g.BR)[0], fr)
	h, v := []rune(g.H)[0], []rune(g.V)[0]
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		p.B.Set(x, r.Y, h, fr)
		p.B.Set(x, r.Y+r.H-1, h, fr)
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		p.B.Set(r.X, y, v, fr)
		p.B.Set(r.X+r.W-1, y, v, fr)
	}
	if title != "" && r.W > 6 {
		ts := p.T.Dim()
		if focused {
			ts = p.T.Title()
		}
		if p.TitleOverride != nil {
			ts = *p.TitleOverride
		}
		t := term.Truncate(title, r.W-6, g.Ellipsis)
		p.B.Text(r.X+2, r.Y, " "+t+" ", ts, 0)
	}
	if note != "" && r.W > 12 {
		room := r.W - 6
		t := term.Truncate(note, room, g.Ellipsis)
		w := term.StringWidth(t) + 2
		p.B.Text(r.X+r.W-2-w, r.Y, " "+t+" ", p.T.Dim(), 0)
	}
	return Rect{r.X + 1, r.Y + 1, r.W - 2, r.H - 2}
}

// HLine draws a horizontal rule.
func (p *Painter) HLine(x, y, w int, st term.Style) {
	h := []rune(p.G.H)[0]
	for i := 0; i < w; i++ {
		p.B.Set(x+i, y, h, st)
	}
}

// Scrollbar draws a thin scroll indicator down column x, from y over h rows.
func (p *Painter) Scrollbar(x, y, h, total, off, visible int) {
	if total <= visible || h < 2 {
		return
	}
	thumb := h * visible / total
	if thumb < 1 {
		thumb = 1
	}
	top := 0
	if total-visible > 0 {
		top = (h - thumb) * off / (total - visible)
	}
	for i := 0; i < h; i++ {
		if i >= top && i < top+thumb {
			p.B.Set(x, y+i, []rune(p.G.Scroll)[0], p.T.Acc())
		} else {
			p.B.Set(x, y+i, []rune(p.G.ScrollTrack)[0], p.T.fg(p.T.Border))
		}
	}
}

// Bar draws a progress bar w cells wide at x,y. frac < 0 means unknown size and
// shows a sliding segment instead.
func (p *Painter) Bar(x, y, w int, frac float64, hot bool) {
	if w <= 0 {
		return
	}
	fill := p.T.fg(p.T.Bar)
	if hot {
		fill = p.T.fg(p.T.BarH)
	}
	track := p.T.fg(p.T.Border)
	g := p.G
	full, empty := []rune(g.Full)[0], []rune(g.Empty)[0]
	if frac < 0 {
		seg := w / 4
		if seg < 2 {
			seg = 2
		}
		pos := (p.Tick / 2) % (w + seg)
		for i := 0; i < w; i++ {
			if i >= pos-seg && i < pos {
				p.B.Set(x+i, y, full, fill)
			} else {
				p.B.Set(x+i, y, empty, track)
			}
		}
		return
	}
	if frac > 1 {
		frac = 1
	}
	cells := frac * float64(w)
	n := int(cells)
	for i := 0; i < w; i++ {
		switch {
		case i < n:
			p.B.Set(x+i, y, full, fill)
		case i == n && len(g.Eighths) > 0:
			e := int((cells - float64(n)) * 8)
			if e > 0 {
				p.B.Set(x+i, y, []rune(g.Eighths[e-1])[0], fill)
			} else {
				p.B.Set(x+i, y, empty, track)
			}
		default:
			p.B.Set(x+i, y, empty, track)
		}
	}
}

// Spinner returns the current spinner frame.
func (p *Painter) Spinner() string { return p.G.Spin[(p.Tick/1)%len(p.G.Spin)] }
