// Package engine is ghapk's brain: it knows how to look at a repository's
// builds, decide which artifact suits this phone, and run the download →
// verify → unpack → validate → install job. It has no UI; the TUI and the plain
// command-line mode both drive it and just render what it reports.
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"ghapk/internal/apk"
	"ghapk/internal/config"
	"ghapk/internal/device"
	"ghapk/internal/ghub"
	"ghapk/internal/pick"
)

// Engine holds the long-lived state of one ghapk session.
type Engine struct {
	GH       *ghub.Client
	Dev      *device.Device
	Log      *Logger
	CacheDir string
	CLI      map[string]string // flag overrides, canonical key -> value

	mu        sync.Mutex
	cfg       *config.Config
	repo      string
	repoInfo  ghub.Repo
	user      string
	idx       []ghub.Artifact // artifact index, newest first
	idxOK     bool
	idxFull   bool
	installed device.Installed
	inRepo    string // text of the repo's committed .github/ghapk.conf
	note      string // one-shot notice for the UI (e.g. a setting that was ignored)
}

// TakeNote returns and clears the pending notice, if any.
func (e *Engine) TakeNote() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := e.note
	e.note = ""
	return n
}

// listRuns asks GitHub for runs. A configured workflow that does not exist in
// this repository is not fatal: it is dropped (with a notice) and every
// workflow is listed instead, rather than reporting the repository as missing.
func (e *Engine) listRuns(ctx context.Context, repo string, f RunFilter, limit int) ([]ghub.Run, error) {
	q := ghub.RunsQuery{Branch: f.Branch, Workflow: f.Workflow, Status: f.Status, Limit: limit}
	runs, err := e.GH.Runs(ctx, repo, q)
	var nf *ghub.WorkflowNotFound
	if errors.As(err, &nf) {
		e.Log.Logf("%v; listing all workflows instead", err)
		e.mu.Lock()
		e.note = fmt.Sprintf("workflow %q from your settings isn't in %s — showing all workflows (x clears filters)", nf.Name, repo)
		e.mu.Unlock()
		q.Workflow = ""
		return e.GH.Runs(ctx, repo, q)
	}
	return runs, err
}

// New builds an engine. cli carries flag overrides (already canonical keys).
func New(cli map[string]string) *Engine {
	cache := config.CacheDir()
	e := &Engine{CacheDir: cache, CLI: cli}
	e.Log = NewLogger(cache)
	e.GH = ghub.New(cache)
	e.GH.Log = func(s string) { e.Log.Logf("%s", s) }
	e.Dev = device.New()
	e.cfg = config.Load(config.LoadOpts{CLI: cli})
	return e
}

// Config returns the effective settings.
func (e *Engine) Config() *config.Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg
}

// Repo is the canonical OWNER/NAME in use, "" before one is chosen.
func (e *Engine) Repo() string { e.mu.Lock(); defer e.mu.Unlock(); return e.repo }

// RepoInfo returns repository metadata.
func (e *Engine) RepoInfo() ghub.Repo { e.mu.Lock(); defer e.mu.Unlock(); return e.repoInfo }

// User is the GitHub login gh runs as.
func (e *Engine) User() string { e.mu.Lock(); defer e.mu.Unlock(); return e.user }

// ReloadConfig re-reads every config layer (after the settings editor saved).
func (e *Engine) ReloadConfig(inRepo string) {
	e.mu.Lock()
	e.inRepo = inRepo
	e.cfg = config.Load(config.LoadOpts{Repo: e.repo, CLI: e.CLI, InRepo: inRepo})
	e.mu.Unlock()
}

// Preflight describes the state of the tools ghapk depends on.
type Preflight struct {
	GHPath    string
	GHFound   bool
	AuthOK    bool
	AuthText  string
	User      string
	Remaining int
	Limit     int
	Reset     time.Time
	Err       error
}

