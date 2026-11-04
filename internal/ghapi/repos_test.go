package ghapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListUserReposPagePaginationAndAffiliations(t *testing.T) {
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GHAPK_TEST_ARGS", args)
	script := `#!/bin/sh
printf '%s\n' "$@" > "$GHAPK_TEST_ARGS"
printf '%s' '[{"full_name":"owner/private","private":true},{"full_name":"collab/shared"}]'
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	repos, hasMore, err := ListUserReposPage(2, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].FullName != "owner/private" || !repos[0].Private || repos[1].FullName != "collab/shared" || hasMore {
		t.Fatalf("bad page: %+v hasMore=%v", repos, hasMore)
	}
	data, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"user/repos?", "per_page=50", "page=2", "affiliation=owner,collaborator,organization_member"} {
		if !strings.Contains(string(data), a) {
			t.Fatalf("missing %s: %s", a, data)
		}
	}
	if strings.Contains(string(data), "--paginate") || strings.Contains(string(data), "--slurp") {
		t.Fatal("single page should not paginate or require a newer gh")
	}
}

func TestListUserReposPageHasMoreOnFullPage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
printf '%s' '[{"full_name":"one/repo"},{"full_name":"two/repo"}]'
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	repos, hasMore, err := ListUserReposPage(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || !hasMore {
		t.Fatalf("full page = %+v hasMore=%v, want 2 and true", repos, hasMore)
	}
}

func TestListUserReposPageFailures(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		wantError    bool
	}{
		{"empty list", `printf '%s' '[]'`, false},
		{"invalid json", `printf '%s' '[{"full_name":"ok/repo"}]invalid'`, true},
		{"empty response", `printf ''`, true},
		{"api error", `printf 'authentication required\n' >&2; exit 1`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+tc.script+"\n"), 0755); err != nil {
				t.Fatal(err)
			}
			_, _, err := ListUserReposPage(1, 100)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
