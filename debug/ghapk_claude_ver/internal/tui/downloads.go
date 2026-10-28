package tui

import (
	"context"
	"fmt"
	"strings"

	"ghapk/internal/apk"
	"ghapk/internal/engine"
	"ghapk/internal/term"
	"ghapk/internal/ui"
)

type downloadsScreen struct {
	list   ui.List
	sel    int
	saved  ui.List
	focus  int // 0 jobs, 1 saved APKs
	detail bool
	cached []engine.CachedAPK
	jobsH  int
	savedH int
}

func (d *downloadsScreen) show(a *App) { d.refreshSaved(a) }

func (d *downloadsScreen) refreshSaved(a *App) {
	d.cached = a.eng.CachedAPKs()
	d.saved.SetN(len(d.cached))
}

func (d *downloadsScreen) hints(a *App) []hint {
	hs := []hint{}
	if d.focus == 1 {
		return []hint{{"↵", "install", keyEv(term.KeyEnter)}, {"x", "delete", runeEv('x')}, {"y", "copy path", runeEv('y')}, {"esc", "jobs", keyEv(term.KeyEsc)}}
	}
	if j := d.current(a); j != nil {
		v := j.job.Snapshot()
		if v.State == engine.JobRunning || v.State == engine.JobAsking {
			hs = append(hs, hint{"c", "cancel", runeEv('c')})
		} else {
			hs = append(hs, hint{"x", "remove", runeEv('x')})
		}
	}
	hs = append(hs, hint{"s", "saved apks", runeEv('s')})
	if d.detail {
		hs = append([]hint{{"esc", "back", keyEv(term.KeyEsc)}}, hs...)
	}
	return hs
}

func (d *downloadsScreen) current(a *App) *jobEntry {
	if d.list.Cur >= 0 && d.list.Cur < len(a.jobs) {
		return a.jobs[d.list.Cur]
	}
	return nil
}

type dlLayout struct {
	jobs, saved, det ui.Rect
	narrow           bool
}

func (d *downloadsScreen) layout(r ui.Rect) dlLayout {
	var l dlLayout
	if r.W < 80 {
		l.narrow = true
		if d.detail && d.focus == 0 {
			l.det = r
			return l
		}
		jh := r.H * 3 / 5
		if jh < 5 {
			jh = r.H
		}
		l.jobs = ui.Rect{X: r.X, Y: r.Y, W: r.W, H: jh}
		if r.H-jh >= 4 {
			l.saved = ui.Rect{X: r.X, Y: r.Y + jh, W: r.W, H: r.H - jh}
		}
		return l
	}
	lw := 38
	jh := r.H * 3 / 5
	l.jobs = ui.Rect{X: r.X, Y: r.Y, W: lw, H: jh}
	l.saved = ui.Rect{X: r.X, Y: r.Y + jh, W: lw, H: r.H - jh}
	l.det = ui.Rect{X: r.X + lw, Y: r.Y, W: r.W - lw, H: r.H}
	return l
}

func (d *downloadsScreen) draw(a *App, r ui.Rect) {
	if d.cached == nil {
		d.refreshSaved(a)
	}
	d.list.SetN(len(a.jobs))
	l := d.layout(r)
	if l.jobs.W > 0 {
		d.drawJobs(a, l.jobs)
	}
	if l.saved.W > 0 {
		d.drawSaved(a, l.saved)
	}
	if l.det.W > 0 {
		d.drawDetail(a, l.det)
	}
}

func jobGlyph(a *App, v engine.JobView) (string, term.Style) {
	th, g := a.th, a.gl
	switch v.State {
	case engine.JobRunning:
		return a.P.Spinner(), th.Caution()
	case engine.JobAsking:
		return "?", th.Caution().With(term.Bold)
	case engine.JobDone:
		if v.Res.NotLanded {
			return g.Warn, th.Caution()
		}
		return g.Check, th.Good()
	case engine.JobCancelled:
		return g.Skip, th.Dim()
	}
	return g.Cross, th.Bad()
}