// Preflight checks gh, its login, and the rate limit.
func (e *Engine) Preflight(ctx context.Context) Preflight {
	var p Preflight
	p.GHPath, p.GHFound = e.GH.Available()
	if !p.GHFound {
		p.Err = fmt.Errorf("the GitHub CLI (gh) is not installed: pkg install gh")
		return p
	}
	p.AuthOK, p.AuthText = e.GH.AuthOK(ctx)
	if !p.AuthOK {
		p.Err = &ghub.Error{Class: ghub.ClassAuth}
		return p
	}
	if u, err := e.GH.User(ctx); err == nil {
		p.User = u
		e.mu.Lock()
		e.user = u
		e.mu.Unlock()
	}
	if r, l, rs, err := e.GH.RateLimit(ctx); err == nil {
		p.Remaining, p.Limit, p.Reset = r, l, rs
	}
	return p
}

// SetRepo switches to a repository. input may be OWNER/NAME or a URL. The
// canonical name and default branch come from GitHub; the config layers are
// reloaded for the new repository (including a config committed in the repo).
func (e *Engine) SetRepo(ctx context.Context, input string) error {
	repo, host, ok := ghub.ParseRepo(input)
	if !ok {
		return fmt.Errorf("could not parse %q: use OWNER/NAME or a GitHub URL", input)
	}
	e.GH.Host = host
	info, err := e.GH.GetRepo(ctx, repo)
	if err != nil {
		return err
	}
	if info.FullName != "" {
		repo = info.FullName // canonical capitalisation
	}
	e.mu.Lock()
	e.repo, e.repoInfo = repo, info
	e.idx, e.idxOK, e.idxFull = nil, false, false
	e.installed = device.Installed{}
	e.mu.Unlock()
	inRepo := e.GH.RepoConfig(ctx, repo, config.RepoConfigPath)
	e.ReloadConfig(inRepo)
	e.Log.Logf("repo %s (default branch %s)", repo, info.DefaultBranch)
	return nil
}

// ---- runs and artifacts ------------------------------------------------------

// RunFilter narrows the run list.
type RunFilter struct {
	Branch   string
	Workflow string
	Status   string // "", success, failure, in_progress
}

// RunView is a run plus what we know about its artifacts.
type RunView struct {
	Run       ghub.Run
	Artifacts int    // live artifacts known from the index (-1 = unknown)
	HasAPK    bool   // some artifact name looks like an APK build
	Best      string // name of the best artifact, if known
}

// Runs lists recent runs merged with the artifact index, so the list can show
// which runs have something to download without a request per run.
func (e *Engine) Runs(ctx context.Context, f RunFilter, refresh bool) ([]RunView, error) {
	repo := e.Repo()
	cfg := e.Config()
	runs, err := e.listRuns(ctx, repo, f, cfg.Limit())
	if err != nil {
		return nil, err
	}
	idx := e.loadIndex(ctx, refresh)
	byRun := map[int64][]ghub.Artifact{}
	for _, a := range idx {
		byRun[a.WorkflowRun.ID] = append(byRun[a.WorkflowRun.ID], a)
	}
	pol := e.Policy()
	out := make([]RunView, 0, len(runs))
	for _, r := range runs {
		v := RunView{Run: r, Artifacts: -1}
		if arts, ok := byRun[r.ID]; ok {
			v.Artifacts = len(arts)
			c := pick.Rank(arts, pol)
			if len(c) > 0 && c[0].Match {
				v.HasAPK, v.Best = true, c[0].Art.Name
			}
		} else if e.idxOK && e.idxFull {
			v.Artifacts = 0
		}
		out = append(out, v)
	}
	return out, nil
}

func (e *Engine) loadIndex(ctx context.Context, refresh bool) []ghub.Artifact {
	e.mu.Lock()
	if e.idxOK && !refresh {
		idx := e.idx
		e.mu.Unlock()
		return idx
	}
	repo := e.repo
	e.mu.Unlock()
	arts, full, err := e.GH.ArtifactIndex(ctx, repo, 5)
	if err != nil {
		e.Log.Logf("artifact index unavailable: %v", err)
		return nil
	}
	sort.SliceStable(arts, func(i, j int) bool { return arts[i].CreatedAt.After(arts[j].CreatedAt) })
	e.mu.Lock()
	e.idx, e.idxOK, e.idxFull = arts, true, full
	e.mu.Unlock()
	return arts
}

