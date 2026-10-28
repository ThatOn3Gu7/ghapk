// Package ghub talks to GitHub by shelling out to the `gh` CLI. Using gh (not
// net/http) means authentication, proxies and DNS all work exactly the way they
// already do for the user's gh — which matters on Termux, where Go's own
// resolver has no /etc/resolv.conf to read.
//
// Everything in here is the Go form of the shell script's "gateway": retry with
// full-jitter backoff, rate-limit waits, error classification, and an ETag
// cache so a repeated request that GitHub answers with 304 costs nothing
// against the rate limit.
package ghub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"ghapk/internal/exit"
)

// Class is what kind of failure a gh call ended in.
type Class string

const (
	ClassOK          Class = "ok"
	ClassAuth        Class = "auth"
	ClassRateLimit   Class = "ratelimit"
	ClassForbidden   Class = "forbidden"
	ClassNotModified Class = "notmodified"
	ClassMissing     Class = "missing"
	ClassGone        Class = "gone"
	ClassServer      Class = "server"
	ClassClient      Class = "client"
	ClassNetwork     Class = "network"
	ClassTimeout     Class = "timeout"
	ClassUnknown     Class = "unknown"
)

// Error is a classified gh failure.
type Error struct {
	Class    Class
	Status   int
	Msg      string
	Endpoint string    // the API path that failed, without the query ("repos/a/b/actions/runs")
	Reset    time.Time // rate limit reset, when known
}

func (e *Error) Error() string {
	switch e.Class {
	case ClassAuth:
		return "GitHub CLI is not authenticated (run: gh auth login)"
	case ClassRateLimit:
		if !e.Reset.IsZero() {
			return "GitHub rate limit exhausted, resets in " + time.Until(e.Reset).Round(time.Second).String()
		}
		return "GitHub rate limit exhausted"
	case ClassForbidden:
		return "GitHub refused access (token lacks the scope, or no access to this repository)"
	case ClassMissing:
		if e.Endpoint != "" {
			return "not found on GitHub: " + e.Endpoint
		}
		return "not found on GitHub (wrong name, or no access)"
	case ClassGone:
		return "gone: GitHub no longer has it (expired)"
	case ClassNetwork:
		return "network problem reaching GitHub"
	case ClassTimeout:
		return "GitHub took too long to answer"
	case ClassServer:
		return "GitHub had a server error"
	}
	if e.Msg != "" {
		return e.Msg
	}
	return string(e.Class)
}

// Code is the process exit code for this error.
func (e *Error) Code() int {
	switch e.Class {
	case ClassAuth:
		return exit.Auth
	case ClassRateLimit:
		return exit.RateLimit
	case ClassForbidden:
		return exit.Scope
	case ClassGone:
		return exit.Gone
	case ClassServer, ClassNetwork, ClassTimeout:
		return exit.Network
	}
	return exit.API
}

// IsClass reports whether err is a classified error of class c.
func IsClass(err error, c Class) bool {
	var e *Error
	return errors.As(err, &e) && e.Class == c
}

// Stats counts what the client did, for the status line and the summary.
type Stats struct {
	Calls, Retries, Hits, Misses int
}

// Client runs gh.
type Client struct {
	Bin      string
	Host     string // GH_HOST for GitHub Enterprise; "" for github.com
	CacheDir string
	Timeout  time.Duration
	Retries  int
	NoCache  bool
	// Log receives one line per notable event (already redacted).
	Log func(string)
	// OnWait is told when the client is sleeping (rate limit, backoff).
	OnWait func(d time.Duration, why string)

	mu    sync.Mutex
	stats Stats
}

// New returns a client with the defaults the shell script used.
func New(cacheDir string) *Client {
	t := 30 * time.Second
	if v, _ := strconv.Atoi(os.Getenv("GHAPK_API_TIMEOUT")); v > 0 {
		t = time.Duration(v) * time.Second
	}
	r := 4
	if v, err := strconv.Atoi(os.Getenv("GHAPK_API_RETRIES")); err == nil && v >= 0 {
		r = v
	}
	return &Client{Bin: "gh", CacheDir: cacheDir, Timeout: t, Retries: r,
		NoCache: os.Getenv("GHAPK_NO_HTTP_CACHE") == "1"}
}

// Stats returns a snapshot of the counters.
func (c *Client) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

func (c *Client) bump(f func(*Stats)) {
	c.mu.Lock()
	f(&c.stats)
	c.mu.Unlock()
}

func (c *Client) logf(format string, a ...any) {
	if c.Log != nil {
		c.Log(Redact(fmt.Sprintf(format, a...)))
	}
}

var (
	reTok1 = regexp.MustCompile(`(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]+`)
	reTok2 = regexp.MustCompile(`github_pat_[A-Za-z0-9_]+`)
	reAuth = regexp.MustCompile(`(?i)(authorization:|bearer)\s*\S+`)
	reTok3 = regexp.MustCompile(`(?i)token[=:]\s*\S+`)
)

