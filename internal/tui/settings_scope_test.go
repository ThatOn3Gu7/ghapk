package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ThatOn3Gu7/ghapk/internal/config"
	"github.com/ThatOn3Gu7/ghapk/internal/device"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
	tea "github.com/charmbracelet/bubbletea"
)

func TestSettingsDefaultsFollowRepoAndPersist(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := startSelectionSession(t, Options{Settings: config.Defaults(), StartRepo: "acme/one"})
	m.focus = PanelSettings
	for _, key := range []string{"theme", "glyphs", "notify-on", "speak", "toast", "mouse", "watch", "pkg-check"} {
		m.settingsUI.cursor = rowIndex(key)
		if got := m.settingsUI.scope(&m); got != "global" {
			t.Fatalf("%s scope = %s", key, got)
		}
	}
	for _, key := range []string{"branch", "workflow", "source", "artifact", "file", "exclude", "package"} {
		m.settingsUI.cursor = rowIndex(key)
		if got := m.settingsUI.scope(&m); got != "this repo" {
			t.Fatalf("%s scope = %s", key, got)
		}
	}
	m.settingsUI.cursor = rowIndex("theme")
	m = updateKey(t, m, "enter") // mocha, global
	m.settingsUI.cursor = rowIndex("notify-on")
	m = updateKey(t, m, "enter") // success, global
	m.settingsUI.cursor = rowIndex("file")
	m = updateKey(t, m, "enter")
	m.settingsUI.input.SetValue("*release.apk")
	m = updateKey(t, m, "enter")
	if m.settings.Theme != "mocha" || m.settings.NotifyOn != "success" || m.settings.FilePattern != "*release.apk" {
		t.Fatalf("changes not applied: %+v", m.settings)
	}
	if config.HasRepoSetting("acme/one", "theme") || config.HasRepoSetting("acme/one", "notify-on") || !config.HasRepoSetting("acme/one", "file") {
		t.Fatal("settings saved to the wrong scopes")
	}
	if err := m.setRepo("acme/two", true); err != nil {
		t.Fatal(err)
	}
	if m.settings.Theme != "mocha" || m.settings.NotifyOn != "success" || m.settings.FilePattern != "" {
		t.Fatalf("wrong settings in second repo: %+v", m.settings)
	}
	restarted := startSelectionSession(t, Options{Settings: config.Load()})
	if restarted.settings.Repo != "acme/two" || restarted.settings.Theme != "mocha" || restarted.settings.NotifyOn != "success" {
		t.Fatalf("global settings not restored after restart: %+v", restarted.settings)
	}
	if err := restarted.setRepo("acme/one", true); err != nil {
		t.Fatal(err)
	}
	if restarted.settings.FilePattern != "*release.apk" || restarted.settings.Theme != "mocha" {
		t.Fatalf("repo selection not restored: %+v", restarted.settings)
	}
}

func TestPerSettingScopeToggleAndLegacyOverrides(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	old := "paper"
	if err := config.SaveSetting("acme/one", "theme", &old); err != nil {
		t.Fatal(err)
	}
	m := startSelectionSession(t, Options{Settings: config.Defaults(), StartRepo: "acme/one"})
	m.focus = PanelSettings
	m.settingsUI.cursor = rowIndex("theme")
	m = updateKey(t, m, "enter") // global action -> mocha; old override remains
	if m.settings.Theme != "paper" || config.LoadSavedScope("").Theme != "mocha" || !strings.Contains(m.toast, "repo override") {
		t.Fatalf("legacy override lost or not reported: %+v %s", m.settings, m.toast)
	}
	m = updateKey(t, m, "s")
	if m.settingsUI.scope(&m) != "this repo" {
		t.Fatal("scope did not switch for theme")
	}
	m.settingsUI.cursor = rowIndex("notify-on")
	if m.settingsUI.scope(&m) != "global" {
		t.Fatal("changing theme scope leaked into notifications")
	}
	m.settingsUI.cursor = rowIndex("theme")
	m = updateKey(t, m, "d")
	if m.settings.Theme != "mocha" || config.HasRepoSetting("acme/one", "theme") {
		t.Fatal("reset did not restore inheritance")
	}
	m = updateKey(t, m, "enter") // this repo: mocha -> emerald
	if m.settings.Theme != "emerald" {
		t.Fatal("repo override failed")
	}
	if err := m.setRepo("acme/two", true); err != nil {
		t.Fatal(err)
	}
	if m.settings.Theme != "mocha" {
		t.Fatalf("override leaked to other repo: %q", m.settings.Theme)
	}
	restarted := startSelectionSession(t, Options{Settings: config.Load(), StartRepo: "acme/one"})
	if restarted.settings.Theme != "emerald" {
		t.Fatalf("repo override not restored: %q", restarted.settings.Theme)
	}
}

