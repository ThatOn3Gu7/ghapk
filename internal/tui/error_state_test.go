package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ThatOn3Gu7/ghapk/internal/config"
	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

func notFound(endpoint string) error {
	return &ghapi.Error{Class: ghapi.ClassMissing, Status: 404, Endpoint: endpoint, Msg: "gh: Not Found (HTTP 404)"}
}

func TestRunsFailureShowsPersistentExplanation(t *testing.T) {
	m := newTestModel()
	mm, _ := m.Update(RunsMsg{Err: notFound("repos/acme/rocket/actions/workflows/ghost.yml/runs")})
	m = mm.(Model)
	m.settings.Workflow = "ghost.yml"
	m.focus = PanelRuns
	v := m.View()
	for _, want := range []string{"Not found on GitHub", "ghost.yml", "HTTP 404", "not in this repo", "retry", "workflow"} {
		if !strings.Contains(v, want) {
			t.Errorf("error state is missing %q\n%s", want, v)
		}
	}
	if strings.Contains(v, "gh api") {
		t.Error("raw gh plumbing leaked into the panel")
	}
	// it must outlive the toast
	m.toast = ""
	if !strings.Contains(m.View(), "Not found on GitHub") {
		t.Error("error state vanished with the toast")
	}
}

func TestRunsErrorClearsOnReload(t *testing.T) {
	m := newTestModel()
	mm, _ := m.Update(RunsMsg{Err: errors.New("boom")})
	m = mm.(Model)
	if m.runsErr == nil {
		t.Fatal("error not kept")
	}
	mm, _ = m.Update(RunsMsg{List: []ghapi.Run{{ID: 1, RunNumber: 1, DisplayTitle: "ok", Status: "completed", Conclusion: "success"}}})
	m = mm.(Model)
	m.focus, m.toast = PanelRuns, ""
	if m.runsErr != nil || strings.Contains(m.View(), "boom") {
		t.Fatal("error survived a successful load")
	}
	mm, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 24})
	_ = mm
}

func TestShortErrIsFriendlyAndRuneSafe(t *testing.T) {
	if got := shortErr(notFound("repos/a/b")); got != "Not found on GitHub" {
		t.Fatalf("got %q", got)
	}
	long := errors.New(strings.Repeat("é", 200))
	got := shortErr(long)
	if r := []rune(got); len(r) != 90 || r[89] != '…' {
		t.Fatalf("rune-safe truncation failed: %d runes", len([]rune(got)))
	}
	if !strings.HasPrefix(fmt.Sprint(got), "é") {
		t.Fatal("cut mangled the text")
	}
}

func TestErrorStateFitsNarrowScreens(t *testing.T) {
	for _, w := range []int{40, 50, 64, 80} {
		lines := renderAt(t, w, 24, func(m *Model) {
			m.runs.SetItems(nil)
			m.runsErr = notFound("repos/some-owner/some-quite-long-repository-name/actions/workflows/android-ci.yml/runs")
			m.focus = PanelRuns
		})
		lineWidths(t, w, 24, lines)
	}
}

func TestStaleWorkflowFallsBackToAllRuns(t *testing.T) {
	if !staleWorkflowErr(&ghapi.WorkflowNotFoundError{Name: "x"}) {
		t.Fatal("WorkflowNotFoundError must count as stale")
	}
	if !staleWorkflowErr(notFound("repos/a/b/actions/workflows/x.yml/runs")) {
		t.Fatal("a 404 on a workflow endpoint must count as stale")
	}
	if staleWorkflowErr(notFound("repos/a/b/actions/runs")) || staleWorkflowErr(errors.New("x")) {
		t.Fatal("a missing repo or an unrelated error must not be treated as a stale workflow")
	}

	m := newTestModel()
	m.settings.Workflow = "Android CI"
	mm, _ := m.Update(RunsMsg{Workflow: "Android CI", List: []ghapi.Run{{ID: 1, RunNumber: 1, DisplayTitle: "ok", Status: "completed", Conclusion: "success"}}, IgnoredWorkflow: "Android CI"})
	m = mm.(Model)
	m.focus = PanelRuns
	if m.wfIgnored != "Android CI" || !strings.Contains(m.toast, "showing all runs") || !m.toastErr {
		t.Fatalf("fallback not announced: ignored=%q toast=%q", m.wfIgnored, m.toast)
	}
	if v := m.View(); !strings.Contains(v, "wf:Android CI ≠") {
		t.Fatalf("header should mark the filter as not applied:\n%s", v)
	}
	// a later clean load removes the marker
	mm, _ = m.Update(RunsMsg{List: []ghapi.Run{{ID: 1, RunNumber: 1, DisplayTitle: "ok"}}})
	if mm.(Model).wfIgnored != "" {
		t.Fatal("marker should clear after a normal load")
	}
}

