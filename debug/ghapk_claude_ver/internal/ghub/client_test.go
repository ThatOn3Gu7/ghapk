package ghub

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setup puts the fake gh first on PATH, backed by generated fixtures.
func setup(t *testing.T) *Client {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	root, _ := filepath.Abs("../../scripts/fakegh")
	fix := filepath.Join(t.TempDir(), "fake")
	if out, err := exec.Command("python3", filepath.Join(root, "gen.py"), fix).CombinedOutput(); err != nil {
		t.Fatalf("gen: %v\n%s", err, out)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKEGH_DIR", fix)
	t.Setenv("FAKEGH_SPEED", "0")
	c := New(t.TempDir())
	c.Retries = 1
	return c
}

func TestRunsAndArtifacts(t *testing.T) {
	c := setup(t)
	ctx := context.Background()
	runs, err := c.Runs(ctx, "acme/nazo", RunsQuery{Limit: 50})
	if err != nil || len(runs) != 12 {
		t.Fatalf("runs=%d err=%v", len(runs), err)
	}
	if !runs[0].Active() || !runs[1].OK() || runs[2].Conclusion != "failure" {
		t.Fatal("run states")
	}
	br, _ := c.Runs(ctx, "acme/nazo", RunsQuery{Branch: "feature/quiz-ui"})
	if len(br) != 1 {
		t.Fatalf("branch filter: %d", len(br))
	}
	wf, _ := c.Runs(ctx, "acme/nazo", RunsQuery{Workflow: "lint.yml"})
	if len(wf) != 1 {
		t.Fatalf("workflow filter: %d", len(wf))
	}
	idx, complete, err := c.ArtifactIndex(ctx, "acme/nazo", 5)
	if err != nil || !complete || len(idx) != 12 {
		t.Fatalf("index %d %v %v", len(idx), complete, err)
	}
	ra, err := c.RunArtifacts(ctx, "acme/nazo", 9011)
	if err != nil || len(ra) != 7 {
		t.Fatalf("run artifacts %d %v", len(ra), err)
	}
}

func TestETagCache(t *testing.T) {
	c := setup(t)
	ctx := context.Background()
	if _, _, err := c.Cached(ctx, "repos/acme/nazo"); err != nil {
		t.Fatal(err)
	}
	b, hit, err := c.Cached(ctx, "repos/acme/nazo")
	if err != nil || !hit || len(b) == 0 {
		t.Fatalf("second call should be a 304 served from cache: hit=%v err=%v", hit, err)
	}
	if s := c.Stats(); s.Hits != 1 || s.Misses != 1 {
		t.Fatalf("stats %+v", s)
	}
}

func TestClassification(t *testing.T) {
	c := setup(t)
	ctx := context.Background()
	if _, err := c.GetRepo(ctx, "nobody/nothing"); !IsClass(err, ClassMissing) {
		t.Fatalf("want missing, got %v", err)
	}
	t.Setenv("FAKEGH_FAIL", "auth")
	if _, err := c.GetRepo(ctx, "acme/nazo"); !IsClass(err, ClassAuth) {
		t.Fatalf("want auth, got %v", err)
	}
	t.Setenv("FAKEGH_FAIL", "ratelimit")
	c.Retries = 0
	c.OnWait = func(time.Duration, string) {}
	start := time.Now()
	_, err := c.GetRepo(ctx, "acme/nazo")
	if !IsClass(err, ClassRateLimit) || time.Since(start) > 3*time.Minute {
		t.Fatalf("want ratelimit, got %v", err)
	}
	cases := []struct {
		rc  int
		se  string
		cls Class
	}{
		{1, "gh: Not Found (HTTP 404)", ClassMissing},
		{1, "gh: HTTP 304", ClassNotModified},
		{1, "gh: Resource not accessible (HTTP 403)", ClassForbidden},
		{1, "gh: secondary rate limit (HTTP 403)", ClassRateLimit},
		{1, "dial tcp: lookup api.github.com: no such host", ClassNetwork},
		{124, "", ClassTimeout},
		{1, "gh: Gone (HTTP 410)", ClassGone},
		{1, "gh: oops (HTTP 502)", ClassServer},
		{0, "", ClassOK},
	}
	for _, x := range cases {
		if got, _ := Classify(x.rc, x.se); got != x.cls {
			t.Errorf("Classify(%d,%q)=%s want %s", x.rc, x.se, got, x.cls)
		}
	}
}

func TestDownloadProgress(t *testing.T) {
	c := setup(t)
	dest := filepath.Join(t.TempDir(), "a.zip")
	var last int64
	calls := 0
	err := c.DownloadArtifact(context.Background(), "acme/nazo", 5001, dest, 10*time.Second, func(d int64) { last = d; calls++ })
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(dest)
	if st.Size() != last || last < 3_000_000 || calls < 2 {
		t.Fatalf("size=%d last=%d calls=%d", st.Size(), last, calls)
	}
	// a missing artifact must come back as a classified error
	err = c.DownloadArtifact(context.Background(), "acme/nazo", 999999, dest, 10*time.Second, nil)
	if !IsClass(err, ClassMissing) {
		t.Fatalf("want missing, got %v", err)
	}
}

func TestStall(t *testing.T) {
	c := setup(t)
	t.Setenv("FAKEGH_SPEED", "20000") // 20 KB/s: the 64 KB chunks arrive 3 s apart
	err := c.DownloadArtifact(context.Background(), "acme/nazo", 5001, filepath.Join(t.TempDir(), "a.zip"), 2*time.Second, nil)
	if err != ErrStalled {
		t.Fatalf("want ErrStalled, got %v", err)
	}
}

func TestParseRepo(t *testing.T) {
	cases := map[string][2]string{
		"acme/nazo":                          {"acme/nazo", ""},
		"https://github.com/acme/nazo.git":   {"acme/nazo", ""},
		"https://github.com/acme/nazo/pulls": {"acme/nazo", ""},
		"git@github.com:acme/nazo.git":       {"acme/nazo", ""},
		"https://ghe.corp.io/acme/nazo":      {"acme/nazo", "ghe.corp.io"},
		"https://www.github.com/acme/nazo/":  {"acme/nazo", ""},
	}
	for in, w := range cases {
		r, h, ok := ParseRepo(in)
		if !ok || r != w[0] || h != w[1] {
			t.Errorf("ParseRepo(%q)=%q,%q,%v", in, r, h, ok)
		}
	}
	for _, bad := range []string{"", "nope", "a/b/c", "https://github.com/x"} {
		if _, _, ok := ParseRepo(bad); ok {
			t.Errorf("ParseRepo(%q) should fail", bad)
		}
	}
}

func TestRedact(t *testing.T) {
	s := Redact("Authorization: Bearer ghp_abcDEF123 and github_pat_11AA_bb token=xyz")
	for _, bad := range []string{"ghp_abc", "github_pat_", "xyz"} {
		if contains(s, bad) {
			t.Fatalf("leaked %q in %q", bad, s)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestWorkflowResolution(t *testing.T) {
	c := setup(t)
	ctx := context.Background()
	for _, w := range []string{"ci.yml", "CI.YML", "Android CI", "android ci", ".github/workflows/ci.yml", "ci", "101"} {
		runs, err := c.Runs(ctx, "acme/nazo", RunsQuery{Workflow: w, Limit: 50})
		if err != nil || len(runs) == 0 {
			t.Fatalf("workflow %q: runs=%d err=%v", w, len(runs), err)
		}
	}
	_, err := c.Runs(ctx, "acme/nazo", RunsQuery{Workflow: "Nonexistent"})
	var nf *WorkflowNotFound
	if !errors.As(err, &nf) || IsClass(err, ClassMissing) {
		t.Fatalf("unknown workflow must be a WorkflowNotFound, not a missing repo: %v", err)
	}
}

func TestMissingErrorNamesEndpoint(t *testing.T) {
	c := setup(t)
	_, err := c.Runs(context.Background(), "nobody/nothing", RunsQuery{})
	var e *Error
	if !errors.As(err, &e) || e.Class != ClassMissing || e.Endpoint != "repos/nobody/nothing/actions/runs" {
		t.Fatalf("endpoint not recorded: %#v", err)
	}
	if !strings.Contains(err.Error(), "repos/nobody/nothing/actions/runs") {
		t.Fatalf("message should name the call: %v", err)
	}
}

func TestEndpointOf(t *testing.T) {
	got := endpointOf([]string{"--include", "-H", "If-None-Match: x", "repos/a/b/actions/runs?per_page=5"})
	if got != "repos/a/b/actions/runs" {
		t.Fatal(got)
	}
}
