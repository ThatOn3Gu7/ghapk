package term

import (
	"bytes"
	"math/rand"
	"strconv"
	"strings"
	"testing"
)

func feedAll(s string) []Event {
	var p Parser
	evs := p.Feed([]byte(s))
	return append(evs, p.Flush()...)
}

func TestParserKeys(t *testing.T) {
	cases := []struct {
		in   string
		want Event
	}{
		{"a", runeKey('a', 0)},
		{"é", runeKey('é', 0)},
		{"\r", key(KeyEnter, 0)},
		{"\t", key(KeyTab, 0)},
		{"\x7f", key(KeyBackspace, 0)},
		{"\x03", runeKey('c', ModCtrl)},
		{"\x1b", key(KeyEsc, 0)},
		{"\x1b[A", key(KeyUp, 0)},
		{"\x1b[B", key(KeyDown, 0)},
		{"\x1b[C", key(KeyRight, 0)},
		{"\x1b[D", key(KeyLeft, 0)},
		{"\x1bOA", key(KeyUp, 0)},
		{"\x1b[H", key(KeyHome, 0)},
		{"\x1b[F", key(KeyEnd, 0)},
		{"\x1b[1~", key(KeyHome, 0)},
		{"\x1b[3~", key(KeyDelete, 0)},
		{"\x1b[5~", key(KeyPgUp, 0)},
		{"\x1b[6~", key(KeyPgDn, 0)},
		{"\x1b[Z", key(KeyBacktab, ModShift)},
		{"\x1b[1;5C", key(KeyRight, ModCtrl)},
		{"\x1b[1;2A", key(KeyUp, ModShift)},
		{"\x1b[15~", key(KeyF5, 0)},
		{"\x1bOP", key(KeyF1, 0)},
		{"\x1bx", runeKey('x', ModAlt)},
		{"\x1b[13;2u", key(KeyEnter, ModShift)},
	}
	for _, c := range cases {
		got := feedAll(c.in)
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("%q: got %+v want %+v", c.in, got, c.want)
		}
	}
}

func TestParserSequenceAndSplit(t *testing.T) {
	// two keys in one read
	got := feedAll("j\x1b[Bk")
	if len(got) != 3 || !got[0].Is('j') || got[1].Key != KeyDown || !got[2].Is('k') {
		t.Fatalf("%+v", got)
	}
	// a sequence split across reads must wait, not emit Esc
	var p Parser
	if evs := p.Feed([]byte("\x1b")); len(evs) != 0 || !p.Pending() {
		t.Fatalf("lone esc should be pending: %+v", evs)
	}
	if evs := p.Feed([]byte("[")); len(evs) != 0 {
		t.Fatalf("%+v", evs)
	}
	evs := p.Feed([]byte("A"))
	if len(evs) != 1 || evs[0].Key != KeyUp {
		t.Fatalf("%+v", evs)
	}
	// ...but a timeout flush turns a lone ESC into Esc
	p.Feed([]byte("\x1b"))
	if evs := p.Flush(); len(evs) != 1 || evs[0].Key != KeyEsc {
		t.Fatalf("%+v", evs)
	}
	// split UTF-8
	p = Parser{}
	if evs := p.Feed([]byte{0xe2, 0x82}); len(evs) != 0 {
		t.Fatalf("%+v", evs)
	}
	if evs := p.Feed([]byte{0xac}); len(evs) != 1 || evs[0].Rune != '€' {
		t.Fatalf("%+v", evs)
	}
}

func TestParserMouse(t *testing.T) {
	ev := feedAll("\x1b[<0;10;5M")[0]
	if ev.Kind != EvMouse || ev.Button != BtnLeft || ev.Action != MousePress || ev.X != 9 || ev.Y != 4 {
		t.Fatalf("%+v", ev)
	}
	ev = feedAll("\x1b[<0;10;5m")[0]
	if ev.Action != MouseRelease {
		t.Fatalf("%+v", ev)
	}
	ev = feedAll("\x1b[<32;3;3M")[0]
	if ev.Action != MouseDrag {
		t.Fatalf("%+v", ev)
	}
	ev = feedAll("\x1b[<64;1;1M")[0]
	if ev.Button != BtnWheelUp || ev.Action != MouseWheel {
		t.Fatalf("%+v", ev)
	}
	ev = feedAll("\x1b[<65;1;1M")[0]
	if ev.Button != BtnWheelDown {
		t.Fatalf("%+v", ev)
	}
}

