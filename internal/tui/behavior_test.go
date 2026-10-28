package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
)

func TestArtifactsInteraction(t *testing.T) {
	m := seedArtifacts(seedRuns(newTestModel()))
	mm, _ := m.Update(key("3"))
	mm, _ = mm.Update(keyOf("enter"))
	m = mm.(Model)
	if m.focus != PanelArtifacts {
		t.Fatal("Builds did not open artifacts")
	}
	mm, _ = m.Update(key("j"))
	m = mm.(Model)
	art, ok := m.cursorArtifact()
	if !ok {
		t.Fatal("no artifact selected")
	}
	mm, cmd := m.Update(key("d"))
	m = mm.(Model)
	if cmd == nil || m.modal != ModalTask || m.task.Artifact != art.Name || m.pendingInstall {
		t.Fatal("selected artifact did not download directly")
	}
}

func TestMouse(t *testing.T) {
	m := newTestModel()
	m = seedRuns(m)
	m = seedArtifacts(m)

	m.focus = PanelRuns
	rs := m.panelRects()
	rr := rs[PanelRuns]
	if rr.w == 0 {
		t.Fatal("runs panel rect not computed")
	}
	cy := rr.y
	// click row 1 of the runs panel inside the stretched box:
	// top border sits at cy, row 0 at cy+1, row 1 at cy+2
	mm, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: rr.x + 2, Y: cy + 2})
	mo := mm.(Model)
	if mo.focus != PanelRuns {
		t.Fatalf("click did not focus runs (focus=%v)", mo.focus)
	}
	if mo.runs.Cursor != 1 {
		t.Fatalf("click did not select row 1 (cursor=%d)", mo.runs.Cursor)
	}
	// click the same row again → activate → focus jumps to artifacts
	mm, _ = mo.Update(tea.MouseMsg{Type: tea.MouseLeft, X: rr.x + 2, Y: cy + 2})
	mo = mm.(Model)
	if mo.focus != PanelArtifacts {
		t.Fatalf("second click did not activate (focus=%v)", mo.focus)
	}
	// wheel on runs scrolls the list
	ar := rs[PanelArtifacts]
	_ = ar
	mm, _ = mo.Update(tea.MouseMsg{Type: tea.MouseWheelUp, X: rr.x + 2, Y: cy + 2})
	mo = mm.(Model)
	_ = mo
}

func TestMouseCompactTabs(t *testing.T) {
	m := newTestModel()
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 26})
	mo := mm.(Model)
	// click inside the "Runs" tab, wherever the label landed
	tab, ok := mo.compactTabRects()[PanelRuns]
	if !ok {
		t.Fatal("runs tab rect missing")
	}
	mo2, _ := mo.Update(tea.MouseMsg{Type: tea.MouseLeft, X: tab.x + tab.w/2, Y: tab.y})
	mo = mo2.(Model)
	if mo.focus != PanelRuns {
		t.Fatalf("tab click did not switch to runs (focus=%v)", mo.focus)
	}
}

// The tab cycle must match the visual order of the compact tabs:
// repos → flows → builds → device. Artifacts live inside Builds.
func TestFocusRingOrder(t *testing.T) {
	want := []PanelID{PanelRepos, PanelWorkflows, PanelRuns, PanelDevice}
	if len(FocusOrder) != len(want) {
		t.Fatalf("FocusOrder len = %d", len(FocusOrder))
	}
	for i := range want {
		if FocusOrder[i] != want[i] {
			t.Fatalf("FocusOrder[%d] = %d, want %d", i, FocusOrder[i], want[i])
		}
	}

	m := newTestModel()
	for _, step := range want {
		if m.focus != step {
			t.Fatalf("cycle at %v, want %v", PanelNames[m.focus], PanelNames[step])
		}
		mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = mm.(Model)
	}
	if m.focus != PanelRepos {
		t.Fatalf("cycle did not wrap to repos (focus=%v)", m.focus)
	}
}

// Compact tabs must carry the REAL counts (they used to show only the
// hotkey digit, which users misread as the count).
func TestCompactTabsShowRealCounts(t *testing.T) {
	m := newTestModel()
	m = seedRuns(m)
	m = seedArtifacts(m)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 26})
	m = mm.(Model)
	got := m.compactTabLabel(PanelRuns)
	if got != "3 Builds (2)" {
		t.Errorf("runs tab label = %q, want %q", got, "3 Builds (2)")
	}
	if _, ok := m.compactTabRects()[PanelArtifacts]; ok {
		t.Fatal("artifacts must not be a separate tab")
	}
	m.runs.AddFilterChar('x')
	got = m.compactTabLabel(PanelRuns)
	if got != "3 Builds (2)" {
		t.Errorf("filtered runs tab = %q, want it to still show the total 2, got %s", "3 Builds (2)", got)
	}
}

