// Package ghapi wraps the GitHub CLI (`gh`) exactly like the original script
// did: all API access goes through `gh api`, downloads through `gh api -H
// "Accept: application/octet-stream"` or the artifacts zip endpoint, and zip
// extraction is done natively with archive/zip (no unzip dependency).
package ghapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"time"

	"github.com/ThatOn3Gu7/ghapk/internal/activity"
)

var (
	ErrNoGH      = errors.New("gh CLI not found on PATH")
	ErrNoAuth    = errors.New("gh is not authenticated (run: gh auth login)")
	ErrOffline   = errors.New("cannot reach GitHub; check your connection")
	ErrAuthCheck = errors.New("could not check GitHub authentication")
)

// Available reports whether gh is installed.
func Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// Version returns gh's version string.
func Version() string {
	out, err := quickCmd(5*time.Second, "gh", "--version")
	if err != nil {
		return ""
	}
	fields := strings.Fields(out)
	if len(fields) >= 3 {
		return fields[2]
	}
	return out
}

// AuthStatus checks credentials and connectivity without mistaking network
// failures for a need to log in. Do not expose gh's potentially sensitive output.
func AuthStatus() (user string, err error) {
	if !Available() {
		return "", ErrNoGH
	}
	// One API call proves both credentials and connectivity. Running
	// `gh auth status` first doubled the startup wait, especially offline.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := activity.CommandContext(ctx, "gh", "api", "user", "--jq", ".login")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		output := stdout.String() + "\n" + stderr.String()
		err := authCheckError(ctx, output)
		if errors.Is(err, ErrNoAuth) && !definitelySignedOut(output) && localToken() {
			// gh's wording matched a "log in" phrase, yet gh does hold a token
			// on this machine and GitHub did not reject it with a 401. Telling
			// someone who is signed in to run `gh auth login` is wrong, so say
			// what is actually known and let them carry on.
			err = ErrAuthCheck
		}
		if errors.Is(err, ErrAuthCheck) {
			err = &authDetail{reason: failureReason(output)}
		}
		activity.Log("error", "auth check: "+firstLine(strings.TrimSpace(stderr.String()+" "+stdout.String())))
		return "", err
	}
	if user := strings.TrimSpace(stdout.String()); user != "" {
		return user, nil
	}
	return "", ErrAuthCheck
}

// authDetail is ErrAuthCheck plus a short, credential-free reason, so the setup
// screen can say why the check failed instead of a generic sentence.
type authDetail struct{ reason string }

func (e *authDetail) Error() string { return ErrAuthCheck.Error() + " (" + e.reason + ")" }
func (e *authDetail) Unwrap() error { return ErrAuthCheck }

// AuthReason returns the short reason attached to a failed sign-in check, or "".
func AuthReason(err error) string {
	var d *authDetail
	if errors.As(err, &d) {
		return d.reason
	}
	return ""
}

// failureReason names a failure without echoing gh's own text, which can
// mention tokens: the HTTP status when there is one, else a generic phrase.
func failureReason(output string) string {
	if m := httpStatusRe.FindStringSubmatch(output); m != nil {
		return "GitHub answered HTTP " + m[1]
	}
	if strings.TrimSpace(output) == "" {
		return "gh returned nothing"
	}
	return "gh exited with an error"
}

// definitelySignedOut is true only when GitHub itself rejected the credentials
// (a 401 / bad token). Phrases like "gh auth login" also appear in unrelated
// gh messages, so they are not proof on their own.
func definitelySignedOut(output string) bool {
	t := strings.ToLower(output)
	return strings.Contains(t, "http 401") || strings.Contains(t, "bad credentials") ||
		strings.Contains(t, "invalid token") || strings.Contains(t, "token is invalid") ||
		(strings.Contains(t, "token") && strings.Contains(t, "invalid"))
}

