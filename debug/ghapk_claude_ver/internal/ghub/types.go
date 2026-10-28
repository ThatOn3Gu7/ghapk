package ghub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Repo is the subset of a repository ghapk shows.
type Repo struct {
	FullName      string    `json:"full_name"`
	Private       bool      `json:"private"`
	DefaultBranch string    `json:"default_branch"`
	Description   string    `json:"description"`
	PushedAt      time.Time `json:"pushed_at"`
	HTMLURL       string    `json:"html_url"`
}

// Run is one workflow run.
type Run struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"` // workflow name
	DisplayTitle string    `json:"display_title"`
	HeadBranch   string    `json:"head_branch"`
	HeadSHA      string    `json:"head_sha"`
	Status       string    `json:"status"`     // queued, in_progress, completed
	Conclusion   string    `json:"conclusion"` // success, failure, cancelled, ...
	Event        string    `json:"event"`
	RunNumber    int       `json:"run_number"`
	RunAttempt   int       `json:"run_attempt"`
	WorkflowID   int64     `json:"workflow_id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	StartedAt    time.Time `json:"run_started_at"`
	HTMLURL      string    `json:"html_url"`
	Actor        struct {
		Login string `json:"login"`
	} `json:"actor"`
}

// Short is the 7-character commit id.
func (r Run) Short() string {
	if len(r.HeadSHA) > 7 {
		return r.HeadSHA[:7]
	}
	return r.HeadSHA
}

// Active reports whether the run is still going.
func (r Run) Active() bool { return r.Status != "completed" && r.Status != "" }

// OK reports whether the run finished successfully.
func (r Run) OK() bool { return r.Conclusion == "success" }

// Duration is how long the run took (or has been running).
func (r Run) Duration() time.Duration {
	start := r.StartedAt
	if start.IsZero() {
		start = r.CreatedAt
	}
	end := r.UpdatedAt
	if r.Active() || end.IsZero() {
		end = time.Now()
	}
	if end.Before(start) {
		return 0
	}
	return end.Sub(start)
}

// Artifact is one uploaded build artifact.
type Artifact struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Size        int64     `json:"size_in_bytes"`
	Expired     bool      `json:"expired"`
	Digest      string    `json:"digest"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	WorkflowRun struct {
		ID         int64  `json:"id"`
		HeadBranch string `json:"head_branch"`
		HeadSHA    string `json:"head_sha"`
	} `json:"workflow_run"`
}

// Release and Asset describe published releases (the other APK source).
type Release struct {
	ID          int64     `json:"id"`
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []Asset   `json:"assets"`
}

// Asset is one file attached to a release.
type Asset struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	State string `json:"state"`
}

// Workflow is a workflow definition.
type Workflow struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
}

// File returns the workflow's file name (the thing --workflow takes).
func (w Workflow) File() string {
	if i := strings.LastIndexByte(w.Path, '/'); i >= 0 {
		return w.Path[i+1:]
	}
	return w.Path
}

// Branch is a repository branch.
type Branch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
}

// Commit is one entry of a compare.
type Commit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string    `json:"name"`
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

// Subject is the first line of the commit message.
func (c Commit) Subject() string {
	m := c.Commit.Message
	if i := strings.IndexByte(m, '\n'); i >= 0 {
		m = m[:i]
	}
	return m
}

// Job is one job of a run, for the "why did it fail" view.
type Job struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Steps      []struct {
		Name       string `json:"name"`
		Conclusion string `json:"conclusion"`
		Number     int    `json:"number"`
	} `json:"steps"`
}

// ---- endpoints --------------------------------------------------------------