// RunArtifacts ranks the artifacts of one run for this device.
func (e *Engine) RunArtifacts(ctx context.Context, runID int64) ([]pick.Candidate, error) {
	idx := e.loadIndex(ctx, false)
	var arts []ghub.Artifact
	for _, a := range idx {
		if a.WorkflowRun.ID == runID {
			arts = append(arts, a)
		}
	}
	e.mu.Lock()
	full := e.idxFull
	e.mu.Unlock()
	if len(arts) == 0 && !full { // older than the indexed window: ask for this run directly
		var err error
		arts, err = e.GH.RunArtifacts(ctx, e.Repo(), runID)
		if err != nil {
			return nil, err
		}
	}
	return pick.Rank(arts, e.Policy()), nil
}

// Policy builds the artifact-ranking policy from settings and the device. When
// the target app is installed, its ABI goes first: the new build should match
// what is already there.
func (e *Engine) Policy() pick.Policy {
	cfg := e.Config()
	cats := append([]string(nil), e.Dev.Cats...)
	e.mu.Lock()
	inst := e.installed
	e.mu.Unlock()
	if inst.ABI != "" {
		if c := pick.ABICategory(inst.ABI); c != "unknown" {
			out := []string{c}
			for _, x := range cats {
				if x != c {
					out = append(out, x)
				}
			}
			cats = out
		}
	}
	return pick.Policy{
		Arch: cfg.Str("arch"), Variant: cfg.Str("variant"), Cats: cats, SDK: e.Dev.SDK,
		AllArch: cfg.Bool("all-arch"), Name: cfg.Str("artifact"),
		Include: cfg.Patterns("artifact-pattern"), Exclude: cfg.Patterns("exclude"),
	}
}

// Package is the package name to compare against: the setting, else what the
// history remembers for this repo.
func (e *Engine) Package() string {
	if p := e.Config().Str("package"); p != "" {
		return p
	}
	if h, ok := config.History(e.Repo()); ok && h.Pkg != "" {
		return h.Pkg
	}
	// Like the shell script: an APK kept from an earlier run names the app.
	for _, c := range e.CachedAPKs() {
		if in, err := apk.ReadInfo(c.Path); err == nil && in.Package != "" {
			return in.Package
		}
	}
	return ""
}

// RefreshInstalled reads the installed state of the target app.
func (e *Engine) RefreshInstalled(ctx context.Context) device.Installed {
	pkg := e.Package()
	in := e.Dev.Query(ctx, pkg)
	e.mu.Lock()
	e.installed = in
	e.mu.Unlock()
	if pkg != "" {
		e.Log.Logf("installed %s: found=%v code=%d abi=%s", pkg, in.Found, in.VersionCode, in.ABI)
	}
	return in
}

// Installed returns the last known installed state.
func (e *Engine) Installed() device.Installed { e.mu.Lock(); defer e.mu.Unlock(); return e.installed }

// ---- planning -----------------------------------------------------------------

// Source is one thing that can be downloaded: a workflow artifact or a release
// asset.
type Source struct {
	Kind    string // "artifact" or "release"
	Run     ghub.Run
	Art     ghub.Artifact
	Release ghub.Release
	Asset   ghub.Asset
	Name    string
	Size    int64
	Digest  string
	Score   int
}

// Label is a short human description.
func (s Source) Label() string {
	if s.Kind == "release" {
		return s.Release.TagName + " · " + s.Name
	}
	return fmt.Sprintf("#%d · %s", s.Run.ID, s.Name)
}

// FromCandidate builds a Source from a ranked artifact.
func FromCandidate(run ghub.Run, c pick.Candidate) Source {
	return Source{Kind: "artifact", Run: run, Art: c.Art, Name: c.Art.Name, Size: c.Art.Size, Digest: c.Art.Digest, Score: c.Score}
}

