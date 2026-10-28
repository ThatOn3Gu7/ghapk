package tui

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"ghapk/internal/config"
	"ghapk/internal/device"
	"ghapk/internal/engine"
	"ghapk/internal/exit"
	"ghapk/internal/ghub"
	"ghapk/internal/ui"
)

type funcMsg func(*App)
type logMsg struct{}
type jobMsg struct{ e *jobEntry }
type askMsg struct{ req *askReq }

// async runs work on a goroutine and applies its result on the UI goroutine.
func (a *App) async(work func(ctx context.Context) func(*App)) {
	go func() {
		if f := work(a.ctx); f != nil && a.ctx.Err() == nil {
			a.post(funcMsg(f))
		}
	}()
}

func (a *App) handleMsg(m any) {
	a.dirty = true
	switch m := m.(type) {
	case funcMsg:
		m(a)
	case logMsg:
	case jobMsg:
		a.onJob(m.e)
	case askMsg:
		a.asks = append(a.asks, m.req)
	}
}

// ---- start-up -------------------------------------------------------------------

func (a *App) startup() {
	a.async(func(ctx context.Context) func(*App) {
		p := a.eng.Preflight(ctx)
		return func(a *App) {
			a.pre, a.preDone = p, true
			if p.Err != nil {
				a.showSetupHelp(p)
				return
			}
			a.chooseRepo()
		}
	})
	a.screens[a.tab].show(a)
}

func (a *App) showSetupHelp(p engine.Preflight) {
	th := a.th
	rows := []trow{}
	if !p.GHFound {
		rows = append(rows, textRow("The GitHub CLI (gh) is not installed.", th.Bad()), blankRow(),
			textRow("ghapk reads your builds through gh, so it needs it. In Termux:", th.Base()), blankRow(),
			textRow("    pkg install gh", th.Acc()), blankRow(),
			textRow("Then start ghapk again.", th.Base()))
	} else {
		rows = append(rows, textRow("gh is installed, but you are not signed in.", th.Bad()), blankRow(),
			textRow("Sign in once, then press r in the Builds tab (or restart ghapk):", th.Base()), blankRow(),
			textRow("    gh auth login", th.Acc()), blankRow(),
			textRow("Choose GitHub.com → HTTPS → paste a token or log in with the browser. The token needs the “repo” and “workflow” scopes for private repositories.", th.Dim()))
	}
	a.modal = &textModal{title: "Setup needed", rows: rows, danger: true, footer: "Esc close · the other tabs still work"}
}

func (a *App) chooseRepo() {
	if a.opts.PickRepo {
		a.openRepoPicker()
		return
	}
	repo := a.opts.Repo
	if repo == "" {
		repo = a.eng.Config().Str("repo")
	}
	if repo != "" {
		a.setRepo(repo)
		return
	}
	a.async(func(ctx context.Context) func(*App) {
		r, ok := a.eng.GH.RepoFromDir(ctx)
		return func(a *App) {
			switch {
			case ok && r != "":
				a.setRepo(r)
			case a.state.LastRepo != "":
				a.setRepo(a.state.LastRepo)
			default:
				a.openRepoPicker()
			}
		}
	})
}

// setRepo switches repositories and reloads everything that depends on it.
func (a *App) setRepo(input string) {
	a.builds.loading = true
	a.repoErr = nil
	a.async(func(ctx context.Context) func(*App) {
		err := a.eng.SetRepo(ctx, input)
		return func(a *App) {
			if err != nil {
				a.builds.loading = false
				a.repoErr = err
				a.showError("Can't open "+input, err)
				return
			}
			a.repoSet = true
			a.state.Touch(a.eng.Repo())
			a.applyAppearance()
			a.builds.reset()
			a.builds.load(a, true)
			a.refreshInstalled()
			if a.opts.StartPlan {
				a.opts.StartPlan = false
				a.planNewest()
			}
		}
	})
}

func (a *App) refreshInstalled() {
	a.async(func(ctx context.Context) func(*App) {
		a.eng.RefreshInstalled(ctx)
		return func(a *App) {}
	})
}

