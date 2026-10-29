package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ThatOn3Gu7/ghapk/internal/config"
	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
)

func startSelectionSession(t *testing.T, opts Options) Model {
	t.Helper()
	m := New(opts)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 26})
	m = mm.(Model)
	mm, _ = m.Update(AuthCheckedMsg{Version: "2", User: "tester"})
	return mm.(Model)
}
func selectWorkflow(m Model, name string) Model {
	m.focus = PanelWorkflows
	m.workflows.SelectByID(name)
	mm, _ := m.Update(keyOf("enter"))
	return mm.(Model)
}

func TestSelectionsSurviveRestartAndFollowRepo(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	t.Setenv("GHAPK_REPO", "")
	t.Setenv("GHAPK_BRANCH", "")
	t.Setenv("GHAPK_WORKFLOW", "")
	opts := Options{Settings: config.Defaults()}
	opts.Settings.Repo = "acme/one"
	m := startSelectionSession(t, opts)
	m = seedBranches(t, m)
	m.repoBranchFocused = true
	m.branchList.SelectByID("feature/test")
	mm, _ := m.Update(keyOf("enter"))
	m = mm.(Model)
	mm, _ = m.Update(WorkflowsMsg{Repo: "acme/one", List: []ghapi.Workflow{{Name: "CI"}, {Name: "Release"}}})
	m = mm.(Model)
	m = selectWorkflow(m, "Release")
	// A newer, non-favourite selection must win over a pinned repo.
	config.TouchRecent("acme/favourite")
	config.ToggleFavourite("acme/favourite")
	config.TouchRecent("acme/one")
	restarted := startSelectionSession(t, Options{Settings: config.Load()})
	if restarted.settings.Repo != "acme/one" || restarted.branchSel != "feature/test" || restarted.settings.Workflow != "Release" {
		t.Fatalf("not restored: %+v", restarted.settings)
	}
	initialWorkflow, ok := restarted.workflows.Selected()
	if !ok || initialWorkflow.ID != "Release" {
		t.Fatalf("workflow not visible while loading: %+v", initialWorkflow)
	}
	item, _ := restarted.branchList.Selected()
	if item.ID != "feature/test" {
		t.Fatalf("branch cursor: %+v", item)
	}
	mm, _ = restarted.Update(WorkflowsMsg{Repo: "acme/one", List: []ghapi.Workflow{{Name: "CI"}, {Name: "Release"}}})
	restarted = mm.(Model)
	item, _ = restarted.workflows.Selected()
	if item.ID != "Release" {
		t.Fatalf("workflow cursor: %+v", item)
	}
	mm, _ = restarted.Update(GitRemoteMsg{Repo: "other/project"})
	restarted = mm.(Model)
	item, _ = restarted.repos.Selected()
	if item.ID != "acme/one" {
		t.Fatalf("late git result moved repo selection: %+v", item)
	}
	// Each repo gets its own choices, including explicit all/any.
	if err := restarted.setRepo("acme/two", true); err != nil {
		t.Fatal(err)
	}
	restarted.selectBranch("other-branch")
	mm, _ = restarted.Update(WorkflowsMsg{Repo: "acme/two", List: []ghapi.Workflow{{Name: "Two CI"}}})
	restarted = mm.(Model)
	restarted = selectWorkflow(restarted, "Two CI")
	if err := restarted.setRepo("acme/one", true); err != nil {
		t.Fatal(err)
	}
	if restarted.branchSel != "feature/test" || restarted.settings.Workflow != "Release" {
		t.Fatal("one's choices lost after switching repos")
	}
	restarted.selectBranch("")
	mm, _ = restarted.Update(WorkflowsMsg{Repo: "acme/one", List: []ghapi.Workflow{{Name: "Release"}}})
	restarted = mm.(Model)
	restarted = selectWorkflow(restarted, "")
	reloaded := startSelectionSession(t, Options{Settings: config.Load()})
	if reloaded.branchSel != "" || reloaded.settings.Workflow != "" {
		t.Fatal("all/any did not survive restart")
	}
	if err := reloaded.setRepo("acme/two", true); err != nil {
		t.Fatal(err)
	}
	if reloaded.branchSel != "other-branch" || reloaded.settings.Workflow != "Two CI" {
		t.Fatal("two's choices lost")
	}
}

