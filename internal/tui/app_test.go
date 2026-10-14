package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/ghapi"
)

func newTestModel() Model {
	s := config.Defaults()
	s.Repo = "acme/rocket"
	m := New(Options{Settings: s})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mm.(Model)
	mm, _ = m.Update(AuthCheckedMsg{Version: "2.62.0", User: "tester"})
	m = mm.(Model)
	return m
}

func seedRuns(m Model) Model {
	mm, _ := m.Update(WorkflowsMsg{List: []ghapi.Workflow{{ID: 1, Name: "CI"}, {ID: 2, Name: "Release"}}})
	m = mm.(Model)
	mm, _ = m.Update(RunsMsg{List: []ghapi.Run{
		{ID: 101, RunNumber: 41, DisplayTitle: "fix login crash", HeadBranch: "main",
			Status: "completed", Conclusion: "success", CreatedAt: time.Now().Add(-2 * time.Hour), Name: "CI"},
		{ID: 100, RunNumber: 40, DisplayTitle: "bump deps", HeadBranch: "main",
			Status: "in_progress", CreatedAt: time.Now().Add(-3 * time.Hour), Name: "CI"},
	}})
	m = mm.(Model)
	return m
}

func seedArtifacts(m Model) Model {
	mm, _ := m.Update(ArtifactsMsg{RunID: 101, List: []ghapi.Artifact{
		{ID: 9001, Name: "app-arm64-v8a-debug.apk", SizeInBytes: 14 << 20, ExpiresAt: time.Now().Add(90 * 24 * time.Hour)},
		{ID: 9002, Name: "app-universal-debug.apk", SizeInBytes: 20 << 20},
		{ID: 9003, Name: "symbols-mapping.txt", SizeInBytes: 1 << 20},
	}})
	return mm.(Model)
}

func key(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Alt: false}
	}
	return tea.KeyMsg{Type: tea.KeyEnter}
}

func TestDashboardRenders(t *testing.T) {
	m := newTestModel()
	m = seedRuns(m)
	m = seedArtifacts(m)
	v := m.View()
	for _, want := range []string{"ghapk", "acme/rocket", "Repositories", "Runs", "fix login", "Artifacts", "Device", "@tester"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q", want)
		}
	}
	// best artifact should be arm64 debug (star on first row)
	if !strings.Contains(v, "app-arm64-v8a-debug.apk") {
		t.Error("expected arm64 artifact listed")
	}
	lines := strings.Count(v, "\n")
	if lines < 30 {
		t.Errorf("dashboard too short: %d lines", lines)
	}
}

func TestKeyNavigation(t *testing.T) {
	m := newTestModel()
	m = seedRuns(m)
	m = seedArtifacts(m)
	var mm tea.Model = m
	// tab through panels
	for range 3 {
		mm, _ = mm.Update(keyOf("tab"))
	}
	mo := mm.(Model)
	if mo.focus != PanelArtifacts {
		t.Errorf("after 3 tabs focus = %v, want Artifacts", mo.focus)
	}
	// move down in artifacts
	mm, _ = mo.Update(key("j"))
	// open confirm modal with d
	mm, _ = mm.Update(key("d"))
	mo = mm.(Model)
	if mo.modal != ModalConfirm {
		t.Errorf("d did not open confirm modal (modal=%v)", mo.modal)
	}
	v := mo.View()
	if !strings.Contains(v, "Confirm download") && !strings.Contains(v, "Download") {
		t.Error("confirm modal not rendered")
	}
	// cancel with esc
	mm, _ = mo.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mo = mm.(Model)
	if mo.modal != ModalNone {
		t.Error("esc did not close the modal")
	}
}

