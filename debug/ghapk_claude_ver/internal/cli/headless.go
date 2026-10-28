package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ghapk/internal/engine"
	"ghapk/internal/exit"
	"ghapk/internal/ghub"
)

type plainOut struct {
	color bool
	tty   bool
}

func newPlainOut() plainOut {
	st, err := os.Stderr.Stat()
	tty := err == nil && st.Mode()&os.ModeCharDevice != 0
	return plainOut{tty: tty, color: tty && os.Getenv("NO_COLOR") == ""}
}

func (o plainOut) c(code, s string) string {
	if !o.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (o plainOut) say(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) }

// stdPrompter asks on the terminal when there is one, and takes the safe
// default when there is not (cron, pipes).
type stdPrompter struct{ o plainOut }

func (p stdPrompter) Ask(ctx context.Context, q engine.Question) int {
	if !p.o.tty {
		p.o.say("%s — taking the default: %s", q.Title, q.Options[q.Default])
		return q.Default
	}
	fmt.Fprint(os.Stderr, "\r\x1b[K")
	p.o.say("\n%s", p.o.c("1", q.Title))
	if q.Body != "" {
		p.o.say("%s", q.Body)
	}
	for i, o := range q.Options {
		mark := " "
		if i == q.Default {
			mark = "*"
		}
		p.o.say(" %s %d) %s", mark, i+1, o)
	}
	fmt.Fprintf(os.Stderr, "choose [1-%d, enter = %d, q = cancel]: ", len(q.Options), q.Default+1)
	ch := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		ch <- strings.TrimSpace(line)
	}()
	select {
	case <-ctx.Done():
		return -1
	case l := <-ch:
		if l == "" {
			return q.Default
		}
		if l == "q" {
			return -1
		}
		if n, err := strconv.Atoi(l); err == nil && n >= 1 && n <= len(q.Options) {
			return n - 1
		}
		return q.Default
	}
}

func errCode(err error) int {
	var ge *ghub.Error
	var na *engine.NoArtifactError
	switch {
	case errors.As(err, &na):
		return exit.NoArtifact
	case errors.As(err, &ge):
		return ge.Code()
	}
	return exit.API
}

func headless(c *cli) int {
	o := newPlainOut()
	eng := newEngine(c)
	defer eng.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; cancel() }()

	pre := eng.Preflight(ctx)
	if pre.Err != nil {
		if !pre.GHFound {
			o.say("ghapk: the GitHub CLI (gh) is not installed. In Termux: pkg install gh")
			return exit.Dep
		}
		o.say("ghapk: gh is not signed in. Run: gh auth login")
		return exit.Auth
	}
	repo := eng.Config().Str("repo")
	if repo == "" {
		if r, ok := eng.GH.RepoFromDir(ctx); ok {
			repo = r
		}
	}
	if repo == "" {
		repo = loadLast()
	}
	if repo == "" {
		o.say("ghapk: no repository. Use --repo owner/name, or run ghapk with no arguments to pick one.")
		return exit.Usage
	}
	if err := eng.SetRepo(ctx, repo); err != nil {
		o.say("ghapk: %v", err)
		return errCode(err)
	}
	o.say("%s  %s", o.c("1", eng.Repo()), o.c("2", "as @"+pre.User))

	cfg := eng.Config()
	f := engine.RunFilter{Branch: cfg.Str("branch"), Workflow: cfg.Str("workflow")}
	if c.listBranches || c.listWorkflows {
		return listThings(ctx, eng, c)
	}

	var plan *engine.Plan
	var err error
	if c.runID != 0 {
		plan, err = eng.PlanRun(ctx, c.runID, 0)
	} else {
		plan, err = eng.PlanNewest(ctx, f)
		if err == nil && plan.Active != nil && c.watch {
			a := plan.Active
			o.say("%s is still building; waiting…", fmt.Sprintf("#%d", a.RunNumber))
			run, werr := eng.WatchRun(ctx, a.ID, 6*time.Second, time.Duration(cfg.Int("watch-timeout"))*time.Second, func(r ghub.Run) {
				if o.tty {
					fmt.Fprintf(os.Stderr, "\r\x1b[K  %s %s", r.Status, time.Since(r.StartedAt).Round(time.Second))
				}
			})
			if o.tty {
				fmt.Fprint(os.Stderr, "\r\x1b[K")
			}
			if werr != nil {
				o.say("ghapk: %v", werr)
				return errCode(werr)
			}
			if !run.OK() {
				o.say("ghapk: build #%d %s", run.RunNumber, run.Conclusion)
				if c.strict {
					return exit.BuildFail
				}
			} else {
				plan, err = eng.PlanRun(ctx, run.ID, 0)
			}
		}
	}
	if err != nil {
		printNoArtifact(o, err)
		return errCode(err)
	}
	printPlan(o, eng, plan)
	if c.dryRun {
		o.say("\n(dry run — nothing downloaded)")
		return 0
	}

	job := eng.NewJob(plan.Source, engine.JobOpts{NoInstall: c.noOpen, Yes: c.yes})
	done := make(chan struct{})
	job.Start(ctx, stdPrompter{o}, func() {})
	go func() { job.Wait(); close(done) }()
	renderJob(o, job, done)
	v := job.Snapshot()
	if v.State == engine.JobDone && v.Res.APK != "" {
		saveLast(eng.Repo())
		if c.printPath {
			fmt.Println(v.Res.APK)
		}
	}
	if v.State == engine.JobFailed {
		o.say("\nghapk: %s", firstLine(v.Err))
	}
	return v.ExitCode()
}

