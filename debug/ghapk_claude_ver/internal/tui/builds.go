package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ghapk/internal/config"
	"ghapk/internal/engine"
	"ghapk/internal/ghub"
	"ghapk/internal/pick"
	"ghapk/internal/term"
	"ghapk/internal/ui"
	"ghapk/internal/version"
)

type artsEntry struct {
	cands   []pick.Candidate
	err     error
	loading bool
}

type buildsScreen struct {
	runs     []engine.RunView
	view     []int
	list     ui.List
	err      error
	loading  bool
	seq      int
	loadedAt time.Time
	filter   engine.RunFilter
	text     *ui.Input
	textOn   bool
	marks    map[int64]bool
	arts     map[int64]*artsEntry
	artList  ui.List
	focus    int // 0 runs, 1 artifacts
	detail   bool
	selRun   int64
	topRun   int64

	watching int64
	watchEnd func()

	runsH, artsH int
}

func newBuildsScreen() *buildsScreen {
	return &buildsScreen{text: ui.NewInput(""), marks: map[int64]bool{}, arts: map[int64]*artsEntry{}}
}

func (b *buildsScreen) reset() {
	b.runs, b.view, b.err = nil, nil, nil
	b.marks = map[int64]bool{}
	b.arts = map[int64]*artsEntry{}
	b.filter = engine.RunFilter{}
	b.text.SetText("")
	b.textOn, b.detail, b.focus = false, false, 0
	b.selRun, b.topRun = 0, 0
	b.list = ui.List{}
	b.artList = ui.List{}
}

func (b *buildsScreen) invalidateArts() { b.arts = map[int64]*artsEntry{}; b.selRun = 0 }

// effFilter merges the interactive filter with the saved settings.
func (b *buildsScreen) effFilter(a *App) engine.RunFilter {
	cfg := a.eng.Config()
	f := b.filter
	switch f.Branch {
	case "":
		f.Branch = cfg.Str("branch")
	case "*":
		f.Branch = ""
	}
	switch f.Workflow {
	case "":
		f.Workflow = cfg.Str("workflow")
	case "*":
		f.Workflow = ""
	}
	return f
}

// ---- loading ----------------------------------------------------------------

func (b *buildsScreen) load(a *App, refresh bool) {
	if a.eng.Repo() == "" {
		return
	}
	b.seq++
	seq := b.seq
	b.loading = true
	f := b.effFilter(a)
	auto := !refresh
	a.async(func(ctx context.Context) func(*App) {
		runs, err := a.eng.Runs(ctx, f, refresh)
		return func(a *App) {
			bs := a.builds
			if seq != bs.seq {
				return
			}
			bs.loading = false
			bs.loadedAt = time.Now()
			bs.err = err
			if n := a.eng.TakeNote(); n != "" {
				a.notify("warn", "%s", n)
			}
			if err != nil {
				if len(bs.runs) == 0 {
					return
				}
				a.notify("err", "refresh failed: %s", errOneLine(err))
				return
			}
			var keep int64
			if r := bs.current(); r != nil {
				keep = r.Run.ID
			}
			prevTop := bs.topRun
			bs.runs = runs
			bs.refilter(keep)
			if len(runs) > 0 {
				bs.topRun = runs[0].Run.ID
				if auto && prevTop != 0 && bs.topRun != prevTop {
					rv := runs[0]
					switch {
					case rv.Run.Active():
						a.notify("info", "new build %s started", runLabel(rv.Run.RunNumber, rv.Run.ID))
					case rv.Run.OK():
						a.notify("ok", "new build %s finished — I to install", runLabel(rv.Run.RunNumber, rv.Run.ID))
					}
				}
			}
		}
	})
}

func (b *buildsScreen) autoRefresh(a *App) {
	if a.tab != TabBuilds || b.loading || a.modal != nil || !a.repoSet || b.textOn {
		return
	}
	secs := a.eng.Config().Int("refresh")
	if secs <= 0 {
		return
	}
	every := time.Duration(secs) * time.Second
	for _, r := range b.runs {
		if r.Run.Active() {
			every = 8 * time.Second
			if every > time.Duration(secs)*time.Second {
				every = time.Duration(secs) * time.Second
			}
			break
		}
	}
	if time.Since(b.loadedAt) < every {
		return
	}
	active := false
	for _, r := range b.runs {
		active = active || r.Run.Active()
	}
	b.load(a, active)
}

// refilter rebuilds the visible list, keeping the cursor on run keep if it is
// still there.
func (b *buildsScreen) refilter(keep int64) {
	q := strings.Fields(strings.ToLower(b.text.String()))
	b.view = b.view[:0]
	for i, r := range b.runs {
		hay := strings.ToLower(fmt.Sprintf("#%d %s %s %s %s %s", r.Run.RunNumber, r.Run.DisplayTitle, r.Run.HeadBranch, r.Run.Actor.Login, r.Run.Short(), r.Run.Name))
		ok := true
		for _, t := range q {
			if !strings.Contains(hay, t) {
				ok = false
				break
			}
		}
		if ok {
			b.view = append(b.view, i)
		}
	}
	b.list.SetN(len(b.view))
	if keep != 0 {
		for vi, ri := range b.view {
			if b.runs[ri].Run.ID == keep {
				b.list.Cur = vi
				break
			}
		}
	}
}

