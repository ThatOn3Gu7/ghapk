// Package tui is ghapk's interactive interface: the event loop, the five
// screens (Builds, Downloads, Device, Settings, Log) and the dialogs on top of
// them. It talks to the engine package for everything that touches GitHub or
// the phone, and to package ui for drawing.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ghapk/internal/config"
	"ghapk/internal/engine"
	"ghapk/internal/term"
	"ghapk/internal/ui"
)

// Screen indexes.
const (
	TabBuilds = iota
	TabDownloads
	TabDevice
	TabSettings
	TabLog
	numTabs
)

var tabNames = [numTabs]string{"Builds", "Downloads", "Device", "Settings", "Log"}
var tabShort = [numTabs]string{"Builds", "Down", "Dev", "Set", "Log"}

// Options configures a TUI session.
type Options struct {
	Repo      string // repository to open ("" = work it out)
	PickRepo  bool   // always show the repo picker first
	Tab       int
	LastAPK   *string // receives the path of the last saved APK, for the exit summary
	NoMouse   bool
	StartPlan bool // open the "install newest" plan straight away
}

type hint struct {
	key, label string
	ev         term.Event
}

type screen interface {
	draw(a *App, r ui.Rect)
	key(a *App, ev term.Event) bool
	hints(a *App) []hint
	click(a *App, h ui.Hit, ev term.Event) bool
	wheel(a *App, h ui.Hit, dir int) bool
	show(a *App)
}

type modal interface {
	draw(a *App)
	key(a *App, ev term.Event) (done bool)
	click(a *App, h ui.Hit, ok bool, ev term.Event) (done bool)
	wheel(a *App, dir int)
}

type toast struct {
	text  string
	kind  string // ok, err, warn, info
	until time.Time
}

// App is the whole TUI state. All of it is touched only by the UI goroutine;
// background work reports back through a.post.
type App struct {
	eng  *engine.Engine
	t    *term.Terminal
	buf  *term.Buffer
	th   *ui.Theme
	gl   *ui.Glyphs
	P    ui.Painter
	hits ui.Hits
	opts Options

	W, H  int
	tick  int
	dirty bool
	quit  bool

	ctx    context.Context
	cancel context.CancelFunc
	msgs   chan any

	tab     int
	screens [numTabs]screen
	modal   modal
	toast   toast

	pre     engine.Preflight
	preDone bool
	state   config.State
	repoErr error
	repoSet bool

	asks        []*askReq
	planLoading bool
	jobs        []*jobEntry
	lastAPK     string

	builds    *buildsScreen
	downloads *downloadsScreen
	device    *deviceScreen
	settings  *settingsScreen
	logs      *logScreen
}

// New builds an App with a w×h virtual screen. Run attaches it to a terminal;
// tests and screenshots drive it directly through Event and Frame.
func New(eng *engine.Engine, w, h int, opts Options) *App {
	a := &App{eng: eng, opts: opts, W: w, H: h, msgs: make(chan any, 256), dirty: true}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.buf = term.NewBuffer(w, h)
	a.applyAppearance()
	a.state = config.LoadState()
	a.builds = newBuildsScreen()
	a.downloads = &downloadsScreen{}
	a.device = &deviceScreen{}
	a.settings = newSettingsScreen()
	a.logs = &logScreen{follow: true}
	a.screens = [numTabs]screen{a.builds, a.downloads, a.device, a.settings, a.logs}
	a.tab = opts.Tab
	eng.Log.OnAdd(func() { a.post(logMsg{}) })
	return a
}

func (a *App) applyAppearance() {
	cfg := a.eng.Config()
	a.th = ui.NewTheme(cfg.Str("theme"))
	a.gl = ui.NewGlyphs(cfg.Str("glyphs"))
	a.P = ui.Painter{B: a.buf, T: a.th, G: a.gl}
	a.dirty = true
}

// post delivers a message to the UI goroutine from anywhere.
func (a *App) post(m any) {
	select {
	case a.msgs <- m:
	case <-a.ctx.Done():
	}
}