func (d *downloadsScreen) drawJobs(a *App, r ui.Rect) {
	p, th := &a.P, a.th
	focused := d.focus == 0
	in := p.Box(r, "Jobs", fmt.Sprint(len(a.jobs)), focused)
	a.hits.Add(r, "jobs.pane", 0)
	d.jobsH = in.H
	if len(a.jobs) == 0 {
		p.Text(in.X+1, in.Y, "Nothing yet.", th.Dim(), in.W-2)
		p.Text(in.X+1, in.Y+1, "Press 1, pick a build,", th.Dim(), in.W-2)
		p.Text(in.X+1, in.Y+2, "then ↵ to install it.", th.Dim(), in.W-2)
		return
	}
	from, to := d.list.Visible(in.H)
	for i := from; i < to; i++ {
		y := in.Y + i - from
		v := a.jobs[i].job.Snapshot()
		sel := i == d.list.Cur
		if sel {
			p.Row(in.X, y, in.W, a.rowStyle(term.Style{}, true, focused))
		}
		st := func(s term.Style) term.Style { return a.rowStyle(s, sel, focused) }
		g, gs := jobGlyph(a, v)
		p.Text(in.X+1, y, g, st(gs), 1)
		right := ""
		switch {
		case v.State == engine.JobRunning && v.Prog.Active && v.Prog.Fraction() >= 0:
			right = fmt.Sprintf("%d%%", int(v.Prog.Fraction()*100))
		case v.State == engine.JobRunning:
			right = "…"
		case v.State == engine.JobAsking:
			right = "asks"
		default:
			right = ui.Ago(a.jobs[i].at)
		}
		name := v.Src.Name
		nw := in.W - 5 - len(right)
		p.Text(in.X+3, y, term.Truncate(name, nw, a.gl.Ellipsis), st(th.Base()), nw)
		p.Text(in.X+in.W-1-len(right), y, right, st(th.Dim()), len(right))
		a.hits.Add(ui.Rect{X: in.X, Y: y, W: in.W, H: 1}, "job.row", i)
	}
	p.Scrollbar(in.X+in.W-1, in.Y, in.H, len(a.jobs), d.list.Off, in.H)
}

func (d *downloadsScreen) drawSaved(a *App, r ui.Rect) {
	p, th := &a.P, a.th
	focused := d.focus == 1
	size := int64(0)
	for _, c := range d.cached {
		size += c.Size
	}
	note := ""
	if len(d.cached) > 0 {
		note = ui.Bytes(size)
	}
	in := p.Box(r, "Saved APKs", note, focused)
	a.hits.Add(r, "saved.pane", 0)
	d.savedH = in.H
	if len(d.cached) == 0 {
		p.Text(in.X+1, in.Y, "None kept yet.", th.Dim(), in.W-2)
		return
	}
	from, to := d.saved.Visible(in.H)
	for i := from; i < to; i++ {
		y := in.Y + i - from
		c := d.cached[i]
		sel := i == d.saved.Cur && focused
		if sel {
			p.Row(in.X, y, in.W, a.rowStyle(term.Style{}, true, true))
		}
		st := func(s term.Style) term.Style { return a.rowStyle(s, sel, true) }
		sz := ui.Bytes(c.Size)
		nw := in.W - 4 - len(sz)
		p.Text(in.X+1, y, term.Truncate(shortPath(c.Path), nw, a.gl.Ellipsis), st(th.Base()), nw)
		p.Text(in.X+in.W-1-len(sz), y, sz, st(th.Dim()), len(sz))
		a.hits.Add(ui.Rect{X: in.X, Y: y, W: in.W, H: 1}, "saved.row", i)
	}
	p.Scrollbar(in.X+in.W-1, in.Y, in.H, len(d.cached), d.saved.Off, in.H)
}