func TestSelectionOverridesAndFallbacks(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	t.Setenv("GHAPK_REPO", "")
	t.Setenv("GHAPK_BRANCH", "")
	t.Setenv("GHAPK_WORKFLOW", "")
	branch, wf := "saved-branch", "saved-wf"
	if err := config.RememberSelection("a/repo", true, &branch, &wf); err != nil {
		t.Fatal(err)
	}
	opts := Options{Settings: config.Defaults(), RepoOverride: true, BranchOverride: true, WorkflowOverride: true}
	opts.Settings.Repo = "a/repo"
	opts.Settings.Branch = "cli-branch"
	opts.Settings.Workflow = "cli-wf"
	m := startSelectionSession(t, opts)
	if m.branchSel != "cli-branch" || m.settings.Workflow != "cli-wf" {
		t.Fatal("CLI overrides lost")
	}
	t.Setenv("GHAPK_REPO", "b/repo")
	t.Setenv("GHAPK_BRANCH", "env-branch")
	t.Setenv("GHAPK_WORKFLOW", "env-wf")
	m = startSelectionSession(t, Options{Settings: config.Load()})
	if m.settings.Repo != "b/repo" || m.branchSel != "env-branch" || m.settings.Workflow != "env-wf" {
		t.Fatalf("env overrides lost: %+v", m.settings)
	}
	if err := m.setRepo("a/repo", false); err != nil {
		t.Fatal(err)
	}
	if m.branchSel != "env-branch" || m.settings.Workflow != "env-wf" {
		t.Fatal("env overrides lost after switch")
	}
}

func TestSelectionWriteFailureAndUnavailableWorkflow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHAPK_CONFIG_DIR", dir)
	t.Setenv("GHAPK_REPO", "")
	t.Setenv("GHAPK_BRANCH", "")
	t.Setenv("GHAPK_WORKFLOW", "")
	branch, wf := "feature", "removed workflow"
	if err := config.RememberSelection("a/repo", true, &branch, &wf); err != nil {
		t.Fatal(err)
	}
	m := startSelectionSession(t, Options{Settings: config.Load()})
	mm, _ := m.Update(WorkflowsMsg{Repo: "a/repo", List: []ghapi.Workflow{{Name: "different"}}})
	m = mm.(Model)
	it, _ := m.workflows.Selected()
	if it.ID != wf || it.Detail != "unavailable" {
		t.Fatalf("missing saved workflow silently deselected: %+v", it)
	}
	m.openWorkflowPicker()
	it, _ = m.workflowPicker.list.Selected()
	if it.ID != wf {
		t.Fatal("picker lost saved workflow")
	}
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHAPK_CONFIG_DIR", file)
	m.selectBranch("new")
	if m.branchSel != "new" || !strings.Contains(m.toast, "could not save") || !m.toastUntil.After(time.Now()) {
		t.Fatalf("failed save must be visible and not block session: %q", m.toast)
	}
}

func TestRecentFallbackUsesTimeNotFavourite(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHAPK_CONFIG_DIR", dir)
	t.Setenv("GHAPK_REPO", "")
	recent := "old/fav\t2020-01-01T00:00:00Z\tfav\nnew/repo\t2025-01-01T00:00:00Z\n"
	if err := os.WriteFile(filepath.Join(dir, "recents"), []byte(recent), 0600); err != nil {
		t.Fatal(err)
	}
	m := startSelectionSession(t, Options{Settings: config.Defaults()})
	if m.settings.Repo != "new/repo" {
		t.Fatalf("picked favourite, not most recent: %s", m.settings.Repo)
	}
}

