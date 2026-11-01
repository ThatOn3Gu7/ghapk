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
)

func TestOfflineStartupNavigationAndRecovery(t *testing.T) {
	for _, size := range [][2]int{{32, 18}, {40, 24}, {80, 26}} {
		for _, ascii := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/ascii=%v", size, ascii), func(t *testing.T) {
				t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
				settings := config.Defaults()
				settings.Repo = "owner/repo"
				if ascii {
					settings.Glyphs = "ascii"
				}
				m := New(Options{Settings: settings})
				mm, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m = mm.(Model)
				if !strings.Contains(components.StripANSI(m.View()), "checking GitHub") {
					t.Fatal("startup check is invisible")
				}
				mm, _ = m.Update(AuthCheckedMsg{Version: "2.23", Err: fmt.Errorf("check: %w", ghapi.ErrOffline)})
				m = mm.(Model)
				if m.screen != ScreenDashboard || m.authChecking || m.branchLoading || m.busy {
					t.Fatalf("wrong offline state: screen=%d busy=%v branch=%v", m.screen, m.busy, m.branchLoading)
				}
				view := components.StripANSI(m.View())
				if !strings.Contains(view, "GitHub offline") || !strings.Contains(view, "r:retry") {
					t.Fatalf("offline guidance missing: %s", view)
				}
				lineWidths(t, size[0], size[1], strings.Split(view, "\n"))
				m = updateKey(t, m, "5")
				if m.focus != PanelSettings || !strings.Contains(components.StripANSI(m.View()), "GitHub offline") {
					t.Fatal("settings not accessible offline")
				}
				// Printable text must remain editable while offline, including r.
				m.settingsUI.cursor = rowIndex("file")
				m = updateKey(t, m, "enter")
				m = updateKey(t, m, "r")
				if !strings.Contains(m.settingsUI.input.Value(), "r") || m.authChecking {
					t.Fatal("retry intercepted text editing")
				}
				m = updateKey(t, m, "esc")
				m, _ = tapHint(t, m, "r")
				if !m.authChecking || !strings.Contains(components.StripANSI(m.View()), "checking GitHub") {
					t.Fatal("retry did not show checking")
				}
				before := components.StripANSI(m.authSpinner())
				mm, cmd := m.Update(m.spin.Tick())
				m = mm.(Model)
				if cmd == nil || before == components.StripANSI(m.authSpinner()) {
					t.Fatal("checking spinner did not animate")
				}
				mm, cmd = m.Update(key("r"))
				m = mm.(Model)
				if cmd != nil {
					t.Fatal("repeated retry started another check")
				}
				mm, _ = m.Update(AuthCheckedMsg{Version: "2.23", Err: ghapi.ErrOffline})
				m = mm.(Model)
				if m.authChecking || m.focus != PanelSettings || !strings.Contains(components.StripANSI(m.View()), "GitHub offline") {
					t.Fatal("failed retry lost state or warning")
				}
				m = updateKey(t, m, "r")
				mm, cmd = m.Update(AuthCheckedMsg{Version: "2.23", User: "alice"})
				m = mm.(Model)
				if m.authChecking || m.authErr != nil || m.focus != PanelSettings || m.settings.Repo != "owner/repo" || cmd == nil {
					t.Fatal("reconnection did not preserve position and refresh")
				}
				if strings.Contains(components.StripANSI(m.View()), "GitHub offline") {
					t.Fatal("offline warning remained after reconnect")
				}
			})
		}
	}
}

func TestOfflinePickerAndLocalBranches(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := New(Options{Settings: config.Defaults()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m = mm.(Model)
	mm, _ = m.Update(AuthCheckedMsg{Err: ghapi.ErrOffline})
	m = mm.(Model)
	if m.modal != ModalNone {
		t.Fatal("offline startup hid dashboard with picker")
	}
	mm, _ = m.Update(key("R"))
	m = mm.(Model)
	if m.repoPicker == nil || m.repoPicker.loading || !strings.Contains(m.repoPicker.errMsg, "offline") {
		t.Fatal("offline repo picker did not show local-only status")
	}
	m = pressText(m, "someone/project")
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	if m.settings.Repo != "someone/project" {
		t.Fatalf("could not select repo offline: %q", m.settings.Repo)
	}
	mm, _ = m.Update(LocalBranchesMsg{Repo: m.settings.Repo, Names: []string{"local-branch"}})
	m = mm.(Model)
	mm, cmd := m.Update(key("B"))
	m = mm.(Model)
	if cmd != nil || m.branchPicker == nil || m.branchPicker.loading || len(m.branchPicker.options) < 2 {
		t.Fatal("local branch picker not usable offline")
	}
	m.branchPicker.list.SelectByID("local-branch")
	mm, cmd = m.Update(keyOf("enter"))
	m = mm.(Model)
	if m.branchSel != "local-branch" || cmd != nil {
		t.Fatal("local branch selection failed or fetched network")
	}
}

func TestOfflinePickerRetryAndRecovery(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := New(Options{Settings: config.Defaults()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m = mm.(Model)
	mm, _ = m.Update(AuthCheckedMsg{Err: ghapi.ErrOffline})
	m = mm.(Model)
	mm, _ = m.Update(key("R"))
	m = mm.(Model)
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = mm.(Model)
	if cmd == nil || !m.authChecking || !m.repoPicker.loading || !strings.Contains(m.repoPicker.view(&m), "Checking GitHub") {
		t.Fatal("picker retry lacks feedback")
	}
	mm, _ = m.Update(AuthCheckedMsg{Err: ghapi.ErrOffline})
	m = mm.(Model)
	if m.repoPicker.loading || m.authChecking || m.repoPicker.errMsg == "" {
		t.Fatal("failed picker retry kept spinning")
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = mm.(Model)
	mm, cmd = m.Update(AuthCheckedMsg{Version: "2", User: "alice"})
	m = mm.(Model)
	if cmd == nil || m.repoPicker == nil || !m.repoPicker.loading || !m.userReposLoading || m.authErr != nil {
		t.Fatal("reconnected picker did not start discovery")
	}
}

func TestSetupRetryShowsRealSpinner(t *testing.T) {
	for _, size := range [][2]int{{32, 18}, {40, 24}, {80, 26}} {
		for _, reason := range []error{ghapi.ErrNoGH, ghapi.ErrNoAuth, ghapi.ErrAuthCheck} {
			t.Run(fmt.Sprintf("%v/%v", size, reason), func(t *testing.T) {
				t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
				m := New(Options{Settings: config.Defaults()})
				mm, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m = mm.(Model)
				mm, _ = m.Update(AuthCheckedMsg{Err: reason})
				m = mm.(Model)
				if m.screen != ScreenSetup || m.authChecking {
					t.Fatal("authentication failure not in setup")
				}
				mm, cmd := m.Update(key("r"))
				m = mm.(Model)
				view := components.StripANSI(m.View())
				if cmd == nil || !m.authChecking || !strings.Contains(view, "Checking GitHub") || !strings.Contains(view, "quit") {
					t.Fatalf("setup retry lacks feedback: %s", view)
				}
				lineWidths(t, size[0], size[1], strings.Split(view, "\n"))
				mm, cmd = m.Update(m.spin.Tick())
				m = mm.(Model)
				if cmd == nil {
					t.Fatal("spinner stopped while checking")
				}
				mm, _ = m.Update(AuthCheckedMsg{Err: reason})
				m = mm.(Model)
				if m.authChecking || !errors.Is(m.authErr, reason) || strings.Contains(components.StripANSI(m.View()), "Checking GitHub") {
					t.Fatal("finished check still spinning")
				}
			})
		}
	}
}
