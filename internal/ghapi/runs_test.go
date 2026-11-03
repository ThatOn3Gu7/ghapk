package ghapi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeGH(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GHAPK_TEST_ARGS", args)
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return args
}

func TestListRunsPaginatesPast100(t *testing.T) {
	dir := t.TempDir()
	page1 := filepath.Join(dir, "page1.json")
	page2 := filepath.Join(dir, "page2.json")
	writeRuns := func(path string, start, n int) {
		var b strings.Builder
		b.WriteString(`{"workflow_runs":[`)
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"id":%d,"run_number":%d}`, start+i, start+i)
		}
		b.WriteString(`]}`)
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeRuns(page1, 1, 100)
	writeRuns(page2, 101, 20)
	t.Setenv("GHAPK_TEST_PAGE1", page1)
	t.Setenv("GHAPK_TEST_PAGE2", page2)
	args := fakeGH(t, `#!/bin/sh
printf '%s\n' "$@" >> "$GHAPK_TEST_ARGS"
case "$*" in
  *page=1*) cat "$GHAPK_TEST_PAGE1" ;;
  *) cat "$GHAPK_TEST_PAGE2" ;;
esac
`)

	runs, err := ListRuns("acme/rocket", "", "", 120)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 120 {
		t.Fatalf("got %d runs, want 120", len(runs))
	}
	if runs[0].ID != 1 || runs[119].ID != 120 {
		t.Fatalf("unexpected run order: first=%d last=%d", runs[0].ID, runs[119].ID)
	}
	data, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"per_page=100", "page=1", "per_page=20", "page=2"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q in gh calls: %s", want, data)
		}
	}
}

func TestListRunsRejectsUnknownWorkflow(t *testing.T) {
	fakeGH(t, `#!/bin/sh
printf '%s' '{"workflows":[]}'
`)
	if _, err := ListRuns("acme/rocket", "Deployy", "", 20); err == nil || !strings.Contains(err.Error(), "no workflow matching") {
		t.Fatalf("unknown workflow error = %v", err)
	}
}

func TestResolveWorkflowRejectsAmbiguousPartialMatch(t *testing.T) {
	fakeGH(t, `#!/bin/sh
printf '%s' '{"workflows":[{"id":1,"name":"Deploy Staging","path":".github/workflows/deploy-staging.yml"},{"id":2,"name":"Deploy Production","path":".github/workflows/deploy-production.yml"}]}'
`)
	if _, err := resolveWorkflow("acme/rocket", "Deploy"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous workflow error = %v", err)
	}
}