func (b *buildsScreen) current() *engine.RunView {
	if b.list.Cur < 0 || b.list.Cur >= len(b.view) {
		return nil
	}
	return &b.runs[b.view[b.list.Cur]]
}

// syncSelection loads the artifact list for whichever run the cursor is on.
func (b *buildsScreen) syncSelection(a *App) {
	r := b.current()
	if r == nil {
		b.selRun = 0
		return
	}
	if r.Run.ID == b.selRun {
		return
	}
	b.selRun = r.Run.ID
	b.artList = ui.List{}
	b.focus = 0
	if _, ok := b.arts[r.Run.ID]; ok {
		return
	}
	id := r.Run.ID
	e := &artsEntry{loading: true}
	b.arts[id] = e
	a.async(func(ctx context.Context) func(*App) {
		cands, err := a.eng.RunArtifacts(ctx, id)
		return func(a *App) {
			e.cands, e.err, e.loading = cands, err, false
			if a.builds.selRun == id {
				a.builds.artList.SetN(len(cands))
			}
		}
	})
}

func (b *buildsScreen) curArts() *artsEntry {
	if b.selRun == 0 {
		return nil
	}
	return b.arts[b.selRun]
}

// ---- layout ------------------------------------------------------------------

type buildsLayout struct {
	runs, arts, det ui.Rect
	narrow          bool
}

func (b *buildsScreen) layout(r ui.Rect) buildsLayout {
	var l buildsLayout
	if r.W < 64 {
		l.narrow = true
		if !b.detail {
			l.runs = r
			return l
		}
		ah := r.H / 2
		if ah < 6 {
			ah = 6
		}
		if ah > 12 {
			ah = 12
		}
		l.arts = ui.Rect{X: r.X, Y: r.Y, W: r.W, H: ah}
		l.det = ui.Rect{X: r.X, Y: r.Y + ah, W: r.W, H: r.H - ah}
		return l
	}
	lw := r.W * 42 / 100
	if lw < 38 {
		lw = 38
	}
	l.runs = ui.Rect{X: r.X, Y: r.Y, W: lw, H: r.H}
	rw := r.W - lw
	ah := r.H * 2 / 5
	if ah < 6 {
		ah = 6
	}
	if ah > 12 {
		ah = 12
	}
	l.arts = ui.Rect{X: r.X + lw, Y: r.Y, W: rw, H: ah}
	l.det = ui.Rect{X: r.X + lw, Y: r.Y + ah, W: rw, H: r.H - ah}
	return l
}

// ---- drawing -----------------------------------------------------------------

func (b *buildsScreen) draw(a *App, r ui.Rect) {
	l := b.layout(r)
	if l.runs.W > 0 {
		b.drawRuns(a, l.runs)
	}
	if l.arts.W > 0 {
		b.drawArts(a, l.arts)
		b.drawDetails(a, l.det)
	}
}

func (a *App) rowStyle(st term.Style, sel, focused bool) term.Style {
	if !sel {
		return st
	}
	s := a.th.Sel(focused)
	if s.Attr&term.Reverse != 0 {
		st.Attr |= term.Reverse
		return st
	}
	st.BG = s.BG
	if focused {
		st.Attr |= term.Bold
	}
	return st
}

func (a *App) statusGlyph(r ghub.Run) (string, term.Style) {
	th, g := a.th, a.gl
	switch {
	case r.Active():
		return a.P.Spinner(), th.Caution()
	case r.OK():
		return g.Check, th.Good()
	}
	switch r.Conclusion {
	case "failure", "timed_out", "startup_failure":
		return g.Cross, th.Bad()
	case "cancelled", "skipped", "stale":
		return g.Skip, th.Dim()
	}
	return g.Unknown, th.Dim()
}