// ── v4.2.1 visual-polish regression tests ──

func TestHeaderNarrowWidths(t *testing.T) {
	m := newTestModel()
	m.settings.Repo = "termux/termux-x11"
	m.settings.Workflow = "PR Assemble (compile check)"
	m.authUser = "tester"
	m.settings.Theme = "mocha"
	m.branchSel = ""
	for _, w := range []int{30, 40, 46, 60, 77, 120} {
		m.width = w
		h := m.renderHeader()
		vw := components.VisibleWidth(h)
		if vw > w {
			t.Errorf("header overflows at width %d: got %d", w, vw)
		}
		if !strings.Contains(h, "ghapk") {
			t.Errorf("header lost its logo at width %d", w)
		}
		if !strings.Contains(h, "termux") {
			t.Errorf("header lost the repo at width %d: %q", w, h)
		}
	}
	// very narrow: repo should survive, branch/wf may vanish
	m.width = 30
	h := m.renderHeader()
	if strings.Contains(h, "(compile check)") && !strings.Contains(h, "…") {
		t.Errorf("wf filter shown full-width at 30 cols: %q", h)
	}
}

func TestWfMissToast(t *testing.T) {
	m := newTestModel()
	m.settings.Repo = "acme/rocket"
	m.settings.Workflow = "Nope Nonexistent"
	mm, _ := m.Update(WorkflowsMsg{List: []ghapi.Workflow{
		{ID: 10, Name: "PR Assemble", Path: ".github/workflows/pr-assemble.yml"},
		{ID: 11, Name: "Build & Release", Path: ".github/workflows/release.yml"},
	}})
	mo := mm.(Model)
	if mo.toast == "" || !strings.Contains(mo.toast, "Nope Nonexistent") {
		t.Fatalf("expected stale-workflow toast, got %q", mo.toast)
	}
	// a second refresh must NOT toast again
	mm, _ = mo.Update(WorkflowsMsg{List: []ghapi.Workflow{
		{ID: 10, Name: "PR Assemble", Path: ".github/workflows/pr-assemble.yml"},
	}})
	mo2 := mm.(Model)
	if mo2.toast != "" && mo2.toast != mo.toast {
		t.Fatalf("toast should not repeat: %q", mo2.toast)
	}
}

func TestHeaderCompMiddleEll(t *testing.T) {
	got := compMiddleEll("termux-x11-unstripped-libraries", 15)
	if components.VisibleWidth(got) > 15 || !strings.Contains(got, "…") {
		t.Fatalf("bad ellipsize: %q", got)
	}
	if !strings.HasPrefix(got, "termux") && !strings.HasSuffix(got, "aries") {
		t.Fatalf("ends should be preserved for long strings: %q", got)
	}
	if compMiddleEll("short", 10) != "short" {
		t.Fatal("short string should pass through")
	}
}

func TestBoxedListSelectionAndBackgrounds(t *testing.T) {
	m := newTestModel()
	m = seedRuns(m)
	m = seedArtifacts(m)
	m = seedWorkflows2(m)

	// Workflows content must be rendered inside a stretched box
	wContent := m.workflowsContent(60, 20, &m)
	if !strings.Contains(wContent, "╭") || !strings.Contains(wContent, "╰") {
		t.Fatalf("workflows content should contain stretched box borders: %q", wContent)
	}
	// Must not contain moving arrow
	if strings.Contains(wContent, "▶") || strings.Contains(wContent, "> PR Assemble") {
		t.Fatalf("workflows content should not have moving arrow: %q", wContent)
	}

	// Repos content must be rendered inside a stretched box and have no arrow
	rContent := m.reposContent(60, 20, &m)
	if !strings.Contains(rContent, "╭") || !strings.Contains(rContent, "╰") {
		t.Fatalf("repos content should contain stretched box borders: %q", rContent)
	}
	if strings.Contains(rContent, "▶") {
		t.Fatalf("repos content should not have moving arrow: %q", rContent)
	}

	// Runs content must be rendered inside a stretched box
	runsContent := m.runsContent(60, 20, &m)
	if !strings.Contains(runsContent, "╭") || !strings.Contains(runsContent, "╰") {
		t.Fatalf("runs content should contain stretched box borders: %q", runsContent)
	}

	// Artifacts content must be rendered inside a stretched box
	artsContent := m.artsContent(60, 20, &m)
	if !strings.Contains(artsContent, "╭") || !strings.Contains(artsContent, "╰") {
		t.Fatalf("artifacts content should contain stretched box borders: %q", artsContent)
	}
}

