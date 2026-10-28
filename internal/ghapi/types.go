package ghapi

import "time"

// Repo is the subset of GET /repos/{owner}/{repo} the UI needs.
type Repo struct {
	FullName      string `json:"full_name"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Stars         int    `json:"stargazers_count"`
	HTMLURL       string `json:"html_url"`
}

// Workflow is one entry of GET /repos/{owner}/{repo}/actions/workflows.
type Workflow struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
}

// Run is one entry of GET /repos/{owner}/{repo}/actions/runs.
type Run struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	DisplayTitle string    `json:"display_title"`
	HeadBranch   string    `json:"head_branch"`
	HeadSHA      string    `json:"head_sha"`
	Status       string    `json:"status"`     // queued, in_progress, completed
	Conclusion   string    `json:"conclusion"` // success, failure, cancelled...
	Event        string    `json:"event"`
	RunNumber    int       `json:"run_number"`
	HTMLURL      string    `json:"html_url"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	WorkflowID   int64     `json:"workflow_id"`
}

func (r Run) Finished() bool { return r.Status == "completed" }
func (r Run) Active() bool   { return r.Status == "in_progress" || r.Status == "queued" }

// Artifact is one entry of GET .../actions/runs/{run_id}/artifacts.
type Artifact struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	SizeInBytes        int64     `json:"size_in_bytes"`
	Digest             string    `json:"digest"`
	Expired            bool      `json:"expired"`
	CreatedAt          time.Time `json:"created_at"`
	ExpiresAt          time.Time `json:"expires_at"`
	ArchiveDownloadURL string    `json:"archive_download_url"`
	WorkflowRun        struct {
		ID int64 `json:"id"`
	} `json:"workflow_run"`

	// Filled in locally, not from the API:
	Score    int  `json:"-"`
	Rejected bool `json:"-"`
}

// Branch is one entry of GET /repos/{owner}/{repo}/branches.
type Branch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
}

// Release asset (for the releases fallback).
type ReleaseAsset struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
	URL    string `json:"url"`
	// Filled locally:
	Score    int  `json:"-"`
	Rejected bool `json:"-"`
}

type Release struct {
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	PublishedAt time.Time      `json:"published_at"`
	Prerelease  bool           `json:"prerelease"`
	Assets      []ReleaseAsset `json:"assets"`
	HTMLURL     string         `json:"html_url"`
}

type workflowsResp struct {
	Workflows []Workflow `json:"workflows"`
}

type runsResp struct {
	TotalCount   int   `json:"total_count"`
	WorkflowRuns []Run `json:"workflow_runs"`
}

type artifactsResp struct {
	TotalCount int        `json:"total_count"`
	Artifacts  []Artifact `json:"artifacts"`
}

type searchReposResp struct {
	Items []struct {
		FullName    string `json:"full_name"`
		Description string `json:"description"`
		Stars       int    `json:"stargazers_count"`
	} `json:"items"`
}