func (b *buildsScreen) drawRuns(a *App, r ui.Rect) {
	p, th := &a.P, a.th
	focused := b.focus == 0
	note := ""
	if len(b.runs) > 0 {
		note = fmt.Sprintf("%d/%d", b.list.Cur+1, len(b.view))
		if len(b.view) != len(b.runs) {
			note += fmt.Sprintf(" of %d", len(b.runs))
		}
	}
	if b.loading {
		note = p.Spinner() + " " + note
	}
	in := p.Box(r, "Builds", note, focused)
	a.hits.Add(r, "runs.pane", 0)
	top := in.Y
	if b.textOn || b.text.String() != "" {
		p.Text(in.X+1, top, "/", th.Acc(), 1)
		b.text.Draw(p, in.X+3, top, in.W-4, b.textOn, "")
		a.hits.Add(ui.Rect{X: in.X, Y: top, W: in.W, H: 1}, "runs.filter", 0)
		top++
	}
	h := in.Y + in.H - top
	b.runsH = h
	if h < 1 {
		return
	}
	switch {
	case a.eng.Repo() == "":
		msg := "No repository yet."
		if a.repoErr != nil {
			msg = "Couldn't open the repository."
		}
		p.Text(in.X+1, top, msg, th.Dim(), in.W-2)
		p.Text(in.X+1, top+1, "Press R to choose one.", th.Dim(), in.W-2)
		return
	case b.err != nil && len(b.runs) == 0:
		lines := ui.Wrap(errOneLine(b.err), in.W-2)
		for i, ln := range lines {
			if i >= h-2 {
				break
			}
			p.Text(in.X+1, top+i, ln, th.Bad(), in.W-2)
		}
		var ge *ghub.Error
		hint := "Press r to try again."
		if asErr(b.err, &ge) {
			if hh := hintFor(ge); hh != "" {
				hint = hh
			}
		}
		for i, ln := range ui.Wrap(hint, in.W-2) {
			if len(lines)+1+i >= h {
				break
			}
			p.Text(in.X+1, top+len(lines)+1+i, ln, th.Dim(), in.W-2)
		}
		return
	case len(b.view) == 0:
		switch {
		case b.loading:
			p.Text(in.X+1, top, p.Spinner()+" loading builds…", th.Dim(), in.W-2)
		case len(b.runs) > 0:
			p.Text(in.X+1, top, "No build matches the filter.", th.Dim(), in.W-2)
			p.Text(in.X+1, top+1, "x clears filters", th.Dim(), in.W-2)
		default:
			p.Text(in.X+1, top, "No builds found.", th.Dim(), in.W-2)
			if s := a.filterSummary(); s != "" {
				p.Text(in.X+1, top+1, "Filtered by: "+s, th.Dim(), in.W-2)
				p.Text(in.X+1, top+2, "x clears filters", th.Dim(), in.W-2)
			}
		}
		return
	}
	numW := 4
	for _, rv := range b.runs {
		if n := len(fmt.Sprint(rv.Run.RunNumber)) + 1; n > numW {
			numW = n
		}
	}
	from, to := b.list.Visible(h)
	for i := from; i < to; i++ {
		y := top + i - from
		rv := b.runs[b.view[i]]
		sel := i == b.list.Cur
		b.drawRunRow(a, in.X, y, in.W-1, rv, numW, sel, focused, b.marks[rv.Run.ID])
		a.hits.Add(ui.Rect{X: in.X, Y: y, W: in.W, H: 1}, "runs.row", i)
	}
	p.Scrollbar(in.X+in.W-1, top, h, len(b.view), b.list.Off, h)
}

