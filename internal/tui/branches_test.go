package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/cellbuf"
)

func tapBranch(t *testing.T, m Model, x, y int) (Model, tea.Cmd) {
	t.Helper()
	mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y})
	if cmd != nil {
		t.Fatal("branch acted before touch release")
	}
	m = mm.(Model)
	mm, cmd = m.Update(tea.MouseMsg{Type: tea.MouseRelease, Action: tea.MouseActionRelease, X: x, Y: y})
	return mm.(Model), cmd
}

func seedBranches(t *testing.T, m Model) Model {
	t.Helper()
	mm, _ := m.Update(RepoInfoMsg{RequestedRepo: m.settings.Repo, Repo: ghapi.Repo{DefaultBranch: "main"}})
	m = mm.(Model)
	mm, _ = m.Update(BranchesMsg{Repo: m.settings.Repo, List: []ghapi.Branch{{Name: "main"}, {Name: "feature/test"}, {Name: "release/1", Protected: true}}})
	m = mm.(Model)
	return m
}

func TestBranchDashboardNavigationAndFiltering(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := seedBranches(t, seedArtifacts(seedRuns(newTestModel())))
	m.repos.SelectByID(m.settings.Repo)
	mm, cmd := m.Update(keyOf("enter"))
	m = mm.(Model)
	if cmd == nil || m.repoBranchFocused || m.focus != PanelRepos {
		t.Fatal("first enter did not select repo")
	}
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	if !m.repoBranchFocused || m.focus != PanelRepos {
		t.Fatal("second enter did not focus branch box")
	}
	m.branchList.SelectByID("feature/test")
	mm, cmd = m.Update(keyOf("enter"))
	m = mm.(Model)
	if cmd == nil || m.focus != PanelRepos || m.branchSel != "feature/test" || m.settings.Branch != "feature/test" || len(m.runs.Items) != 0 || len(m.arts.Items) != 0 {
		t.Fatal("branch not applied or old builds still visible")
	}
	mm, _ = m.Update(RunsMsg{Repo: m.settings.Repo, Workflow: m.settings.Workflow, Branch: "main", List: []ghapi.Run{{ID: 999}}})
	m = mm.(Model)
	if len(m.runs.Items) != 0 {
		t.Fatal("stale branch builds displayed")
	}
	mm, _ = m.Update(RunsMsg{Repo: m.settings.Repo, Workflow: m.settings.Workflow, Branch: "feature/test", List: []ghapi.Run{{ID: 5, HeadBranch: "feature/test"}}})
	m = mm.(Model)
	if len(m.runs.Items) != 1 {
		t.Fatal("selected branch builds ignored")
	}
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	if m.focus != PanelWorkflows {
		t.Fatal("second branch enter did not open workflows")
	}
	m.focus = PanelRepos
	m.repoBranchFocused = true
	m.branchList.SelectByID("")
	mm, cmd = m.Update(keyOf("enter"))
	m = mm.(Model)
	if cmd == nil || m.branchSel != "" || m.settings.Branch != "" || m.focus != PanelRepos {
		t.Fatal("all branches did not clear filter")
	}
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	if m.focus != PanelWorkflows {
		t.Fatal("all branches did not advance")
	}
}

func TestBranchSourcesAreScopedAndCursorStable(t *testing.T) {
	m := seedBranches(t, newTestModel())
	m.repoBranchFocused = true
	m.branchList.SelectByID("feature/test")
	mm, _ := m.Update(LocalBranchesMsg{Repo: "other/repo", Names: []string{"wrong"}})
	m = mm.(Model)
	mm, _ = m.Update(BranchesMsg{Repo: "other/repo", List: []ghapi.Branch{{Name: "stale"}}})
	m = mm.(Model)
	for _, it := range m.branchList.Items {
		if it.ID == "wrong" || it.ID == "stale" {
			t.Fatal("another repo's branches leaked")
		}
	}
	mm, _ = m.Update(LocalBranchesMsg{Repo: "ACME/ROCKET", Names: []string{"main", "local/only"}})
	m = mm.(Model)
	sel, _ := m.branchList.Selected()
	if sel.ID != "feature/test" {
		t.Fatal("local result moved cursor")
	}
	found := false
	for _, it := range m.branchList.Items {
		if it.ID == "local/only" {
			found = it.Detail == "local"
		}
	}
	if !found {
		t.Fatal("missing local branch label")
	}
	mm, _ = m.Update(BranchesMsg{Repo: "acme/rocket", List: []ghapi.Branch{{Name: "main"}, {Name: "feature/test"}, {Name: "api/new"}}})
	m = mm.(Model)
	sel, _ = m.branchList.Selected()
	if sel.ID != "feature/test" {
		t.Fatal("API result moved cursor")
	}
	m.settings.Repo = "elsewhere/repo"
	m.setRepo("elsewhere/repo", false)
	for _, it := range m.branchList.Items {
		if it.ID == "local/only" || it.ID == "feature/test" || it.ID == "main" {
			t.Fatal("old repo branch remained after switching")
		}
	}
}