func firstLine(err error) string {
	if err == nil {
		return "failed"
	}
	s := strings.TrimSpace(err.Error())
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func printNoArtifact(o plainOut, err error) {
	o.say("ghapk: %v", err)
	var na *engine.NoArtifactError
	if errors.As(err, &na) && len(na.Seen) > 0 {
		o.say("  artifacts that exist but did not match: %s", strings.Join(na.Seen, ", "))
		o.say("  change --artifact-pattern (or --arch / --variant) so one of them matches")
	}
}

func printPlan(o plainOut, eng *engine.Engine, p *engine.Plan) {
	r := p.Run
	if p.Source.Kind == "release" {
		o.say("release  %s · %s (%s)", p.Source.Release.TagName, p.Source.Name, engine.FmtBytes(p.Source.Size))
	} else {
		o.say("build    #%d · %s · %s · %s", r.RunNumber, r.HeadBranch, r.Short(), r.DisplayTitle)
		o.say("artifact %s (%s)", p.Source.Name, engine.FmtBytes(p.Source.Size))
	}
	in := p.Installed
	switch {
	case in.Found:
		note := ""
		switch {
		case p.Downgrade:
			note = o.c("31", "  — OLDER than installed")
		case p.Same:
			note = o.c("33", "  — same version")
		case p.Version != "":
			note = o.c("32", "  — newer")
		}
		o.say("phone    %s %s (code %d)%s", in.Pkg, in.VersionName, in.VersionCode, note)
	case in.Known:
		o.say("phone    %s is not installed", in.Pkg)
	}
	if p.Active != nil {
		o.say("note     #%d is still building (use --watch to wait for it)", p.Active.RunNumber)
	}
	if len(p.Changes) > 0 {
		o.say("changes  %d commit(s) since your last install:", len(p.Changes))
		for i, ch := range p.Changes {
			if i >= 8 {
				o.say("         …")
				break
			}
			o.say("         %s %s", ch.SHA[:7], ch.Subject())
		}
	}
	o.say("")
}

func renderJob(o plainOut, j *engine.Job, done <-chan struct{}) {
	shown := map[string]engine.StepState{}
	tick := time.NewTicker(120 * time.Millisecond)
	defer tick.Stop()
	line := false
	flush := func() {
		v := j.Snapshot()
		for _, s := range v.Steps {
			if s.State == shown[s.ID] || s.State == engine.StepPending || s.State == engine.StepRunning {
				continue
			}
			shown[s.ID] = s.State
			if line {
				fmt.Fprint(os.Stderr, "\r\x1b[K")
				line = false
			}
			g := map[engine.StepState]string{engine.StepOK: o.c("32", "✓"), engine.StepWarn: o.c("33", "!"), engine.StepSkip: o.c("2", "–"), engine.StepFail: o.c("31", "✗")}[s.State]
			o.say("%s %-9s %s", g, s.Title, s.Detail)
		}
		if o.tty && v.Prog.Active {
			pr := v.Prog
			w := 24
			frac := pr.Fraction()
			if frac < 0 {
				frac = 0
			}
			n := int(frac * float64(w))
			bar := strings.Repeat("█", n) + strings.Repeat("░", w-n)
			fmt.Fprintf(os.Stderr, "\r\x1b[K  %s %3d%%  %s  %s/s", bar, int(frac*100), engine.FmtBytes(pr.Done), engine.FmtBytes(int64(pr.Rate)))
			line = true
		}
	}
	for {
		select {
		case <-done:
			flush()
			if line {
				fmt.Fprint(os.Stderr, "\r\x1b[K")
			}
			return
		case <-tick.C:
			flush()
		}
	}
}

func listThings(ctx context.Context, eng *engine.Engine, c *cli) int {
	if c.listBranches {
		bs, err := eng.GH.Branches(ctx, eng.Repo())
		if err != nil {
			fmt.Fprintln(os.Stderr, "ghapk:", err)
			return errCode(err)
		}
		for _, b := range bs {
			fmt.Println(b.Name)
		}
		return 0
	}
	ws, err := eng.GH.Workflows(ctx, eng.Repo())
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghapk:", err)
		return errCode(err)
	}
	for _, w := range ws {
		fmt.Printf("%s\t%s\n", w.File(), w.Name)
	}
	return 0
}

