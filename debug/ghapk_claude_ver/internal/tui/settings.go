package tui

import (
	"fmt"
	"strings"

	"ghapk/internal/config"
	"ghapk/internal/term"
	"ghapk/internal/ui"
)

type settingsLine struct {
	header string
	s      *config.Setting
}

type settingsScreen struct {
	lines  []settingsLine
	list   ui.List
	global bool
	viewH  int
}

func newSettingsScreen() *settingsScreen {
	s := &settingsScreen{}
	vis := config.Visible()
	for _, g := range config.Groups {
		first := true
		for i := range vis {
			if vis[i].Group != g {
				continue
			}
			if first {
				s.lines = append(s.lines, settingsLine{header: g})
				first = false
			}
			s.lines = append(s.lines, settingsLine{s: &vis[i]})
		}
	}
	s.list.SetN(len(s.lines))
	s.list.Cur = 1
	return s
}

func (s *settingsScreen) show(a *App) {}

func (s *settingsScreen) hints(a *App) []hint {
	scope := "this repo"
	if s.global || a.eng.Repo() == "" {
		scope = "everywhere"
	}
	return []hint{{"↵", "change", keyEv(term.KeyEnter)}, {"d", "reset", runeEv('d')}, {"s", "saves: " + scope, runeEv('s')}}
}

func (s *settingsScreen) cur() *config.Setting {
	if s.list.Cur >= 0 && s.list.Cur < len(s.lines) {
		return s.lines[s.list.Cur].s
	}
	return nil
}

// move steps the cursor, hopping over group headings.
func (s *settingsScreen) move(d, h int) {
	i := s.list.Cur + d
	step := 1
	if d < 0 {
		step = -1
	}
	if d == 1 || d == -1 {
		for i >= 0 && i < len(s.lines) && s.lines[i].s == nil {
			i += step
		}
		if i < 0 || i >= len(s.lines) {
			return
		}
	} else {
		if i < 0 {
			i = 0
		}
		if i >= len(s.lines) {
			i = len(s.lines) - 1
		}
		for i >= 0 && i < len(s.lines) && s.lines[i].s == nil {
			i += step
			if i >= len(s.lines) {
				i = len(s.lines) - 1
				step = -1
			}
		}
	}
	s.list.Set(i, h)
	// show the group heading when the first item of a group is selected
	if i > 0 && s.lines[i-1].s == nil && s.list.Off == i {
		s.list.Off = i - 1
	}
}

func (s *settingsScreen) draw(a *App, r ui.Rect) {
	p, th := &a.P, a.th
	scope := "saving: this repo"
	if s.global || a.eng.Repo() == "" {
		scope = "saving: everywhere"
	}
	wide := r.W >= 96
	listR := r
	var helpR ui.Rect
	if wide {
		lw := r.W * 58 / 100
		listR = ui.Rect{X: r.X, Y: r.Y, W: lw, H: r.H}
		helpR = ui.Rect{X: r.X + lw, Y: r.Y, W: r.W - lw, H: r.H}
	} else if r.H >= 18 {
		hh := 6
		listR = ui.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H - hh}
		helpR = ui.Rect{X: r.X, Y: r.Y + r.H - hh, W: r.W, H: hh}
	}
	in := p.Box(listR, "Settings", scope, true)
	a.hits.Add(listR, "set.pane", 0)
	s.viewH = in.H
	cfg := a.eng.Config()
	s.list.SetN(len(s.lines))
	from, to := s.list.Visible(in.H)
	keyW := 16
	for i := from; i < to; i++ {
		y := in.Y + i - from
		ln := s.lines[i]
		if ln.s == nil {
			p.Text(in.X+1, y, ln.header, th.Title(), in.W-2)
			continue
		}
		set := ln.s
		sel := i == s.list.Cur
		if sel {
			p.Row(in.X, y, in.W, a.rowStyle(term.Style{}, true, true))
		}
		st := func(x term.Style) term.Style { return a.rowStyle(x, sel, true) }
		val := cfg.Str(set.Key)
		src := cfg.Source(set.Key)
		vs := th.Base()
		switch set.Kind {
		case config.Bool:
			if config.NormBool(val) == "1" {
				val, vs = "on", th.Good()
			} else {
				val, vs = "off", th.Dim()
			}
		default:
			if val == "" {
				val, vs = "—", th.Dim()
			}
		}
		if src == config.SrcDef {
			vs = th.Dim()
			if set.Kind == config.Bool && val == "on" {
				vs = th.Good()
			}
		}
		badge := src
		bs := th.Dim()
		if src == config.SrcCLI || src == config.SrcEnv {
			bs = th.Caution()
		}
		p.Text(in.X+1, y, padTo(set.Key, keyW), st(th.Base()), keyW)
		vw := in.W - keyW - 4 - len(badge) - 2
		if vw > 0 {
			p.Text(in.X+1+keyW+1, y, term.Truncate(val, vw, a.gl.Ellipsis), st(vs), vw)
		}
		p.Text(in.X+in.W-2-len(badge), y, badge, st(bs), len(badge))
		a.hits.Add(ui.Rect{X: in.X, Y: y, W: in.W, H: 1}, "set.row", i)
	}
	p.Scrollbar(in.X+in.W-1, in.Y, in.H, len(s.lines), s.list.Off, in.H)
	if helpR.W > 0 {
		s.drawHelp(a, helpR)
	}
}

