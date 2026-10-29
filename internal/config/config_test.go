package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestUntrustedRepoConfigCannotWeakenSafetyOrTriggerUninstall(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("GHAPK_CONFIG_DIR", cfgDir)

	malicious := `
# Malicious .github/ghapk.conf or working-dir .ghapk.conf
repo = acme/rocket
workflow = android-ci.yml
branch = main
artifact-pattern = *debug*
exclude = *wear*
source = releases
uninstall-first = 1
downgrade = allow
pkg-check = 0
sig-check = 0
verify-digest = 0
verify-install = 0
package = com.attacker.spoof
out = /tmp/pwn
no-open = 1
arch = any
`
	localPath := filepath.Join(t.TempDir(), ".ghapk.conf")
	if err := os.WriteFile(localPath, []byte(malicious), 0o644); err != nil {
		t.Fatal(err)
	}

	s := loadWithLocal(localPath, "")

	// Safety and destructive settings MUST remain untouched at safe defaults.
	if !s.PkgCheck || !s.SigCheck || !s.VerifyDigest || !s.VerifyInstall {
		t.Fatalf("untrusted config weakened safety policies: %+v", s)
	}
	if s.Downgrade != "auto" {
		t.Fatalf("untrusted config changed Downgrade to %q, want auto", s.Downgrade)
	}
	if s.PackageName != "" {
		t.Fatalf("untrusted config spoofed PackageName to %q", s.PackageName)
	}
	if s.NoOpen {
		t.Fatal("untrusted config changed NoOpen")
	}
	if s.Arch != "auto" {
		t.Fatalf("untrusted config weakened Arch to %q, want auto", s.Arch)
	}

	// Benign selection hints apply when unset by user.
	if s.Repo != "acme/rocket" || s.Workflow != "android-ci.yml" || s.Branch != "main" || s.ExcludePattern != "*wear*" || s.Source != "releases" {
		t.Fatalf("benign hints did not apply cleanly: %+v", s)
	}

	// Explicit ApplyRemoteRepoConfig reports blocked keys accurately.
	s2 := Defaults()
	s2.Branch = "develop" // already set by user -> remote hint must not overwrite!
	blocked := ApplyRemoteRepoConfig(&s2, malicious)
	for _, wantBlocked := range []string{"arch", "downgrade", "no-open", "out", "package", "pkg-check", "sig-check", "uninstall-first", "verify-digest", "verify-install"} {
		if !slices.Contains(blocked, wantBlocked) {
			t.Errorf("expected %q in blocked keys, got %v", wantBlocked, blocked)
		}
	}
	if s2.Branch != "develop" {
		t.Errorf("remote config overwrote user's branch %q", s2.Branch)
	}
	if !s2.PkgCheck || !s2.SigCheck || !s2.VerifyDigest || s2.Downgrade != "auto" {
		t.Fatalf("ApplyRemoteRepoConfig weakened local safety: %+v", s2)
	}
}

func TestTrustedLayersRoundTripAndPrecedence(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("GHAPK_CONFIG_DIR", cfgDir)
	t.Setenv("GHAPK_HISTORY", filepath.Join(cfgDir, "history.json"))

	glob := Defaults()
	glob.Theme = "mocha"
	glob.Arch = "arm64"
	glob.Branch = "main"
	glob.Source = "auto"
	glob.KeepCount = 5
	glob.ExcludePattern = "*mapping*"
	if err := SaveGlobal(glob); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	repoSet := glob
	repoSet.Repo = "acme/rocket"
	repoSet.Branch = "release"
	repoSet.Downgrade = "never"
	if err := SaveRepo(repoSet); err != nil {
		t.Fatalf("SaveRepo: %v", err)
	}

	// Untrusted local file tries to override branch and source=releases;
	// trusted global/per-repo config must win!
	localPath := filepath.Join(t.TempDir(), ".ghapk.conf")
	_ = os.WriteFile(localPath, []byte("branch=untrusted\nsource=releases\n"), 0o644)

	loaded := loadWithLocal(localPath, "acme/rocket")
	if loaded.Theme != "mocha" || loaded.Arch != "arm64" || loaded.KeepCount != 5 || loaded.ExcludePattern != "*mapping*" {
		t.Fatalf("global settings lost: %+v", loaded)
	}
	if loaded.Branch != "release" || loaded.Downgrade != "never" {
		t.Fatalf("per-repo settings did not override global/untrusted: %+v", loaded)
	}
	if loaded.Source != "auto" {
		t.Fatalf("untrusted source=releases overrode explicit global source=auto: got %q", loaded.Source)
	}

	// History persistence
	if err := RecordInstall("acme/rocket", HistoryEntry{Pkg: "com.acme.rocket", VCode: 42, Sig: "abcd"}); err != nil {
		t.Fatalf("RecordInstall: %v", err)
	}
	h, ok := History("acme/rocket")
	if !ok || h.Pkg != "com.acme.rocket" || h.VCode != 42 || h.Installs != 1 {
		t.Fatalf("unexpected history entry: %+v (ok=%v)", h, ok)
	}
}

func TestFreshConfigHasNoDefaultRepository(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHAPK_CONFIG_DIR", dir)
	t.Setenv("GHAPK_REPO", "")
	if repo := Defaults().Repo; repo != "" {
		t.Fatalf("default repository = %q, want none", repo)
	}
	if repo := loadWithLocal(filepath.Join(dir, ".ghapk.conf"), "").Repo; repo != "" {
		t.Fatalf("fresh config repository = %q, want none", repo)
	}
	if recent := LoadRecents(); len(recent) != 0 {
		t.Fatalf("fresh config has recent repositories: %+v", recent)
	}
	selection, err := LoadSelections()
	if err != nil || selection.LastRepo != "" || len(selection.Repos) != 0 {
		t.Fatalf("fresh config has remembered selections: %+v, %v", selection, err)
	}
}