func doDoctor(c *cli) int {
	o := newPlainOut()
	eng := newEngine(c)
	defer eng.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pre := eng.Preflight(ctx)
	if repo := eng.Config().Str("repo"); repo != "" && pre.Err == nil {
		_ = eng.SetRepo(ctx, repo)
	}
	bad := 0
	group := ""
	for _, ch := range eng.Doctor(ctx) {
		if ch.Group != group {
			group = ch.Group
			fmt.Printf("\n%s\n", o.c("1", group))
		}
		g := map[int]string{engine.CheckOK: o.c("32", "✓"), engine.CheckWarn: o.c("33", "!"), engine.CheckFail: o.c("31", "✗"), engine.CheckInfo: " "}[ch.State]
		fmt.Printf("  %s %-16s %s\n", g, ch.Name, ch.Value)
		if ch.Hint != "" && ch.State != engine.CheckOK {
			fmt.Printf("      %s\n", o.c("2", ch.Hint))
		}
		if ch.State == engine.CheckFail {
			bad++
		}
	}
	fmt.Println()
	if bad > 0 {
		return exit.Doctor
	}
	return 0
}

func printCompletion(shell string) int {
	flags := "--repo --branch --workflow --run --name --artifact-pattern --file --exclude --source --limit --out --keep --yes --no-open --watch --print-path --arch --variant --package --downgrade --all-arch --set --set-global --unset --theme --ascii --unicode --no-mouse --doctor --clean --plain --tui --dry-run --completion --version --help"
	switch shell {
	case "bash":
		fmt.Printf("_ghapk() { COMPREPLY=( $(compgen -W \"%s\" -- \"${COMP_WORDS[COMP_CWORD]}\") ); }\ncomplete -F _ghapk ghapk\n", flags)
	case "zsh":
		fmt.Printf("#compdef ghapk\n_arguments '*:option:(%s)'\n", flags)
	case "fish":
		for _, f := range strings.Fields(flags) {
			fmt.Printf("complete -c ghapk -l %s\n", strings.TrimPrefix(f, "--"))
		}
	default:
		fmt.Fprintln(os.Stderr, "ghapk: completion supports bash, zsh and fish")
		return exit.Usage
	}
	return 0
}