// ---- running ----------------------------------------------------------------

// Run takes over the terminal until the user quits.
func (a *App) Run(t *term.Terminal) error {
	a.t = t
	mouse := a.eng.Config().Bool("mouse") && !a.opts.NoMouse
	if err := t.Start(term.Options{Mouse: mouse}); err != nil {
		return err
	}
	defer t.Close()
	defer func() {
		if r := recover(); r != nil {
			t.Close()
			panic(r)
		}
	}()
	t.SetTitle("ghapk")
	a.W, a.H = t.Size()
	a.buf.Resize(a.W, a.H)
	a.startup()
	a.loop()
	a.cancel()
	for _, j := range a.jobs {
		if j.job.Running() {
			j.job.Cancel()
		}
	}
	if a.opts.LastAPK != nil {
		*a.opts.LastAPK = a.lastAPK
	}
	return nil
}

func (a *App) loop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	a.render()
	for !a.quit {
		select {
		case ev := <-a.t.Events:
			a.Event(ev)
		case m := <-a.msgs:
			a.handleMsg(m)
		case <-ticker.C:
			a.onTick()
		}
	drain:
		for i := 0; i < 64; i++ {
			select {
			case ev := <-a.t.Events:
				a.Event(ev)
			case m := <-a.msgs:
				a.handleMsg(m)
			default:
				break drain
			}
		}
		a.afterEvent()
		if a.dirty {
			a.render()
		}
	}
}

func (a *App) onTick() {
	a.tick++
	if a.animating() {
		a.dirty = true
	}
	if !a.toast.until.IsZero() && time.Now().After(a.toast.until) {
		a.toast = toast{}
		a.dirty = true
	}
	a.builds.autoRefresh(a)
}

// animating says whether anything on screen moves without input.
func (a *App) animating() bool {
	if a.builds.loading || a.pickerLoading() {
		return true
	}
	for _, j := range a.jobs {
		if j.job.Running() {
			return true
		}
	}
	return a.device.loading || a.builds.watching != 0 || a.planLoading
}

// ---- events -----------------------------------------------------------------

// Event handles one input event.
func (a *App) Event(ev term.Event) {
	a.dirty = true
	switch ev.Kind {
	case term.EvResize:
		a.W, a.H = ev.W, ev.H
		a.buf.Resize(a.W, a.H)
		if a.t != nil {
			a.t.Invalidate()
		}
		return
	case term.EvMouse:
		a.mouse(ev)
		return
	}
	if ev.IsCtrl('c') {
		a.requestQuit()
		return
	}
	if ev.IsCtrl('l') {
		if a.t != nil {
			a.t.Invalidate()
		}
		return
	}
	if a.modal != nil {
		if a.modal.key(a, ev) {
			a.modal = nil
		}
		return
	}
	if ev.Kind != term.EvKey {
		return
	}
	if a.screens[a.tab].key(a, ev) {
		return
	}
	a.globalKey(ev)
}

func (a *App) globalKey(ev term.Event) {
	switch {
	case ev.Is('q'):
		a.requestQuit()
	case ev.Is('?') || ev.Key == term.KeyF1:
		a.openHelp()
	case ev.Is('1'), ev.Is('2'), ev.Is('3'), ev.Is('4'), ev.Is('5'):
		a.setTab(int(ev.Rune - '1'))
	case ev.Key == term.KeyTab:
		a.setTab((a.tab + 1) % numTabs)
	case ev.Key == term.KeyBacktab:
		a.setTab((a.tab + numTabs - 1) % numTabs)
	case ev.Is('R'):
		a.openRepoPicker()
	case ev.Is('I'):
		a.planNewest()
	case ev.Is('A'):
		if len(a.asks) > 0 {
			a.showNextAsk()
		}
	}
}

func (a *App) setTab(n int) {
	if n < 0 || n >= numTabs || n == a.tab {
		return
	}
	a.tab = n
	a.screens[n].show(a)
}

