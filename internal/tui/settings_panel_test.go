package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/score"
	"github.com/ghapk/ghapk/internal/tui/components"
	"github.com/ghapk/ghapk/internal/tui/theme"
)

func rowIndex(key string) int {
	for i, r := range settingRows {
		if r.Key == key {
			return i
		}
	}
	panic(key)
}
func TestSettingsPanelScopesResetAndPrecedence(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := buildModel(t, 40, 24)
	m.focus = PanelSettings
	m.settingsUI.cursor = rowIndex("theme")
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.settings.Theme != "mocha" || config.LoadForRepo(m.settings.Repo).Theme != "mocha" {
		t.Fatalf("repo save not applied: %s", m.settings.Theme)
	}
	mm, _ = m.Update(key("s"))
	m = mm.(Model)
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if config.Load().Theme != "emerald" || m.settings.Theme != "mocha" {
		t.Fatalf("global vs repo precedence: global %s active %s", config.Load().Theme, m.settings.Theme)
	}
	mm, _ = m.Update(key("s"))
	m = mm.(Model)
	mm, _ = m.Update(key("d"))
	m = mm.(Model)
	if m.settings.Theme != "emerald" {
		t.Fatalf("reset did not reveal global: %s", m.settings.Theme)
	}
	t.Setenv("GHAPK_THEME", "paper")
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.settings.Theme != "paper" || !strings.Contains(m.toast, "env override") {
		t.Fatalf("env override ignored: %s %s", m.settings.Theme, m.toast)
	}
	m.opts.CLIOverrides = map[string]string{"theme": "action"}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.settings.Theme != "action" || !strings.Contains(m.toast, "cli override") {
		t.Fatalf("cli override ignored: %s %s", m.settings.Theme, m.toast)
	}
}
func TestSettingsPanelSizingTouchAndLiveOptions(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	for _, size := range [][2]int{{32, 18}, {40, 24}, {64, 18}, {96, 30}, {120, 40}} {
		m := buildModel(t, size[0], size[1])
		m.focus = PanelSettings
		lines := strings.Split(m.View(), "\n")
		if len(lines) != size[1] {
			t.Fatalf("%v height %d", size, len(lines))
		}
		lineWidths(t, size[0], size[1], lines)
		if !strings.Contains(components.StripANSI(m.View()), "[default]") {
			t.Fatalf("%v source badge missing: %s", size, components.StripANSI(m.View()))
		}
		for p, r := range m.compactTabRects() {
			if r.x+r.w > size[0] {
				t.Fatalf("%v tab %v offscreen", size, p)
			}
		}
	}
	m := buildModel(t, 40, 24)
	m.focus = PanelSettings
	m.settingsUI.cursor = rowIndex("glyphs")
	m.settingsUI.adjust(&m)
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.settings.Glyphs != "ascii" || m.st.G != theme.ASCIIGlyphs {
		t.Fatal("glyph setting did not apply")
	}
	m.settingsUI.cursor = rowIndex("all-arch")
	m.settingsUI.adjust(&m)
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if !m.settings.AllArch || !score.PolicyAllowsArch("app-x86.apk", prefsFromSettings(m.settings).Arch, score.Device{ABIs: []string{"arm64-v8a"}}) {
		t.Fatal("all-arch did not affect policy")
	}
	m.settingsUI.cursor = 0
	m.settingsUI.adjust(&m)
	before := m.settingsUI.cursor
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseWheelDown, X: 5, Y: 5})
	m = mm.(Model)
	if m.settingsUI.cursor <= before {
		t.Fatal("mouse wheel did not scroll")
	}
	m.settingsUI.cursor = rowIndex("limit")
	m.settingsUI.adjust(&m)
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	m.settingsUI.input.SetValue("0")
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if !m.settingsUI.editing || m.settings.RunLimit == 0 {
		t.Fatal("invalid integer saved")
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mm.(Model)
	if m.settingsUI.editing {
		t.Fatal("escape did not cancel")
	}
	_ = os.Remove(config.RepoPath(m.settings.Repo))
}
func TestSettingsTouchAndRefreshTimer(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := buildModel(t, 40, 24)
	m.focus = PanelSettings
	m.settingsUI.cursor = 0
	m.settingsUI.adjust(&m)
	_ = m.View()
	// The second setting appears after the group heading and first row.
	x, y := 5, 5
	mm, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y})
	m = mm.(Model)
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseRelease, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease, X: x, Y: y})
	m = mm.(Model)
	if m.settingsUI.cursor != 1 || m.settingsUI.editing {
		t.Fatalf("first tap did not select: %d", m.settingsUI.cursor)
	}
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y})
	m = mm.(Model)
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseRelease, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease, X: x, Y: y})
	m = mm.(Model)
	if !m.settingsUI.editing {
		t.Fatal("second tap did not edit")
	}
	hints := m.statusBar().Hints
	if len(hints) != 2 || hints[0].Action != "enter" || hints[1].Action != "esc" {
		t.Fatalf("editing hints: %+v", hints)
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mm.(Model)
	m.settingsUI.cursor = rowIndex("auto-refresh")
	m.settingsUI.adjust(&m)
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	m.settingsUI.input.SetValue("0")
	seq := m.refreshSeq
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.settings.AutoRefreshSecs != 0 || m.refreshSeq <= seq || cmd != nil {
		t.Fatalf("timer not stopped: %d seq=%d cmd=%v", m.settings.AutoRefreshSecs, m.refreshSeq, cmd != nil)
	}
	_, cmd = m.Update(RefreshTickMsg{Seq: seq})
	if cmd != nil {
		t.Fatal("stale timer was not discarded")
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	m.settingsUI.input.SetValue("10")
	mm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.settings.AutoRefreshSecs != 10 || cmd == nil {
		t.Fatal("timer not restarted")
	}
}
func TestEditingFilterDoesNotPersistTemporaryEnvOrCLIChoice(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	t.Setenv("GHAPK_BRANCH", "env-branch")
	branch := "old-selection"
	if err := config.RememberSelection("acme/rocket", true, &branch, nil); err != nil {
		t.Fatal(err)
	}
	m := buildModel(t, 40, 24)
	m.focus = PanelSettings
	m.settingsUI.cursor = rowIndex("branch")
	v := "saved-branch"
	m.settingsUI.change(&m, &v)
	if m.branchSel != "env-branch" {
		t.Fatalf("environment not respected: %q", m.branchSel)
	}
	t.Setenv("GHAPK_BRANCH", "")
	restarted := startSelectionSession(t, Options{Settings: config.Load()})
	if restarted.branchSel != "saved-branch" {
		t.Fatalf("temporary value persisted instead of saved branch: %q", restarted.branchSel)
	}
}
