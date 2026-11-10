package ghapi

import (
	"context"
	"fmt"
	"time"
)

// Step is one step of a job.
type Step struct {
	Number     int    `json:"number"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// Job is one entry of GET .../actions/runs/{id}/jobs.
type Job struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	Steps      []Step `json:"steps"`
}

// Failed reports whether the job ended badly.
func (j Job) Failed() bool {
	return j.Conclusion == "failure" || j.Conclusion == "timed_out" || j.Conclusion == "startup_failure"
}

// FailedStep returns the first step that failed, if any.
func (j Job) FailedStep() (Step, bool) {
	for _, s := range j.Steps {
		if s.Conclusion == "failure" || s.Conclusion == "timed_out" {
			return s, true
		}
	}
	return Step{}, false
}

type jobsResp struct {
	Jobs []Job `json:"jobs"`
}

// ListJobs returns the jobs of one workflow run.
func ListJobs(ctx context.Context, repo string, runID int64) ([]Job, error) {
	var out jobsResp
	if err := apiContext(ctx, &out, defaultTimeout, fmt.Sprintf("repos/%s/actions/runs/%d/jobs?per_page=100", repo, runID)); err != nil {
		return nil, err
	}
	return out.Jobs, nil
}

// JobLog returns the plain-text log of one job. GitHub answers with a redirect
// that gh follows. Logs can be large, so the caller trims what it keeps.
func JobLog(ctx context.Context, repo string, jobID int64) (string, error) {
	out, err := runGH(ctx, 40*time.Second, fmt.Sprintf("repos/%s/actions/jobs/%d/logs", repo, jobID))
	if err != nil {
		return "", err
	}
	return string(out), nil
}
