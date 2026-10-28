package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
	"github.com/muesli/termenv"
)

func TestRepoAndWorkflowEnterSelectThenAdvance(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	// A repository already configured at startup also syncs on first Enter.
	current := seedRuns(newTestModel())
	current.repos.SelectByID("acme/rocket")
	first, cmd := current.Update(keyOf("enter"))
	current = first.(Model)
	if cmd == nil || current.focus != PanelRepos {
		t.Fatal("configured repo skipped first sync")
	}
	first, _ = current.Update(keyOf("enter"))
	current = first.(Model)
	if current.focus != PanelRepos || !current.repoBranchFocused {
		t.Fatal("configured repo did not advance to branches")
	}
	first, _ = current.Update(keyOf("enter"))
	current = first.(Model)
	first, _ = current.Update(keyOf("enter"))
	current = first.(Model)
	if current.focus != PanelWorkflows {
		t.Fatal("branch did not advance to workflows")
	}

	m := seedRuns(newTestModel())
	m.repos.SetItems([]components.Item{{ID: "acme/rocket", Label: "acme/rocket"}, {ID: "other/app", Label: "other/app"}})
	m.repos.SelectByID("other/app")
	mm, cmd := m.Update(keyOf("enter"))
	m = mm.(Model)
	if cmd == nil || m.focus != PanelRepos || m.settings.Repo != "other/app" {
		t.Fatalf("first repo selection: repo=%s focus=%v cmd=%v", m.settings.Repo, m.focus, cmd != nil)
	}
	selected, ok := m.repos.Selected()
	if !ok || selected.ID != "other/app" {
		t.Fatalf("repo cursor lost after sync: %+v", selected)
	}
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	if m.focus != PanelRepos || !m.repoBranchFocused {
		t.Fatalf("second repo enter did not focus branches")
	}
	mm, cmd = m.Update(keyOf("enter"))
	m = mm.(Model)
	if cmd == nil || !m.navBranchSet || m.focus != PanelRepos {
		t.Fatal("first branch enter did not select")
	}
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	if m.focus != PanelWorkflows {
		t.Fatal("second branch enter did not advance")
	}
	mm, _ = m.Update(WorkflowsMsg{Repo: "other/app", List: []ghapi.Workflow{{Name: "CI"}, {Name: "Release"}}})
	m = mm.(Model)
	m.workflows.SelectByID("CI")
	mm, cmd = m.Update(keyOf("enter"))
	m = mm.(Model)
	if cmd == nil || m.focus != PanelWorkflows || m.settings.Workflow != "CI" {
		t.Fatalf("first workflow selection: workflow=%q focus=%v", m.settings.Workflow, m.focus)
	}
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	if m.focus != PanelRuns || m.settings.Workflow != "CI" {
		t.Fatalf("second workflow enter: focus=%v", m.focus)
	}

	// A different workflow must sync first, not advance immediately.
	m.focus = PanelWorkflows
	m.workflows.SelectByID("Release")
	mm, cmd = m.Update(keyOf("enter"))
	m = mm.(Model)
	if cmd == nil || m.focus != PanelWorkflows || m.settings.Workflow != "Release" {
		t.Fatal("changed workflow did not sync first")
	}
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	if m.focus != PanelRuns {
		t.Fatal("changed workflow did not advance on second enter")
	}

	// Even the default, empty-ID Any workflow needs a first activation.
	m.focus = PanelWorkflows
	m.navWorkflowSet = false
	m.settings.Workflow = ""
	m.workflows.SelectByID("")
	mm, cmd = m.Update(keyOf("enter"))
	m = mm.(Model)
	if cmd == nil || m.focus != PanelWorkflows || !m.navWorkflowSet {
		t.Fatal("Any workflow skipped its first selection")
	}
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	if m.focus != PanelRuns {
		t.Fatal("Any workflow did not advance on second enter")
	}

	// Returning to another repo clears the remembered workflow activation.
	m.focus = PanelRepos
	m.repoBranchFocused = false
	m.repos.SetItems([]components.Item{{ID: "acme/rocket", Label: "acme/rocket"}})
	mm, cmd = m.Update(keyOf("enter"))
	m = mm.(Model)
	if cmd == nil || m.focus != PanelRepos || m.navWorkflowSet || m.settings.Repo != "acme/rocket" {
		t.Fatal("changed repo did not reset workflow selection")
	}
}

func TestLateWorkflowAndRunResultsAreIgnored(t *testing.T) {
	m := seedRuns(newTestModel())
	m.settings.Workflow = "Release"
	beforeRuns := len(m.runs.Items)
	mm, _ := m.Update(RunsMsg{Repo: "acme/rocket", Workflow: "CI", List: []ghapi.Run{{ID: 999}}})
	m = mm.(Model)
	if len(m.runs.Items) != beforeRuns {
		t.Fatal("stale workflow runs replaced the current selection")
	}
	mm, _ = m.Update(WorkflowsMsg{Repo: "other/app", List: []ghapi.Workflow{{Name: "stale"}}})
	m = mm.(Model)
	for _, item := range m.workflows.Items {
		if item.Label == "stale" {
			t.Fatal("stale repo workflows accepted")
		}
	}
	mm, _ = m.Update(ReleasesMsg{Repo: "other/app", List: []ghapi.Release{{TagName: "stale"}}})
	m = mm.(Model)
	if len(m.releases) != 0 {
		t.Fatal("stale repo releases accepted")
	}
	mm, _ = m.Update(WorkflowFileMsg{Repo: "other/app", Path: "ci.yml", YAML: "stale"})
	m = mm.(Model)
	if _, ok := m.wfCache["ci.yml"]; ok {
		t.Fatal("stale workflow YAML accepted")
	}
	mm, _ = m.Update(RunsMsg{Repo: "acme/rocket", Workflow: "Release", List: []ghapi.Run{{ID: 200}}})
	m = mm.(Model)
	if len(m.runs.Items) != 1 || m.selectedRunID != 200 {
		t.Fatal("current workflow runs ignored")
	}
}

