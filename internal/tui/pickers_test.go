package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/ghapi"
)

func pickerTestModel() Model {
	m := seedRuns(newTestModel())
	next, _ := m.Update(RunsMsg{List: []ghapi.Run{
		{ID: 101, RunNumber: 41, DisplayTitle: "fix login crash", HeadBranch: "main", Status: "completed", Conclusion: "success", CreatedAt: time.Now()},
		{ID: 102, RunNumber: 42, DisplayTitle: "feature branch build", HeadBranch: "feat/pr-99", Status: "completed", Conclusion: "success", CreatedAt: time.Now()},
	}})
	return next.(Model)
}

func pressRune(m Model, r rune) Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return next.(Model)
}

func pressText(m Model, s string) Model {
	for _, r := range s {
		m = pressRune(m, r)
	}
	return m
}

func pressEnter(m Model) Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return next.(Model)
}

func TestRepoPickerRendersUserReposSearchHitsAndCustomSlug(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := pickerTestModel()

	m = pressRune(m, 'R')
	if m.modal != ModalPickRepo || m.repoPicker == nil {
		t.Fatalf("expected ModalPickRepo, got %v", m.modal)
	}

	// UserReposMsg immediately populates user's own GitHub repositories.
	next, _ := m.Update(UserReposMsg{List: []ghapi.Repo{
		{FullName: "acme/secret-app", Private: true},
		{FullName: "acme/public-sdk", Private: false},
	}})
	m = next.(Model)
	view := m.View()
	if !strings.Contains(view, "acme/secret-app") || !strings.Contains(view, "private") {
		t.Fatalf("expected user repos in RepoPicker view, got:\n%s", view)
	}

	// SearchResultsMsg immediately rebuilds visible list.
	m = pressText(m, "acme")
	next, _ = m.Update(SearchResultsMsg{
		Query: "acme",
		Hits:  []ghapi.SearchHit{{FullName: "acme/search-hit", Stars: 99}},
	})
	m = next.(Model)
	if !strings.Contains(m.View(), "acme/search-hit") {
		t.Fatalf("SearchResultsMsg did not rebuild RepoPicker list:\n%s", m.View())
	}

	// Custom owner/repo slug offers "Open “neworg/custom”" and switches repo on Enter.
	m.repoPicker.input.SetValue("")
	m = pressText(m, "neworg/custom")
	if !strings.Contains(m.View(), "neworg/custom") {
		t.Fatalf("expected custom slug option in RepoPicker:\n%s", m.View())
	}
	m = pressEnter(m)
	if m.modal != ModalNone {
		t.Fatalf("expected ModalNone after Enter, got %v", m.modal)
	}
	if m.settings.Repo != "neworg/custom" {
		t.Fatalf("expected repo switched to neworg/custom, got %q", m.settings.Repo)
	}
}

func TestBranchPickerBadgesFilteringAndCustomBranch(t *testing.T) {
	m := pickerTestModel()
	m.repo.DefaultBranch = "main"

	m = pressRune(m, 'B')
	if m.modal != ModalPickBranch || m.branchPicker == nil {
		t.Fatalf("expected ModalPickBranch, got %v", m.modal)
	}

	next, _ := m.Update(BranchesMsg{List: []ghapi.Branch{
		{Name: "main", Protected: true},
		{Name: "release/1.0", Protected: true},
	}})
	m = next.(Model)
	v := m.View()
	for _, want := range []string{"(all branches)", "main", "default", "release/1.0", "protected", "feat/pr-99", "recent run"} {
		if !strings.Contains(v, want) {
			t.Errorf("BranchPicker missing %q in view:\n%s", want, v)
		}
	}

	// Type a custom PR branch name not in the list and press Enter.
	m = pressText(m, "pr/unlisted-branch")
	m = pressEnter(m)
	if m.modal != ModalNone {
		t.Fatalf("expected ModalNone after Enter, got %v", m.modal)
	}
	if m.branchSel != "pr/unlisted-branch" || m.settings.Branch != "pr/unlisted-branch" {
		t.Fatalf("expected branchSel=pr/unlisted-branch, got %q", m.branchSel)
	}
}

func TestWorkflowPickerSyncsWorkflowPanelAndFiltersByYAMLFile(t *testing.T) {
	m := pickerTestModel()
	next, _ := m.Update(WorkflowsMsg{List: []ghapi.Workflow{
		{ID: 1, Name: "Android CI", Path: ".github/workflows/android.yml"},
		{ID: 2, Name: "Nightly Release", Path: ".github/workflows/nightly-release.yml"},
	}})
	m = next.(Model)

	m = pressRune(m, 'W')
	if m.modal != ModalPickWorkflow || m.workflowPicker == nil {
		t.Fatalf("expected ModalPickWorkflow, got %v", m.modal)
	}
	if !strings.Contains(m.View(), "nightly-release.yml") {
		t.Fatalf("expected workflow basename detail in WorkflowPicker:\n%s", m.View())
	}

	// Multi-token filter across workflow name + .yml filename
	m = pressText(m, "nightly yml")
	m = pressEnter(m)
	if m.modal != ModalNone {
		t.Fatalf("expected ModalNone, got %v", m.modal)
	}
	if m.settings.Workflow != "Nightly Release" {
		t.Fatalf("expected workflow 'Nightly Release', got %q", m.settings.Workflow)
	}
	sel, ok := m.workflows.Selected()
	if !ok || sel.ID != "Nightly Release" {
		t.Fatalf("dashboard Workflows panel did not sync to 'Nightly Release': %+v", sel)
	}
}

func TestModalsRespectViewportHeightAcrossSizes(t *testing.T) {
	for _, sz := range [][2]int{{66, 14}, {77, 26}, {130, 30}} {
		for _, k := range []rune{'R', 'B', 'W', 's'} {
			m := pickerTestModel()
			next, _ := m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			m = next.(Model)
			m = pressRune(m, k)
			h := lipgloss.Height(m.View())
			if h > sz[1] {
				t.Errorf("modal %q at %dx%d rendered %d lines (> %d)", string(k), sz[0], sz[1], h, sz[1])
			}
		}
	}
}
