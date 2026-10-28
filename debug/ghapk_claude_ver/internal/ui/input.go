package ui

import (
	"strings"

	"ghapk/internal/term"
)

// Input is a single-line text field.
type Input struct {
	Text []rune
	Pos  int
}

// NewInput creates an input holding s with the cursor at the end.
func NewInput(s string) *Input {
	r := []rune(s)
	return &Input{Text: r, Pos: len(r)}
}

// String is the current text.
func (in *Input) String() string { return string(in.Text) }

// SetText replaces the text.
func (in *Input) SetText(s string) { in.Text = []rune(s); in.Pos = len(in.Text) }

func (in *Input) insert(rs []rune) {
	t := make([]rune, 0, len(in.Text)+len(rs))
	t = append(t, in.Text[:in.Pos]...)
	t = append(t, rs...)
	t = append(t, in.Text[in.Pos:]...)
	in.Text = t
	in.Pos += len(rs)
}

// Handle applies an editing key. It returns true when the event was used.
func (in *Input) Handle(ev term.Event) bool {
	switch ev.Kind {
	case term.EvPaste:
		s := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(ev.Text)
		in.insert([]rune(s))
		return true
	case term.EvKey:
	default:
		return false
	}
	switch ev.Key {
	case term.KeyRune:
		if ev.Mod&term.ModCtrl != 0 {
			switch ev.Rune {
			case 'a':
				in.Pos = 0
			case 'e':
				in.Pos = len(in.Text)
			case 'u':
				in.Text, in.Pos = in.Text[in.Pos:], 0
			case 'k':
				in.Text = in.Text[:in.Pos]
			case 'w':
				i := in.Pos
				for i > 0 && in.Text[i-1] == ' ' {
					i--
				}
				for i > 0 && in.Text[i-1] != ' ' {
					i--
				}
				in.Text = append(append([]rune{}, in.Text[:i]...), in.Text[in.Pos:]...)
				in.Pos = i
			default:
				return false
			}
			return true
		}
		if ev.Mod&term.ModAlt != 0 {
			return false
		}
		in.insert([]rune{ev.Rune})
	case term.KeyBackspace:
		if in.Pos > 0 {
			in.Text = append(in.Text[:in.Pos-1], in.Text[in.Pos:]...)
			in.Pos--
		}
	case term.KeyDelete:
		if in.Pos < len(in.Text) {
			in.Text = append(in.Text[:in.Pos], in.Text[in.Pos+1:]...)
		}
	case term.KeyLeft:
		if in.Pos > 0 {
			in.Pos--
		}
	case term.KeyRight:
		if in.Pos < len(in.Text) {
			in.Pos++
		}
	case term.KeyHome:
		in.Pos = 0
	case term.KeyEnd:
		in.Pos = len(in.Text)
	default:
		return false
	}
	return true
}

// Draw paints the field in w columns, scrolling horizontally to keep the
// cursor visible. The cursor is drawn as a reversed cell.
func (in *Input) Draw(p *Painter, x, y, w int, focused bool, placeholder string) {
	if w <= 0 {
		return
	}
	if len(in.Text) == 0 && !focused {
		p.Text(x, y, term.Truncate(placeholder, w, p.G.Ellipsis), p.T.Dim(), w)
		return
	}
	start := 0
	for term.StringWidth(string(in.Text[start:in.Pos])) > w-1 {
		start++
	}
	col := 0
	for i := start; i <= len(in.Text) && col < w; i++ {
		r, st := ' ', p.T.Base()
		if i < len(in.Text) {
			r = in.Text[i]
		}
		if focused && i == in.Pos {
			st = st.With(term.Reverse)
		}
		col += p.B.Set(x+col, y, r, st)
		if i < len(in.Text) && term.RuneWidth(r) == 0 {
			col++
		}
	}
	if len(in.Text) == 0 && focused && placeholder != "" && w > 2 {
		p.Text(x+2, y, term.Truncate(placeholder, w-2, p.G.Ellipsis), p.T.Dim(), w-2)
	}
}