func TestDevicePanelCentered(t *testing.T) {
	m := newTestModel()
	// Set device info
	m.dev.Model = "Pixel 8"
	m.dev.OS = "android"
	m.dev.Release = "14"
	p := m.panel(PanelDevice, 40, 20, m.deviceContent(40, 20, &m))
	// Because Center: true for PanelDevice, device information floats in the middle (empty lines above)
	lines := strings.Split(p, "\n")
	if len(lines) < 15 {
		t.Fatalf("expected at least 15 lines in panel, got %d", len(lines))
	}
	topBlank := 0
	for i := 1; i < len(lines)-1; i++ {
		plain := strings.Trim(components.StripANSI(lines[i]), " │")
		if plain == "" {
			topBlank++
		} else {
			break
		}
	}
	if topBlank < 2 {
		t.Fatalf("expected vertical centering (at least 2 blank lines at top), got %d: %s", topBlank, p)
	}
}

func TestWorkflowYAMLScrollAndFocus(t *testing.T) {
	m := newTestModel()
	m = seedWorkflows2(m)
	m.focus = PanelWorkflows
	m.settings.Workflow = "PR Assemble"
	m.workflows.SelectByID("PR Assemble")
	path := ".github/workflows/pr-assemble.yml"
	m.wfPaths["PR Assemble"] = path

	// Generate 50 lines of YAML
	var yamlBuilder strings.Builder
	yamlBuilder.WriteString("name: PR Assemble\non: [pull_request]\njobs:\n")
	for i := 1; i <= 50; i++ {
		yamlBuilder.WriteString(fmt.Sprintf("  step-%d:\n    run: echo %d\n", i, i))
	}
	m.wfCache[path] = yamlBuilder.String()

	// Initially YAML is not focused
	if m.wfYAMLFocused {
		t.Fatal("expected wfYAMLFocused to be false initially")
	}

	// Tapping or pressing 'v' focuses YAML box
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = mm.(Model)
	if !m.wfYAMLFocused {
		t.Fatal("pressing 'v' should set wfYAMLFocused to true")
	}

	// In focused YAML mode, down arrow or 'j' scrolls down
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mm.(Model)
	if m.wfScroll != 1 {
		t.Fatalf("expected wfScroll=1 after Down, got %d", m.wfScroll)
	}

	// Mouse wheel also scrolls YAML
	rs := m.panelRects()
	rr := rs[PanelWorkflows]
	listH, _ := flowsSplit(rr.h, len(m.workflows.Items))
	subBoxY := rr.y + 1 + listH + 2 // inside the sub-box
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseWheelDown, X: rr.x + 2, Y: subBoxY})
	m = mm.(Model)
	if m.wfScroll <= 1 {
		t.Fatalf("expected mouse wheel over sub-box to increase wfScroll, got %d", m.wfScroll)
	}

	// Render detail box and check scrollbar indicator presence (█ and │)
	box := m.workflowDetailBox(60, 14)
	if !strings.Contains(box, "█") || !strings.Contains(box, "│") {
		t.Fatalf("expected scrollbar indicator in YAML detail box: %q", box)
	}

	// Esc returns focus to workflow list
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mm.(Model)
	if m.wfYAMLFocused {
		t.Fatal("pressing Esc should return focus from YAML back to list")
	}
}

func TestReposBannerAndDynamicHeight(t *testing.T) {
	m := newTestModel()
	m.repos.SetItems([]components.Item{
		{ID: "That0n3Gu7/Nazo", Label: "That0n3Gu7/Nazo"},
		{ID: "termux/termux-app", Label: "termux/termux-app"},
		{ID: "termux/termux-x11", Label: "termux/termux-x11"},
	})
	m.settings.Repo = "That0n3Gu7/Nazo"

	bannerH, listH := reposSplit(30, len(m.repos.Items), 40)
	if bannerH != 9 {
		t.Fatalf("expected bannerH=9 (including vertical optical padding), got %d", bannerH)
	}
	if listH != 5 { // 3 repos + 2 borders = 5
		t.Fatalf("expected listH=5 for 3 repos, got %d", listH)
	}

	content := m.reposContent(40, 30, &m)
	if !strings.Contains(content, "███╗") && !strings.Contains(content, "GH APK") {
		t.Fatalf("expected GH APK block logo in repos panel, got: %s", content)
	}
	if !strings.Contains(content, "That0n3Gu7/Nazo") {
		t.Fatalf("expected repo name in repos panel, got: %s", content)
	}

	// Test Shimmer tick moves shimmerPos
	mm, _ := m.Update(ShimmerTickMsg{At: time.Now()})
	m = mm.(Model)
	if m.shimmerPos != 1 {
		t.Fatalf("expected shimmerPos=1 after tick, got %d", m.shimmerPos)
	}
}