func (a *App) openRepoPicker() {
	recent := a.state.Recent
	cur := a.eng.Repo()
	a.openPickerAsync("Repository", cur, "Open", func(ctx context.Context) ([]pickItem, error) {
		var items []pickItem
		seen := map[string]bool{}
		for _, r := range recent {
			items = append(items, pickItem{Label: r, Sub: "recent", Value: r})
			seen[strings.ToLower(r)] = true
		}
		repos, err := a.eng.GH.MyRepos(ctx, 60)
		for _, r := range repos {
			if seen[strings.ToLower(r.FullName)] {
				continue
			}
			sub := ""
			if r.Private {
				sub = "private"
			}
			items = append(items, pickItem{Label: r.FullName, Sub: sub, Value: r.FullName})
		}
		if len(items) > 0 {
			err = nil
		}
		return items, err
	}, func(it pickItem) { a.setRepo(it.Value) })
}

// ---- jobs ------------------------------------------------------------------------

type jobEntry struct {
	job      *engine.Job
	at       time.Time
	finished bool
}

type askReq struct {
	q     engine.Question
	e     *jobEntry
	reply chan int
}

type jobPrompter struct {
	a *App
	e *jobEntry
}

func (p *jobPrompter) Ask(ctx context.Context, q engine.Question) int {
	req := &askReq{q: q, e: p.e, reply: make(chan int, 1)}
	p.a.post(askMsg{req})
	select {
	case i := <-req.reply:
		return i
	case <-ctx.Done():
		p.a.post(funcMsg(func(a *App) { a.dropAsk(req) }))
		return -1
	}
}

func (a *App) dropAsk(req *askReq) {
	for i, r := range a.asks {
		if r == req {
			a.asks = append(a.asks[:i], a.asks[i+1:]...)
			return
		}
	}
}

func (a *App) startJob(src engine.Source, opts engine.JobOpts) {
	j := a.eng.NewJob(src, opts)
	e := &jobEntry{job: j, at: time.Now()}
	a.jobs = append([]*jobEntry{e}, a.jobs...)
	a.downloads.sel = 0
	a.downloads.list.Cur = 0
	j.Start(a.ctx, &jobPrompter{a: a, e: e}, func() { a.post(jobMsg{e}) })
	a.setTab(TabDownloads)
	a.notify("info", "started %s", src.Label())
}

func (a *App) showNextAsk() {
	if len(a.asks) == 0 || a.modal != nil {
		return
	}
	req := a.asks[0]
	a.asks = a.asks[1:]
	v := req.e.job.Snapshot()
	title := req.q.Title
	body := req.q.Body
	if lbl := v.Src.Label(); lbl != "" {
		body = lbl + "\n\n" + body
	}
	a.modal = &questionModal{title: title, body: body, options: req.q.Options, sel: req.q.Default, danger: req.q.Danger,
		answer: func(i int) { req.reply <- i }}
}

func (a *App) onJob(e *jobEntry) {
	v := e.job.Snapshot()
	if v.State == engine.JobRunning || v.State == engine.JobAsking || e.finished {
		return
	}
	e.finished = true
	switch v.State {
	case engine.JobDone:
		if v.Res.APK != "" {
			a.lastAPK = v.Res.APK
		}
		switch {
		case v.Res.NotLanded:
			a.notify("warn", "handed off, but the phone is not on the new build yet")
		case v.Res.InstalledNow > 0:
			a.notify("ok", "installed %s", v.Src.Name)
		case v.Res.HandedOff:
			a.notify("ok", "handed to the installer")
		default:
			a.notify("ok", "saved %s", shortPath(v.Res.APK))
		}
		a.refreshInstalled()
	case engine.JobCancelled:
		a.notify("warn", "cancelled")
	default:
		a.notify("err", "failed: %s", errOneLine(v.Err))
	}
}

