package ghapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name, stderr string
		timedOut     bool
		want         Class
		status       int
	}{
		{"missing", "gh: Not Found (HTTP 404)", false, ClassMissing, 404},
		{"auth", "gh: Bad credentials (HTTP 401)", false, ClassAuth, 401},
		{"auth text", "To get started with GitHub CLI, please run:  gh auth login", false, ClassAuth, 0},
		{"rate", "gh: API rate limit exceeded for user ID 1. (HTTP 403)", false, ClassRateLimit, 403},
		{"secondary", "gh: You have exceeded a secondary rate limit (HTTP 403)", false, ClassRateLimit, 403},
		{"429", "gh: Too Many Requests (HTTP 429)", false, ClassRateLimit, 429},
		{"scope", "gh: Resource not accessible by personal access token (HTTP 403)", false, ClassForbidden, 403},
		{"gone", "gh: Gone (HTTP 410)", false, ClassGone, 410},
		{"502", "gh: Bad Gateway (HTTP 502)", false, ClassServer, 502},
		{"dns", "error connecting to api.github.com\ncheck your internet connection", false, ClassNetwork, 0},
		{"timeout", "", true, ClassTimeout, 0},
		{"other", "gh: Unprocessable Entity (HTTP 422)", false, ClassOther, 422},
	}
	for _, c := range cases {
		e := classify("repos/a/b/actions/runs?per_page=5", c.stderr, errors.New("exit status 1"), c.timedOut)
		if e.Class != c.want || e.Status != c.status {
			t.Errorf("%s: got %s/%d, want %s/%d", c.name, e.Class, e.Status, c.want, c.status)
		}
		if e.Endpoint != "repos/a/b/actions/runs" {
			t.Errorf("%s: endpoint %q still has its query", c.name, e.Endpoint)
		}
	}
}

