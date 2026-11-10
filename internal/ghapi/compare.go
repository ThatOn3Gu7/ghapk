package ghapi

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Commit is one entry of a compare: the fields the install preview shows.
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
	return strings.TrimSpace(m)
}

// Short is the abbreviated SHA.
func (c Commit) Short() string {
	if len(c.SHA) > 7 {
		return c.SHA[:7]
	}
	return c.SHA
}

type compareResp struct {
	Status  string   `json:"status"`
	AheadBy int      `json:"ahead_by"`
	Commits []Commit `json:"commits"`
}

// CompareCommits lists the commits between two SHAs (base...head), oldest
// first. ahead is the full count even when GitHub trims the list. It is the
// source of "N commits since your last install"; callers treat any error as
// "no summary" because it is a courtesy, never a requirement.
func CompareCommits(ctx context.Context, repo, base, head string) (ahead int, commits []Commit, err error) {
	var out compareResp
	endpoint := fmt.Sprintf("repos/%s/compare/%s...%s", repo, url.PathEscape(base), url.PathEscape(head))
	if err = apiContext(ctx, &out, defaultTimeout, endpoint); err != nil {
		return 0, nil, err
	}
	return out.AheadBy, out.Commits, nil
}
