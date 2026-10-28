package ghapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListUserReposAllPagesAndAffiliations(t *testing.T) {
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GHAPK_TEST_ARGS", args)
	script := `#!/bin/sh
printf '%s\n' "$@" > "$GHAPK_TEST_ARGS"
printf '%s' '[{"full_name":"owner/private","private":true},{"full_name":"collab/shared"}][{"full_name":"org/project","private":true}]'
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	repos, err := ListUserRepos()
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 3 || repos[0].FullName != "owner/private" || !repos[0].Private || repos[1].FullName != "collab/shared" || repos[2].FullName != "org/project" || !repos[2].Private {
		t.Fatalf("bad pages: %+v", repos)
	}
	data, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"user/repos?", "per_page=100", "affiliation=owner,collaborator,organization_member", "--paginate"} {
		if !strings.Contains(string(data), a) {
			t.Fatalf("missing %s: %s", a, data)
		}
	}
	if strings.Contains(string(data), "--slurp") {
		t.Fatal("requires a newer gh")
	}
}

func TestListUserReposFailures(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		wantError    bool
	}{
		{"empty list", `printf '%s' '[]'`, false},
		{"invalid second page", `printf '%s' '[{"full_name":"ok/repo"}]invalid'`, true},
		{"empty response", `printf ''`, true},
		{"api error", `printf 'authentication required\n' >&2; exit 1`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\n"+tc.script+"\n"), 0755); err != nil {
				t.Fatal(err)
			}
			_, err := ListUserRepos()
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
