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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := activity.CommandContext(ctx, "gh", "auth", "status")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", authCheckError(ctx, string(out), true)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()
	cmd = activity.CommandContext(ctx2, "gh", "api", "user", "--jq", ".login")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", authCheckError(ctx2, stdout.String()+"\n"+stderr.String(), false)
	}
	if user := strings.TrimSpace(stdout.String()); user != "" {
		return user, nil
	}
	return "", ErrAuthCheck
}

func authCheckError(ctx context.Context, output string, status bool) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || connectionFailure(output) {
		return ErrOffline
	}
	text := strings.ToLower(output)
	if strings.Contains(text, "http 401") || strings.Contains(text, "authentication required") ||
		strings.Contains(text, "not logged") || strings.Contains(text, "not authenticated") ||
		strings.Contains(text, "invalid token") || strings.Contains(text, "token is invalid") ||
		strings.Contains(text, "failed to authenticate") || strings.Contains(text, "authentication failed") ||
		strings.Contains(text, "failed to log in") || (strings.Contains(text, "token") && strings.Contains(text, "invalid")) {
		return ErrNoAuth
	}
	// Older versions of gh return unstructured output on auth-status errors.
	if status && strings.TrimSpace(output) == "" {
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
	if !Available() {
		return ErrNoGH
	}
	if !strings.Contains(endpoint, "?") && (strings.Contains(endpoint, "/runs") || strings.Contains(endpoint, "/artifacts") || strings.Contains(endpoint, "/branches") || strings.Contains(endpoint, "/workflows") || strings.Contains(endpoint, "/releases")) {
		endpoint += "?per_page=100"
	}
	full := append([]string{"api", endpoint}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := activity.CommandContext(ctx, "gh", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := firstLine(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("gh api %s: %s", endpoint, msg)
	}
	if v == nil {
		return nil
	}
	dec := json.NewDecoder(&stdout)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decoding %s: %w", endpoint, err)
	}
	return nil
}

// rawAPI runs `gh api <endpoint> [args...]` and returns the body verbatim
// (no JSON decoding) — for the contents API with a raw Accept header.
func rawAPI(timeout time.Duration, endpoint string, args ...string) (string, error) {
	if !Available() {
		return "", ErrNoGH
	}
	full := append([]string{"api", endpoint}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := activity.CommandContext(ctx, "gh", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := firstLine(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("gh api %s: %s", endpoint, msg)
	}
	return stdout.String(), nil
}

func quickCmd(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := activity.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}