func TestErrorTextKeepsLegacyShape(t *testing.T) {
	e := classify("repos/a/b", "gh: Not Found (HTTP 404)", errors.New("exit status 1"), false)
	if got := e.Error(); got != "gh api repos/a/b: gh: Not Found (HTTP 404)" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestDescribeNamesTheFailingCall(t *testing.T) {
	e := classify("repos/a/b/actions/workflows/x.yml/runs", "gh: Not Found (HTTP 404)", errors.New("x"), false)
	title, hint := Describe(e)
	if title != "Not found on GitHub" || !strings.Contains(hint, "workflow") {
		t.Fatalf("title=%q hint=%q", title, hint)
	}
	e = classify("repos/a/b/actions/runs", "gh: Not Found (HTTP 404)", errors.New("x"), false)
	if _, hint = Describe(e); !strings.Contains(hint, "repository exists") {
		t.Fatalf("an Actions 404 must not blame the repo name: %q", hint)
	}
	if title, hint = Describe(errors.New("plain")); title != "plain" || hint != "" {
		t.Fatalf("plain error: %q %q", title, hint)
	}
}

// countingGH writes a gh that fails with stderr for the first `fail` calls and
// then prints ok, recording every call.
func countingGH(t *testing.T, fail int, stderr string) string {
	t.Helper()
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := "#!/bin/sh\necho x >> '" + count + "'\nn=$(wc -l < '" + count + "')\n" +
		"if [ \"$n\" -le " + string(rune('0'+fail)) + " ]; then echo '" + stderr + "' >&2; exit 1; fi\nprintf '%s' '{}'\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return count
}

func calls(t *testing.T, path string) int {
	t.Helper()
	b, _ := os.ReadFile(path)
	return strings.Count(string(b), "x")
}

func TestTransientFailuresAreRetried(t *testing.T) {
	old := retryBase
	retryBase = time.Millisecond
	defer func() { retryBase = old }()
	count := countingGH(t, 2, "gh: Bad Gateway (HTTP 502)")
	var v map[string]any
	if err := api(&v, 5*time.Second, "repos/a/b"); err != nil {
		t.Fatalf("should have recovered: %v", err)
	}
	if n := calls(t, count); n != 3 {
		t.Fatalf("calls = %d, want 3", n)
	}
}

func TestRetryGivesUpAndClassifies(t *testing.T) {
	old := retryBase
	retryBase = time.Millisecond
	defer func() { retryBase = old }()
	count := countingGH(t, 9, "gh: Bad Gateway (HTTP 502)")
	err := api(nil, 5*time.Second, "repos/a/b")
	if !IsClass(err, ClassServer) {
		t.Fatalf("want server class, got %v", err)
	}
	if n := calls(t, count); n != maxAttempts {
		t.Fatalf("calls = %d, want %d", n, maxAttempts)
	}
}

func TestNotFoundIsNotRetried(t *testing.T) {
	count := countingGH(t, 9, "gh: Not Found (HTTP 404)")
	err := api(nil, 5*time.Second, "repos/a/b/actions/runs")
	if !IsClass(err, ClassMissing) {
		t.Fatalf("want missing, got %v", err)
	}
	if n := calls(t, count); n != 1 {
		t.Fatalf("a 404 was retried %d times", n)
	}
}

func TestCancelledContextIsNotAGitHubError(t *testing.T) {
	countingGH(t, 9, "gh: Bad Gateway (HTTP 502)")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := apiContext(ctx, nil, 5*time.Second, "repos/a/b")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestWorkflowResolutionForms(t *testing.T) {
	fakeGH(t, `#!/bin/sh
printf '%s' '{"workflows":[{"id":11,"name":"Android CI","path":".github/workflows/android.yml","state":"active"},{"id":12,"name":"Old Release","path":".github/workflows/release.yml","state":"disabled_manually"}]}'
`)
	for _, in := range []string{"Android CI", "android ci", "android", "ANDROID.YML", "Release", "Old Release", "release"} {
		want := "11"
		if strings.Contains(strings.ToLower(in), "release") {
			want = "12"
		}
		got, err := resolveWorkflow("a/b", in)
		if err != nil || got != want {
			t.Fatalf("%q -> %q, %v (want %s)", in, got, err, want)
		}
	}
	_, err := resolveWorkflow("a/b", "Nope")
	var nf *WorkflowNotFoundError
	if !errors.As(err, &nf) || len(nf.Known) != 2 || !strings.Contains(err.Error(), "Android CI") {
		t.Fatalf("not-found error should list what exists: %v", err)
	}
}

func TestSlowGHIsATimeoutNotRetried(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err := api(nil, 150*time.Millisecond, "repos/a/b")
	if !IsClass(err, ClassTimeout) {
		t.Fatalf("want timeout, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("a timeout was retried")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err = apiContext(ctx, nil, 5*time.Second, "repos/a/b"); !IsClass(err, ClassTimeout) {
		t.Fatalf("caller deadline should classify as timeout, got %v", err)
	}
}

func TestCompareCommitsDecodesAndLabels(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := "#!/bin/sh\ncase \"$2\" in *compare/aaa1111...bbb2222) cat <<'J'\n" +
		`{"status":"ahead","ahead_by":2,"commits":[{"sha":"1234567890abcdef","commit":{"message":"fix: a thing\n\nlonger body","author":{"name":"Ada","date":"2026-11-01T10:00:00Z"}}},{"sha":"abc","commit":{"message":"second","author":{"name":"Bob","date":"2026-11-02T10:00:00Z"}}}]}` +
		"\nJ\n;; *) echo 'gh: Not Found (HTTP 404)' >&2; exit 1;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	ahead, cs, err := CompareCommits(context.Background(), "o/r", "aaa1111", "bbb2222")
	if err != nil || ahead != 2 || len(cs) != 2 {
		t.Fatalf("ahead=%d commits=%d err=%v", ahead, len(cs), err)
	}
	if cs[0].Subject() != "fix: a thing" || cs[0].Short() != "1234567" || cs[1].Short() != "abc" {
		t.Fatalf("labels: %q %q", cs[0].Subject(), cs[0].Short())
	}
	if _, _, err := CompareCommits(context.Background(), "o/r", "x", "y"); !IsClass(err, ClassMissing) {
		t.Fatalf("a missing base must be a classified 404, got %v", err)
	}
}
