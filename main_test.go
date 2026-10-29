package main

import (
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