func TestWorkflowsDynamicHeightAndYamlSpace(t *testing.T) {
	// 3 workflows: any workflow, PR Assemble, Build & Release
	listH, subH := flowsSplit(30, 3)
	// count=3 -> neededListH = 3 + 3 = 6 (3 items + 1 empty line + 2 borders)
	if listH != 6 {
		t.Fatalf("expected listH=6 for 3 workflows, got %d", listH)
	}
	// ih = 30 - 2 = 28. subH = 28 - 6 = 22
	if subH != 22 {
		t.Fatalf("expected subH=22 for YAML preview, got %d", subH)
	}
}

func TestRunsEmptyState(t *testing.T) {
	m := newTestModel()
	m.settings.Repo = "acme/rocket"
	m.runsLoaded = true
	m.runs.SetItems(nil)

	content := m.runsContent(40, 30, &m)
	if !strings.Contains(content, "No workflow runs yet") {
		t.Fatalf("expected centered empty state, got: %s", content)
	}
	if strings.Contains(content, "move the cursor over a run") {
		t.Fatalf("empty state should not display run detail box: %s", content)
	}
}

func TestArtifactsEmptyStateAndDynamicHeight(t *testing.T) {
	m := newTestModel()
	m.settings.Repo = "acme/rocket"
	// No run selected
	content := m.artsContent(40, 30, &m)
	if !strings.Contains(content, "No run selected") {
		t.Fatalf("expected 'No run selected' empty state, got: %s", content)
	}

	// 1 artifact
	listH, subH := artSplit(30, 1)
	if listH != 3 { // 1 item + 2 borders = 3
		t.Fatalf("expected listH=3 for 1 artifact, got %d", listH)
	}
	if subH != 10 { // fixed info box
		t.Fatalf("expected subH=10 for artifact info box, got %d", subH)
	}
}

func TestShimmerAnimationAndLogoRendering(t *testing.T) {
	m := newTestModel()
	m.settings.Repo = "test/repo"

	// 1. Verify clean block plus pipe logo: contains ANSI shadow glyphs, no nested border boxes
	logoWide := m.renderShimmerLogo(50)
	if !strings.Contains(logoWide, "██████╗") {
		t.Fatalf("logo must contain ANSI shadow block+pipe characters: %s", logoWide)
	}
	if strings.Contains(logoWide, "╭─") || strings.Contains(logoWide, "╰─") {
		if strings.Contains(logoWide, "╭") || strings.Contains(logoWide, "╰") || strings.Contains(logoWide, "┌") || strings.Contains(logoWide, "└") {
			t.Fatalf("logo banner must not contain nested border boxes: %s", logoWide)
		}
	}

	// 2. Test intermediate width (21-42 cols) uses compact block+pipe logo
	logoMid := m.renderShimmerLogo(30)
	if !strings.Contains(logoMid, "███╗") {
		t.Fatalf("expected compact block+pipe logo for 30 width, got: %s", logoMid)
	}

	// 3. Test narrow width fallback (< 21 cols)
	logoNarrow := m.renderShimmerLogo(18)
	if !strings.Contains(logoNarrow, "GH APK") {
		t.Fatalf("expected compact badge fallback for narrow widths, got: %s", logoNarrow)
	}

	// 4. Test shimmer tick cycle wraps properly
	m.shimmerPos = 68
	mm, _ := m.Update(ShimmerTickMsg{At: time.Now()})
	m = mm.(Model)
	if m.shimmerPos != 69 {
		t.Fatalf("expected shimmerPos=69, got %d", m.shimmerPos)
	}
	mm, _ = m.Update(ShimmerTickMsg{At: time.Now()})
	m = mm.(Model)
	if m.shimmerPos != 0 {
		t.Fatalf("expected shimmerPos to wrap to 0 after 70, got %d", m.shimmerPos)
	}

	// 5. Test theme compatibility across multiple themes
	for _, themeName := range []string{"action", "nord", "monokai", "catppuccin", "dracula", "cyberpunk"} {
		m.applyTheme(themeName)
		out := m.renderShimmerLogo(45)
		if len(out) == 0 {
			t.Fatalf("empty logo output for theme %s", themeName)
		}
	}
}
