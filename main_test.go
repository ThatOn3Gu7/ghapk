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