func TestBranchBoxTouchAndWheel(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	for _, sz := range [][2]int{{32, 18}, {40, 24}, {64, 18}, {80, 26}, {120, 40}} {
		t.Run(fmt.Sprint(sz), func(t *testing.T) {
			m := seedBranches(t, buildModel(t, sz[0], sz[1]))
			m.focus = PanelRepos
			m.repoBranchFocused = false
			for _, it := range m.branchList.Items {
				if it.ID == "feature/test" {
					m.branchList.SelectByID(it.ID)
					break
				}
			}
			r := m.panelRects()[PanelRepos]
			banner, repoH, branchH := reposBranchSplit(r.h, len(m.repos.Items), len(m.branchList.Items), r.w)
			top := r.y + 1 + banner + repoH
			row := top + 1 + m.branchList.Cursor - m.branchList.Offset
			x := r.x + 3
			if row >= top+branchH-1 {
				t.Fatalf("selected branch outside box: %d %d", row, top+branchH)
			}
			lines := strings.Split(m.View(), "\n")
			if len(lines) != m.height {
				t.Fatalf("screen height %d", len(lines))
			}
			lineWidths(t, m.width, m.height, lines)
			screen := cellbuf.NewBuffer(m.width, m.height)
			cellbuf.SetContent(screen, m.View())
			if screen.Cell(x, row).Rune == ' ' && !strings.Contains(components.StripANSI(lines[row]), "feature") {
				t.Fatalf("touch target is not branch row: %q", components.StripANSI(lines[row]))
			}
			mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: x, Y: row})
			got := mm.(Model)
			if cmd != nil || got.navBranchSet {
				t.Fatal("motion activated branch")
			}
			m, cmd = tapBranch(t, m, x, row)
			if cmd == nil || m.branchSel != "feature/test" || !m.repoBranchFocused {
				t.Fatal("first tap did not select branch")
			}
			m, _ = tapBranch(t, m, x, row)
			if m.focus != PanelWorkflows {
				t.Fatal("second tap did not advance to workflows")
			}
		})
	}
}