func (d *downloadsScreen) drawDetail(a *App, r ui.Rect) {
	p, th := &a.P, a.th
	j := d.current(a)
	if j == nil {
		in := p.Box(r, "Job", "", false)
		p.Text(in.X+1, in.Y, "Downloads and installs show up here, step by step.", th.Dim(), in.W-2)
		return
	}
	v := j.job.Snapshot()
	state := map[engine.JobState]string{engine.JobRunning: "running", engine.JobAsking: "needs you", engine.JobDone: "done", engine.JobFailed: "failed", engine.JobCancelled: "cancelled"}[v.State]
	in := p.Box(r, v.Src.Label(), state, false)
	a.hits.Add(r, "det.pane", 0)
	y := in.Y
	bottom := in.Y + in.H
	line := func(spans ...ui.Span) {
		if y < bottom {
			p.Spans(in.X+1, y, spans, in.W-2)
			y++
		}
	}
	if v.Src.Size > 0 {
		line(ui.Sp(ui.Bytes(v.Src.Size), th.Dim()), ui.Sp("  "+v.Src.Kind, th.Dim()))
		y++
	}
	// steps
	for _, s := range v.Steps {
		g, gs := a.gl.Queue, th.Dim()
		switch s.State {
		case engine.StepRunning:
			g, gs = a.P.Spinner(), th.Caution()
		case engine.StepOK:
			g, gs = a.gl.Check, th.Good()
		case engine.StepWarn:
			g, gs = a.gl.Warn, th.Caution()
		case engine.StepSkip:
			g, gs = a.gl.Skip, th.Dim()
		case engine.StepFail:
			g, gs = a.gl.Cross, th.Bad()
		}
		title := th.Base()
		if s.State == engine.StepPending || s.State == engine.StepSkip {
			title = th.Dim()
		}
		detail := ""
		if s.Detail != "" {
			detail = "  " + s.Detail
		}
		line(ui.Sp(g+" ", gs), ui.Sp(fmt.Sprintf("%-9s", s.Title), title), ui.Sp(term.Truncate(detail, in.W-16, a.gl.Ellipsis), th.Dim()))
		if s.State == engine.StepRunning && v.Prog.Active && s.ID == engine.StDownload && y+1 < bottom {
			pr := v.Prog
			bw := in.W - 6
			if bw > 50 {
				bw = 50
			}
			p.Bar(in.X+3, y, bw, pr.Fraction(), true)
			y++
			stats := ""
			if pr.Total > 0 {
				stats = fmt.Sprintf("%d%%  %s / %s", int(pr.Fraction()*100), ui.Bytes(pr.Done), ui.Bytes(pr.Total))
			} else {
				stats = ui.Bytes(pr.Done)
			}
			stats += "  " + ui.Rate(pr.Rate)
			if pr.ETA > 0 {
				stats += "  eta " + ui.Dur(pr.ETA)
			}
			if pr.Method != "" {
				stats += "  via " + pr.Method
			}
			p.Text(in.X+3, y, term.Truncate(stats, in.W-5, a.gl.Ellipsis), th.Dim(), in.W-5)
			y++
		}
	}
	y++
	if v.State == engine.JobAsking && v.Question != nil {
		for _, ln := range ui.Wrap(a.gl.Warn+" "+v.Question.Title+" — press A to answer", in.W-2) {
			line(ui.Sp(ln, th.Caution().With(term.Bold)))
		}
		y++
	}
	// outcome
	switch v.State {
	case engine.JobDone:
		res := v.Res
		lab := func(s string) ui.Span { return ui.Sp(fmt.Sprintf("%-10s", s), th.Dim()) }
		if res.APK != "" {
			for i, ln := range ui.Wrap(res.APK, in.W-13) {
				if i == 0 {
					line(lab("Saved"), ui.Sp(ln, th.Base()))
				} else {
					line(ui.Sp("           "+ln, th.Base()))
				}
			}
		}
		if res.HaveInfo {
			line(lab("App"), ui.Sp(res.Info.Package, th.Base()))
			line(lab("Version"), ui.Sp(fmt.Sprintf("%s (code %d)", res.Info.VersionName, res.Info.VersionCode), th.Base()))
		}
		switch res.Digest {
		case "match":
			line(lab("Checksum"), ui.Sp(a.gl.Check+" matches GitHub's SHA-256", th.Good()))
		case "mismatch":
			line(lab("Checksum"), ui.Sp(a.gl.Cross+" does NOT match GitHub's", th.Bad()))
		}
		switch {
		case res.InstalledNow > 0:
			line(lab("Phone"), ui.Sp(fmt.Sprintf("%s now on code %d", a.gl.Check, res.InstalledNow), th.Good()))
		case res.NotLanded:
			line(lab("Phone"), ui.Sp(a.gl.Warn+" not on the new build yet", th.Caution()))
		case res.HandedOff:
			line(lab("Phone"), ui.Sp("handed to the installer", th.Base()))
		}
		if res.Diag != nil {
			if res.Diag.Code != "" {
				line(lab("Android"), ui.Sp(res.Diag.Code, th.Bad()))
			}
			for _, ln := range ui.Wrap(res.Diag.Fix, in.W-13) {
				line(ui.Sp("           "+ln, th.Caution()))
			}
		}
		if res.Elapsed > 0 {
			line(lab("Took"), ui.Sp(ui.Dur(res.Elapsed), th.Dim()))
		}
	case engine.JobFailed:
		for _, ln := range ui.Wrap(errOneLine(v.Err), in.W-2) {
			line(ui.Sp(ln, th.Bad()))
		}
		line(ui.Sp(fmt.Sprintf("exit code %d · %s", v.ExitCode(), exitHint(v.ExitCode())), th.Dim()))
	case engine.JobCancelled:
		line(ui.Sp("Cancelled.", th.Dim()))
	}
	// log tail
	if y+2 < bottom && len(v.Log) > 0 {
		y++
		room := bottom - y
		lines := v.Log
		if len(lines) > room {
			lines = lines[len(lines)-room:]
		}
		for _, ln := range lines {
			line(ui.Sp(term.Truncate(strings.TrimSpace(ln), in.W-2, a.gl.Ellipsis), th.Dim()))
		}
	}
}