func TestThemeShortcutSavesAndRespectsOverrides(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := startSelectionSession(t, Options{Settings: config.Defaults(), StartRepo: "acme/one"})
	m.focus = PanelRepos
	m = updateKey(t, m, "t")
	if m.settings.Theme != "mocha" || config.LoadSavedScope("").Theme != "mocha" {
		t.Fatalf("theme shortcut did not save: %s", m.settings.Theme)
	}
	if err := m.setRepo("acme/two", true); err != nil {
		t.Fatal(err)
	}
	if m.settings.Theme != "mocha" {
		t.Fatal("theme shortcut did not follow repo")
	}
	m.focus = PanelSettings
	m.settingsUI.cursor = rowIndex("theme")
	m = updateKey(t, m, "s") // opt into repo-specific theme
	m.focus = PanelRepos
	m = updateKey(t, m, "t")
	if m.settings.Theme != "emerald" || !config.HasRepoSetting("acme/two", "theme") || config.LoadSavedScope("").Theme != "mocha" {
		t.Fatalf("shortcut ignored selected theme scope: %+v", m.settings)
	}
	if err := m.setRepo("acme/one", true); err != nil {
		t.Fatal(err)
	}
	if m.settings.Theme != "mocha" {
		t.Fatal("local shortcut affected other repo")
	}
	t.Setenv("GHAPK_THEME", "paper")
	m = updateKey(t, m, "t")
	if m.settings.Theme != "paper" || !strings.Contains(m.toast, "env override") {
		t.Fatalf("shortcut ignored environment override: %q %q", m.settings.Theme, m.toast)
	}
	m.opts.CLIOverrides = map[string]string{"theme": "action"}
	m = updateKey(t, m, "t")
	if m.settings.Theme != "action" || !strings.Contains(m.toast, "cli override") {
		t.Fatalf("shortcut ignored CLI override: %q %q", m.settings.Theme, m.toast)
	}
}

func TestScopeBadgeAndFooterAreTappable(t *testing.T) {
	for _, size := range [][2]int{{32, 18}, {40, 24}, {96, 30}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
			m := buildModel(t, size[0], size[1])
			m.focus = PanelSettings
			m.settingsUI.cursor = rowIndex("theme")
			view := components.StripANSI(m.View())
			if !strings.Contains(view, "save: global") {
				t.Fatalf("global badge missing: %s", view)
			}
			lineWidths(t, size[0], size[1], strings.Split(view, "\n"))
			mm, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 14, Y: 2})
			m = mm.(Model)
			if m.settingsUI.scope(&m) != "this repo" || !strings.Contains(components.StripANSI(m.View()), "save: this repo") {
				t.Fatal("badge tap did not switch to repo")
			}
			m, _ = tapHint(t, m, "s")
			if m.settingsUI.scope(&m) != "global" {
				t.Fatal("footer tap did not switch back")
			}
		})
	}
}

func TestNoRepoScopeAndSaveFailure(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := buildModel(t, 40, 24)
	m.focus = PanelSettings
	m.settings.Repo = ""
	m.settingsUI.cursor = rowIndex("file")
	m = updateKey(t, m, "s")
	if m.settingsUI.scope(&m) != "global" || !strings.Contains(m.toast, "select a repo") {
		t.Fatal("repo scope should require a selected repo")
	}
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHAPK_CONFIG_DIR", path)
	m.settingsUI.cursor = rowIndex("theme")
	old := m.settings.Theme
	m = updateKey(t, m, "enter")
	if m.settings.Theme != old || !strings.Contains(m.toast, "save failed") {
		t.Fatalf("failed save changed active setting: %s, %s", m.settings.Theme, m.toast)
	}
}