func TestBranchFocusKeysFilterAndRepoActions(t *testing.T) {
	m := seedBranches(t, newTestModel())
	m.repoBranchFocused = true
	mm, _ := m.Update(key("/"))
	m = mm.(Model)
	mm, _ = m.Update(key("f"))
	switch v := mm.(type) {
	case Model:
		m = v
	case *Model:
		m = *v
	}
	if m.branchList.Filter != "f" || m.repos.Filter != "" {
		t.Fatal("filter went to wrong box")
	}
	mm, _ = m.Update(keyOf("esc"))
	switch v := mm.(type) {
	case Model:
		m = v
	case *Model:
		m = *v
	}
	mm, _ = m.Update(key("e"))
	m = mm.(Model)
	if m.modal != ModalNone {
		t.Fatal("repo editor opened while branch box focused")
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = mm.(Model)
	if m.repoBranchFocused {
		t.Fatal("left did not return to repos")
	}
}

func TestLocalBranchesMatchOnlyOriginRepo(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	run("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-qm", "initial")
	run("branch", "feature/local")
	run("remote", "add", "origin", "https://github.com/acme/rocket.git")
	t.Chdir(dir)
	msg := fetchLocalBranchesCmd("acme/rocket")().(LocalBranchesMsg)
	if !strings.Contains(strings.Join(msg.Names, "\n"), "feature/local") {
		t.Fatalf("local branch missing: %v", msg.Names)
	}
	other := fetchLocalBranchesCmd("other/repo")().(LocalBranchesMsg)
	if len(other.Names) != 0 {
		t.Fatalf("unrelated local branches leaked: %v", other.Names)
	}
}

func TestBranchSelectorSignalsReleaseFallbackNotFiltered(t *testing.T) {
	m := buildModel(t, 80, 30)
	m.settings.Source = "releases"
	m.branchSel = "feature/test"
	m.releases = []ghapi.Release{{TagName: "v1", Assets: []ghapi.ReleaseAsset{{ID: 1, Name: "release.apk"}}}}
	m.syncReleaseArtifactItems()
	if !strings.Contains(components.StripANSI(m.buildDetails(m.buildsLayout().details)), "Releases: all branches") {
		t.Fatal("release fallback presented as branch-filtered")
	}
}

func TestRecentRunBranchKeepsTouchTargetAfterFiltering(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := seedBranches(t, buildModel(t, 40, 24))
	mm, _ := m.Update(RunsMsg{List: []ghapi.Run{{ID: 1, HeadBranch: "one"}, {ID: 2, HeadBranch: "two"}}})
	m = mm.(Model)
	m.focus = PanelRepos
	m.branchList.SelectByID("two")
	r := m.panelRects()[PanelRepos]
	banner, reposH, branchH := reposBranchSplit(r.h, len(m.repos.Items), len(m.branchList.Items), r.w)
	row := r.y + 1 + banner + reposH + 1 + m.branchList.Cursor - m.branchList.Offset
	x := r.x + 3
	m, _ = tapBranch(t, m, x, row)
	if m.branchSel != "two" {
		t.Fatal("first touch picked wrong branch")
	}
	a, b, c := reposBranchSplit(r.h, len(m.repos.Items), len(m.branchList.Items), r.w)
	if banner != a || reposH != b || branchH != c || row != r.y+1+a+b+1+m.branchList.Cursor-m.branchList.Offset {
		t.Fatal("branch moved after applying filter")
	}
	m, _ = tapBranch(t, m, x, row)
	if m.focus != PanelWorkflows {
		t.Fatal("second touch did not advance")
	}
}

func TestLocalBranchesRemainAvailableWhenAPIUnavailable(t *testing.T) {
	m := newTestModel()
	mm, _ := m.Update(BranchesMsg{Repo: m.settings.Repo, Err: errors.New("offline")})
	m = mm.(Model)
	mm, _ = m.Update(LocalBranchesMsg{Repo: m.settings.Repo, Names: []string{"feature/offline"}})
	m = mm.(Model)
	found := false
	for _, it := range m.branchList.Items {
		if it.ID == "feature/offline" && it.Detail == "local" {
			found = true
		}
	}
	if !found || m.branchLoading {
		t.Fatal("local fallback unavailable after API error")
	}
}

func TestLiveTeaBranchTouch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nprintf '%s' '{\"workflow_runs\":[]}'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	m := seedBranches(t, buildModel(t, 40, 24))
	m.focus = PanelRepos
	m.branchList.SelectByID("feature/test")
	r := m.panelRects()[PanelRepos]
	banner, repoH, _ := reposBranchSplit(r.h, len(m.repos.Items), len(m.branchList.Items), r.w)
	x, y := r.x+3, r.y+1+banner+repoH+1+m.branchList.Cursor-m.branchList.Offset
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	var out syncBuf
	updates := make(chan Model, 100)
	p := tea.NewProgram(touchHarness{model: m, updates: updates}, tea.WithInput(input), tea.WithOutput(&out), tea.WithMouseCellMotion(), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	defer p.Kill()
	p.Send(tea.WindowSizeMsg{Width: 40, Height: 24})
	wait := func(pred func(Model) bool) {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case state := <-updates:
				if pred(state) {
					return
				}
			case err := <-done:
				t.Fatalf("program exited %v", err)
			case <-timer.C:
				t.Fatalf("timed out: %s", components.StripANSI(out.String()))
			}
		}
	}
	click := func() {
		t.Helper()
		if _, err := fmt.Fprintf(writer, "\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x+1, y+1); err != nil {
			t.Fatal(err)
		}
	}
	click()
	wait(func(s Model) bool { return s.branchSel == "feature/test" && s.repoBranchFocused })
	click()
	wait(func(s Model) bool { return s.focus == PanelWorkflows })
	p.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("program did not exit")
	}
}