func (a *App) requestQuit() {
	n := 0
	for _, j := range a.jobs {
		if j.job.Running() {
			n++
		}
	}
	if n == 0 {
		a.quit = true
		return
	}
	a.ask(engine.Question{Title: "Quit ghapk?", Body: fmt.Sprintf("%s still running. Quitting cancels %s.", ui.Plural(n, "download"), map[bool]string{true: "it", false: "them"}[n == 1]),
		Options: []string{"Keep working", "Quit and cancel"}, Default: 0, Danger: true}, func(i int) {
		if i == 1 {
			a.quit = true
		}
	})
}

func (a *App) mouse(ev term.Event) {
	if !a.eng.Config().Bool("mouse") {
		return
	}
	h, ok := a.hits.At(ev.X, ev.Y)
	switch {
	case ev.Action == term.MouseWheel:
		dir := 3
		if ev.Button == term.BtnWheelUp {
			dir = -3
		}
		if a.modal != nil {
			a.modal.wheel(a, dir)
		} else if ok {
			a.screens[a.tab].wheel(a, h, dir)
		}
	case ev.Action == term.MousePress && ev.Button == term.BtnLeft:
		if a.modal != nil {
			if a.modal.click(a, h, ok, ev) {
				a.modal = nil
			}
			return
		}
		if !ok {
			return
		}
		if a.globalClick(h) {
			return
		}
		a.screens[a.tab].click(a, h, ev)
	}
}

func (a *App) globalClick(h ui.Hit) bool {
	switch h.ID {
	case "tab":
		a.setTab(h.N)
		return true
	case "hint":
		hs := a.currentHints()
		if h.N >= 0 && h.N < len(hs) {
			a.Event(hs[h.N].ev)
		}
		return true
	case "activity":
		a.setTab(TabDownloads)
		return true
	case "repo":
		a.openRepoPicker()
		return true
	}
	return false
}

// afterEvent runs housekeeping that depends on state changed by the last batch.
func (a *App) afterEvent() {
	a.builds.syncSelection(a)
	if a.modal == nil && len(a.asks) > 0 {
		a.showNextAsk()
	}
}

// ---- toasts -----------------------------------------------------------------

func (a *App) notify(kind, format string, args ...any) {
	a.toast = toast{text: fmt.Sprintf(format, args...), kind: kind, until: time.Now().Add(4 * time.Second)}
	if kind == "err" {
		a.toast.until = time.Now().Add(7 * time.Second)
	}
	a.dirty = true
}

// ---- drawing ----------------------------------------------------------------

const minW, minH = 32, 9

// Frame renders the current state into the virtual screen and returns it.
func (a *App) Frame() *term.Buffer {
	a.render0()
	return a.buf
}

func (a *App) render() {
	a.render0()
	if a.t != nil {
		_ = a.t.Flush(a.buf)
	}
	a.dirty = false
}

func (a *App) render0() {
	if a.buf.W != a.W || a.buf.H != a.H {
		a.buf.Resize(a.W, a.H)
	}
	a.buf.Clear(term.Style{})
	a.hits.Reset()
	a.P.Tick = a.tick
	if a.W < minW || a.H < minH {
		msg := fmt.Sprintf("ghapk needs at least %d×%d (now %d×%d)", minW, minH, a.W, a.H)
		a.P.Text(0, a.H/2, term.Truncate(msg, a.W, "…"), a.th.Caution(), a.W)
		return
	}
	a.drawHeader()
	body := ui.Rect{X: 0, Y: 2, W: a.W, H: a.H - 3}
	a.screens[a.tab].draw(a, body)
	a.drawFooter()
	a.drawToast()
	if a.modal != nil {
		a.buf.Restyle(0, 0, a.W, a.H, func(s term.Style) term.Style { s.Attr |= term.Dim; return s })
		a.hits.Reset()
		a.modal.draw(a)
	}
}

