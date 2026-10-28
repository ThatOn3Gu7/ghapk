package tui

import (
	"context"
	"fmt"
	"strings"

	"ghapk/internal/engine"
	"ghapk/internal/pick"
	"ghapk/internal/term"
	"ghapk/internal/ui"
	"ghapk/internal/version"
)

// planModal is the "here is what I am about to do" screen shown before an
// install: which build, which artifact, how it compares with what is installed,
// and what changed since the last install.
type planModal struct {
	plan   *engine.Plan
	opts   []int // indexes into plan.Candidates that can be chosen
	sel    int   // position within opts
	btn    int
	scroll int
	viewH  int
	canIns bool
}

type prow struct {
	spans []ui.Span
	hit   string
	n     int
}

func newPlanModal(a *App, p *engine.Plan) *planModal {
	m := &planModal{plan: p, canIns: a.eng.Dev.CanInstall()}
	for i, c := range p.Candidates {
		if c.Skip == "" {
			m.opts = append(m.opts, i)
			if c.Art.ID == p.Source.Art.ID {
				m.sel = len(m.opts) - 1
			}
		}
	}
	if !m.canIns {
		m.btn = 1
	}
	return m
}

func (m *planModal) source() engine.Source {
	if len(m.opts) == 0 {
		return m.plan.Source
	}
	return engine.FromCandidate(m.plan.Run, m.plan.Candidates[m.opts[m.sel]])
}

func runLabel(r int, id int64) string {
	if r > 0 {
		return fmt.Sprintf("#%d", r)
	}
	return fmt.Sprintf("#%d", id)
}

func (m *planModal) rows(a *App, w int) []prow {
	th := a.th
	p := m.plan
	var out []prow
	add := func(spans ...ui.Span) { out = append(out, prow{spans: spans}) }
	label := func(s string) ui.Span { return ui.Sp(fmt.Sprintf("%-9s", s), th.Dim()) }

	r := p.Run
	title := r.DisplayTitle
	if p.Source.Kind == "release" {
		title = p.Source.Release.Name
		if title == "" {
			title = p.Source.Release.TagName
		}
	}
	if p.Source.Kind == "release" {
		add(label("Release"), ui.Sp(p.Source.Release.TagName+" · "+title, th.Base()))
	} else {
		add(label("Build"), ui.Sp(runLabel(r.RunNumber, r.ID), th.Acc()), ui.Sp(" · "+r.HeadBranch+" · "+r.Short(), th.Dim()))
		if title != "" {
			for i, ln := range ui.Wrap(title, w-11) {
				if i == 0 {
					add(ui.Sp(strings.Repeat(" ", 9), th.Dim()), ui.Sp(ln, th.Base()))
				} else {
					add(ui.Sp(strings.Repeat(" ", 9)+ln, th.Base()))
				}
			}
		}
		add(ui.Sp(strings.Repeat(" ", 9)+ui.Ago(r.CreatedAt)+" ago · "+r.Actor.Login, th.Dim()))
	}
	add()
	// version line
	ver := p.Version
	in := p.Installed
	vs := []ui.Span{label("Version")}
	if ver != "" {
		vs = append(vs, ui.Sp(ver, th.Base()))
	} else {
		vs = append(vs, ui.Sp("unknown until downloaded", th.Dim()))
	}
	switch {
	case in.Found:
		inv := in.VersionName
		if inv == "" {
			inv = fmt.Sprint(in.VersionCode)
		}
		vs = append(vs, ui.Sp("  "+a.gl.Right+"  installed "+inv, th.Dim()))
	case in.Known:
		vs = append(vs, ui.Sp("  (not installed yet)", th.Dim()))
	}
	add(vs...)
	switch {
	case p.Downgrade:
		add(ui.Sp(strings.Repeat(" ", 9)+a.gl.Warn+" older than what is installed", th.Bad()))
	case p.Same:
		add(ui.Sp(strings.Repeat(" ", 9)+a.gl.Warn+" same version as installed", th.Caution()))
	case in.Found && ver != "":
		add(ui.Sp(strings.Repeat(" ", 9)+a.gl.Up+" newer than installed", th.Good()))
	}
	if p.Active != nil {
		add(ui.Sp(strings.Repeat(" ", 9)+a.gl.Warn+" "+runLabel(p.Active.RunNumber, p.Active.ID)+" is still building — a newer APK may follow", th.Caution()))
	}
	add()
	// artifacts
	if len(m.opts) > 0 {
		add(ui.Sp("Artifact", th.Title()))
		for pos, ci := range m.opts {
			c := p.Candidates[ci]
			dot := a.gl.Unmark
			if pos == m.sel {
				dot = a.gl.Mark
			}
			st := th.Base()
			if pos == m.sel {
				st = th.Acc()
			}
			name, size := ui.FitLeftRight(c.Art.Name, ui.Bytes(c.Art.Size), w-4, a.gl.Ellipsis)
			pad := w - 4 - term.StringWidth(name) - term.StringWidth(size)
			if pad < 1 {
				pad = 1
			}
			out = append(out, prow{spans: []ui.Span{ui.Sp(dot+" ", st), ui.Sp(name+strings.Repeat(" ", pad), st), ui.Sp(size, th.Dim())}, hit: "plan.art", n: pos})
		}
	} else {
		add(label("Artifact"), ui.Sp(p.Source.Name+"  "+ui.Bytes(p.Source.Size), th.Base()))
	}
	if len(p.Skipped) > 0 {
		add()
		add(ui.Sp("Passed over", th.Dim()))
		for i, s := range p.Skipped {
			if i >= 3 {
				add(ui.Sp(fmt.Sprintf("  …and %d more", len(p.Skipped)-3), th.Dim()))
				break
			}
			add(ui.Sp("  "+runLabel(s.Run.RunNumber, s.Run.ID)+" "+s.Reason, th.Dim()))
		}
	}
	if len(p.Changes) > 0 {
		add()
		add(ui.Sp(fmt.Sprintf("Since your last install · %s", ui.Plural(len(p.Changes), "commit")), th.Title()))
		for _, c := range p.Changes {
			sha := c.SHA
			if len(sha) > 7 {
				sha = sha[:7]
			}
			add(ui.Sp(sha+" ", th.Dim()), ui.Sp(c.Subject(), th.Base()))
		}
	}
	return out
}