func TestBranchHitTargetsRequireActiveRepo(t *testing.T) {
	m := seedBranches(t, buildModel(t, 40, 24))
	m.focus = PanelRepos
	m.repos.Filter = "does-not-match"
	m.settings.Repo = ""
	m.branchList.SetItems(nil)
	r := m.panelRects()[PanelRepos]
	banner, repoH, _ := reposBranchSplit(r.h, len(m.repos.Items), len(m.branchList.Items), r.w)
	mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: r.x + 3, Y: r.y + banner + repoH + 2})
	got := mm.(Model)
	if cmd != nil || got.navBranchSet || got.repoBranchFocused {
		t.Fatal("branch box focused without an active repository")
	}
}

func TestBranchFooterHintReturnsToRepos(t *testing.T) {
	m := seedBranches(t, buildModel(t, 40, 24))
	m.focus = PanelRepos
	m.repoBranchFocused = true
	sb := m.statusBar()
	_, spans := sb.Layout(m.st)
	x := -1
	for _, span := range spans {
		if sb.Hints[span.Index].Action == "left" {
			x = span.Start
			break
		}
	}
	if x < 0 {
		t.Fatal("return hint missing")
	}
	mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x, Y: m.height - 1})
	got := mm.(Model)
	if cmd != nil || got.repoBranchFocused || got.focus != PanelRepos {
		t.Fatal("return hint did not focus repositories")
	}
}

func TestBranchSelectionRetainsUnfilteredReleaseAssets(t *testing.T) {
	for _, source := range []string{"releases", "auto"} {
		t.Run(source, func(t *testing.T) {
			m := seedBranches(t, buildModel(t, 80, 30))
			m.settings.Source = source
			m.releases = []ghapi.Release{{TagName: "v1", Assets: []ghapi.ReleaseAsset{{ID: 1, Name: "release.apk"}}}}
			m.focus = PanelRepos
			m.repoBranchFocused = true
			m.branchList.SelectByID("feature/test")
			mm, cmd := m.Update(keyOf("enter"))
			m = mm.(Model)
			if cmd == nil {
				t.Fatal("missing run fetch")
			}
			if source == "releases" && len(m.arts.Items) == 0 {
				t.Fatal("explicit release assets disappeared")
			}
			mm, _ = m.Update(RunsMsg{Repo: m.settings.Repo, Workflow: m.settings.Workflow, Branch: "feature/test", Err: errors.New("offline")})
			m = mm.(Model)
			if len(m.arts.Items) == 0 || !m.usingReleaseAssets() {
				t.Fatal("release fallback disappeared after run failure")
			}
		})
	}
}

func TestBranchTouchSwipeScrollsWithoutSelecting(t *testing.T) {
	m := seedBranches(t, buildModel(t, 32, 18))
	m.focus = PanelRepos
	r := m.panelRects()[PanelRepos]
	banner, repoH, branchH := reposBranchSplit(r.h, len(m.repos.Items), len(m.branchList.Items), r.w)
	x, y := r.x+3, r.y+1+banner+repoH+branchH-2
	before := m.branchSel
	mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y})
	m = mm.(Model)
	if cmd != nil || m.branchSel != before {
		t.Fatal("press selected branch before swipe")
	}
	old := m.branchList.Cursor
	mm, cmd = m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: x, Y: y - 1})
	m = mm.(Model)
	if cmd != nil || m.branchList.Cursor <= old || m.branchSel != before {
		t.Fatal("swipe did not scroll safely")
	}
	mm, cmd = m.Update(tea.MouseMsg{Type: tea.MouseRelease, Action: tea.MouseActionRelease, X: x, Y: y - 1})
	m = mm.(Model)
	if cmd != nil || m.navBranchSet || m.branchSel != before {
		t.Fatal("swipe release selected a branch")
	}
}