func (a *App) activeJobs() (n int, frac float64, asking int) {
	var sum float64
	for _, j := range a.jobs {
		v := j.job.Snapshot()
		switch v.State {
		case engine.JobRunning:
			n++
			if f := v.Prog.Fraction(); f >= 0 && v.Prog.Active {
				sum += f
			}
		case engine.JobAsking:
			n++
			asking++
		}
	}
	if n > 0 {
		frac = sum / float64(n)
	}
	return
}

func (a *App) drawHeader() {
	p, th := &a.P, a.th
	// row 0: brand, repo, branch/workflow, activity
	x := 1
	x += p.Text(x, 0, " ghapk ", th.Chip(), 0) + 1
	repo := a.eng.Repo()
	if repo == "" {
		repo = "no repository"
	}
	right := a.activitySpans()
	rw := ui.SpansWidth(right)
	room := a.W - x - rw - 3
	if room < 8 {
		right, rw = nil, 0
		room = a.W - x - 1
	}
	lead := ""
	if a.eng.RepoInfo().Private {
		lead = a.gl.Lock + " "
	}
	sub := a.filterSummary()
	repoSpans := []ui.Span{ui.Sp(lead+repo, th.Title())}
	if sub != "" {
		repoSpans = append(repoSpans, ui.Sp("  "+sub, th.Dim()))
	}
	used := p.Spans(x, 0, repoSpans, room)
	a.hits.Add(ui.Rect{X: x, Y: 0, W: used, H: 1}, "repo", 0)
	if rw > 0 {
		p.Right(a.W-1-rw, 0, rw, right)
		a.hits.Add(ui.Rect{X: a.W - 1 - rw, Y: 0, W: rw, H: 1}, "activity", 0)
	}
	// row 1: tabs
	a.drawTabs()
}

func (a *App) filterSummary() string {
	f := a.builds.filter
	cfg := a.eng.Config()
	br := f.Branch
	if br == "" {
		br = cfg.Str("branch")
	}
	var parts []string
	if br != "" {
		parts = append(parts, a.gl.Branch+" "+br)
	}
	wf := f.Workflow
	if wf == "" {
		wf = cfg.Str("workflow")
	}
	if wf != "" {
		parts = append(parts, wf)
	}
	if f.Status != "" {
		parts = append(parts, "only "+strings.ReplaceAll(f.Status, "_", " "))
	}
	return strings.Join(parts, "  ")
}

func (a *App) activitySpans() []ui.Span {
	th := a.th
	if n, frac, asking := a.activeJobs(); n > 0 {
		s := []ui.Span{ui.Sp(a.P.Spinner()+" ", th.Caution())}
		if asking > 0 {
			return append(s, ui.Sp("waiting for you", th.Caution().With(term.Bold)))
		}
		if frac > 0 {
			return append(s, ui.Sp(fmt.Sprintf("%s · %d%%", ui.Plural(n, "job"), int(frac*100)), th.Caution()))
		}
		return append(s, ui.Sp(ui.Plural(n, "job")+" running", th.Caution()))
	}
	if a.builds.watching != 0 {
		return []ui.Span{ui.Sp(a.P.Spinner()+" ", th.Caution()), ui.Sp("watching build…", th.Caution())}
	}
	in := a.eng.Installed()
	if in.Found {
		name := in.VersionName
		if name == "" {
			name = fmt.Sprint(in.VersionCode)
		}
		return []ui.Span{ui.Sp(a.gl.Phone+" ", th.Dim()), ui.Sp(shortPkg(in.Pkg)+" ", th.Dim()), ui.Sp("v"+strings.TrimPrefix(name, "v"), th.Good())}
	}
	if u := a.eng.User(); u != "" {
		return []ui.Span{ui.Sp("@"+u, th.Dim())}
	}
	return nil
}

func shortPkg(p string) string {
	if i := strings.LastIndexByte(p, '.'); i >= 0 && i < len(p)-1 {
		return p[i+1:]
	}
	return p
}