func TestWorkflowRefreshKeepsBrowsingCursor(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := startSelectionSession(t, Options{Settings: config.Settings{Repo: "a/repo", Workflow: "CI"}, WorkflowOverride: true})
	mm, _ := m.Update(WorkflowsMsg{Repo: "a/repo", List: []ghapi.Workflow{{Name: "CI"}, {Name: "Release"}}})
	m = mm.(Model)
	m.workflows.SelectByID("Release")
	mm, _ = m.Update(WorkflowsMsg{Repo: "a/repo", List: []ghapi.Workflow{{Name: "CI"}, {Name: "Release"}, {Name: "New"}}})
	m = mm.(Model)
	it, _ := m.workflows.Selected()
	if it.ID != "Release" {
		t.Fatalf("refresh moved user's cursor: %+v", it)
	}
}

func TestPickerSelectionsPersistAndAllOverridesRepoConfig(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	t.Setenv("GHAPK_REPO", "")
	t.Setenv("GHAPK_BRANCH", "")
	t.Setenv("GHAPK_WORKFLOW", "")
	cfg := config.Defaults()
	cfg.Repo = "a/repo"
	cfg.Branch = "configured"
	cfg.Workflow = "Configured workflow"
	if err := config.SaveRepo(cfg); err != nil {
		t.Fatal(err)
	}
	m := startSelectionSession(t, Options{Settings: cfg})
	mm, _ := m.Update(BranchesMsg{Repo: "a/repo", List: []ghapi.Branch{{Name: "configured"}, {Name: "picked"}}})
	m = mm.(Model)
	mm, _ = m.Update(WorkflowsMsg{Repo: "a/repo", List: []ghapi.Workflow{{Name: "Configured workflow"}, {Name: "Picked workflow"}}})
	m = mm.(Model)
	mm, _ = m.Update(key("B"))
	m = mm.(Model)
	m.branchPicker.list.SelectByID("picked")
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	mm, _ = m.Update(key("W"))
	m = mm.(Model)
	m.workflowPicker.list.SelectByID("Picked workflow")
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	restored := startSelectionSession(t, Options{Settings: config.Load()})
	if restored.branchSel != "picked" || restored.settings.Workflow != "Picked workflow" {
		t.Fatalf("picker choices not persisted: %+v", restored.settings)
	}
	mm, _ = restored.Update(key("B"))
	restored = mm.(Model)
	restored.branchPicker.list.SelectByID("")
	mm, _ = restored.Update(keyOf("enter"))
	restored = mm.(Model)
	mm, _ = restored.Update(key("W"))
	restored = mm.(Model)
	restored.workflowPicker.list.SelectByID("")
	mm, _ = restored.Update(keyOf("enter"))
	restored = mm.(Model)
	restored = startSelectionSession(t, Options{Settings: config.Load()})
	if restored.branchSel != "" || restored.settings.Workflow != "" {
		t.Fatalf("explicit all did not override configured filters: %+v", restored.settings)
	}
}

func TestCustomWorkflowStaysVisibleAndPersists(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	s := config.Defaults()
	s.Repo = "a/repo"
	m := startSelectionSession(t, Options{Settings: s})
	mm, _ := m.Update(key("W"))
	m = mm.(Model)
	m = pressText(m, "custom.yml")
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	it, ok := m.workflows.Selected()
	if !ok || it.ID != "custom.yml" || m.settings.Workflow != "custom.yml" {
		t.Fatalf("custom selection not displayed: %+v", it)
	}
	reloaded := startSelectionSession(t, Options{Settings: config.Load()})
	it, ok = reloaded.workflows.Selected()
	if !ok || it.ID != "custom.yml" {
		t.Fatalf("custom selection not restored: %+v", it)
	}
}

func TestSavedWorkflowFileNameIsNotReportedMissing(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	wf := "build.yml"
	if err := config.RememberSelection("a/repo", true, nil, &wf); err != nil {
		t.Fatal(err)
	}
	m := startSelectionSession(t, Options{Settings: config.Load()})
	mm, _ := m.Update(WorkflowsMsg{Repo: "a/repo", List: []ghapi.Workflow{{ID: 42, Name: "Build", Path: ".github/workflows/build.yml"}}})
	m = mm.(Model)
	it, _ := m.workflows.Selected()
	if it.ID != wf || it.Detail != "current" || m.wfMissToasted {
		t.Fatalf("valid workflow filename reported missing: %+v", it)
	}
}
