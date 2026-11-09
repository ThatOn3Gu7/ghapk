package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectionFlagOverridesAreTracked(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	t.Setenv("GHAPK_REPO", "")
	t.Setenv("GHAPK_BRANCH", "")
	t.Setenv("GHAPK_WORKFLOW", "")
	_, opts := getCmd([]string{"--repo=owner/project", "--branch=feature", "--workflow=Build"})
	if !opts.RepoOverride || !opts.BranchOverride || !opts.WorkflowOverride || opts.Settings.Repo != "owner/project" || opts.Settings.Branch != "feature" || opts.Settings.Workflow != "Build" {
		t.Fatalf("flags lost: %+v", opts)
	}
	_, opts = getCmd(nil)
	if opts.RepoOverride || opts.BranchOverride || opts.WorkflowOverride {
		t.Fatal("unprovided flags marked as explicit")
	}
}

func TestSelectionFlagsRequireValues(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	for _, args := range [][]string{
		{"--repo"},
		{"--repo="},
		{"--repo", "--branch", "main"},
		{"--branch"},
		{"--branch="},
		{"--workflow"},
		{"--workflow="},
		{"--workflow", "--repo", "owner/project"},
	} {
		if code, _ := getCmd(args); code != 2 {
			t.Fatalf("missing value accepted: %v", args)
		}
	}
	if _, opts := getCmd([]string{"--repo", "owner/project"}); opts.Settings.Repo != "owner/project" {
		t.Fatalf("separate-value form failed: %+v", opts.Settings)
	}
}

func TestArtifactFlagClearsSavedExactName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHAPK_CONFIG_DIR", dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("artifact=old-exact\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, opts := getCmd([]string{"--artifact=app-*"})
	if opts.Settings.ArtifactName != "" {
		t.Fatalf("saved exact artifact name was not cleared: %q", opts.Settings.ArtifactName)
	}
	if opts.Settings.ArtifactPattern != "app-*" {
		t.Fatalf("pattern = %q, want app-*", opts.Settings.ArtifactPattern)
	}
	if v, ok := opts.CLIOverrides["artifact"]; !ok || v != "" {
		t.Fatalf("artifact override = %q, present=%v; want empty override", v, ok)
	}
}

func TestAdditionalSettingsFlagsAndInvalidValues(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	code, opts := getCmd([]string{"--out", "/tmp/apks", "--all-arch", "--refresh=15", "--install-wait", "120", "--ascii", "--no-mouse", "--notify-on=failure", "--speak", "--no-identical-check"})
	s := opts.Settings
	if code != 0 || s.OutputDir != "/tmp/apks" || !s.AllArch || s.AutoRefreshSecs != 15 || s.InstallWait != 120 || s.Glyphs != "ascii" || s.Mouse || s.NotifyOn != "failure" || !s.Speak || s.IdenticalCheck {
		t.Fatalf("settings flags: %d %+v", code, s)
	}
	for _, k := range []string{"out", "all-arch", "auto-refresh", "install-wait", "glyphs", "mouse", "notify-on", "speak", "identical-check"} {
		if _, ok := opts.CLIOverrides[k]; !ok {
			t.Fatalf("missing CLI precedence for %s", k)
		}
	}
	for _, args := range [][]string{{"--arch"}, {"--variant"}, {"--limit"}, {"--refresh=-2"}} {
		if code, _ := getCmd(args); code != 2 {
			t.Fatalf("invalid args accepted: %v", args)
		}
	}
}