// Plan is the dry-run: what "install the newest build" would do.
type Plan struct {
	Source      Source
	Candidates  []pick.Candidate // all artifacts of the chosen run, ranked
	Run         ghub.Run
	Skipped     []SkippedRun // newer runs passed over, with the reason
	Active      *ghub.Run    // a newer run still in flight
	Version     string
	Installed   device.Installed
	Downgrade   bool
	Same        bool
	Changes     []ghub.Commit
	ChangesFrom string
}

// SkippedRun says why a run was not chosen.
type SkippedRun struct {
	Run    ghub.Run
	Reason string
}

// PlanNewest finds the newest successful run that holds an artifact matching
// the settings, and the best artifact in it. It is what the shell script did
// for a plain `ghapk` and what the "install newest" action runs.
func (e *Engine) PlanNewest(ctx context.Context, f RunFilter) (*Plan, error) {
	cfg := e.Config()
	src := cfg.Str("source")
	var plan *Plan
	var err error
	if src != "releases" {
		plan, err = e.planActions(ctx, f)
		if err == nil || src == "actions" {
			if plan != nil {
				e.annotate(ctx, plan)
			}
			return plan, err
		}
		e.Log.Logf("no artifact found, trying releases: %v", err)
	}
	plan, err = e.planReleases(ctx)
	if plan != nil {
		e.annotate(ctx, plan)
	}
	return plan, err
}

func (e *Engine) planActions(ctx context.Context, f RunFilter) (*Plan, error) {
	repo := e.Repo()
	cfg := e.Config()
	all, err := e.listRuns(ctx, repo, f, cfg.Limit())
	if err != nil {
		return nil, err
	}
	p := &Plan{}
	var runs []ghub.Run
	for _, r := range all {
		switch {
		case r.Active():
			if p.Active == nil && len(runs) == 0 {
				rr := r
				p.Active = &rr
			}
		case r.OK():
			runs = append(runs, r)
		}
	}
	if len(runs) == 0 {
		return nil, &NoArtifactError{Msg: "no successful workflow runs found"}
	}
	pol := e.Policy()
	idx := e.loadIndex(ctx, false)
	e.mu.Lock()
	full := e.idxFull
	e.mu.Unlock()
	var seen []string
	for _, r := range runs {
		var arts []ghub.Artifact
		for _, a := range idx {
			if a.WorkflowRun.ID == r.ID {
				arts = append(arts, a)
			}
		}
		if len(arts) == 0 && !full {
			if arts, err = e.GH.RunArtifacts(ctx, repo, r.ID); err != nil {
				p.Skipped = append(p.Skipped, SkippedRun{r, "artifact list unavailable"})
				continue
			}
		}
		if len(arts) == 0 {
			p.Skipped = append(p.Skipped, SkippedRun{r, "no unexpired artifacts"})
			continue
		}
		cands := pick.Rank(arts, pol)
		if !cands[0].Match {
			for _, c := range cands {
				seen = append(seen, c.Art.Name)
			}
			p.Skipped = append(p.Skipped, SkippedRun{r, fmt.Sprintf("%d artifact(s), none matched", len(arts))})
			continue
		}
		p.Run, p.Candidates = r, cands
		p.Source = FromCandidate(r, cands[0])
		return p, nil
	}
	return nil, &NoArtifactError{Msg: "no matching artifact in the last " + fmt.Sprint(len(runs)) + " successful run(s)", Seen: uniq(seen), Skipped: p.Skipped}
}