// localToken reports whether gh holds a token on this machine (stored login or
// GH_TOKEN/GITHUB_TOKEN). `gh auth token` reads local state only, so it works
// offline; the token itself is discarded and never logged. Older gh without
// that subcommand simply reports false, which keeps the old behaviour.
func localToken() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "auth", "token")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func authCheckError(ctx context.Context, output string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || connectionFailure(output) {
		return ErrOffline
	}
	text := strings.ToLower(output)
	if strings.Contains(text, "http 401") || strings.Contains(text, "authentication required") ||
		strings.Contains(text, "not logged") || strings.Contains(text, "not authenticated") ||
		strings.Contains(text, "invalid token") || strings.Contains(text, "token is invalid") ||
		strings.Contains(text, "failed to authenticate") || strings.Contains(text, "authentication failed") ||
		strings.Contains(text, "failed to log in") || strings.Contains(text, "to authenticate") ||
		strings.Contains(text, "welcome to github cli") || strings.Contains(text, "gh auth login") ||
		(strings.Contains(text, "token") && strings.Contains(text, "invalid")) {
		return ErrNoAuth
	}
	return ErrAuthCheck
}

func connectionFailure(output string) bool {
	text := strings.ToLower(output)
	for _, fragment := range []string{
		"error connecting to", "failed to connect", "unable to connect", "could not resolve",
		"dial tcp", "dial udp", "no such host", "network is unreachable", "network unreachable",
		"connection refused", "connection reset", "connection aborted", "connection closed",
		"connection timed out", "i/o timeout", "no route to host", "network is down",
		"request timeout", "context deadline exceeded", "temporary failure in name resolution",
		"unexpected eof", ": eof", "server misbehaving", "tls handshake timeout", "proxyconnect", "x509:",
		"http 502", "http 503", "http 504",
	} {
		if strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// api runs `gh api <endpoint> [args...]` and decodes JSON into v.
// A default per_page=100 is added unless the endpoint already has a query.
func api(v any, timeout time.Duration, endpoint string, args ...string) error {
	return apiContext(context.Background(), v, timeout, endpoint, args...)
}

// apiContext is api with a caller-owned cancellation context.
func apiContext(parent context.Context, v any, timeout time.Duration, endpoint string, args ...string) error {
	if !strings.Contains(endpoint, "?") && (strings.Contains(endpoint, "/runs") || strings.Contains(endpoint, "/artifacts") || strings.Contains(endpoint, "/branches") || strings.Contains(endpoint, "/workflows") || strings.Contains(endpoint, "/releases")) {
		endpoint += "?per_page=100"
	}
	out, err := runGH(parent, timeout, endpoint, args...)
	if err != nil {
		return err
	}
	if v == nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decoding %s: %w", stripQuery(endpoint), err)
	}
	return nil
}

// rawAPI runs `gh api <endpoint> [args...]` and returns the body verbatim
// (no JSON decoding) — for the contents API with a raw Accept header.
func rawAPI(timeout time.Duration, endpoint string, args ...string) (string, error) {
	out, err := runGH(context.Background(), timeout, endpoint, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// Retry tuning. Variables so tests can shrink the waits.
var (
	maxAttempts = 3
	retryBase   = 700 * time.Millisecond
)

// runGH is the one place that executes `gh api`. Failures come back as a
// classified *Error. Reads that fail for a transient reason (a 5xx, or no
// connection) are retried with a short jittered back-off; everything else —
// auth, missing, forbidden, rate limit, timeout — fails at once, because
// asking again cannot change the answer. The logged line records which
// endpoint failed and why, so the Log tab can explain a 404 after the fact.
func runGH(parent context.Context, timeout time.Duration, endpoint string, args ...string) ([]byte, error) {
	if !Available() {
		return nil, ErrNoGH
	}
	if parent == nil {
		parent = context.Background()
	}
	full := append([]string{"api", endpoint}, args...)
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(parent, timeout)
		cmd := activity.CommandContext(ctx, "gh", full...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		cancel()
		if err == nil {
			return stdout.Bytes(), nil
		}
		if perr := parent.Err(); errors.Is(perr, context.Canceled) {
			// The caller gave up (a screen changed, the app is quitting): that
			// is not a GitHub failure and must not be reported as one.
			return nil, perr
		} else if perr != nil {
			timedOut = true // the caller's own deadline ran out
		}
		ge := classify(endpoint, stderr.String(), err, timedOut)
		activity.Log("error", fmt.Sprintf("gh api %s -> %s", ge.Endpoint, ge.logLine()))
		if !ge.Retryable() || attempt >= maxAttempts {
			return nil, ge
		}
		if sleepCtx(parent, backoff(attempt)) != nil {
			return nil, parent.Err()
		}
	}
}

func backoff(attempt int) time.Duration {
	d := retryBase << uint(attempt-1)
	return d/2 + time.Duration(rand.Int63n(int64(d/2)+1))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func quickCmd(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := activity.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
