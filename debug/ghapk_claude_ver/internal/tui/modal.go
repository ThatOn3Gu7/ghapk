package tui

import (
	"fmt"
	"strings"

	"ghapk/internal/term"
	"ghapk/internal/ui"
)

// centered returns a w×h rectangle in the middle of the screen, clamped.
func (a *App) centered(w, h int) ui.Rect {
	if w > a.W-2 {
		w = a.W - 2
	}
	if h > a.H-2 {
		h = a.H - 2
	}
	return ui.Rect{X: (a.W - w) / 2, Y: (a.H - h) / 2, W: w, H: h}
}

// dialogWidth is the usual width of a dialog: roomy on a wide screen, nearly
// full width on a phone.
func (a *App) dialogWidth(want int) int {
	if want > a.W-2 {
		return a.W - 2
	}
	return want
}

// ---- text viewer (help, errors, logs) -----------------------------------------

type trow struct {
	spans []ui.Span
	wrap  bool
}

func textRow(s string, st term.Style) trow { return trow{spans: []ui.Span{ui.Sp(s, st)}, wrap: true} }
func blankRow() trow                       { return trow{spans: []ui.Span{ui.Sp("", term.Style{})}} }

type textModal struct {
	title  string
	note   string
	rows   []trow
	scroll int
	danger bool
	footer string
	onKey  func(a *App, ev term.Event) (handled, done bool)
	lines  [][]ui.Span // laid-out rows for the current width
	viewH  int
}

func (m *textModal) layout(a *App, w int) {
	m.lines = m.lines[:0]
	for _, r := range m.rows {
		if r.wrap && len(r.spans) == 1 {
			for _, ln := range ui.Wrap(r.spans[0].S, w) {
				m.lines = append(m.lines, []ui.Span{ui.Sp(ln, r.spans[0].St)})
			}
			continue
		}
		m.lines = append(m.lines, r.spans)
	}
}