func (e *Engine) planReleases(ctx context.Context) (*Plan, error) {
	repo := e.Repo()
	rels, err := e.GH.Releases(ctx, repo, 20)
	if err != nil {
		return nil, err
	}
	cfg := e.Config()
	files, excl := cfg.Patterns("file"), cfg.Patterns("exclude")
	pol := e.Policy()
	for _, r := range rels {
		type cand struct {
			a ghub.Asset
			s int
		}
		var cs []cand
		for _, a := range r.Assets {
			if a.State != "uploaded" || !matchAny(a.Name, files) || matchAny(a.Name, excl) {
				continue
			}
			if pol.Arch != "any" && !pol.AllArch && !pol.AllowsArch(a.Name) || !pol.AllowsVariant(a.Name) {
				continue
			}
			cs = append(cs, cand{a, pol.APKRank(a.Name)})
		}
		if len(cs) == 0 {
			continue
		}
		sort.SliceStable(cs, func(i, j int) bool { return cs[i].s > cs[j].s })
		a := cs[0].a
		return &Plan{Source: Source{Kind: "release", Release: r, Asset: a, Name: a.Name, Size: a.Size, Score: cs[0].s},
			Run: ghub.Run{DisplayTitle: r.Name, HeadBranch: r.TagName, CreatedAt: r.PublishedAt, HTMLURL: r.HTMLURL}}, nil
	}
	return nil, &NoArtifactError{Msg: "no release asset matches"}
}

// annotate adds the version comparison and "what changed" to a plan.
func (e *Engine) annotate(ctx context.Context, p *Plan) {
	p.Version = extractVersion(p.Source.Name)
	p.Installed = e.RefreshInstalled(ctx)
	if p.Installed.Found && p.Version != "" && p.Installed.VersionName != "" {
		switch cmpVersion(p.Version, extractVersion(p.Installed.VersionName)) {
		case -1:
			p.Downgrade = true
		case 0:
			p.Same = true
		}
	}
	if h, ok := config.History(e.Repo()); ok && h.SHA != "" && p.Run.HeadSHA != "" && h.SHA != p.Run.HeadSHA && p.Source.Kind == "artifact" {
		if _, commits, err := e.GH.Compare(ctx, e.Repo(), h.SHA, p.Run.HeadSHA); err == nil {
			p.Changes, p.ChangesFrom = commits, h.SHA
		}
	}
}

// NoArtifactError carries what was seen, so the UI can show names that did not
// match: a pattern miss is the one failure the user can actually fix.
type NoArtifactError struct {
	Msg     string
	Seen    []string
	Skipped []SkippedRun
}

func (e *NoArtifactError) Error() string { return e.Msg }

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// ---- directories -----------------------------------------------------------

// WorkDir is the scratch directory for one job.
func (e *Engine) WorkDir(id int) string {
	return filepath.Join(e.CacheDir, "work", fmt.Sprintf("job-%d-%d", os.Getpid(), id))
}

// APKDir is where finished APKs are kept.
func (e *Engine) APKDir() string { return filepath.Join(e.CacheDir, "apk") }

// CachedAPK is a file in the APK cache.
type CachedAPK struct {
	Path string
	Size int64
	When time.Time
}

// CachedAPKs lists kept APKs, newest first.
func (e *Engine) CachedAPKs() []CachedAPK {
	ents, _ := os.ReadDir(e.APKDir())
	var out []CachedAPK
	for _, en := range ents {
		if en.IsDir() || !strings.HasSuffix(strings.ToLower(en.Name()), ".apk") {
			continue
		}
		if st, err := en.Info(); err == nil {
			out = append(out, CachedAPK{filepath.Join(e.APKDir(), en.Name()), st.Size(), st.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].When.After(out[j].When) })
	return out
}

// CacheSize totals everything under the cache directory.
func (e *Engine) CacheSize() int64 {
	var n int64
	_ = filepath.Walk(e.CacheDir, func(_ string, st os.FileInfo, err error) error {
		if err == nil && !st.IsDir() {
			n += st.Size()
		}
		return nil
	})
	return n
}

// Clean wipes downloads and the HTTP cache. Config and history are kept.
func (e *Engine) Clean() error {
	var first error
	for _, d := range []string{"apk", "work", "http", "tmp"} {
		if err := os.RemoveAll(filepath.Join(e.CacheDir, d)); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Close releases the log file.
func (e *Engine) Close() { e.Log.Close() }