// Redact removes anything that looks like a credential from a log line.
func Redact(s string) string {
	s = reAuth.ReplaceAllString(s, "$1 [redacted]")
	s = reTok1.ReplaceAllString(s, "[redacted-token]")
	s = reTok2.ReplaceAllString(s, "[redacted-token]")
	return reTok3.ReplaceAllString(s, "token [redacted]")
}

// Available reports whether the gh binary can be found.
func (c *Client) Available() (string, bool) {
	p, err := exec.LookPath(c.Bin)
	return p, err == nil
}

func (c *Client) env() []string {
	env := append(os.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1", "GH_NO_UPDATE_NOTIFIER=1", "GH_SPINNER_DISABLED=1")
	if c.Host != "" && c.Host != "github.com" {
		env = append(env, "GH_HOST="+c.Host)
	}
	return env
}

func (c *Client) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	cmd.Env = c.env()
	return cmd
}

// Run executes gh once and returns stdout, stderr and the exit code. It never
// retries; callers that want the gateway use API.
func (c *Client) Run(ctx context.Context, timeout time.Duration, args ...string) (stdout []byte, stderr string, rc int) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var so, se bytes.Buffer
	cmd := c.command(ctx, args...)
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	rc = 0
	if err != nil {
		var ee *exec.ExitError
		switch {
		case errors.As(err, &ee):
			rc = ee.ExitCode()
		case ctx.Err() != nil:
			rc = 124
		default:
			rc = 127
			se.WriteString(err.Error())
		}
	}
	if ctx.Err() != nil && rc != 0 {
		rc = 124
	}
	return so.Bytes(), se.String(), rc
}

var reStatus = regexp.MustCompile(`HTTP ([0-9]{3})`)

var (
	reRate = regexp.MustCompile(`(?i)rate.?limit|secondary|abuse`)
	reNet  = regexp.MustCompile(`(?i)no such host|dial tcp|connection (refused|reset)|network is unreachable|i/o timeout|tls|certificate|temporary failure|EOF`)
)

// Classify maps a finished gh call to a Class, the way the shell gateway did:
// the HTTP status is read from stderr ("gh: ... (HTTP 404)").
func Classify(rc int, stderr string) (Class, int) {
	status := 0
	if m := reStatus.FindAllStringSubmatch(stderr, -1); len(m) > 0 {
		status, _ = strconv.Atoi(m[len(m)-1][1])
	}
	if rc == 124 || rc == 137 {
		return ClassTimeout, status
	}
	switch {
	case status == 401:
		return ClassAuth, status
	case status == 403:
		if reRate.MatchString(stderr) {
			return ClassRateLimit, status
		}
		return ClassForbidden, status
	case status == 304:
		return ClassNotModified, status
	case status == 429:
		return ClassRateLimit, status
	case status == 404:
		return ClassMissing, status
	case status == 410:
		return ClassGone, status
	case status >= 500:
		return ClassServer, status
	case status >= 400:
		return ClassClient, status
	}
	if reNet.MatchString(stderr) {
		return ClassNetwork, status
	}
	if rc == 0 {
		return ClassOK, status
	}
	if rc == 127 {
		return ClassUnknown, status
	}
	return ClassUnknown, status
}

// endpointOf picks the API path out of `gh api` arguments (skipping flags and
// their values) and drops the query string.
func endpointOf(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-H" || a == "-f" || a == "-F" || a == "-X" || a == "--jq" || a == "-q" || a == "--method":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			if j := strings.IndexByte(a, '?'); j >= 0 {
				a = a[:j]
			}
			return a
		}
	}
	return ""
}