func shortPath(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func errOneLine(err error) string {
	if err == nil {
		return "unknown error"
	}
	s := strings.TrimSpace(err.Error())
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// ---- errors -----------------------------------------------------------------------

func (a *App) showError(title string, err error) {
	th := a.th
	rows := []trow{}
	var na *engine.NoArtifactError
	var ge *ghub.Error
	switch {
	case errors.As(err, &na):
		rows = append(rows, textRow(na.Msg, th.Bad()))
		if len(na.Seen) > 0 {
			rows = append(rows, blankRow(), textRow("Artifacts that exist but did not match your settings:", th.Base()))
			for i, s := range na.Seen {
				if i >= 8 {
					rows = append(rows, textRow(fmt.Sprintf("  …and %d more", len(na.Seen)-8), th.Dim()))
					break
				}
				rows = append(rows, textRow("  "+s, th.Acc()))
			}
			rows = append(rows, blankRow(), textRow("Change artifact-pattern (and arch/variant) in the Settings tab so one of these matches.", th.Dim()))
		}
		for i, s := range na.Skipped {
			if i == 0 {
				rows = append(rows, blankRow(), textRow("Runs looked at:", th.Base()))
			}
			if i >= 6 {
				break
			}
			rows = append(rows, textRow(fmt.Sprintf("  %s  %s", runLabel(s.Run.RunNumber, s.Run.ID), s.Reason), th.Dim()))
		}
	case errors.As(err, &ge):
		rows = append(rows, textRow(ge.Error(), th.Bad()), blankRow(), textRow(hintFor(ge), th.Dim()))
	default:
		rows = append(rows, textRow(err.Error(), th.Bad()))
	}
	a.modal = &textModal{title: title, rows: rows, danger: true}
}

func hintFor(e *ghub.Error) string {
	switch e.Class {
	case ghub.ClassAuth:
		return "Run “gh auth login” in Termux, then press r to retry."
	case ghub.ClassRateLimit:
		return "GitHub's request limit is used up for now. It resets on the hour; ghapk caches what it can, so retrying later costs little."
	case ghub.ClassForbidden:
		return "Your token may lack access to this repository (private repos need the “repo” scope). Try “gh auth refresh -s repo,workflow”."
	case ghub.ClassMissing:
		if strings.Contains(e.Endpoint, "/actions/") {
			return "GitHub has no such " + e.Endpoint + ". The repository is fine, so a saved filter or setting is probably stale — press x to clear filters, and check Settings."
		}
		return "GitHub says it does not exist — or you can't see it. Check the spelling, and that gh is signed in to an account with access."
	case ghub.ClassGone:
		return "That artifact has expired (GitHub deletes them after the retention period). Re-run the workflow to build a fresh one."
	case ghub.ClassNetwork, ghub.ClassTimeout:
		return "No connection to GitHub. Check your network and press r to retry."
	case ghub.ClassServer:
		return "GitHub is having trouble. Wait a minute and press r."
	}
	return ""
}

func exitHint(code int) string { return exit.Describe(code) }

// ---- clipboard / browser -----------------------------------------------------------

func (a *App) copyText(s string) {
	if p, ok := device.Tool("termux-clipboard-set"); ok {
		cmd := exec.Command(p)
		cmd.Stdin = strings.NewReader(s)
		if cmd.Run() == nil {
			a.notify("ok", "copied")
			return
		}
	}
	if a.t != nil {
		a.t.Raw("\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(s)) + "\x07")
		a.notify("ok", "copied (if your terminal allows it)")
		return
	}
	a.notify("warn", "no clipboard here")
}

func (a *App) openURL(url string) {
	for _, n := range []string{"termux-open-url", "xdg-open", "open"} {
		if p, ok := device.Tool(n); ok {
			cmd := exec.Command(p, url)
			var out bytes.Buffer
			cmd.Stderr = &out
			if err := cmd.Start(); err == nil {
				go func() { _ = cmd.Wait() }()
				a.notify("ok", "opened in your browser")
				return
			}
		}
	}
	a.copyText(url)
}

// ---- settings glue -------------------------------------------------------------------

// saveSetting writes one setting to the chosen scope and applies it.
func (a *App) saveSetting(key, val string, global bool) error {
	path, err := a.eng.Save(key, val, global)
	if err != nil {
		return err
	}
	scope := "this repo"
	if global || a.eng.Repo() == "" {
		scope = "everywhere"
	}
	switch key {
	case "theme", "glyphs":
		a.applyAppearance()
	case "mouse":
		if a.t != nil {
			a.t.SetMouse(a.eng.Config().Bool("mouse"))
		}
	case "repo", "branch", "workflow", "limit", "source", "artifact", "artifact-pattern", "file", "exclude", "arch", "variant", "all-arch", "package":
		a.builds.invalidateArts()
		a.builds.load(a, true)
		a.refreshInstalled()
	}
	_ = path
	if val == "" {
		a.notify("ok", "%s reset (%s)", key, scope)
	} else {
		a.notify("ok", "%s saved (%s)", key, scope)
	}
	return nil
}

var _ = config.GlobalFile
var _ = ui.Rect{}