func asErr(err error, target **ghub.Error) bool {
	for err != nil {
		if e, ok := err.(*ghub.Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func (b *buildsScreen) drawRunRow(a *App, x, y, w int, rv engine.RunView, numW int, sel, focused, marked bool) {
	p, th := &a.P, a.th
	bg := a.rowStyle(term.Style{}, sel, focused)
	if sel {
		p.Row(x, y, w+1, bg)
	}
	st := func(s term.Style) term.Style { return a.rowStyle(s, sel, focused) }
	g, gst := a.statusGlyph(rv.Run)
	if marked {
		g, gst = a.gl.Mark, th.Acc()
	}
	cx := x + 1
	cx += p.Text(cx, y, g, st(gst), 0) + 1
	num := fmt.Sprintf("#%d", rv.Run.RunNumber)
	p.Text(cx, y, num, st(th.Acc()), numW)
	cx += numW + 1
	// right-hand columns
	age := ui.Ago(rv.Run.CreatedAt)
	apk := " "
	apkSt := th.Dim()
	if rv.HasAPK {
		apk, apkSt = a.gl.Pkg, th.Good()
	} else if rv.Artifacts == 0 && !rv.Run.Active() {
		apk = a.gl.Skip
	}
	rightW := 2 + 4 + 1
	p.Text(x+w-1, y, apk, st(apkSt), 1)
	p.Text(x+w-1-1-len(age), y, age, st(th.Dim()), len(age))
	_ = rightW
	avail := w - (cx - x) - 2 - 5
	if avail < 4 {
		return
	}
	if w >= 44 {
		bw := avail / 4
		if bw > 12 {
			bw = 12
		}
		if bw >= 5 {
			br := term.Truncate(rv.Run.HeadBranch, bw, a.gl.Ellipsis)
			p.Text(cx, y, br, st(th.Dim()), bw)
			cx += bw + 1
			avail -= bw + 1
		}
	}
	title := rv.Run.DisplayTitle
	if title == "" {
		title = rv.Run.Name
	}
	p.Text(cx, y, term.Truncate(title, avail, a.gl.Ellipsis), st(th.Base()), avail)
}

func (b *buildsScreen) drawArts(a *App, r ui.Rect) {
	p, th := &a.P, a.th
	focused := b.focus == 1
	run := b.current()
	title, note := "Artifacts", ""
	if run != nil {
		note = runLabel(run.Run.RunNumber, run.Run.ID)
	}
	in := p.Box(r, title, note, focused)
	a.hits.Add(r, "arts.pane", 0)
	e := b.curArts()
	switch {
	case run == nil:
		p.Text(in.X+1, in.Y, "Select a build.", th.Dim(), in.W-2)
		return
	case e == nil || e.loading:
		p.Text(in.X+1, in.Y, p.Spinner()+" loading…", th.Dim(), in.W-2)
		return
	case e.err != nil:
		for i, ln := range ui.Wrap(errOneLine(e.err), in.W-2) {
			if i >= in.H {
				break
			}
			p.Text(in.X+1, in.Y+i, ln, th.Bad(), in.W-2)
		}
		return
	case len(e.cands) == 0:
		msg := "No artifacts — expired, or the build didn't upload any."
		switch {
		case run.Run.Active():
			msg = "Still building; artifacts appear when it finishes."
		case !run.Run.OK():
			msg = "This build did not succeed, so it has no artifacts. e shows why."
		}
		for i, ln := range ui.Wrap(msg, in.W-2) {
			if i >= in.H {
				break
			}
			p.Text(in.X+1, in.Y+i, ln, th.Dim(), in.W-2)
		}
		return
	}
	b.artList.SetN(len(e.cands))
	b.artsH = in.H
	from, to := b.artList.Visible(in.H)
	for i := from; i < to; i++ {
		c := e.cands[i]
		y := in.Y + i - from
		sel := i == b.artList.Cur && focused
		cur := i == b.artList.Cur
		g, gs := a.gl.Dot, th.Dim()
		switch {
		case c.Skip != "":
			g, gs = a.gl.Skip, th.Dim()
		case c.Choice:
			g, gs = a.gl.Check, th.Good()
		}
		if cur && !focused {
			p.Row(in.X, y, in.W, th.Sel(false))
		}
		if sel {
			p.Row(in.X, y, in.W, a.rowStyle(term.Style{}, true, true))
		}
		st := func(s term.Style) term.Style { return a.rowStyle(s, sel || (cur && !focused), focused) }
		p.Text(in.X+1, y, g, st(gs), 1)
		size := ui.Bytes(c.Art.Size)
		nameW := in.W - 5 - len(size)
		ns := th.Base()
		if c.Skip != "" {
			ns = th.Dim()
		}
		p.Text(in.X+3, y, term.Truncate(c.Art.Name, nameW, a.gl.Ellipsis), st(ns), nameW)
		p.Text(in.X+in.W-1-len(size), y, size, st(th.Dim()), len(size))
		a.hits.Add(ui.Rect{X: in.X, Y: y, W: in.W, H: 1}, "arts.row", i)
	}
	p.Scrollbar(in.X+in.W-1, in.Y, in.H, len(e.cands), b.artList.Off, in.H)
}

func (b *buildsScreen) drawDetails(a *App, r ui.Rect) {
	p, th := &a.P, a.th
	in := p.Box(r, "Details", "", false)
	a.hits.Add(r, "det.pane", 0)
	run := b.current()
	if run == nil {
		return
	}
	rr := run.Run
	y := in.Y
	line := func(spans ...ui.Span) {
		if y < in.Y+in.H {
			p.Spans(in.X+1, y, spans, in.W-2)
			y++
		}
	}
	title := rr.DisplayTitle
	if title == "" {
		title = rr.Name
	}
	for _, ln := range ui.Wrap(title, in.W-2) {
		if y >= in.Y+in.H-3 {
			break
		}
		line(ui.Sp(ln, th.Title()))
	}
	lab := func(s string) ui.Span { return ui.Sp(fmt.Sprintf("%-10s", s), th.Dim()) }
	res := rr.Conclusion
	if rr.Active() {
		res = rr.Status
	}
	g, gs := a.statusGlyph(rr)
	line(lab("Result"), ui.Sp(g+" ", gs), ui.Sp(strings.ReplaceAll(res, "_", " "), th.Status(res)))
	line(lab("Build"), ui.Sp(runLabel(rr.RunNumber, rr.ID), th.Acc()), ui.Sp(fmt.Sprintf("  %s · attempt %d", rr.Event, max(rr.RunAttempt, 1)), th.Dim()))
	line(lab("Branch"), ui.Sp(rr.HeadBranch, th.Base()), ui.Sp("  "+rr.Short(), th.Dim()))
	if rr.Name != "" {
		line(lab("Workflow"), ui.Sp(rr.Name, th.Base()))
	}
	line(lab("By"), ui.Sp(rr.Actor.Login, th.Base()), ui.Sp("  "+ui.Ago(rr.CreatedAt)+" ago", th.Dim()))
	if d := rr.Duration(); d > 0 {
		verb := "took "
		if rr.Active() {
			verb = "running "
		}
		line(lab("Time"), ui.Sp(verb+ui.Dur(d), th.Dim()))
	}
	// comparison with the installed app, from the best artifact's name
	in2 := a.eng.Installed()
	if e := b.curArts(); e != nil && !e.loading {
		var best *pick.Candidate
		for i := range e.cands {
			if e.cands[i].Match {
				best = &e.cands[i]
				break
			}
		}
		if best != nil {
			v := version.Extract(best.Art.Name)
			sp := []ui.Span{lab("Ships"), ui.Sp(best.Art.Name, th.Base())}
			line(sp...)
			if in2.Found && v != "" && in2.VersionName != "" {
				iv := version.Extract(in2.VersionName)
				txt, st := "same as installed", th.Caution()
				switch version.Compare(v, iv) {
				case 1:
					txt, st = "newer than installed "+in2.VersionName, th.Good()
				case -1:
					txt, st = "older than installed "+in2.VersionName, th.Bad()
				}
				line(lab("Phone"), ui.Sp(txt, st))
			}
		} else if len(e.cands) > 0 {
			line(lab("Ships"), ui.Sp("no APK matched your settings", th.Caution()))
		}
	}
	if e := b.curArts(); b.focus == 1 && e != nil && b.artList.Cur < len(e.cands) {
		c := e.cands[b.artList.Cur]
		switch {
		case c.Skip != "":
			line(lab("Skipped"), ui.Sp(c.Skip, th.Caution()))
		case len(c.Tags) > 0:
			line(lab("Matches"), ui.Sp(strings.Join(c.Tags, ", "), th.Dim()), ui.Sp(fmt.Sprintf("  score %d", c.Score), th.Dim()))
		}
	}
	if n := len(b.marks); n > 0 {
		line(lab("Marked"), ui.Sp(fmt.Sprintf("%d (d downloads them)", n), th.Acc()))
	}
	// action chips along the bottom
	if in.H >= 4 {
		cy := in.Y + in.H - 1
		x := in.X + 1
		type act struct{ label string }
		acts := []string{"Install", "Download", "Open"}
		switch {
		case rr.Active():
			acts = append(acts, "Watch")
		case !rr.OK() && rr.Status == "completed":
			acts = append(acts, "Why failed")
		}
		for i, s := range acts {
			t := " " + s + " "
			w := term.StringWidth(t)
			if x+w > in.X+in.W-1 {
				break
			}
			st := th.Chip()
			if i > 0 {
				st = term.Style{FG: th.Text, BG: th.Track}
				if th.Plain || th.Name == "ansi" {
					st = term.Style{Attr: term.Reverse}
				}
			}
			p.Text(x, cy, t, st, 0)
			a.hits.Add(ui.Rect{X: x, Y: cy, W: w, H: 1}, "act", i)
			x += w + 1
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---- keys ----------------------------------------------------------------------

func (b *buildsScreen) hints(a *App) []hint {
	if b.textOn {
		return []hint{{"↵", "done", keyEv(term.KeyEnter)}, {"esc", "clear", keyEv(term.KeyEsc)}}
	}
	hs := []hint{{"↵", "install", keyEv(term.KeyEnter)}, {"d", "download", runeEv('d')}, {"b", "branch", runeEv('b')},
		{"w", "workflow", runeEv('w')}, {"f", "status", runeEv('f')}, {"/", "search", runeEv('/')}, {"I", "newest", runeEv('I')}, {"r", "refresh", runeEv('r')}}
	if b.focus == 1 || b.detail {
		hs = append([]hint{{"esc", "back", keyEv(term.KeyEsc)}}, hs...)
	}
	return hs
}

func (b *buildsScreen) show(a *App) {}

func (b *buildsScreen) focusList() (l *ui.List, h int) {
	if b.focus == 1 {
		return &b.artList, max(b.artsH, 3)
	}
	return &b.list, max(b.runsH, 3)
}

func (b *buildsScreen) key(a *App, ev term.Event) bool {
	if b.textOn {
		switch {
		case ev.Key == term.KeyEnter:
			b.textOn = false
		case ev.Key == term.KeyEsc:
			b.textOn = false
			b.text.SetText("")
			b.refilter(b.curID())
		case ev.Key == term.KeyDown, ev.Key == term.KeyUp:
			b.textOn = false
			return b.key(a, ev)
		default:
			if b.text.Handle(ev) {
				b.refilter(0)
				b.list.Set(0, max(b.runsH, 3))
			}
		}
		return true
	}
	l, h := b.focusList()
	switch {
	case ev.Key == term.KeyDown, ev.Is('j'):
		l.Move(1, h)
	case ev.Key == term.KeyUp, ev.Is('k'):
		l.Move(-1, h)
	case ev.Key == term.KeyPgDn, ev.IsCtrl('f'):
		l.Move(h, h)
	case ev.Key == term.KeyPgUp, ev.IsCtrl('b'):
		l.Move(-h, h)
	case ev.IsCtrl('d'):
		l.Move(h/2, h)
	case ev.IsCtrl('u'):
		l.Move(-h/2, h)
	case ev.Key == term.KeyHome, ev.Is('g'):
		l.Set(0, h)
	case ev.Key == term.KeyEnd, ev.Is('G'):
		l.Set(l.N-1, h)
	case ev.Key == term.KeyRight, ev.Is('l'):
		b.enter(a)
	case ev.Key == term.KeyLeft, ev.Is('h'), ev.Key == term.KeyEsc:
		if b.focus == 1 {
			b.focus = 0
		} else if b.detail {
			b.detail = false
		} else if ev.Key == term.KeyEsc && (b.text.String() != "" || b.filter != (engine.RunFilter{})) {
			b.clearFilters(a)
		} else {
			return ev.Key == term.KeyEsc // swallow stray Esc
		}
	case ev.Key == term.KeyEnter, ev.Is('i'):
		b.install(a)
	case ev.Is('d'):
		b.download(a)
	case ev.Is(' '):
		if r := b.current(); r != nil {
			b.marks[r.Run.ID] = !b.marks[r.Run.ID]
			if !b.marks[r.Run.ID] {
				delete(b.marks, r.Run.ID)
			}
			b.list.Move(1, h)
		}
	case ev.Is('r'):
		b.load(a, true)
		a.refreshInstalled()
		a.notify("info", "refreshing…")
	case ev.Is('/'):
		b.textOn = true
	case ev.Is('x'):
		b.clearFilters(a)
	case ev.Is('b'):
		b.pickBranch(a)
	case ev.Is('w'):
		b.pickWorkflow(a)
	case ev.Is('f'):
		b.cycleStatus(a)
	case ev.Is('o'):
		if r := b.current(); r != nil {
			a.openURL(r.Run.HTMLURL)
		}
	case ev.Is('y'):
		if r := b.current(); r != nil {
			a.copyText(r.Run.HTMLURL)
		}
	case ev.Is('e'):
		b.failLog(a)
	case ev.Is('W'):
		b.watch(a)
	default:
		return false
	}
	return true
}

func (b *buildsScreen) curID() int64 {
	if r := b.current(); r != nil {
		return r.Run.ID
	}
	return 0
}

// enter drills in: runs → artifacts (or the detail page on a narrow screen).
func (b *buildsScreen) enter(a *App) {
	if b.current() == nil {
		return
	}
	if b.layout(ui.Rect{W: a.W, H: a.H}).narrow {
		b.detail = true
		return
	}
	b.focus = 1
}

func (b *buildsScreen) install(a *App) {
	r := b.current()
	if r == nil {
		a.notify("warn", "no build selected")
		return
	}
	var art int64
	if e := b.curArts(); b.focus == 1 && e != nil && b.artList.Cur < len(e.cands) {
		art = e.cands[b.artList.Cur].Art.ID
	}
	if r.Run.Active() {
		a.notify("warn", "still building — W waits for it")
		return
	}
	if !r.Run.OK() {
		a.notify("warn", "that build %s — e shows why", strings.ReplaceAll(r.Run.Conclusion, "_", " "))
		return
	}
	a.planRun(r.Run.ID, art)
}

// download fetches without installing: the marked runs, or the selected one.
func (b *buildsScreen) download(a *App) {
	var ids []int64
	if len(b.marks) > 0 {
		for _, rv := range b.runs {
			if b.marks[rv.Run.ID] {
				ids = append(ids, rv.Run.ID)
			}
		}
	} else if r := b.current(); r != nil {
		ids = []int64{r.Run.ID}
	}
	if len(ids) == 0 {
		a.notify("warn", "no build selected")
		return
	}
	var art int64
	if e := b.curArts(); b.focus == 1 && len(ids) == 1 && e != nil && b.artList.Cur < len(e.cands) {
		art = e.cands[b.artList.Cur].Art.ID
	}
	b.marks = map[int64]bool{}
	for _, id := range ids {
		id := id
		a.async(func(ctx context.Context) func(*App) {
			cands, err := a.eng.RunArtifacts(ctx, id)
			run, rerr := a.eng.GH.GetRun(ctx, a.eng.Repo(), id)
			return func(a *App) {
				if err != nil || rerr != nil {
					a.notify("err", "can't read build %d: %s", id, errOneLine(firstErr(err, rerr)))
					return
				}
				for _, c := range cands {
					if (art != 0 && c.Art.ID == art) || (art == 0 && c.Match) {
						a.startJob(engine.FromCandidate(run, c), engine.JobOpts{NoInstall: true})
						return
					}
				}
				a.notify("warn", "%s has no matching artifact", runLabel(run.RunNumber, run.ID))
			}
		})
	}
}

func firstErr(es ...error) error {
	for _, e := range es {
		if e != nil {
			return e
		}
	}
	return nil
}

func (b *buildsScreen) clearFilters(a *App) {
	changed := b.filter != engine.RunFilter{}
	b.filter = engine.RunFilter{Branch: "*", Workflow: "*"}
	b.text.SetText("")
	b.refilter(b.curID())
	if changed || a.eng.Config().Str("branch") != "" || a.eng.Config().Str("workflow") != "" {
		b.load(a, false)
	}
	a.notify("info", "filters cleared")
}

func (b *buildsScreen) cycleStatus(a *App) {
	order := []string{"", "success", "failure", "in_progress"}
	for i, s := range order {
		if s == b.filter.Status {
			b.filter.Status = order[(i+1)%len(order)]
			break
		}
	}
	label := b.filter.Status
	if label == "" {
		label = "all builds"
	}
	a.notify("info", "showing: %s", strings.ReplaceAll(label, "_", " "))
	b.load(a, false)
}

func (b *buildsScreen) pickBranch(a *App) {
	repo := a.eng.Repo()
	if repo == "" {
		return
	}
	cur := b.effFilter(a).Branch
	a.openPickerAsync("Branch", cur, "Use", func(ctx context.Context) ([]pickItem, error) {
		bs, err := a.eng.GH.Branches(ctx, repo)
		items := []pickItem{{Label: "all branches", Value: "*", Mute: true}}
		def := a.eng.RepoInfo().DefaultBranch
		for _, br := range bs {
			sub := ""
			if br.Name == def {
				sub = "default"
			} else if br.Protected {
				sub = "protected"
			}
			items = append(items, pickItem{Label: br.Name, Sub: sub, Value: br.Name})
		}
		return items, err
	}, func(it pickItem) {
		b.filter.Branch = it.Value
		b.load(a, false)
		b.offerRemember(a, "branch", it.Value)
	})
}

func (b *buildsScreen) pickWorkflow(a *App) {
	repo := a.eng.Repo()
	if repo == "" {
		return
	}
	cur := b.effFilter(a).Workflow
	a.openPickerAsync("Workflow", cur, "Use", func(ctx context.Context) ([]pickItem, error) {
		ws, err := a.eng.GH.Workflows(ctx, repo)
		items := []pickItem{{Label: "all workflows", Value: "*", Mute: true}}
		for _, w := range ws {
			items = append(items, pickItem{Label: w.Name, Sub: w.File(), Value: w.File()})
		}
		return items, err
	}, func(it pickItem) {
		b.filter.Workflow = it.Value
		b.load(a, false)
		b.offerRemember(a, "workflow", it.Value)
	})
}

// offerRemember follows the "remember" setting after a hand-picked filter.
func (b *buildsScreen) offerRemember(a *App, key, val string) {
	if val == "*" {
		val = ""
	}
	cfg := a.eng.Config()
	if cfg.Str(key) == val {
		return
	}
	mode := cfg.Str("remember")
	save := func() {
		if err := a.saveSetting(key, val, false); err != nil {
			a.notify("err", "%s", err.Error())
		}
	}
	switch mode {
	case "yes":
		save()
	case "auto":
		what := val
		if what == "" {
			what = "all " + key + "s"
		}
		a.ask(engineQuestion{Title: "Remember this?", Body: fmt.Sprintf("Always use %s “%s” for %s.", key, what, a.eng.Repo()),
			Options: []string{"Yes, save it for this repo", "Just this time"}, Default: 1}, func(i int) {
			if i == 0 {
				save()
			}
		})
	}
}

func (b *buildsScreen) failLog(a *App) {
	r := b.current()
	if r == nil {
		return
	}
	if r.Run.OK() {
		a.notify("info", "that build succeeded")
		return
	}
	if r.Run.Active() {
		a.notify("info", "still building")
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.modal = &busyModal{title: "Why it failed", msg: "Fetching the log of the failed steps…", cancel: cancel}
	run := r.Run
	go func() {
		txt, err := a.eng.GH.FailureLog(ctx, a.eng.Repo(), run.ID, 400)
		jobs, _ := a.eng.GH.Jobs(ctx, a.eng.Repo(), run.ID)
		if ctx.Err() != nil {
			return
		}
		a.post(funcMsg(func(a *App) {
			a.closeBusy()
			a.showFailLog(run, txt, jobs, err)
		}))
	}()
}

func (a *App) showFailLog(run ghub.Run, txt string, jobs []ghub.Job, err error) {
	th := a.th
	rows := []trow{}
	for _, j := range jobs {
		if j.Conclusion == "failure" || j.Conclusion == "cancelled" || j.Conclusion == "timed_out" {
			rows = append(rows, textRow(a.gl.Cross+" "+j.Name, th.Bad()))
			for _, s := range j.Steps {
				if s.Conclusion == "failure" {
					rows = append(rows, textRow("    failed step: "+s.Name, th.Caution()))
				}
			}
		}
	}
	if len(rows) > 0 {
		rows = append(rows, blankRow())
	}
	if err != nil || strings.TrimSpace(txt) == "" {
		msg := "GitHub has no log for this run (it may have been deleted, or the failure happened before any step ran)."
		if err != nil {
			msg = err.Error()
		}
		rows = append(rows, textRow(msg, th.Dim()))
	}
	for _, ln := range strings.Split(txt, "\n") {
		// gh prefixes "job\tstep\ttimestamp text"; keep the text and the step.
		parts := strings.SplitN(ln, "\t", 3)
		body := ln
		if len(parts) == 3 {
			body = parts[2]
			if i := strings.IndexByte(body, ' '); i > 20 && strings.HasSuffix(body[:i], "Z") {
				body = body[i+1:]
			}
		}
		st := th.Base()
		low := strings.ToLower(body)
		switch {
		case strings.Contains(low, "error") || strings.Contains(low, "failed") || strings.HasPrefix(body, "e: "):
			st = th.Bad()
		case strings.Contains(low, "warning") || strings.HasPrefix(body, "w: "):
			st = th.Caution()
		}
		rows = append(rows, trow{spans: []ui.Span{ui.Sp(strings.TrimRight(body, "\r"), st)}})
	}
	m := &textModal{title: "Why " + runLabel(run.RunNumber, run.ID) + " failed", rows: rows, scroll: 1 << 20, footer: "↑↓ scroll · y copy · Esc close"}
	m.onKey = func(a *App, ev term.Event) (bool, bool) {
		if ev.Is('y') {
			a.copyText(txt)
			return true, false
		}
		return false, false
	}
	a.modal = m
}

// watch waits for the selected (or newest) running build, then offers to install it.
func (b *buildsScreen) watch(a *App) {
	if b.watching != 0 {
		if b.watchEnd != nil {
			b.watchEnd()
		}
		b.watching = 0
		a.notify("info", "stopped watching")
		return
	}
	var target *engine.RunView
	if r := b.current(); r != nil && r.Run.Active() {
		target = r
	} else {
		for i := range b.runs {
			if b.runs[i].Run.Active() {
				target = &b.runs[i]
				break
			}
		}
	}
	if target == nil {
		a.notify("info", "nothing is building right now")
		return
	}
	id := target.Run.ID
	label := runLabel(target.Run.RunNumber, id)
	b.watching = id
	ctx, cancel := context.WithCancel(a.ctx)
	b.watchEnd = cancel
	cfg := a.eng.Config()
	timeout := time.Duration(cfg.Int("watch-timeout")) * time.Second
	a.notify("info", "watching %s…", label)
	go func() {
		run, err := a.eng.WatchRun(ctx, id, 6*time.Second, timeout, func(r ghub.Run) { a.post(funcMsg(func(a *App) { a.builds.load(a, false) })) })
		a.post(funcMsg(func(a *App) {
			if a.builds.watching != id {
				return
			}
			a.builds.watching = 0
			switch {
			case err != nil && ctx.Err() == nil:
				a.notify("err", "%s", errOneLine(err))
			case ctx.Err() != nil:
			case run.OK():
				a.notify("ok", "%s finished", label)
				a.builds.load(a, true)
				if a.modal == nil {
					a.planRun(id, 0)
				}
			default:
				a.notify("err", "%s %s — e shows why", label, run.Conclusion)
				a.builds.load(a, false)
			}
		}))
	}()
}

// ---- mouse ---------------------------------------------------------------------

func (b *buildsScreen) click(a *App, h ui.Hit, ev term.Event) bool {
	switch h.ID {
	case "runs.row":
		b.focus = 0
		if h.N == b.list.Cur {
			if b.layout(ui.Rect{W: a.W, H: a.H}).narrow && !b.detail {
				b.detail = true
			} else {
				b.install(a)
			}
		} else {
			b.list.Set(h.N, max(b.runsH, 3))
			if b.layout(ui.Rect{W: a.W, H: a.H}).narrow {
				b.syncSelection(a)
				b.detail = true
			}
		}
	case "arts.row":
		b.focus = 1
		if h.N == b.artList.Cur {
			b.install(a)
		} else {
			b.artList.Set(h.N, max(b.artsH, 3))
		}
	case "runs.pane":
		b.focus = 0
	case "arts.pane":
		b.focus = 1
	case "runs.filter":
		b.textOn = true
	case "act":
		switch h.N {
		case 0:
			b.install(a)
		case 1:
			b.download(a)
		case 2:
			if r := b.current(); r != nil {
				a.openURL(r.Run.HTMLURL)
			}
		case 3:
			if r := b.current(); r != nil && r.Run.Active() {
				b.watch(a)
			} else {
				b.failLog(a)
			}
		}
	default:
		return false
	}
	return true
}

func (b *buildsScreen) wheel(a *App, h ui.Hit, dir int) bool {
	switch {
	case strings.HasPrefix(h.ID, "runs"):
		b.list.Scroll(dir, max(b.runsH, 3))
	case strings.HasPrefix(h.ID, "arts"):
		b.artList.Scroll(dir, max(b.artsH, 3))
	default:
		return false
	}
	return true
}

var _ = config.GlobalFile