// RateLimit returns remaining/limit/reset for the core budget. This call is
// free: it does not count against the limit.
func (c *Client) RateLimit(ctx context.Context) (remaining, limit int, reset time.Time, err error) {
	out, err := c.API(ctx, "rate_limit")
	if err != nil {
		return 0, 0, time.Time{}, err
	}
	var rl struct {
		Resources struct {
			Core struct {
				Limit, Remaining int
				Reset            int64
			} `json:"core"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(out, &rl); err != nil {
		return 0, 0, time.Time{}, err
	}
	c0 := rl.Resources.Core
	return c0.Remaining, c0.Limit, time.Unix(c0.Reset, 0), nil
}

// User returns the login gh is authenticated as.
func (c *Client) User(ctx context.Context) (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	b, _, err := c.Cached(ctx, "user")
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(b, &u); err != nil {
		return "", err
	}
	return u.Login, nil
}

// AuthOK reports whether gh is logged in (for the host in use).
func (c *Client) AuthOK(ctx context.Context) (bool, string) {
	args := []string{"auth", "status"}
	if c.Host != "" {
		args = append(args, "--hostname", c.Host)
	}
	out, se, rc := c.Run(ctx, c.Timeout, args...)
	return rc == 0, strings.TrimSpace(string(out) + "\n" + se)
}

// Token returns gh's token (only used to feed curl as a download fallback).
func (c *Client) Token(ctx context.Context) string {
	out, _, rc := c.Run(ctx, c.Timeout, "auth", "token")
	if rc != 0 {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GetRepo fetches repository metadata (canonical name, default branch).
func (c *Client) GetRepo(ctx context.Context, repo string) (Repo, error) {
	var r Repo
	err := c.GetJSON(ctx, "repos/"+repo, &r)
	return r, err
}

// MyRepos lists the repositories you most recently pushed to, for the picker.
func (c *Client) MyRepos(ctx context.Context, n int) ([]Repo, error) {
	var rs []Repo
	q := fmt.Sprintf("user/repos?sort=pushed&per_page=%d&affiliation=owner,collaborator,organization_member", n)
	err := c.GetJSON(ctx, q, &rs)
	return rs, err
}

// RunsQuery filters the run list.
type RunsQuery struct {
	Branch   string
	Workflow string // file name or numeric id
	Status   string // "", success, failure, in_progress, ...
	Limit    int
}

// Runs lists recent workflow runs, newest first.
func (c *Client) Runs(ctx context.Context, repo string, q RunsQuery) ([]Run, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	v := url.Values{}
	v.Set("per_page", strconv.Itoa(q.Limit))
	if q.Branch != "" {
		v.Set("branch", q.Branch)
	}
	if q.Status != "" {
		v.Set("status", q.Status)
	}
	base := "repos/" + repo + "/actions/runs"
	if q.Workflow != "" {
		w, err := c.ResolveWorkflow(ctx, repo, q.Workflow)
		if err != nil {
			return nil, err
		}
		base = "repos/" + repo + "/actions/workflows/" + url.PathEscape(w) + "/runs"
	}
	var out struct {
		Runs []Run `json:"workflow_runs"`
	}
	if err := c.GetJSON(ctx, base+"?"+v.Encode(), &out); err != nil {
		return nil, err
	}
	return out.Runs, nil
}

// GetRun fetches one run.
func (c *Client) GetRun(ctx context.Context, repo string, id int64) (Run, error) {
	var r Run
	err := c.GetJSON(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", repo, id), &r)
	return r, err
}

// WorkflowNotFound means a workflow named in the settings does not exist in the
// repository. It is not an HTTP failure of the repo itself, so callers can fall
// back to "all workflows" instead of reporting the repository as missing.
type WorkflowNotFound struct {
	Repo, Name string
	Known      []string // file names that do exist
}

func (e *WorkflowNotFound) Error() string {
	s := fmt.Sprintf("workflow %q does not exist in %s", e.Name, e.Repo)
	if len(e.Known) > 0 {
		k := e.Known
		if len(k) > 4 {
			k = append(k[:4:4], "…")
		}
		s += " (it has: " + strings.Join(k, ", ") + ")"
	}
	return s
}

// ResolveWorkflow turns whatever the user configured — a file name, a numeric
// id, a path like .github/workflows/ci.yml, or the workflow's display name —
// into something the workflows/{id}/runs endpoint accepts. GitHub answers 404
// for display names, so those are looked up in the repository's workflow list.
func (c *Client) ResolveWorkflow(ctx context.Context, repo, w string) (string, error) {
	w = strings.TrimSpace(w)
	if w == "" {
		return "", nil
	}
	if _, err := strconv.ParseInt(w, 10, 64); err == nil {
		return w, nil
	}
	list, err := c.Workflows(ctx, repo)
	if err != nil {
		return "", err
	}
	base := w
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	var known []string
	for _, wf := range list {
		known = append(known, wf.File())
	}
	for _, wf := range list { // exact file name or path first
		if strings.EqualFold(wf.File(), base) || strings.EqualFold(wf.Path, w) {
			return wf.File(), nil
		}
	}
	for _, wf := range list { // then the display name, or the name without its extension
		stem := strings.TrimSuffix(strings.TrimSuffix(wf.File(), ".yml"), ".yaml")
		if strings.EqualFold(wf.Name, w) || strings.EqualFold(stem, w) {
			return wf.File(), nil
		}
	}
	return "", &WorkflowNotFound{Repo: repo, Name: w, Known: known}
}

// Workflows lists the repository's workflows.
func (c *Client) Workflows(ctx context.Context, repo string) ([]Workflow, error) {
	var out struct {
		Workflows []Workflow `json:"workflows"`
	}
	err := c.GetJSON(ctx, "repos/"+repo+"/actions/workflows?per_page=100", &out)
	return out.Workflows, err
}

// Branches lists branches (up to 3 pages of 100).
func (c *Client) Branches(ctx context.Context, repo string) ([]Branch, error) {
	var all []Branch
	for page := 1; page <= 3; page++ {
		var bs []Branch
		if err := c.GetJSON(ctx, fmt.Sprintf("repos/%s/branches?per_page=100&page=%d", repo, page), &bs); err != nil {
			if page > 1 {
				break
			}
			return nil, err
		}
		all = append(all, bs...)
		if len(bs) < 100 {
			break
		}
	}
	return all, nil
}

// RunArtifacts lists the live artifacts of one run.
func (c *Client) RunArtifacts(ctx context.Context, repo string, runID int64) ([]Artifact, error) {
	var out struct {
		Artifacts []Artifact `json:"artifacts"`
	}
	if err := c.GetJSON(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/artifacts?per_page=100", repo, runID), &out); err != nil {
		return nil, err
	}
	var live []Artifact
	for _, a := range out.Artifacts {
		if !a.Expired {
			live = append(live, a)
		}
	}
	return live, nil
}

// ArtifactIndex is one request-per-page listing of the newest artifacts in the
// whole repository (up to 5 pages of 100). It is how ghapk knows which runs have
// something to download without one request per run. complete is true when it
// saw every artifact rather than hitting the page cap.
func (c *Client) ArtifactIndex(ctx context.Context, repo string, pages int) (arts []Artifact, complete bool, err error) {
	if pages <= 0 {
		pages = 5
	}
	for page := 1; page <= pages; page++ {
		var out struct {
			Artifacts []Artifact `json:"artifacts"`
		}
		e := c.GetJSON(ctx, fmt.Sprintf("repos/%s/actions/artifacts?per_page=100&page=%d", repo, page), &out)
		if e != nil {
			if page == 1 {
				return nil, false, e
			}
			return arts, false, nil
		}
		for _, a := range out.Artifacts {
			if !a.Expired {
				arts = append(arts, a)
			}
		}
		if len(out.Artifacts) < 100 {
			return arts, true, nil
		}
	}
	return arts, false, nil
}

// GetArtifact fetches one artifact's metadata (used to tell "expired" from
// "failed" after a download error).
func (c *Client) GetArtifact(ctx context.Context, repo string, id int64) (Artifact, error) {
	var a Artifact
	b, err := c.API(ctx, fmt.Sprintf("repos/%s/actions/artifacts/%d", repo, id))
	if err != nil {
		return a, err
	}
	return a, json.Unmarshal(b, &a)
}

// Releases lists published (non-draft) releases.
func (c *Client) Releases(ctx context.Context, repo string, n int) ([]Release, error) {
	var rs []Release
	if err := c.GetJSON(ctx, fmt.Sprintf("repos/%s/releases?per_page=%d", repo, n), &rs); err != nil {
		return nil, err
	}
	var out []Release
	for _, r := range rs {
		if !r.Draft {
			out = append(out, r)
		}
	}
	return out, nil
}

// Compare lists the commits between two shas (base...head).
func (c *Client) Compare(ctx context.Context, repo, base, head string) (ahead int, commits []Commit, err error) {
	var out struct {
		AheadBy int      `json:"ahead_by"`
		Commits []Commit `json:"commits"`
	}
	if err = c.GetJSON(ctx, fmt.Sprintf("repos/%s/compare/%s...%s", repo, base, head), &out); err != nil {
		return 0, nil, err
	}
	return out.AheadBy, out.Commits, nil
}

// Jobs lists a run's jobs and steps.
func (c *Client) Jobs(ctx context.Context, repo string, runID int64) ([]Job, error) {
	var out struct {
		Jobs []Job `json:"jobs"`
	}
	err := c.GetJSON(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/jobs?per_page=100", repo, runID), &out)
	return out.Jobs, err
}

// RepoConfig fetches .github/ghapk.conf from the repository, "" if absent.
func (c *Client) RepoConfig(ctx context.Context, repo, path string) string {
	var out struct {
		Content string `json:"content"`
	}
	if err := c.GetJSON(ctx, "repos/"+repo+"/contents/"+path, &out); err != nil {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(out.Content, "\n", ""))
	if err != nil {
		return ""
	}
	return string(b)
}

// FailureLog returns the failed-steps log of a run (gh run view --log-failed),
// trimmed to the last n lines.
func (c *Client) FailureLog(ctx context.Context, repo string, runID int64, n int) (string, error) {
	out, se, rc := c.Run(ctx, 60*time.Second, "run", "view", strconv.FormatInt(runID, 10), "-R", repo, "--log-failed")
	if rc != 0 {
		return "", fmt.Errorf("%s", strings.TrimSpace(se))
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}

// ParseRepo accepts OWNER/NAME, https URLs and git@ URLs, and returns the
// canonical OWNER/NAME plus the host ("" for github.com).
func ParseRepo(in string) (repo, host string, ok bool) {
	s := strings.TrimSpace(in)
	s = strings.TrimSuffix(strings.TrimRight(s, "/"), ".git")
	s = strings.TrimRight(s, "/")
	trim := func(n string) string { return strings.TrimSuffix(strings.TrimRight(n, "/"), ".git") }
	switch {
	case strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://"):
		u, err := url.Parse(s)
		if err != nil {
			return "", "", false
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return "", "", false
		}
		host = strings.TrimPrefix(u.Host, "www.")
		repo = parts[0] + "/" + trim(parts[1])
	case strings.HasPrefix(s, "git@"):
		rest := strings.TrimPrefix(s, "git@")
		i := strings.IndexByte(rest, ':')
		if i < 0 {
			return "", "", false
		}
		host = rest[:i]
		p := strings.Split(rest[i+1:], "/")
		if len(p) != 2 || p[0] == "" || p[1] == "" {
			return "", "", false
		}
		repo = p[0] + "/" + trim(p[1])
	default:
		p := strings.Split(s, "/")
		if len(p) != 2 || p[0] == "" || p[1] == "" || strings.ContainsAny(s, " \t:") {
			return "", "", false
		}
		repo = p[0] + "/" + trim(p[1])
	}
	if host == "github.com" {
		host = ""
	}
	return repo, host, true
}

// RepoFromDir asks gh which repository the current directory belongs to.
func (c *Client) RepoFromDir(ctx context.Context) (string, bool) {
	out, _, rc := c.Run(ctx, c.Timeout, "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner")
	s := strings.TrimSpace(string(out))
	return s, rc == 0 && s != ""
}