func TestParserPaste(t *testing.T) {
	got := feedAll("\x1b[200~hello\nworld\x1b[201~q")
	if len(got) != 2 || got[0].Kind != EvPaste || got[0].Text != "hello\nworld" || !got[1].Is('q') {
		t.Fatalf("%+v", got)
	}
	// paste split mid-terminator
	var p Parser
	var all []Event
	for _, c := range []string{"\x1b[200~ab", "c\x1b[20", "1~"} {
		all = append(all, p.Feed([]byte(c))...)
	}
	if len(all) != 1 || all[0].Text != "abc" {
		t.Fatalf("%+v", all)
	}
}

func TestParserNeverPanics(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	alphabet := []byte("\x1b[<;0123456789MmAB~OuZ\r\x7f é")
	for i := 0; i < 5000; i++ {
		n := r.Intn(30)
		b := make([]byte, n)
		for j := range b {
			b[j] = alphabet[r.Intn(len(alphabet))]
		}
		var p Parser
		p.Feed(b)
		p.Flush()
	}
}

// ---- renderer vs a tiny VT emulator ----------------------------------------

type vt struct {
	w, h    int
	cells   []Cell
	x, y    int
	cur     Style
	sawSync int
}

func newVT(w, h int) *vt {
	v := &vt{w: w, h: h, cells: make([]Cell, w*h)}
	for i := range v.cells {
		v.cells[i] = Cell{R: ' ', W: 1}
	}
	return v
}

// feed interprets the subset of escapes the renderer emits.
func (v *vt) feed(t *testing.T, s string, d Depth) {
	for i := 0; i < len(s); {
		c := s[i]
		if c == 0x1b {
			if i+1 >= len(s) || s[i+1] != '[' {
				t.Fatalf("unexpected escape at %d", i)
			}
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			params, fin := s[i+2:j], s[j]
			i = j + 1
			switch fin {
			case 'H':
				p := strings.Split(params, ";")
				y, _ := strconv.Atoi(p[0])
				x, _ := strconv.Atoi(p[1])
				v.y, v.x = y-1, x-1
			case 'J':
				for k := range v.cells {
					v.cells[k] = Cell{R: ' ', W: 1}
				}
			case 'm':
				v.sgr(params)
			case 'h', 'l':
				if params == "?2026" {
					v.sawSync++
				}
			default:
				t.Fatalf("unknown CSI %q", params+string(fin))
			}
			continue
		}
		r, n := rune(c), 1
		if c >= 0x80 {
			for _, rr := range s[i:] {
				r = rr
				break
			}
			n = len(string(r))
		}
		w := RuneWidth(r)
		if v.x >= 0 && v.x < v.w && v.y >= 0 && v.y < v.h {
			v.cells[v.y*v.w+v.x] = Cell{R: r, W: uint8(w), Style: v.cur}
			if w == 2 && v.x+1 < v.w {
				v.cells[v.y*v.w+v.x+1] = Cell{W: 0, Style: v.cur}
			}
		}
		v.x += w
		i += n
	}
}

func (v *vt) sgr(params string) {
	if params == "" {
		v.cur = Style{}
		return
	}
	f := strings.Split(params, ";")
	for k := 0; k < len(f); k++ {
		n, _ := strconv.Atoi(f[k])
		switch {
		case n == 0:
			v.cur = Style{}
		case n == 1:
			v.cur.Attr |= Bold
		case n == 2:
			v.cur.Attr |= Dim
		case n == 3:
			v.cur.Attr |= Italic
		case n == 4:
			v.cur.Attr |= Underline
		case n == 7:
			v.cur.Attr |= Reverse
		case n == 9:
			v.cur.Attr |= Strike
		case n == 38 || n == 48:
			col := Color{}
			if f[k+1] == "2" {
				r, _ := strconv.Atoi(f[k+2])
				g, _ := strconv.Atoi(f[k+3])
				b, _ := strconv.Atoi(f[k+4])
				col = Color{kind: 1, R: uint8(r), G: uint8(g), B: uint8(b), A16: 0}
				k += 4
			} else {
				i, _ := strconv.Atoi(f[k+2])
				col = Color{kind: 2, idx: uint8(i)}
				k += 2
			}
			if n == 38 {
				v.cur.FG = col
			} else {
				v.cur.BG = col
			}
		case (n >= 30 && n <= 37) || (n >= 90 && n <= 97):
			i := n - 30
			if n >= 90 {
				i = n - 90 + 8
			}
			v.cur.FG = ANSI(uint8(i))
		case (n >= 40 && n <= 47) || (n >= 100 && n <= 107):
			i := n - 40
			if n >= 100 {
				i = n - 100 + 8
			}
			v.cur.BG = ANSI(uint8(i))
		}
	}
}

