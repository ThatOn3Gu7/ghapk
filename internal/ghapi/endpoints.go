package ghapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
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
	q := []string{}
	if workflow != "" {
		// a workflow "name" resolves to its id/path first
		wf, err := resolveWorkflow(repo, workflow)
		if err == nil && wf != "" {
			ep = "repos/" + repo + "/actions/workflows/" + wf + "/runs"
		}
	}
	if branch != "" {
		q = append(q, "branch="+url.QueryEscape(branch))
	}
	if limit <= 0 {
		limit = 20
	}
	q = append(q, fmt.Sprintf("per_page=%d", limit))
	if len(q) > 0 {
		ep += "?" + strings.Join(q, "&")
	}
	var r runsResp
	if err := api(&r, 45*time.Second, ep); err != nil {
		return nil, err
	}
	return r.WorkflowRuns, nil
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
	for _, w := range wfs {
		if strings.EqualFold(w.Name, wf) || strings.HasSuffix(w.Path, "/"+wf) {
			return fmt.Sprint(w.ID), nil
		}
	}
	for _, w := range wfs {
		if strings.Contains(strings.ToLower(w.Name), needle) {
			return fmt.Sprint(w.ID), nil
		}
	}
	return "", fmt.Errorf("no workflow matching %q", wf)
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
	dec := json.NewDecoder(strings.NewReader(out))
	var branches []Branch
	for {
		var page []Branch
		err = dec.Decode(&page)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decoding branches for %s: %w", repo, err)
		}
		branches = append(branches, page...)
	}
	return branches, nil
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

// ListUserRepos fetches repositories recently pushed by or accessible to the
// authenticated user to seed the repository picker.
func ListUserRepos(limit int) ([]Repo, error) {
	if limit <= 0 {
		limit = 40
	}
	var rs []Repo
	if err := api(&rs, defaultTimeout, fmt.Sprintf("user/repos?sort=pushed&per_page=%d", limit)); err != nil {
		return nil, err
	}
	return rs, nil
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
