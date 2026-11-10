package ghapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuthStatusDistinguishesConnectivityAndCredentials(t *testing.T) {
	cases := []struct {
		name, script string
		want         error
		user         string
	}{
		{"offline", `echo 'error connecting to api.github.com' >&2; exit 1`, ErrOffline, ""},
		{"network before token", `echo 'Failed to log in: Get https://api.github.com: dial tcp: network is unreachable' >&2; exit 1`, ErrOffline, ""},
		{"dns failure", `echo 'lookup api.github.com: no such host' >&2; exit 1`, ErrOffline, ""},
		{"missing login", `echo 'You are not logged into any GitHub hosts.' >&2; exit 1`, ErrNoAuth, ""},
		{"gh welcome", `echo 'Welcome to GitHub CLI!' >&2; echo 'To authenticate, please run gh auth login.' >&2; exit 1`, ErrNoAuth, ""},
		{"invalid token", `echo 'The token in secret-file is invalid' >&2; exit 1`, ErrNoAuth, ""},
		{"unknown failure", `echo 'surprise secret-token' >&2; exit 1`, ErrAuthCheck, ""},
		{"unauthorized", `echo 'Bad credentials (HTTP 401)' >&2; exit 1`, ErrNoAuth, ""},
		{"empty user", `exit 0`, ErrAuthCheck, ""},
		{"success", `echo 'alice'; echo 'warning on stderr' >&2`, nil, "alice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			script := "#!/bin/sh\nif [ \"$1\" != api ]; then echo 'unexpected gh command' >&2; exit 1; fi\n" + tc.script + "\n"
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			user, err := AuthStatus()
			if !errors.Is(err, tc.want) || user != tc.user {
				t.Fatalf("user=%q error=%v, want %q %v", user, err, tc.user, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("credential-related gh output leaked")
			}
		})
	}
}

func TestAuthTimeoutIsConnectivityFailure(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	<-ctx.Done()
	if !errors.Is(authCheckError(ctx, ""), ErrOffline) {
		t.Fatal("timeout was classified as missing authentication")
	}
}

// A "log in" phrase in gh's output is not proof of being signed out when gh
// holds a token locally and GitHub did not reject it.
func TestAuthStatusTrustsLocalTokenUnlessRejected(t *testing.T) {
	cases := []struct {
		name, apiOut string
		hasToken     bool
		want         error
		reason       string
	}{
		{"login phrase but token present", `echo 'To authenticate, please run gh auth login.' >&2; exit 1`, true, ErrAuthCheck, "gh exited with an error"},
		{"login phrase and no token", `echo 'To authenticate, please run gh auth login.' >&2; exit 1`, false, ErrNoAuth, ""},
		{"rejected despite token", `echo 'Bad credentials (HTTP 401)' >&2; exit 1`, true, ErrNoAuth, ""},
		{"status code is shown", `echo 'gh: please run gh auth login (HTTP 403)' >&2; exit 1`, true, ErrAuthCheck, "GitHub answered HTTP 403"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			tok := "exit 1"
			if tc.hasToken {
				tok = "echo ghp_secretsecret; exit 0"
			}
			script := "#!/bin/sh\nif [ \"$1\" = auth ]; then " + tok + "; fi\n" + tc.apiOut + "\n"
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			_, err := AuthStatus()
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
			if got := AuthReason(err); got != tc.reason {
				t.Fatalf("reason=%q, want %q", got, tc.reason)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("token leaked into error")
			}
		})
	}
}