func (a *App) drawTabs() {
	p, th := &a.P, a.th
	names := tabNames
	need := 1
	for i := range names {
		need += len(names[i]) + 4
	}
	if need > a.W {
		names = tabShort
	}
	x := 1
	runCount, asking := 0, 0
	for _, j := range a.jobs {
		switch j.job.Snapshot().State {
		case engine.JobRunning:
			runCount++
		case engine.JobAsking:
			asking++
		}
	}
	for i := 0; i < numTabs; i++ {
		label := fmt.Sprintf(" %d %s", i+1, names[i])
		badge := ""
		if i == TabDownloads {
			switch {
			case asking > 0:
				badge = " ?"
			case runCount > 0:
				badge = " " + a.gl.Run
			}
		}
		w := term.StringWidth(label+badge) + 1
		if x+w > a.W {
			break
		}
		if i == a.tab {
			p.Text(x, 1, label+badge+" ", th.Chip(), 0)
		} else {
			p.Text(x, 1, label, th.Dim(), 0)
			if badge != "" {
				p.Text(x+term.StringWidth(label), 1, badge, th.Caution(), 0)
			}
		}
		a.hits.Add(ui.Rect{X: x, Y: 1, W: w, H: 1}, "tab", i)
		x += w + 1
	}
}

func (a *App) currentHints() []hint {
	hs := append([]hint{}, a.screens[a.tab].hints(a)...)
	hs = append(hs, hint{"?", "help", runeEv('?')}, hint{"q", "quit", runeEv('q')})
	return hs
}

func (a *App) drawFooter() {
	p, th := &a.P, a.th
	y := a.H - 1
	hs := a.currentHints()
	// the last two hints (help, quit) are always kept, right-aligned
	tail := hs[len(hs)-2:]
	hs = hs[:len(hs)-2]
	tailW := 0
	for _, h := range tail {
		tailW += term.StringWidth(h.key) + 1 + term.StringWidth(h.label) + 2
	}
	tailW -= 2
	x := 1
	limit := a.W - 1 - tailW - 2
	for i, h := range hs {
		w := term.StringWidth(h.key) + 1 + term.StringWidth(h.label)
		if x+w > limit {
			continue
		}
		p.Text(x, y, h.key, th.Key(), 0)
		p.Text(x+term.StringWidth(h.key)+1, y, h.label, th.Dim(), 0)
		a.hits.Add(ui.Rect{X: x, Y: y, W: w, H: 1}, "hint", i)
		x += w + 2
	}
	x = a.W - 1 - tailW
	for i, h := range tail {
		w := term.StringWidth(h.key) + 1 + term.StringWidth(h.label)
		p.Text(x, y, h.key, th.Key(), 0)
		p.Text(x+term.StringWidth(h.key)+1, y, h.label, th.Dim(), 0)
		a.hits.Add(ui.Rect{X: x, Y: y, W: w, H: 1}, "hint", len(hs)+i)
		x += w + 2
	}
}

func (a *App) drawToast() {
	if a.toast.text == "" {
		return
	}
	th := a.th
	st, g := th.Base(), ""
	switch a.toast.kind {
	case "ok":
		st, g = th.Good(), a.gl.Check
	case "err":
		st, g = th.Bad(), a.gl.Cross
	case "warn":
		st, g = th.Caution(), a.gl.Warn
	default:
		st, g = th.Acc(), a.gl.Dot
	}
	txt := " " + g + " " + a.toast.text + " "
	w := term.StringWidth(txt)
	if w > a.W-2 {
		txt = term.Truncate(txt, a.W-2, a.gl.Ellipsis)
		w = term.StringWidth(txt)
	}
	st.BG = th.Track
	if th.Plain || th.Name == "ansi" {
		st.Attr |= term.Reverse
	}
	y := a.H - 2
	x := a.W - w - 1
	a.P.Text(x, y, txt, st, 0)
}

func runeEv(r rune) term.Event {
	return term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: r}
}

func keyEv(k term.Key) term.Event { return term.Event{Kind: term.EvKey, Key: k} }