// ---- input --------------------------------------------------------------------

func (d *downloadsScreen) key(a *App, ev term.Event) bool {
	if d.focus == 1 {
		return d.keySaved(a, ev)
	}
	h := max(d.jobsH, 3)
	switch {
	case ev.Key == term.KeyDown, ev.Is('j'):
		d.list.Move(1, h)
	case ev.Key == term.KeyUp, ev.Is('k'):
		d.list.Move(-1, h)
	case ev.Key == term.KeyHome, ev.Is('g'):
		d.list.Set(0, h)
	case ev.Key == term.KeyEnd, ev.Is('G'):
		d.list.Set(len(a.jobs)-1, h)
	case ev.Key == term.KeyEnter, ev.Key == term.KeyRight, ev.Is('l'):
		d.detail = true
	case ev.Key == term.KeyEsc, ev.Key == term.KeyLeft, ev.Is('h'):
		if !d.detail {
			return ev.Key == term.KeyEsc
		}
		d.detail = false
	case ev.Is('s'):
		d.refreshSaved(a)
		d.focus = 1
		d.detail = false
	case ev.Is('c'):
		if j := d.current(a); j != nil && j.job.Running() {
			j.job.Cancel()
			a.notify("warn", "cancelling…")
		}
	case ev.Is('x'):
		if j := d.current(a); j != nil && !j.job.Running() {
			a.removeJob(j)
		}
	case ev.Is('X'):
		keep := a.jobs[:0:0]
		for _, j := range a.jobs {
			if j.job.Running() {
				keep = append(keep, j)
			}
		}
		a.jobs = keep
		d.list.SetN(len(keep))
	case ev.Is('a'):
		if len(a.asks) > 0 {
			a.showNextAsk()
		}
	default:
		return false
	}
	return true
}

