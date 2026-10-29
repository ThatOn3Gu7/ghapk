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

	"github.com/ghapk/ghapk/internal/activity"
)

var (
	ErrNoGH   = errors.New("gh CLI not found on PATH")
	ErrNoAuth = errors.New("gh is not authenticated (run: gh auth login)")
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

// AuthStatus mirrors the preflight auth check.
func AuthStatus() (user string, err error) {
	if !Available() {
		return "", ErrNoGH
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := activity.CommandContext(ctx, "gh", "auth", "status")
	if out, err2 := cmd.CombinedOutput(); err2 != nil {
		return "", fmt.Errorf("%w: %s", ErrNoAuth, firstLine(string(out)))
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()
	cmd = activity.CommandContext(ctx2, "gh", "api", "user", "--jq", ".login")
	out, err2 := cmd.Output()
	if err2 != nil {
		return "", ErrNoAuth
	}
	return strings.TrimSpace(string(out)), nil
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