func (m *planModal) buttons(a *App) []string {
	if m.canIns {
		return []string{"Install", "Download only", "Cancel"}
	}
	return []string{"Download", "Cancel"}
}

func (m *planModal) draw(a *App) {
	p, th := &a.P, a.th
	w := a.dialogWidth(78)
	rows := m.rows(a, w-4)
	h := len(rows) + 5
	if h > a.H-2 {
		h = a.H - 2
	}
	if h < 9 {
		h = 9
	}
	r := a.centered(w, h)
	title := "Install this build?"
	if m.plan.Downgrade {
		title = "Install an OLDER build?"
	}
	if !m.canIns {
		title = "Download this build?"
	}
	if m.plan.Downgrade {
		fs, ts := th.Bad(), th.Bad().With(term.Bold)
		p.FrameOverride, p.TitleOverride = &fs, &ts
		defer func() { p.FrameOverride, p.TitleOverride = nil, nil }()
	}
	in := p.Box(r, title, "", true)
	a.hits.Add(ui.Rect{X: 0, Y: 0, W: a.W, H: a.H}, "modal.bg", 0)
	a.hits.Add(r, "modal.box", 0)
	m.viewH = in.H - 2
	if max := len(rows) - m.viewH; m.scroll > max {
		m.scroll = max
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	for i := 0; i < m.viewH && m.scroll+i < len(rows); i++ {
		row := rows[m.scroll+i]
		y := in.Y + i
		p.Spans(in.X+1, y, row.spans, in.W-2)
		if row.hit != "" {
			a.hits.Add(ui.Rect{X: in.X, Y: y, W: in.W, H: 1}, row.hit, row.n)
		}
	}
	p.Scrollbar(in.X+in.W-1, in.Y, m.viewH, len(rows), m.scroll, m.viewH)
	// buttons
	y := in.Y + in.H - 1
	x := in.X + 1
	for i, b := range m.buttons(a) {
		txt := " " + b + " "
		st := th.Dim()
		if i == m.btn {
			st = th.Chip()
			if m.plan.Downgrade && i == 0 {
				st = term.Style{FG: term.RGB(12, 16, 24, 0), BG: th.Err, Attr: term.Bold}
			}
		}
		x += p.Text(x, y, txt, st, 0)
		a.hits.Add(ui.Rect{X: x - term.StringWidth(txt), Y: y, W: term.StringWidth(txt), H: 1}, "plan.btn", i)
		x += 2
	}
	help := "↑↓ artifact"
	if len(m.opts) < 2 {
		help = ""
	}
	if in.W-(x-in.X) > len(help)+2 {
		p.Right(in.X+1, y, in.W-2, []ui.Span{ui.Sp(help, th.Dim())})
	}
}

func (m *planModal) run(a *App, btn int) bool {
	names := m.buttons(a)
	label := names[btn]
	switch label {
	case "Install":
		a.startJob(m.source(), engine.JobOpts{})
	case "Download only", "Download":
		a.startJob(m.source(), engine.JobOpts{NoInstall: true})
	}
	return true
}

func (m *planModal) key(a *App, ev term.Event) bool {
	nb := len(m.buttons(a))
	switch {
	case ev.Key == term.KeyEsc, ev.Is('q'), ev.Is('n'):
		return true
	case ev.Key == term.KeyEnter:
		return m.run(a, m.btn)
	case ev.Is('i') && m.canIns:
		return m.run(a, 0)
	case ev.Is('d'):
		if m.canIns {
			return m.run(a, 1)
		}
		return m.run(a, 0)
	case ev.Key == term.KeyRight, ev.Key == term.KeyTab, ev.Is('l'):
		m.btn = (m.btn + 1) % nb
	case ev.Key == term.KeyLeft, ev.Key == term.KeyBacktab, ev.Is('h'):
		m.btn = (m.btn + nb - 1) % nb
	case ev.Key == term.KeyDown, ev.Is('j'):
		if m.sel+1 < len(m.opts) {
			m.sel++
			m.rebuildForSelection(a)
		}
	case ev.Key == term.KeyUp, ev.Is('k'):
		if m.sel > 0 {
			m.sel--
			m.rebuildForSelection(a)
		}
	case ev.Key == term.KeyPgDn:
		m.scroll += m.viewH
	case ev.Key == term.KeyPgUp:
		m.scroll -= m.viewH
	}
	return false
}

// rebuildForSelection refreshes the version comparison for another artifact.
func (m *planModal) rebuildForSelection(a *App) {
	src := m.source()
	m.plan.Source = src
	ver := version.Extract(src.Name)
	m.plan.Version = ver
	m.plan.Downgrade, m.plan.Same = false, false
	in := m.plan.Installed
	if in.Found && ver != "" && in.VersionName != "" {
		switch version.Compare(ver, version.Extract(in.VersionName)) {
		case -1:
			m.plan.Downgrade = true
		case 0:
			m.plan.Same = true
		}
	}
}

func (m *planModal) click(a *App, h ui.Hit, ok bool, ev term.Event) bool {
	if !ok || h.ID == "modal.bg" {
		return true
	}
	switch h.ID {
	case "plan.btn":
		m.btn = h.N
		return m.run(a, h.N)
	case "plan.art":
		m.sel = h.N
		m.rebuildForSelection(a)
	}
	return false
}

func (m *planModal) wheel(a *App, dir int) { m.scroll += dir }

// planNewest works out what "install the newest build" would do and shows it.
func (a *App) planNewest() {
	if a.eng.Repo() == "" {
		a.notify("warn", "pick a repository first (R)")
		return
	}
	a.planLoading = true
	ctx, cancel := context.WithCancel(a.ctx)
	a.modal = &busyModal{title: "Newest build", msg: "Looking through recent builds for the best APK…", cancel: func() { cancel(); a.planLoading = false }}
	f := a.builds.effFilter(a)
	go func() {
		plan, err := a.eng.PlanNewest(ctx, f)
		if ctx.Err() != nil {
			return
		}
		a.post(funcMsg(func(a *App) {
			a.planLoading = false
			a.closeBusy()
			if err != nil {
				a.showError("Nothing to install", err)
				return
			}
			a.modal = newPlanModal(a, plan)
		}))
	}()
}

// planRun is the same for one chosen run (and optionally one artifact).
func (a *App) planRun(runID, artifactID int64) {
	a.planLoading = true
	ctx, cancel := context.WithCancel(a.ctx)
	a.modal = &busyModal{title: "Build", msg: "Checking this build against your phone…", cancel: func() { cancel(); a.planLoading = false }}
	go func() {
		plan, err := a.eng.PlanRun(ctx, runID, artifactID)
		if ctx.Err() != nil {
			return
		}
		a.post(funcMsg(func(a *App) {
			a.planLoading = false
			a.closeBusy()
			if err != nil {
				a.showError("Can't use this build", err)
				return
			}
			a.modal = newPlanModal(a, plan)
		}))
	}()
}

var _ = pick.Candidate{}