func (s *settingsScreen) drawHelp(a *App, r ui.Rect) {
	p, th := &a.P, a.th
	set := s.cur()
	if set == nil {
		return
	}
	cfg := a.eng.Config()
	in := p.Box(r, set.Key, cfg.Source(set.Key), false)
	y := in.Y
	for _, ln := range ui.Wrap(set.Help, in.W-2) {
		if y >= in.Y+in.H {
			return
		}
		p.Text(in.X+1, y, ln, th.Base(), in.W-2)
		y++
	}
	y++
	add := func(label, v string) {
		if y < in.Y+in.H && v != "" {
			p.Spans(in.X+1, y, []ui.Span{ui.Sp(fmt.Sprintf("%-9s", label), th.Dim()), ui.Sp(v, th.Base())}, in.W-2)
			y++
		}
	}
	d := set.Default
	if d == "" {
		d = "—"
	}
	add("default", d)
	if len(set.Choices) > 0 {
		add("choices", strings.Join(set.Choices, ", "))
	}
	if src := cfg.Source(set.Key); src == config.SrcCLI || src == config.SrcEnv {
		y++
		for _, ln := range ui.Wrap(a.gl.Warn+" set by a command-line flag or environment variable; that wins over saved values until you restart.", in.W-2) {
			if y < in.Y+in.H {
				p.Text(in.X+1, y, ln, th.Caution(), in.W-2)
				y++
			}
		}
	}
}

func (s *settingsScreen) key(a *App, ev term.Event) bool {
	h := max(s.viewH, 3)
	switch {
	case ev.Key == term.KeyDown, ev.Is('j'):
		s.move(1, h)
	case ev.Key == term.KeyUp, ev.Is('k'):
		s.move(-1, h)
	case ev.Key == term.KeyPgDn:
		s.move(h, h)
	case ev.Key == term.KeyPgUp:
		s.move(-h, h)
	case ev.Key == term.KeyHome, ev.Is('g'):
		s.list.Set(1, h)
		s.list.Off = 0
	case ev.Key == term.KeyEnd, ev.Is('G'):
		s.move(len(s.lines), h)
	case ev.Key == term.KeyEnter, ev.Is(' '), ev.Key == term.KeyRight, ev.Is('l'):
		s.edit(a, ev.Is(' '))
	case ev.Is('d'), ev.Key == term.KeyDelete, ev.Key == term.KeyBackspace:
		if set := s.cur(); set != nil {
			s.save(a, set.Key, "")
		}
	case ev.Is('s'):
		if a.eng.Repo() == "" {
			a.notify("info", "no repository open: saving everywhere")
			return true
		}
		s.global = !s.global
		if s.global {
			a.notify("info", "changes apply to every repository")
		} else {
			a.notify("info", "changes apply to %s only", a.eng.Repo())
		}
	default:
		return false
	}
	return true
}

func (s *settingsScreen) save(a *App, key, val string) {
	global := s.global || key == "repo" || a.eng.Repo() == ""
	if err := a.saveSetting(key, val, global); err != nil {
		a.notify("err", "%s", err.Error())
	}
}

// edit changes the selected setting: toggles a switch, picks from a list, or
// asks for text. quick (space) cycles instead of opening a list.
func (s *settingsScreen) edit(a *App, quick bool) {
	set := s.cur()
	if set == nil {
		return
	}
	cur := a.eng.Config().Str(set.Key)
	switch set.Kind {
	case config.Bool:
		next := "1"
		if config.NormBool(cur) == "1" {
			next = "0"
		}
		s.save(a, set.Key, next)
	case config.Enum:
		if quick {
			next := set.Choices[0]
			for i, c := range set.Choices {
				if c == cur {
					next = set.Choices[(i+1)%len(set.Choices)]
				}
			}
			s.save(a, set.Key, next)
			return
		}
		var items []pickItem
		for _, c := range set.Choices {
			items = append(items, pickItem{Label: c, Value: c})
		}
		a.openPicker(set.Key, items, cur, "", func(it pickItem) { s.save(a, set.Key, it.Value) })
	default:
		raw := a.eng.Config().Str(set.Key)
		if set.Kind == config.String && cur == set.Default {
			raw = cur
		}
		a.modal = &inputModal{title: set.Key, prompt: set.Help, in: ui.NewInput(raw),
			hint: "Empty removes the saved value (back to the default).",
			validate: func(v string) error {
				_, err := config.Validate(set.Key, v)
				return err
			},
			submit: func(v string) { s.save(a, set.Key, v) }}
	}
}

func (s *settingsScreen) click(a *App, h ui.Hit, ev term.Event) bool {
	switch h.ID {
	case "set.row":
		if h.N == s.list.Cur {
			s.edit(a, false)
		} else {
			s.list.Set(h.N, max(s.viewH, 3))
		}
		return true
	}
	return false
}

func (s *settingsScreen) wheel(a *App, h ui.Hit, dir int) bool {
	s.list.Scroll(dir, max(s.viewH, 3))
	return true
}