func backoff(attempt int) time.Duration {
	capS := 1 << uint(attempt+1)
	if capS > 30 {
		capS = 30
	}
	s := rand.Intn(capS)
	if s < 1 {
		s = 1
	}
	return time.Duration(s) * time.Second
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// API is the gateway: `gh api <args>` with classification, retry with
// full-jitter backoff, and rate-limit waits. It returns stdout of the final
// attempt. Auth, forbidden, missing, gone, client errors and 304 are never
// retried.
func (c *Client) API(ctx context.Context, args ...string) ([]byte, error) {
	full := append([]string{"api"}, args...)
	attempt := 0
	for {
		attempt++
		c.bump(func(s *Stats) { s.Calls++ })
		out, se, rc := c.Run(ctx, c.Timeout, full...)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		cls, status := Classify(rc, se)
		if se = strings.TrimSpace(se); se != "" {
			first := se
			if len(first) > 200 {
				first = first[:200]
			}
			c.logf("gh api %s -> rc=%d %s", strings.Join(args, " "), rc, strings.ReplaceAll(first, "\n", " "))
		}
		switch cls {
		case ClassOK:
			return out, nil
		case ClassRateLimit:
			wait, reset := c.rateWait(ctx)
			if (wait > 120*time.Second || wait <= 0) && attempt > c.Retries {
				return nil, &Error{Class: ClassRateLimit, Status: status, Msg: se, Endpoint: endpointOf(args), Reset: reset}
			}
			if wait > 120*time.Second {
				wait = 120 * time.Second
			}
			if wait < time.Second {
				wait = backoff(attempt)
			}
			c.bump(func(s *Stats) { s.Retries++ })
			if c.OnWait != nil {
				c.OnWait(wait, "rate limited")
			}
			if err := sleep(ctx, wait); err != nil {
				return nil, err
			}
		case ClassServer, ClassNetwork, ClassTimeout, ClassUnknown:
			if attempt > c.Retries {
				return nil, &Error{Class: cls, Status: status, Msg: se, Endpoint: endpointOf(args)}
			}
			d := backoff(attempt)
			c.bump(func(s *Stats) { s.Retries++ })
			if c.OnWait != nil {
				c.OnWait(d, "retrying")
			}
			if err := sleep(ctx, d); err != nil {
				return nil, err
			}
		default:
			return out, &Error{Class: cls, Status: status, Msg: se, Endpoint: endpointOf(args)}
		}
	}
}

// rateWait asks GitHub when the core limit resets. The rate_limit endpoint is
// free: it does not count against the budget.
func (c *Client) rateWait(ctx context.Context) (time.Duration, time.Time) {
	out, _, rc := c.Run(ctx, c.Timeout, "api", "rate_limit")
	if rc != 0 {
		return 0, time.Time{}
	}
	var rl struct {
		Resources struct {
			Core struct {
				Reset int64 `json:"reset"`
			} `json:"core"`
		} `json:"resources"`
	}
	if json.Unmarshal(out, &rl) != nil || rl.Resources.Core.Reset == 0 {
		return 0, time.Time{}
	}
	reset := time.Unix(rl.Resources.Core.Reset, 0)
	w := time.Until(reset)
	if w < 0 {
		w = 0
	}
	return w, reset
}

// ---- conditional requests (ETag cache) -------------------------------------

func (c *Client) cacheKey(args []string) string {
	h := sha256.Sum256([]byte(strings.Join(args, "\x00")))
	return hex.EncodeToString(h[:])[:24]
}

func (c *Client) cachePaths(args []string) (etag, body string) {
	k := c.cacheKey(args)
	d := filepath.Join(c.CacheDir, "http")
	return filepath.Join(d, k+".etag"), filepath.Join(d, k+".body")
}

// Cached is API for single-page GETs: it sends the ETag from last time and, on
// a 304, answers from disk. fromCache is true when the answer was a 304.
func (c *Client) Cached(ctx context.Context, endpoint string, extra ...string) (body []byte, fromCache bool, err error) {
	args := append([]string{endpoint}, extra...)
	if c.NoCache || c.CacheDir == "" {
		b, err := c.API(ctx, args...)
		return b, false, err
	}
	etagP, bodyP := c.cachePaths(args)
	etag, _ := os.ReadFile(etagP)
	cached, _ := os.ReadFile(bodyP)

	call := func(withTag bool) ([]byte, error) {
		a := []string{"--include"}
		if withTag && len(etag) > 0 && len(cached) > 0 {
			a = append(a, "-H", "If-None-Match: "+strings.TrimSpace(string(etag)))
		}
		return c.API(ctx, append(a, args...)...)
	}
	raw, err := call(true)
	if IsClass(err, ClassNotModified) {
		if len(cached) > 0 {
			c.bump(func(s *Stats) { s.Hits++ })
			return cached, true, nil
		}
		_ = os.Remove(etagP) // an etag with no body is a broken cache entry
		raw, err = call(false)
	}
	if err != nil {
		return nil, false, err
	}
	hdr, payload := splitHTTP(raw)
	if len(bytes.TrimSpace(payload)) > 0 {
		_ = os.MkdirAll(filepath.Dir(bodyP), 0o700)
		_ = os.WriteFile(bodyP, payload, 0o600)
		if tag := headerValue(hdr, "etag"); tag != "" {
			_ = os.WriteFile(etagP, []byte(tag), 0o600)
		}
		c.bump(func(s *Stats) { s.Misses++ })
	}
	return payload, false, nil
}

// splitHTTP splits `gh api --include` output into the header block and body.
func splitHTTP(raw []byte) (hdr string, body []byte) {
	s := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(s, []byte("HTTP/")) {
		return "", s
	}
	if i := bytes.Index(s, []byte("\n\n")); i >= 0 {
		return string(s[:i]), s[i+2:]
	}
	return string(s), nil
}

func headerValue(hdr, name string) string {
	for _, ln := range strings.Split(hdr, "\n") {
		if i := strings.IndexByte(ln, ':'); i > 0 && strings.EqualFold(strings.TrimSpace(ln[:i]), name) {
			return strings.TrimSpace(ln[i+1:])
		}
	}
	return ""
}

// GetJSON fetches endpoint (cached) and decodes it into v.
func (c *Client) GetJSON(ctx context.Context, endpoint string, v any) error {
	b, _, err := c.Cached(ctx, endpoint)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("unreadable answer from %s: %w", endpoint, err)
	}
	return nil
}

// ClearCache removes the HTTP cache.
func (c *Client) ClearCache() error { return os.RemoveAll(filepath.Join(c.CacheDir, "http")) }