func randomBuffer(r *rand.Rand, w, h int) *Buffer {
	b := NewBuffer(w, h)
	glyphs := []rune("abcxyz  ─│█é界字✓")
	cols := []Color{Default, RGB(10, 200, 30, 2), RGB(250, 20, 20, 1), RGB(5, 5, 5, 0)}
	for k := 0; k < w*h/2; k++ {
		st := Style{FG: cols[r.Intn(len(cols))], BG: cols[r.Intn(len(cols))], Attr: Attr(r.Intn(4))}
		b.Set(r.Intn(w), r.Intn(h), glyphs[r.Intn(len(glyphs))], st)
	}
	return b
}

// Property: after any sequence of frames, a terminal that only saw the diffed
// output shows exactly the last frame.
func TestRendererDiffMatchesBuffer(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for _, depth := range []Depth{DepthTrue, Depth256, Depth16} {
		var out bytes.Buffer
		rd := NewRenderer(&out, depth)
		term := newVT(30, 8)
		for frame := 0; frame < 25; frame++ {
			b := randomBuffer(r, 30, 8)
			if frame%4 == 3 { // a small change on top of the previous frame
				b = NewBuffer(30, 8)
				copy(b.Cells, rd.prev.Cells)
				b.Text(3, 2, "changed", Style{FG: RGB(1, 2, 3, 4)}, 0)
			}
			out.Reset()
			if err := rd.Flush(b); err != nil {
				t.Fatal(err)
			}
			term.feed(t, out.String(), depth)
			for y := 0; y < 8; y++ {
				for x := 0; x < 30; x++ {
					want, got := b.At(x, y), term.cells[y*30+x]
					if want.R != got.R || want.W != got.W {
						t.Fatalf("depth %d frame %d (%d,%d): want %q/%d got %q/%d", depth, frame, x, y, want.R, want.W, got.R, got.W)
					}
					if want.W != 0 && want.Style.Attr != got.Style.Attr {
						t.Fatalf("depth %d frame %d (%d,%d): attr want %b got %b", depth, frame, x, y, want.Style.Attr, got.Style.Attr)
					}
				}
			}
		}
	}
}

func TestRendererIdenticalFrameSendsNothing(t *testing.T) {
	var out bytes.Buffer
	rd := NewRenderer(&out, DepthTrue)
	b := NewBuffer(10, 3)
	b.Text(0, 0, "hi", Style{}, 0)
	rd.Flush(b)
	out.Reset()
	rd.Flush(b)
	if out.Len() != 0 {
		t.Fatalf("second identical frame wrote %q", out.String())
	}
}

func TestRendererSmallChangeIsSmall(t *testing.T) {
	var out bytes.Buffer
	rd := NewRenderer(&out, DepthTrue)
	b := NewBuffer(80, 24)
	rd.Flush(b)
	out.Reset()
	b.Text(5, 5, "x", Style{}, 0)
	rd.Flush(b)
	if out.Len() > 60 {
		t.Fatalf("one-cell change wrote %d bytes: %q", out.Len(), out.String())
	}
}

func TestColorFallbacks(t *testing.T) {
	if got := rgb256(128, 128, 128); got < 232 || got > 255 {
		t.Errorf("grey should use the ramp, got %d", got)
	}
	if got := rgb256(255, 0, 0); got != 196 {
		t.Errorf("red = %d", got)
	}
	if got := nearest16(0, 200, 0); got != 2 && got != 10 {
		t.Errorf("green = %d", got)
	}
}

func TestWidthAndTruncate(t *testing.T) {
	if StringWidth("a界b") != 4 {
		t.Error("cjk width")
	}
	if got := Truncate("hello world", 8, "…"); got != "hello w…" {
		t.Errorf("%q", got)
	}
	if got := Truncate("界界界", 5, "…"); StringWidth(got) > 5 {
		t.Errorf("%q", got)
	}
	if got := PadRight("ab", 4); got != "ab  " {
		t.Errorf("%q", got)
	}
}
