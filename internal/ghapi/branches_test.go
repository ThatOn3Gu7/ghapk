package ghapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListBranchesPaginates(t *testing.T) {
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GHAPK_TEST_ARGS", args)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$GHAPK_TEST_ARGS\"\nprintf '%s' '[{\"name\":\"main\"}][{\"name\":\"feature/two\",\"protected\":true}]'\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	branches, err := ListBranches("acme/rocket")
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 || branches[1].Name != "feature/two" || !branches[1].Protected {
		t.Fatalf("wrong pages: %+v", branches)
	}
	data, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{"repos/acme/rocket/branches?per_page=100", "--paginate"} {
		if !strings.Contains(string(data), arg) {
			t.Fatalf("missing %s: %s", arg, data)
		}
	}
}

func TestListRunsUsesBranchQuery(t *testing.T) {
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GHAPK_TEST_ARGS", args)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$GHAPK_TEST_ARGS\"\nprintf '%s' '{\"workflow_runs\":[]}'\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := ListRuns("acme/rocket", "", "feature/slash", 20); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "branch=feature%2Fslash") {
		t.Fatalf("branch not sent to GitHub: %s", data)
	}
}

func TestListBranchesRejectsBadPage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nprintf '%s' '[{\"name\":\"main\"}]not-json'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := ListBranches("acme/rocket"); err == nil {
		t.Fatal("invalid later page accepted")
	}
}