func TestDeviceProbingMessageIsCentered(t *testing.T) {
	m := newTestModel()
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	m.focus = PanelDevice
	for _, ln := range strings.Split(m.View(), "\n") {
		if i := strings.Index(ln, "probing device"); i >= 0 {
			if col := len([]rune(ln[:i])); col < 40 || col > 70 {
				t.Fatalf("message starts at column %d of 120; it should sit near the middle", col)
			}
			return
		}
	}
	t.Skip("device already probed in this model")
}

func TestSelectedAnyWorkflowRowIsReadable(t *testing.T) {
	m := newTestModel()
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	mm, _ = m.Update(WorkflowsMsg{Repo: m.settings.Repo, List: []ghapi.Workflow{{ID: 1, Name: "CI", Path: ".github/workflows/ci.yml"}}})
	m = mm.(Model)
	m.focus = PanelWorkflows
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	found := false
	for _, ln := range strings.Split(m.View(), "\n") {
		if strings.Contains(ln, "any workflow") {
			found = true
			// #A371F7 is the accent the label used to be drawn in, on a bar of the same colour
			if strings.Contains(ln, "38;2;163;113;247") && strings.Contains(ln, "48;2;163;113;247") {
				t.Fatalf("label is drawn in the highlight's own colour: %q", ln)
			}
		}
	}
	if !found {
		t.Fatal("the selected first row lost its label")
	}
}

func TestMissingRepoIsNotBlamedOnFilters(t *testing.T) {
	m := newTestModel()
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	m.focus = PanelRuns
	mm, _ = m.Update(RepoInfoMsg{RequestedRepo: m.settings.Repo, Err: notFound("repos/acme/rocket")})
	m = mm.(Model)
	mm, _ = m.Update(RunsMsg{Err: notFound("repos/acme/rocket/actions/runs")})
	m = mm.(Model)
	v := m.View()
	if !strings.Contains(v, "Repository not found") || strings.Contains(v, "saved branch or workflow") {
		t.Fatalf("a missing repo must be reported as such:\n%s", v)
	}
	if !strings.Contains(v, "pick repo") {
		t.Fatal("offer the repo picker")
	}
}

func TestStartupNoticeIsShownAsFailureToast(t *testing.T) {
	m := New(Options{StartupNotice: "key bindings ignored (bad json) — using defaults"})
	if !m.toastErr || !strings.Contains(m.toast, "key bindings ignored") || m.toastIfFresh() == "" {
		t.Fatalf("notice not shown: toast=%q err=%v", m.toast, m.toastErr)
	}
}

func TestWrapHardBreaksAfterSlashes(t *testing.T) {
	rows := wrapHard("HTTP 404 · repos/acme/rocket/actions/workflows/Android CI/runs", 28)
	for _, r := range rows {
		if runewidth.StringWidth(r) > 28 {
			t.Fatalf("row too wide: %q", r)
		}
	}
	if strings.Join(rows, "") != "HTTP 404 · repos/acme/rocket/actions/workflows/Android CI/runs" {
		t.Fatalf("text was altered: %q", rows)
	}
	for _, r := range rows[:len(rows)-1] {
		if !strings.HasSuffix(r, "/") && !strings.Contains(r, " ") {
			t.Errorf("row %q was cut mid-word", r)
		}
	}
}

func TestSetupContinueAnywayOnlyWhenSignOutIsNotProven(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	boot := func(err error) Model {
		m := New(Options{Settings: config.Defaults()})
		mm, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
		mm, _ = mm.Update(AuthCheckedMsg{Err: err})
		return mm.(Model)
	}
	m := boot(ghapi.ErrNoAuth)
	if v := components.StripANSI(m.View()); strings.Contains(v, "continue anyway") {
		t.Fatalf("offered to continue although signed out: %s", v)
	}
	mm, _ := m.Update(key("c"))
	if mm.(Model).screen != ScreenSetup {
		t.Fatal("c bypassed a real sign-out")
	}
	m = boot(ghapi.ErrAuthCheck)
	if v := components.StripANSI(m.View()); !strings.Contains(v, "continue anyway") {
		t.Fatalf("no way forward: %s", v)
	}
	mm, _ = m.Update(key("c"))
	if mm.(Model).screen != ScreenDashboard {
		t.Fatal("c did not open the dashboard")
	}
}