func TestBuildActionChipsAndTouchTargets(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	for _, size := range [][2]int{{32, 18}, {32, 30}, {40, 30}, {63, 30}, {64, 18}, {64, 30}, {80, 30}, {120, 30}} {
		width, height := size[0], size[1]
		for action, label := range []string{"Install", "Download", "Open"} {
			t.Run(fmt.Sprintf("%dx%d/%s", width, height, label), func(t *testing.T) {
				m := buildModel(t, width, height)
				if width < 64 {
					m.focus = PanelArtifacts
					m.buildDetail = true
					m.updateListBounds()
				}
				r := m.buildsLayout().details
				line := strings.Split(m.View(), "\n")[r.y+r.h-2]
				plain := components.StripANSI(line)
				_, hits := m.buildActionChips(r.w - 4)
				i := strings.Index(plain, label)
				if i < 0 {
					t.Fatalf("%s not visible in %q", label, plain)
				}
				x := components.VisibleWidth(plain[:i])
				if x < r.x+2+hits[action].start || x >= r.x+2+hits[action].end {
					t.Fatalf("visible label and touch target disagree: x=%d hit=%+v", x, hits[action])
				}
				primary := components.StyleSeq(m.st.ButtonFocus.Padding(0, 0))
				secondary := components.StyleSeq(lipgloss.NewStyle().Foreground(m.st.P.Text).Background(m.st.P.Selection).Bold(true))
				if primary == "" || secondary == "" || !strings.Contains(line, primary) || !strings.Contains(line, secondary) {
					t.Fatal("highlighted action styles missing from rendered details")
				}
				mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x, Y: r.y + r.h - 2})
				m = mm.(Model)
				if cmd == nil {
					t.Fatalf("%s did not start", label)
				}
				if (action == 0 && m.modal != ModalPlan) || (action == 1 && (m.focus != PanelDownloads || len(m.jobs) != 1 || m.jobs[0].spec.install)) {
					t.Fatalf("wrong action: modal=%v task=%+v pending=%v", m.modal, m.task, m.pendingInstall)
				}
				if action == 2 && m.modal != ModalNone {
					t.Fatalf("open unexpectedly showed modal %v", m.modal)
				}
			})
		}
	}
}

func TestOpenButtonLaunchesCorrectTarget(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "opened")
	t.Setenv("OPEN_MARKER", marker)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "termux-open-url"), []byte("#!/bin/sh\nprintf '%s' \"$1\" > \"$OPEN_MARKER\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, release := range []bool{false, true} {
		m := buildModel(t, 100, 30)
		url := "https://github.com/acme/rocket/actions/runs/101"
		if release {
			url = "https://github.com/acme/rocket/releases/tag/v1"
			m.settings.Source = "releases"
			m.releases = []ghapi.Release{{TagName: "v1", HTMLURL: url, Assets: []ghapi.ReleaseAsset{{ID: 42, Name: "app-debug.apk"}}}}
			m.fetchArtifactsForSelection()
		} else {
			rr := m.runs.Items[0].ID.(runRow)
			rr.run.HTMLURL = url
			m.runs.Items[0].ID = rr
		}
		_ = os.Remove(marker)
		r := m.buildsLayout().details
		_, hits := m.buildActionChips(r.w - 4)
		mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: r.x + 2 + hits[2].start, Y: r.y + r.h - 2})
		m = mm.(Model)
		if cmd == nil || m.modal != ModalNone {
			t.Fatal("open did not start directly")
		}
		cmd()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			data, err := os.ReadFile(marker)
			if err == nil {
				if string(data) != url {
					t.Fatalf("opened %q, want %q", data, url)
				}
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatal("opener not invoked")
		}
	}
}

func TestDownloadActionOnlySaves(t *testing.T) {
	m := buildModel(t, 100, 30)
	mm, cmd := m.Update(key("d"))
	m = mm.(Model)
	if cmd == nil || m.focus != PanelDownloads || len(m.jobs) != 1 || m.jobs[0].spec.install {
		t.Fatal("download did not start")
	}
	if m.jobs[0].spec.install {
		t.Fatal("download-only job must not install")
	}

}

func TestActionChipsInNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(old)
	m := buildModel(t, 40, 24)
	m.focus = PanelArtifacts
	m.buildDetail = true
	m.updateListBounds()
	r := m.buildsLayout().details
	if !strings.Contains(components.StripANSI(m.View()), "INSTALL") {
		t.Fatal("no-color actions are not distinguished")
	}
	_, hits := m.buildActionChips(r.w - 4)
	mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: r.x + 2 + hits[0].start, Y: r.y + r.h - 2})
	m = mm.(Model)
	if cmd == nil || m.modal != ModalPlan || m.plan == nil {
		t.Fatal("no-color install touch failed")
	}
}
