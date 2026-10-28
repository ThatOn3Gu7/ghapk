package tui

import (
	"context"
	"strings"

	"ghapk/internal/config"
	"ghapk/internal/device"
	"ghapk/internal/engine"
	"ghapk/internal/term"
	"ghapk/internal/ui"
)

type deviceScreen struct {
	checks  []engine.Check
	loading bool
	loaded  bool
	off     int
	viewH   int
	lines   int
}

func (d *deviceScreen) show(a *App) {
	if !d.loaded && !d.loading {
		d.run(a)
	}
}

func (d *deviceScreen) run(a *App) {
	d.loading = true
	a.async(func(ctx context.Context) func(*App) {
		cs := a.eng.Doctor(ctx)
		return func(a *App) {
			d.checks, d.loading, d.loaded = cs, false, true
		}
	})
}

func (d *deviceScreen) hints(a *App) []hint {
	return []hint{{"r", "re-check", runeEv('r')}, {"p", "app package", runeEv('p')}, {"j/k", "scroll", runeEv('j')}}
}

func (d *deviceScreen) draw(a *App, r ui.Rect) {
	p, th := &a.P, a.th
	note := ""
	if d.loading {
		note = p.Spinner() + " checking"
	}
	in := p.Box(r, "Device & setup", note, true)
	a.hits.Add(r, "dev.pane", 0)
	d.viewH = in.H
	if !d.loaded {
		p.Text(in.X+1, in.Y, p.Spinner()+" looking at your phone…", th.Dim(), in.W-2)
		return
	}
	type line struct {
		spans []ui.Span
	}
	var lines []line
	group := ""
	nameW := 16
	for _, c := range d.checks {
		if c.Group != group {
			if group != "" {
				lines = append(lines, line{})
			}
			group = c.Group
			lines = append(lines, line{[]ui.Span{ui.Sp(strings.ToUpper(group), th.Title())}})
		}
		g, gs := a.gl.Dot, th.Dim()
		vs := th.Base()
		switch c.State {
		case engine.CheckOK:
			g, gs = a.gl.Check, th.Good()
		case engine.CheckWarn:
			g, gs, vs = a.gl.Warn, th.Caution(), th.Caution()
		case engine.CheckFail:
			g, gs, vs = a.gl.Cross, th.Bad(), th.Bad()
		}
		val := c.Value
		avail := in.W - 2 - 2 - nameW - 1
		wrapped := ui.Wrap(val, avail)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		for i, w := range wrapped {
			if i == 0 {
				lines = append(lines, line{[]ui.Span{ui.Sp(g+" ", gs), ui.Sp(padTo(c.Name, nameW)+" ", th.Dim()), ui.Sp(w, vs)}})
			} else {
				lines = append(lines, line{[]ui.Span{ui.Sp(strings.Repeat(" ", 2+nameW+1)+w, vs)}})
			}
		}
		if c.Hint != "" && (c.State == engine.CheckWarn || c.State == engine.CheckFail || c.State == engine.CheckInfo) {
			for _, w := range ui.Wrap(c.Hint, avail) {
				lines = append(lines, line{[]ui.Span{ui.Sp(strings.Repeat(" ", 2+nameW+1)+w, th.Dim())}})
			}
		}
	}
	d.lines = len(lines)
	if max := len(lines) - in.H; d.off > max {
		d.off = max
	}
	if d.off < 0 {
		d.off = 0
	}
	for i := 0; i < in.H && d.off+i < len(lines); i++ {
		p.Spans(in.X+1, in.Y+i, lines[d.off+i].spans, in.W-2)
	}
	p.Scrollbar(in.X+in.W-1, in.Y, in.H, len(lines), d.off, in.H)
}

func padTo(s string, w int) string { return term.PadRight(s, w) }

func (d *deviceScreen) key(a *App, ev term.Event) bool {
	page := max(d.viewH, 3)
	switch {
	case ev.Key == term.KeyDown, ev.Is('j'):
		d.off++
	case ev.Key == term.KeyUp, ev.Is('k'):
		d.off--
	case ev.Key == term.KeyPgDn:
		d.off += page
	case ev.Key == term.KeyPgUp:
		d.off -= page
	case ev.Key == term.KeyHome, ev.Is('g'):
		d.off = 0
	case ev.Key == term.KeyEnd, ev.Is('G'):
		d.off = 1 << 20
	case ev.Is('r'):
		a.eng.Dev.Reset()
		d.run(a)
		a.notify("info", "checking again…")
	case ev.Is('p'):
		a.editPackage()
	default:
		return false
	}
	return true
}

// editPackage asks for the Android package name to track.
func (a *App) editPackage() {
	cur := a.eng.Package()
	a.modal = &inputModal{title: "App package", prompt: "The Android package name of the app you install, e.g. com.example.app. ghapk compares it with what the phone has.",
		hint: "Leave empty to learn it from the next downloaded APK.", in: ui.NewInput(cur),
		validate: func(s string) error {
			if s != "" && !device.ValidPackage(s) {
				return errString("that is not a valid package name")
			}
			return nil
		},
		submit: func(s string) {
			if err := a.saveSetting("package", s, false); err != nil {
				a.notify("err", "%s", err.Error())
				return
			}
			a.device.run(a)
		}}
}

type errString string

func (e errString) Error() string { return string(e) }

func (d *deviceScreen) click(a *App, h ui.Hit, ev term.Event) bool { return false }

func (d *deviceScreen) wheel(a *App, h ui.Hit, dir int) bool {
	d.off += dir
	return true
}

var _ = config.GlobalFile