func (a *App) removeJob(e *jobEntry) {
	out := a.jobs[:0:0]
	for _, j := range a.jobs {
		if j != e {
			out = append(out, j)
		}
	}
	a.jobs = out
	a.downloads.list.SetN(len(out))
}

func (d *downloadsScreen) keySaved(a *App, ev term.Event) bool {
	h := max(d.savedH, 3)
	switch {
	case ev.Key == term.KeyDown, ev.Is('j'):
		d.saved.Move(1, h)
	case ev.Key == term.KeyUp, ev.Is('k'):
		d.saved.Move(-1, h)
	case ev.Key == term.KeyEsc, ev.Key == term.KeyLeft, ev.Is('h'), ev.Is('s'):
		d.focus = 0
	case ev.Key == term.KeyEnter, ev.Is('i'):
		d.installSaved(a)
	case ev.Is('y'):
		if d.saved.Cur < len(d.cached) {
			a.copyText(d.cached[d.saved.Cur].Path)
		}
	case ev.Is('x'):
		if d.saved.Cur < len(d.cached) {
			c := d.cached[d.saved.Cur]
			a.ask(engineQuestion{Title: "Delete this APK?", Body: shortPath(c.Path), Options: []string{"Delete", "Keep"}, Default: 1, Danger: true}, func(i int) {
				if i == 0 {
					if err := a.eng.RemoveCached(c.Path); err != nil {
						a.notify("err", "%s", err.Error())
					}
					d.refreshSaved(a)
				}
			})
		}
	default:
		return false
	}
	return true
}

// installSaved hands a kept APK to the Android installer again.
func (d *downloadsScreen) installSaved(a *App) {
	if d.saved.Cur >= len(d.cached) {
		return
	}
	c := d.cached[d.saved.Cur]
	if !a.eng.Dev.CanInstall() {
		a.notify("warn", "this machine can't install APKs")
		return
	}
	body := shortPath(c.Path)
	if in, err := apk.ReadInfo(c.Path); err == nil && in.Package != "" {
		body = fmt.Sprintf("%s\n%s %s (code %d)", shortPath(c.Path), in.Package, in.VersionName, in.VersionCode)
	}
	a.ask(engineQuestion{Title: "Install this saved APK?", Body: body, Options: []string{"Install", "Cancel"}}, func(i int) {
		if i != 0 {
			return
		}
		a.async(func(ctx context.Context) func(*App) {
			how, err := a.eng.Dev.Open(ctx, c.Path, nil)
			return func(a *App) {
				if err != nil {
					a.notify("err", "%s", errOneLine(err))
					return
				}
				a.notify("ok", "opened with %s — confirm on the phone", how)
				a.refreshInstalled()
			}
		})
	})
}

func (d *downloadsScreen) click(a *App, h ui.Hit, ev term.Event) bool {
	switch h.ID {
	case "job.row":
		d.focus = 0
		if h.N == d.list.Cur {
			d.detail = true
		} else {
			d.list.Set(h.N, max(d.jobsH, 3))
			if d.layout(ui.Rect{W: a.W, H: a.H}).narrow {
				d.detail = true
			}
		}
	case "saved.row":
		d.focus = 1
		if h.N == d.saved.Cur {
			d.installSaved(a)
		} else {
			d.saved.Set(h.N, max(d.savedH, 3))
		}
	case "jobs.pane":
		d.focus = 0
	case "saved.pane":
		d.focus = 1
	default:
		return false
	}
	return true
}

func (d *downloadsScreen) wheel(a *App, h ui.Hit, dir int) bool {
	switch {
	case strings.HasPrefix(h.ID, "job"):
		d.list.Scroll(dir, max(d.jobsH, 3))
	case strings.HasPrefix(h.ID, "saved"):
		d.saved.Scroll(dir, max(d.savedH, 3))
	default:
		return false
	}
	return true
}
