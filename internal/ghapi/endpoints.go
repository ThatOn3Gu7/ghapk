package ghapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// GetRepo fetches repo metadata.
func GetRepo(repo string) (Repo, error) {
	var r Repo
	err := api(&r, defaultTimeout, "repos/"+repo)
	return r, err
}

// ListWorkflows mirrors `gh api repos/:repo/actions/workflows`.
func ListWorkflows(repo string) ([]Workflow, error) {
	var w workflowsResp
	if err := api(&w, defaultTimeout, "repos/"+repo+"/actions/workflows"); err != nil {
		return nil, err
	}
	var out []Workflow
	for _, wf := range w.Workflows {
		if wf.State == "disabled" || wf.State == "disabled_inactivity" {
			continue
		}
		out = append(out, wf)
	}
	return out, nil
}

// ListRuns mirrors the script's run scan: newest first, optionally filtered
// by workflow and/or branch, capped at limit.
func ListRuns(repo, workflow, branch string, limit int) ([]Run, error) {
	ep := "repos/" + repo + "/actions/runs"
	if workflow != "" {
		// A workflow name must resolve; falling back to every workflow would
		// silently widen the selection after a typo or API error.
		wf, err := resolveWorkflow(repo, workflow)
		if err != nil {
			return nil, err
		}
		if wf == "" {
			return nil, fmt.Errorf("no workflow matching %q", workflow)
		}
		ep = "repos/" + repo + "/actions/workflows/" + wf + "/runs"
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 1000 {
		limit = 1000
	}
	base := []string{}
	if branch != "" {
		base = append(base, "branch="+url.QueryEscape(branch))
	}
	var out []Run
	maxPages := (limit + 99) / 100
	for page := 1; page <= maxPages && len(out) < limit; page++ {
		perPage := limit - len(out)
		if perPage > 100 {
			perPage = 100
		}
		q := append([]string(nil), base...)
		q = append(q, fmt.Sprintf("per_page=%d", perPage), fmt.Sprintf("page=%d", page))
		var r runsResp
		if err := api(&r, 45*time.Second, ep+"?"+strings.Join(q, "&")); err != nil {
			return nil, err
		}
		out = append(out, r.WorkflowRuns...)
		if len(r.WorkflowRuns) < perPage {
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func resolveWorkflow(repo, wf string) (string, error) {
	// numeric id or .yml path can be used as-is
	if wf == "" {
		return "", nil
	}
	allNumeric := true
	for _, c := range wf {
		if c < '0' || c > '9' {
			allNumeric = false
			break
		}
	}
	if allNumeric || strings.HasSuffix(wf, ".yml") || strings.HasSuffix(wf, ".yaml") {
		return wf, nil
	}
	wfs, err := ListWorkflows(repo)
	if err != nil {
		return "", err
	}
	needle := strings.ToLower(wf)
	var partial []Workflow
	for _, w := range wfs {
		if strings.EqualFold(w.Name, wf) || strings.HasSuffix(w.Path, "/"+wf) {
			return fmt.Sprint(w.ID), nil
		}
		if strings.Contains(strings.ToLower(w.Name), needle) {
			partial = append(partial, w)
		}
	}
	switch len(partial) {
	case 0:
		return "", fmt.Errorf("no workflow matching %q", wf)
	case 1:
		return fmt.Sprint(partial[0].ID), nil
	default:
		names := make([]string, 0, len(partial))
		for _, w := range partial {
			names = append(names, w.Name)
		}
		sort.Strings(names)
		return "", fmt.Errorf("workflow %q is ambiguous: matches %s", wf, strings.Join(names, ", "))
	}
}

// FetchWorkflowFile downloads a workflow's YAML source via the contents
// API (raw Accept header), so the UI can preview what a workflow does.
// Some repos report unconventional paths ("dynamic/x/y"): first try the
// reported path, then fall back to .github/workflows/<base>.yml
func FetchWorkflowFile(repo, path string) (string, error) {
	path = strings.TrimPrefix(path, "/")
	if i := strings.LastIndex(path, "@"); i > 0 { // some APIs append @ref
		path = path[:i]
	}
	if path == "" {
		return "", fmt.Errorf("empty workflow path")
	}
	out, err := rawAPI(30*time.Second, "repos/"+repo+"/contents/"+path,
		"-H", "Accept: application/vnd.github.raw")
	if err == nil {
		return out, nil
	}
	if !strings.Contains(path, ".github/workflows/") {
		base := path
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		for _, ext := range []string{".yml", ".yaml"} {
			out2, err2 := rawAPI(30*time.Second,
				"repos/"+repo+"/contents/.github/workflows/"+base+ext,
				"-H", "Accept: application/vnd.github.raw")
			if err2 == nil {
				return out2, nil
			}
		}
	}
	return "", fmt.Errorf("workflow file not found in this repo")
}

// ListArtifacts mirrors `gh api repos/:repo/actions/runs/:id/artifacts`.
func ListArtifacts(repo string, runID int64) ([]Artifact, error) {
	var a artifactsResp
	if err := api(&a, defaultTimeout, fmt.Sprintf("repos/%s/actions/runs/%d/artifacts", repo, runID)); err != nil {
		return nil, err
	}
	return a.Artifacts, nil
}

// ListBranches mirrors the --branch list path.
func ListBranches(repo string) ([]Branch, error) {
	// Older gh versions lack --slurp; --paginate emits one JSON array per
	// page, so decode the concatenated arrays as a stream.
	out, err := rawAPI(defaultTimeout, "repos/"+repo+"/branches?per_page=100", "--paginate")
	if err != nil {
		return nil, err
	}
	return decodePages[Branch](out, "branches for "+repo)
}

// ListReleases fetches recent releases (for the releases fallback source).
func ListReleases(repo string, limit int) ([]Release, error) {
	if limit <= 0 {
		limit = 5
	}
	var r []Release
	if err := api(&r, defaultTimeout, fmt.Sprintf("repos/%s/releases?per_page=%d", repo, limit)); err != nil {
		return nil, err
	}
	return r, nil
}

// ListUserRepos returns all repositories accessible to the authenticated user,
// including owned, collaborator, and organization repositories.
func ListUserRepos() ([]Repo, error) {
	const endpoint = "user/repos?sort=pushed&direction=desc&per_page=100&affiliation=owner,collaborator,organization_member"
	out, err := rawAPI(2*time.Minute, endpoint, "--paginate")
	if err != nil {
		return nil, err
	}
	return decodePages[Repo](out, "authenticated repositories")
}

// gh --paginate emits consecutive JSON arrays; decode all of them without
// requiring --slurp, which is absent on older versions of gh.
func decodePages[T any](out, what string) ([]T, error) {
	dec := json.NewDecoder(strings.NewReader(out))
	var result []T
	pages := 0
	for {
		var page []T
		err := dec.Decode(&page)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decoding %s: %w", what, err)
		}
		result = append(result, page...)
		pages++
	}
	if pages == 0 {
		return nil, fmt.Errorf("decoding %s: empty response", what)
	}
	return result, nil
}

// SearchRepos powers the repo picker's suggestions.
func SearchRepos(query string, limit int) ([]SearchHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	var sr searchReposResp
	if err := api(&sr, defaultTimeout,
		"search/repositories?q="+url.QueryEscape(query)+fmt.Sprintf("&per_page=%d", limit)); err != nil {
		return nil, err
	}
	out := make([]SearchHit, 0, len(sr.Items))
	for _, it := range sr.Items {
		out = append(out, SearchHit{FullName: it.FullName, Description: it.Description, Stars: it.Stars})
	}
	return out, nil
}

type SearchHit struct {
	FullName    string
	Description string
	Stars       int
}
