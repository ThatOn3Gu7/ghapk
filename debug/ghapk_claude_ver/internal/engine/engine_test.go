package engine

import (
	"archive/zip"
	"context"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ghapk/internal/config"
	"ghapk/internal/exit"
	"ghapk/internal/ghub"
)

type scripted struct {
	mu      sync.Mutex
	answers map[string]int // question kind -> option
	asked   []Question
}

func (s *scripted) Ask(ctx context.Context, q Question) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, q)
	if a, ok := s.answers[q.Kind]; ok {
		return a
	}
	return q.Default
}

func (s *scripted) kinds() []string {
	var k []string
	for _, q := range s.asked {
		k = append(k, q.Kind)
	}
	return k
}

// world sets up fake gh + fake phone and returns a ready engine.
func world(t *testing.T, cli map[string]string) *Engine {
	t.Helper()
	for _, tool := range []string{"python3", "openssl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool)
		}
	}
	root, _ := filepath.Abs("../../scripts/fakegh")
	fix := filepath.Join(t.TempDir(), "fake")
	if out, err := exec.Command("python3", filepath.Join(root, "gen.py"), fix).CombinedOutput(); err != nil {
		t.Fatalf("gen: %v\n%s", err, out)
	}
	t.Setenv("PATH", root+":"+filepath.Join(root, "bin")+":"+os.Getenv("PATH"))
	t.Setenv("FAKEGH_DIR", fix)
	t.Setenv("FAKEGH_SPEED", "0")
	t.Setenv("FAKEPHONE_DIR", t.TempDir())
	t.Setenv("FAKE_INSTALL_DELAY", "1")
	t.Setenv("TERMUX_VERSION", "1")
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	t.Setenv("GHAPK_CACHE_DIR", t.TempDir())
	t.Setenv("GHAPK_HISTORY", filepath.Join(t.TempDir(), "h.json"))
	t.Setenv("GHAPK_RISH", "off")
	if cli == nil {
		cli = map[string]string{}
	}
	if _, ok := cli["install-wait"]; !ok {
		cli["install-wait"] = "15"
	}
	e := New(cli)
	e.GH.Retries = 0
	ctx := context.Background()
	if p := e.Preflight(ctx); p.Err != nil || p.User != "thaton3" {
		t.Fatalf("preflight: %+v", p)
	}
	if err := e.SetRepo(ctx, "acme/nazo"); err != nil {
		t.Fatal(err)
	}
	return e
}

func runJob(t *testing.T, e *Engine, src Source, opts JobOpts, p Prompter) JobView {
	t.Helper()
	j := e.NewJob(src, opts)
	j.Start(context.Background(), p, nil)
	done := make(chan struct{})
	go func() { j.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("job timed out")
	}
	return j.Snapshot()
}