func keyOf(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestHelpAndSettings(t *testing.T) {
	m := newTestModel()
	var mm tea.Model = m
	mm, _ = mm.Update(key("?"))
	mo := mm.(Model)
	if mo.modal != ModalHelp {
		t.Fatal("? did not open help")
	}
	if !strings.Contains(mo.View(), "download the selected artifact") {
		t.Error("help content missing")
	}
	mm, _ = mo.Update(key("?"))
	mo = mm.(Model)
	if mo.modal != ModalNone {
		t.Fatal("? again did not close help")
	}
	mm, _ = mo.Update(key("s"))
	mo = mm.(Model)
	if mo.modal != ModalSettings {
		t.Fatal("s did not open settings")
	}
	if !strings.Contains(mo.View(), "Arch preference") {
		t.Error("settings rows missing")
	}
}

func TestCompactLayout(t *testing.T) {
	m := newTestModel()
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 26})
	m = mm.(Model)
	if !m.compact {
		t.Fatal("width 70 should switch to compact layout")
	}
	m = seedRuns(m)
	v := m.View()
	if !strings.Contains(v, "Repos") {
		t.Error("compact tab bar missing")
	}
	if !strings.Contains(v, "fix login crash") && m.focus == PanelRuns {
		t.Error("compact runs content missing")
	}
}

func TestQuitRestoresNothing(t *testing.T) {
	m := newTestModel()
	mm, _ := m.Update(key("q"))
	mo := mm.(Model)
	if !mo.quitting {
		t.Fatal("q did not set quitting")
	}
	if mo.View() != "" {
		t.Fatal("view after quit should be empty (alternate screen is torn down)")
	}
}

func TestSetupScreen(t *testing.T) {
	s := config.Defaults()
	m := New(Options{Settings: s})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	mm, _ = m.Update(AuthCheckedMsg{Err: ghapi.ErrNoGH})
	m = mm.(Model)
	if m.screen != ScreenSetup {
		t.Fatal("missing gh should land on the setup screen")
	}
	if !strings.Contains(m.View(), "pkg install gh") {
		t.Error("setup screen should show install guidance")
	}
}

// Regression: loading repo metadata must NOT pin the branch filter to the
// default branch — that silently hid almost every run.
func TestRepoInfoDoesNotRestrictBranch(t *testing.T) {
	m := newTestModel()
	if m.branchSel != "" {
		t.Fatalf("precondition: branchSel starts empty, got %q", m.branchSel)
	}
	mm, _ := m.Update(RepoInfoMsg{Repo: ghapi.Repo{FullName: "acme/rocket", DefaultBranch: "main"}})
	mo := mm.(Model)
	if mo.branchSel != "" {
		t.Errorf("RepoInfoMsg pinned branch to %q — runs on other branches become invisible", mo.branchSel)
	}
}

// Badge numbers must match the actual list contents, including filters.
func TestBadgesMatchLists(t *testing.T) {
	m := newTestModel()
	m = seedRuns(m)
	m = seedArtifacts(m)
	if got := listBadge("Runs", &m.runs, 0); got != "2" {
		t.Errorf("runs badge = %q, want 2", got)
	}
	m.runs.AddFilterChar('x')
	got := listBadge("Runs", &m.runs, 0)
	if got != "1/2" {
		t.Errorf("filtered badge = %q, want 1/2", got)
	}
}

func seedWorkflows2(m Model) Model {
	mm, _ := m.Update(WorkflowsMsg{List: []ghapi.Workflow{
		{ID: 10, Name: "PR Assemble", Path: ".github/workflows/pr-assemble.yml"},
		{ID: 11, Name: "Build & Release", Path: ".github/workflows/release.yml"},
	}})
	return mm.(Model)
}

// The full frame must never exceed terminal height; overflow rows scroll
// and wipe the header (seen in pty captures during rapid key input).
func TestViewFitsHeight(t *testing.T) {
	for _, dims := range [][2]int{{66, 38}, {66, 14}, {77, 26}, {130, 30}, {120, 12}} {
		m := newTestModel()
		m = seedRuns(m)
		m = seedArtifacts(m)
		m = seedWorkflows2(m)
		m.runsLoaded = true
		mm, _ := m.Update(tea.WindowSizeMsg{Width: dims[0], Height: dims[1]})
		m = mm.(Model)
		for _, panel := range []PanelID{PanelRepos, PanelWorkflows, PanelRuns, PanelArtifacts, PanelDevice} {
			m.focus = panel
			v := m.View()
			rows := strings.Count(v, "\n")
			if rows > dims[1] {
				t.Errorf("%dx%d panel=%d -> view has %d rows (max %d)", dims[0], dims[1], panel, rows, dims[1])
			}
		}
	}
}
