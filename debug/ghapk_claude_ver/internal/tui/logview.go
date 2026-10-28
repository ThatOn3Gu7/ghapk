package tui

import (
	"fmt"

	"ghapk/internal/term"
	"ghapk/internal/ui"
)

type logScreen struct {
	off    int
	follow bool
	viewH  int
	n      int
}

func (l *logScreen) show(a *App) { l.follow = true }

func (l *logScreen) hints(a *App) []hint {
	f := "follow"
	if l.follow {
		f = "following"
	}
	return []hint{{"f", f, runeEv('f')}, {"y", "copy", runeEv('y')}, {"g/G", "top/end", runeEv('G')}}
}

func (l *logScreen) draw(a *App, r ui.Rect) {
	p, th := &a.P, a.th
	lines := a.eng.Log.Lines()
	l.n = len(lines)
	note := a.eng.Log.Path()
	if note != "" && len(note) > r.W-14 {
		note = ""
	}
	in := p.Box(r, "Log", note, true)
	a.hits.Add(r, "log.pane", 0)
	l.viewH = in.H
	if max := len(lines) - in.H; l.follow || l.off > max {
		l.off = max
	}
	if l.off < 0 {
		l.off = 0
	}
	if len(lines) == 0 {
		p.Text(in.X+1, in.Y, "Nothing logged yet.", th.Dim(), in.W-2)
		return
	}
	for i := 0; i < in.H && l.off+i < len(lines); i++ {
		ln := lines[l.off+i]
		st := th.Base()
		switch {
		case containsAny(ln.Text, "fail", "error", "HTTP 4", "HTTP 5"):
			st = th.Caution()
		}
		ts := ln.At.Format("15:04:05")
		p.Text(in.X+1, in.Y+i, ts, th.Dim(), 8)
		p.Text(in.X+10, in.Y+i, term.Truncate(ln.Text, in.W-12, a.gl.Ellipsis), st, in.W-11)
	}
	p.Scrollbar(in.X+in.W-1, in.Y, in.H, len(lines), l.off, in.H)
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		for i := 0; i+len(x) <= len(s); i++ {
			if s[i:i+len(x)] == x {
				return true
			}
		}
	}
	return false
}

func (l *logScreen) key(a *App, ev term.Event) bool {
	page := max(l.viewH, 3)
	move := func(d int) { l.follow = false; l.off += d }
	switch {
	case ev.Key == term.KeyDown, ev.Is('j'):
		move(1)
	case ev.Key == term.KeyUp, ev.Is('k'):
		move(-1)
	case ev.Key == term.KeyPgDn:
		move(page)
	case ev.Key == term.KeyPgUp:
		move(-page)
	case ev.Key == term.KeyHome, ev.Is('g'):
		l.follow = false
		l.off = 0
	case ev.Key == term.KeyEnd, ev.Is('G'):
		l.follow = true
	case ev.Is('f'):
		l.follow = !l.follow
	case ev.Is('y'):
		var s string
		lines := a.eng.Log.Lines()
		if len(lines) > 60 {
			lines = lines[len(lines)-60:]
		}
		for _, ln := range lines {
			s += fmt.Sprintf("[%s] %s\n", ln.At.Format("15:04:05"), ln.Text)
		}
		a.copyText(s)
	default:
		return false
	}
	return true
}

func (l *logScreen) click(a *App, h ui.Hit, ev term.Event) bool { return false }
func (l *logScreen) wheel(a *App, h ui.Hit, dir int) bool {
	l.follow = false
	l.off += dir
	return true
}
