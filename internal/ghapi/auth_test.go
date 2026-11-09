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