func TestPlanNewest(t *testing.T) {
	e := world(t, map[string]string{"package": "quiz.thaton3app.nazo"})
	plan, err := e.PlanNewest(context.Background(), RunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Run.ID != 9011 || plan.Source.Name != "nazo-debug-arm64-v8a" {
		t.Fatalf("plan: run=%d art=%s", plan.Run.ID, plan.Source.Name)
	}
	if plan.Active == nil || plan.Active.ID != 9012 {
		t.Fatal("should notice the run in flight")
	}
	if !plan.Installed.Found || plan.Installed.VersionCode != 141 || plan.Installed.ABI != "arm64-v8a" {
		t.Fatalf("installed: %+v", plan.Installed)
	}
	if len(plan.Candidates) != 7 || !plan.Candidates[0].Choice {
		t.Fatalf("candidates: %d", len(plan.Candidates))
	}
	// 9010 failed, so nothing newer than 9011 may have been skipped for artifacts
	if len(plan.Skipped) != 0 {
		t.Fatalf("skipped: %+v", plan.Skipped)
	}
}

func TestFullInstall(t *testing.T) {
	e := world(t, map[string]string{"confirm-install": "0"})
	plan, err := e.PlanNewest(context.Background(), RunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	p := &scripted{}
	v := runJob(t, e, plan.Source, JobOpts{}, p)
	if v.State != JobDone || v.ExitCode() != 0 {
		t.Fatalf("state=%v code=%d err=%v log=%v", v.State, v.Code, v.Err, v.Log)
	}
	for _, s := range v.Steps {
		if s.State != StepOK {
			t.Errorf("step %s = %v (%s)", s.ID, s.State, s.Detail)
		}
	}
	r := v.Res
	if !r.Verified || r.InstalledNow != 142 || r.Info.Package != "quiz.thaton3app.nazo" || r.Info.VersionName != "1.4.2" {
		t.Fatalf("result: %+v", r)
	}
	if r.Digest != "match" || r.Cert.Scheme != "v2" || r.Method != "termux-open" {
		t.Fatalf("result: digest=%s cert=%+v method=%s", r.Digest, r.Cert, r.Method)
	}
	if _, err := os.Stat(r.APK); err != nil {
		t.Fatal("apk not saved")
	}
	if h, ok := config.History("acme/nazo"); !ok || h.Installs != 1 || h.Run != 9011 || h.VCode != "142" || h.Sig == "" {
		t.Fatalf("history: %+v", h)
	}
	if len(p.asked) != 0 {
		t.Fatalf("a clean install must not ask anything, asked %v", p.kinds())
	}
	if ents, _ := os.ReadDir(e.WorkDir(0) + "/.."); len(ents) != 0 {
		t.Errorf("scratch space not cleaned: %v", ents)
	}
}

func TestSignatureMismatchRefused(t *testing.T) {
	t.Setenv("FAKEPHONE_CERT", strings.Repeat("ab", 64)) // some other key
	e := world(t, map[string]string{"confirm-install": "0"})
	plan, _ := e.PlanNewest(context.Background(), RunFilter{})
	p := &scripted{}
	v := runJob(t, e, plan.Source, JobOpts{}, p)
	if v.State != JobFailed || v.Code != exit.SigMismatch {
		t.Fatalf("state=%v code=%d err=%v", v.State, v.Code, v.Err)
	}
	if k := p.kinds(); len(k) != 1 || k[0] != "sig" || !p.asked[0].Danger {
		t.Fatalf("asked %v", k)
	}
	// ...and "install anyway" goes through
	p2 := &scripted{answers: map[string]int{"sig": 1}}
	v = runJob(t, e, plan.Source, JobOpts{}, p2)
	if v.State != JobDone || !v.Res.HandedOff {
		t.Fatalf("anyway: state=%v err=%v", v.State, v.Err)
	}
}

func TestDowngradeAsksAndSkipRefuses(t *testing.T) {
	e := world(t, map[string]string{"confirm-install": "0"})
	cands, err := e.RunArtifacts(context.Background(), 9007)
	if err != nil {
		t.Fatal(err)
	}
	runs, _ := e.GH.Runs(context.Background(), "acme/nazo", ghub.RunsQuery{Limit: 50})
	var run ghub.Run
	for _, r := range runs {
		if r.ID == 9007 {
			run = r
		}
	}
	src := FromCandidate(run, cands[0])
	p := &scripted{}
	v := runJob(t, e, src, JobOpts{}, p)
	if v.State != JobFailed || v.Code != exit.Downgrade || len(p.asked) != 1 || p.asked[0].Kind != "downgrade" {
		t.Fatalf("auto: state=%v code=%d asked=%v err=%v", v.State, v.Code, p.kinds(), v.Err)
	}
	e.Config().Set("downgrade", "skip")
	p = &scripted{}
	v = runJob(t, e, src, JobOpts{}, p)
	if v.Code != exit.Downgrade || len(p.asked) != 0 {
		t.Fatalf("skip must refuse without asking: code=%d asked=%v", v.Code, p.kinds())
	}
}

func TestDownloadOnlyAndAllCandidates(t *testing.T) {
	e := world(t, nil)
	plan, _ := e.PlanNewest(context.Background(), RunFilter{})
	v := runJob(t, e, plan.Source, JobOpts{NoInstall: true}, &scripted{})
	if v.State != JobDone || v.Res.HandedOff || v.Steps[5].State != StepSkip {
		t.Fatalf("download-only: %+v", v.Steps)
	}
	if got := e.CachedAPKs(); len(got) != 1 {
		t.Fatalf("cache: %v", got)
	}
}

func TestNoMatchingArtifactExplains(t *testing.T) {
	e := world(t, map[string]string{"artifact-pattern": "*nothing-like-this*", "source": "actions"})
	_, err := e.PlanNewest(context.Background(), RunFilter{})
	nae, ok := err.(*NoArtifactError)
	if !ok || len(nae.Seen) == 0 {
		t.Fatalf("want NoArtifactError that lists what it saw, got %v", err)
	}
}

func TestZipSlipRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "evil.zip")
	f, _ := os.Create(p)
	w := zip.NewWriter(f)
	h := &zip.FileHeader{Name: "../../escape.txt"}
	fw, _ := w.CreateHeader(h)
	fw.Write([]byte("x"))
	w.Close()
	f.Close()
	dir := t.TempDir()
	if _, err := extractZip(p, dir); err == nil {
		t.Fatal("zip slip must be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "..", "escape.txt")); err == nil {
		t.Fatal("file escaped")
	}
}

func TestNestedZip(t *testing.T) {
	dir := t.TempDir()
	inner := filepath.Join(dir, "inner.zip")
	f, _ := os.Create(inner)
	w := zip.NewWriter(f)
	fw, _ := w.Create("deep/app.apk")
	fw.Write([]byte("apk"))
	w.Close()
	f.Close()
	unpackNested(dir, nil)
	found := false
	for _, f := range listFiles(dir) {
		if filepath.Base(f) == "app.apk" {
			found = true
		}
	}
	if !found {
		t.Fatal("nested zip not unpacked")
	}
	_ = hex.EncodeToString
}

func TestStaleWorkflowSettingFallsBack(t *testing.T) {
	e := world(t, map[string]string{"package": "quiz.thaton3app.nazo"})
	ctx := context.Background()
	runs, err := e.Runs(ctx, RunFilter{Workflow: "Some Old Workflow"}, true)
	if err != nil || len(runs) == 0 {
		t.Fatalf("must fall back to all workflows, got %d runs, err=%v", len(runs), err)
	}
	if n := e.TakeNote(); !strings.Contains(n, "Some Old Workflow") {
		t.Fatalf("user should be told which setting was ignored: %q", n)
	}
	if e.TakeNote() != "" {
		t.Fatal("note should be one-shot")
	}
	plan, err := e.PlanNewest(ctx, RunFilter{Workflow: "ghost.yml"})
	if err != nil || plan.Run.ID != 9011 {
		t.Fatalf("plan with stale workflow: %v", err)
	}
	e.TakeNote()
	if runs, err = e.Runs(ctx, RunFilter{Workflow: "Android CI"}, false); err != nil || len(runs) == 0 || e.TakeNote() != "" {
		t.Fatal("a display name that exists must resolve silently")
	}
}