func TestStartupAndRepoSwitchApplyInterfaceAndPackage(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	for key, val := range map[string]string{"theme": "paper", "mouse": "0", "package": "com.example.one"} {
		if err := config.SaveSetting("acme/one", key, &val); err != nil {
			t.Fatal(err)
		}
	}
	m := startSelectionSession(t, Options{Settings: config.Defaults(), StartRepo: "acme/one"})
	opts := prepareStartupGlyphs(Options{Settings: config.Defaults(), StartRepo: "acme/one"})
	if opts.Settings.Theme != "paper" || opts.Settings.Mouse {
		t.Fatalf("startup interface preferences: %+v", opts.Settings)
	}
	if m.instPkg != "com.example.one" {
		t.Fatalf("package not loaded: %q", m.instPkg)
	}
	m.instApp = device.InstalledApp{Package: m.instPkg, Queryable: true}
	m.instFound = true
	// Select the other repo through the real Update path to test mouse mode commands.
	m.focus = PanelRepos
	m.repos.SetItems([]components.Item{{ID: "acme/two", Label: "acme/two"}})
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.instPkg != "" || m.instFound || m.instApp.Queryable || !m.settings.Mouse {
		t.Fatalf("stale package or mouse on repo switch: %+v", m)
	}
	if cmd == nil {
		t.Fatal("repo switch command missing")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("repo switch did not batch commands")
	}
	found := false
	for _, c := range batch {
		if reflect.ValueOf(c).Pointer() == reflect.ValueOf(tea.EnableMouseCellMotion).Pointer() {
			found = true
		}
	}
	if !found {
		t.Fatal("mouse was not re-enabled for new repo")
	}
}

func TestGlobalTextAndBooleanEditsUseSelectedScope(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	global := "*.apk"
	local := "*debug.apk"
	if err := config.SaveSetting("", "file", &global); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveSetting("acme/one", "file", &local); err != nil {
		t.Fatal(err)
	}
	m := startSelectionSession(t, Options{Settings: config.Defaults(), StartRepo: "acme/one"})
	m.focus = PanelSettings
	m.settingsUI.cursor = rowIndex("file")
	m = updateKey(t, m, "s") // edit global while repo override remains active
	m = updateKey(t, m, "enter")
	if got := m.settingsUI.input.Value(); got != global {
		t.Fatalf("global editor started with %q", got)
	}
	m.settingsUI.input.SetValue("*release.apk")
	m = updateKey(t, m, "enter")
	if m.settings.FilePattern != local || config.LoadSavedScope("").FilePattern != "*release.apk" || !strings.Contains(m.toast, "repo override") {
		t.Fatalf("global edit ignored repo override: %+v %s", m.settings, m.toast)
	}
	m = updateKey(t, m, "s")
	m = updateKey(t, m, "d")
	if m.settings.FilePattern != "*release.apk" {
		t.Fatal("repo reset did not reveal global pattern")
	}
	m.settingsUI.cursor = rowIndex("toast")
	t.Setenv("GHAPK_TOAST", "1")
	m = updateKey(t, m, "enter") // target was off, even if environment says on
	if !config.LoadSavedScope("").Toast || !m.settings.Toast || !strings.Contains(m.toast, "env override") {
		t.Fatal("boolean edit did not use underlying global value")
	}
}

func TestRepoSwitchDisablesMouseWhenRepoOverridesIt(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	value := "0"
	if err := config.SaveSetting("acme/two", "mouse", &value); err != nil {
		t.Fatal(err)
	}
	m := startSelectionSession(t, Options{Settings: config.Defaults(), StartRepo: "acme/one"})
	m.focus = PanelRepos
	m.repos.SetItems([]components.Item{{ID: "acme/two", Label: "acme/two"}})
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.settings.Mouse || cmd == nil {
		t.Fatal("repo mouse override not loaded")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("repo switch did not batch commands")
	}
	for _, c := range batch {
		if reflect.ValueOf(c).Pointer() == reflect.ValueOf(tea.DisableMouse).Pointer() {
			return
		}
	}
	t.Fatal("mouse was not disabled for the new repo")
}

func TestStartupInterfaceRespectsEnvironmentAndCLI(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	for key, value := range map[string]string{"theme": "paper", "glyphs": "ascii", "mouse": "0"} {
		if err := config.SaveSetting("acme/one", key, &value); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GHAPK_THEME", "emerald")
	opts := prepareStartupGlyphs(Options{Settings: config.Defaults(), StartRepo: "acme/one"})
	if opts.Settings.Theme != "emerald" || opts.Settings.Glyphs != "ascii" || opts.Settings.Mouse {
		t.Fatalf("startup did not respect saved/environment values: %+v", opts.Settings)
	}
	opts = prepareStartupGlyphs(Options{Settings: config.Defaults(), StartRepo: "acme/one", CLIOverrides: map[string]string{"theme": "mocha", "glyphs": "unicode", "mouse": "1"}})
	if opts.Settings.Theme != "mocha" || opts.Settings.Glyphs != "unicode" || !opts.Settings.Mouse {
		t.Fatalf("startup did not respect CLI overrides: %+v", opts.Settings)
	}
}