func (m *textModal) draw(a *App) {
	p, th := &a.P, a.th
	w := a.dialogWidth(82)
	inner := w - 4
	m.layout(a, inner)
	h := len(m.lines) + 3
	if h > a.H-2 {
		h = a.H - 2
	}
	if h < 5 {
		h = 5
	}
	r := a.centered(w, h)
	if m.danger {
		fs, ts := th.Bad(), th.Bad().With(term.Bold)
		p.FrameOverride, p.TitleOverride = &fs, &ts
		defer func() { p.FrameOverride, p.TitleOverride = nil, nil }()
	}
	in := p.Box(r, m.title, m.note, true)
	a.hits.Add(ui.Rect{X: 0, Y: 0, W: a.W, H: a.H}, "modal.bg", 0)
	a.hits.Add(r, "modal.box", 0)
	m.viewH = in.H - 1
	if m.viewH < 1 {
		m.viewH = 1
	}
	if max := len(m.lines) - m.viewH; m.scroll > max {
		m.scroll = max
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	for i := 0; i < m.viewH && m.scroll+i < len(m.lines); i++ {
		p.Spans(in.X+1, in.Y+i, m.lines[m.scroll+i], in.W-2)
	}
	p.Scrollbar(in.X+in.W-1, in.Y, m.viewH, len(m.lines), m.scroll, m.viewH)
	foot := m.footer
	if foot == "" {
		foot = "Esc close"
		if len(m.lines) > m.viewH {
			foot = "↑↓ scroll · Esc close"
		}
	}
	p.Text(in.X+1, in.Y+in.H-1, term.Truncate(foot, in.W-2, "…"), th.Dim(), in.W-2)
}

func (m *textModal) key(a *App, ev term.Event) bool {
	if m.onKey != nil {
		if handled, done := m.onKey(a, ev); handled {
			return done
		}
	}
	page := m.viewH
	if page < 1 {
		page = 5
	}
	switch {
	case ev.Key == term.KeyEsc, ev.Key == term.KeyEnter, ev.Is('q'):
		return true
	case ev.Key == term.KeyDown, ev.Is('j'):
		m.scroll++
	case ev.Key == term.KeyUp, ev.Is('k'):
		m.scroll--
	case ev.Key == term.KeyPgDn, ev.Is(' '), ev.IsCtrl('d'):
		m.scroll += page
	case ev.Key == term.KeyPgUp, ev.IsCtrl('u'):
		m.scroll -= page
	case ev.Key == term.KeyHome, ev.Is('g'):
		m.scroll = 0
	case ev.Key == term.KeyEnd, ev.Is('G'):
		m.scroll = 1 << 20
	}
	return false
}

func (m *textModal) click(a *App, h ui.Hit, ok bool, ev term.Event) bool {
	return !ok || h.ID == "modal.bg"
}

func (m *textModal) wheel(a *App, dir int) { m.scroll += dir }

// ---- question ------------------------------------------------------------------

type questionModal struct {
	title, body string
	options     []string
	sel         int
	danger      bool
	answer      func(int)
}

func (m *questionModal) draw(a *App) {
	p, th := &a.P, a.th
	w := a.dialogWidth(64)
	body := ui.Wrap(m.body, w-4)
	h := len(body) + len(m.options) + 4
	r := a.centered(w, h)
	if m.danger {
		fs, ts := th.Bad(), th.Bad().With(term.Bold)
		p.FrameOverride, p.TitleOverride = &fs, &ts
		defer func() { p.FrameOverride, p.TitleOverride = nil, nil }()
	}
	in := p.Box(r, m.title, "", true)
	a.hits.Add(ui.Rect{X: 0, Y: 0, W: a.W, H: a.H}, "modal.bg", 0)
	a.hits.Add(r, "modal.box", 0)
	y := in.Y
	for _, ln := range body {
		if y >= in.Y+in.H-len(m.options)-2 {
			break
		}
		p.Text(in.X+1, y, ln, th.Base(), in.W-2)
		y++
	}
	y = in.Y + in.H - len(m.options) - 1
	for i, o := range m.options {
		st := th.Base()
		prefix := fmt.Sprintf(" %d  ", i+1)
		if i == m.sel {
			st = th.Sel(true)
			p.Row(in.X, y, in.W, st)
		}
		p.Text(in.X+1, y, prefix+o, st, in.W-2)
		a.hits.Add(ui.Rect{X: in.X, Y: y, W: in.W, H: 1}, "q.opt", i)
		y++
	}
	p.Text(in.X+1, in.Y+in.H-1, "↵ choose · Esc cancel", th.Dim(), in.W-2)
}

func (m *questionModal) choose(i int) bool {
	if m.answer != nil {
		m.answer(i)
	}
	return true
}

func (m *questionModal) key(a *App, ev term.Event) bool {
	n := len(m.options)
	switch {
	case ev.Key == term.KeyEsc:
		return m.choose(-1)
	case ev.Key == term.KeyEnter:
		return m.choose(m.sel)
	case ev.Key == term.KeyDown, ev.Is('j'), ev.Key == term.KeyTab:
		m.sel = (m.sel + 1) % n
	case ev.Key == term.KeyUp, ev.Is('k'), ev.Key == term.KeyBacktab:
		m.sel = (m.sel + n - 1) % n
	case ev.Kind == term.EvKey && ev.Key == term.KeyRune && ev.Rune >= '1' && int(ev.Rune-'1') < n:
		return m.choose(int(ev.Rune - '1'))
	}
	return false
}

func (m *questionModal) click(a *App, h ui.Hit, ok bool, ev term.Event) bool {
	if ok && h.ID == "q.opt" {
		return m.choose(h.N)
	}
	return false
}

func (m *questionModal) wheel(a *App, dir int) {}

// ask shows a yes/no style question from the UI itself.
func (a *App) ask(q engineQuestion, cb func(int)) {
	a.modal = &questionModal{title: q.Title, body: q.Body, options: q.Options, sel: q.Default, danger: q.Danger, answer: cb}
}

// ---- input ---------------------------------------------------------------------

type inputModal struct {
	title, prompt, hint string
	in                  *ui.Input
	validate            func(string) error
	submit              func(string)
	err                 string
}

func (m *inputModal) draw(a *App) {
	p, th := &a.P, a.th
	w := a.dialogWidth(66)
	prompt := ui.Wrap(m.prompt, w-4)
	hint := ui.Wrap(m.hint, w-4)
	h := len(prompt) + len(hint) + 6
	if m.err != "" {
		h++
	}
	r := a.centered(w, h)
	in := p.Box(r, m.title, "", true)
	a.hits.Add(ui.Rect{X: 0, Y: 0, W: a.W, H: a.H}, "modal.bg", 0)
	a.hits.Add(r, "modal.box", 0)
	y := in.Y
	for _, ln := range prompt {
		p.Text(in.X+1, y, ln, th.Base(), in.W-2)
		y++
	}
	y++
	p.Text(in.X+1, y, "›", th.Acc(), 1)
	m.in.Draw(p, in.X+3, y, in.W-4, true, "")
	y++
	if m.err != "" {
		p.Text(in.X+1, y, term.Truncate(m.err, in.W-2, "…"), th.Bad(), in.W-2)
		y++
	}
	for _, ln := range hint {
		y++
		p.Text(in.X+1, y, ln, th.Dim(), in.W-2)
	}
	p.Text(in.X+1, in.Y+in.H-1, "↵ save · Esc cancel", th.Dim(), in.W-2)
}

func (m *inputModal) key(a *App, ev term.Event) bool {
	switch {
	case ev.Key == term.KeyEsc:
		return true
	case ev.Key == term.KeyEnter:
		v := strings.TrimSpace(m.in.String())
		if m.validate != nil {
			if err := m.validate(v); err != nil {
				m.err = err.Error()
				return false
			}
		}
		m.submit(v)
		return true
	}
	m.err = ""
	m.in.Handle(ev)
	return false
}

func (m *inputModal) click(a *App, h ui.Hit, ok bool, ev term.Event) bool {
	return !ok || h.ID == "modal.bg"
}
func (m *inputModal) wheel(a *App, dir int) {}

// ---- busy ----------------------------------------------------------------------

type busyModal struct {
	title, msg string
	cancel     func()
}

func (m *busyModal) draw(a *App) {
	p, th := &a.P, a.th
	w := a.dialogWidth(48)
	lines := ui.Wrap(m.msg, w-6)
	r := a.centered(w, len(lines)+4)
	in := p.Box(r, m.title, "", true)
	a.hits.Add(ui.Rect{X: 0, Y: 0, W: a.W, H: a.H}, "modal.bg", 0)
	a.hits.Add(r, "modal.box", 0)
	for i, ln := range lines {
		pre := "  "
		if i == 0 {
			pre = p.Spinner() + " "
		}
		p.Text(in.X+1, in.Y+i, pre+ln, th.Base(), in.W-2)
	}
	p.Text(in.X+1, in.Y+in.H-1, "Esc cancel", th.Dim(), in.W-2)
}

func (m *busyModal) key(a *App, ev term.Event) bool {
	if ev.Key == term.KeyEsc || ev.Is('q') {
		if m.cancel != nil {
			m.cancel()
		}
		return true
	}
	return false
}

func (m *busyModal) click(a *App, h ui.Hit, ok bool, ev term.Event) bool { return false }
func (m *busyModal) wheel(a *App, dir int)                               {}

// closeBusy removes a busy dialog if one is showing.
func (a *App) closeBusy() {
	if _, ok := a.modal.(*busyModal); ok {
		a.modal = nil
	}
}
