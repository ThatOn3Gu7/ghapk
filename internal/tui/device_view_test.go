package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
	"github.com/ghapk/ghapk/internal/tui/theme"
)

func richDeviceModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := buildModel(t, width, height)
	m.focus = PanelDevice
	m.dev = device.Info{OS: "android", Model: "Pixel 8", Release: "Android 14", SDK: 34, ABIs: []string{"arm64-v8a", "armeabi-v7a"}, Kernel: "6.1", Cores: 8, MemTotalMB: 8192, MemFreeMB: 4096, DiskTotalGB: 128, DiskFreeGB: 32, InstallVia: "termux-open", CanInstall: true, HasRish: true, Terminal: "termux", Shell: "bash"}
	m.ghVersion = "2.62.0"
	m.authUser = "tester"
	m.instPkg = "com.example.app"
	m.instFound = true
	m.instApp = device.InstalledApp{Package: m.instPkg, VersionName: "1.2", VersionCode: 12, CertSHA256: strings.Repeat("a", 64), Queryable: true}
	m.deviceChecks = deviceChecks{setupOK: true, setup: device.Setup{Installers: []string{"termux-open", "am"}, RishPresent: true, Notifications: true, PackageManager: "rish"}, rateOK: true, rate: ghapi.RateLimit{Remaining: 4800, Limit: 5000, Reset: time.Now().Add(time.Hour)}, cacheOK: true, cacheBytes: 1000, freeOK: true, freeBytes: 800 << 20}
	m.deviceHistoryRepo = m.settings.Repo
	m.deviceHistoryLoaded = true
	m.deviceHistoryOK = true
	m.deviceHistory = config.HistoryEntry{Installs: 2, Run: 42, When: 1700000000}
	return m
}

func TestDeviceRichDetailsWrapAndScroll(t *testing.T) {
	t.Setenv("GHAPK_ASCII", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("GHAPK_CACHE_DIR", t.TempDir()+"/a-very-long-cache-path-that-must-wrap")
	for _, sz := range [][2]int{{32, 18}, {40, 24}, {80, 26}, {120, 40}} {
		t.Run(fmt.Sprint(sz), func(t *testing.T) {
			m := richDeviceModel(t, sz[0], sz[1])
			all := components.StripANSI(strings.Join(m.deviceLines(m.width), "\n"))
			for _, label := range []string{"Device", "Pixel 8", "Android 14", "arm64-v8a", "kernel", "cpu", "ram", "storage", "Session", "terminal", "shell", "GitHub", "signed in", "@tester", "rate limit", "4800 / 5000", "Install", "termux-open", "pkg manager", "reachable", "Shizuku", "notifications", "App", "com.example.", "installed", "signing key", "last handoff", "Files", "config", "cache", "free space", "log"} {
				if !strings.Contains(all, label) {
					t.Errorf("%q missing in %s", label, all)
				}
			}
			view := m.View()
			lineWidths(t, m.width, m.height, strings.Split(view, "\n"))
			if !strings.Contains(components.StripANSI(view), "Pixel 8") {
				t.Fatal("top of device page missing")
			}
			mm, _ := m.Update(key("G"))
			m = mm.(Model)
			view = m.View()
			lineWidths(t, m.width, m.height, strings.Split(view, "\n"))
			plain := components.StripANSI(view)
			if !strings.Contains(plain, "log") {
				t.Fatalf("bottom inaccessible: %s", plain)
			}
			mm, _ = m.Update(key("g"))
			m = mm.(Model)
			if m.deviceScroll != 0 {
				t.Fatal("g did not return to top")
			}
			mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseWheelDown, X: 5, Y: 5})
			m = mm.(Model)
			if m.deviceScroll != 3 {
				t.Fatalf("wheel: %d", m.deviceScroll)
			}
			mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, Action: tea.MouseActionPress, X: 5, Y: 10})
			m = mm.(Model)
			mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseMotion, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 5, Y: 6})
			m = mm.(Model)
			wantScroll := min(7, max(0, len(m.deviceLines(m.width))-(m.height-5)))
			if m.deviceScroll != wantScroll {
				t.Fatalf("swipe: %d, want %d", m.deviceScroll, wantScroll)
			}
			mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseRelease, Action: tea.MouseActionRelease, X: 5, Y: 6})
			m = mm.(Model)
			if m.deviceDrag {
				t.Fatal("drag did not end")
			}
		})
	}
}

func TestDeviceUnknownDoesNotClaimNotInstalled(t *testing.T) {
	m := richDeviceModel(t, 80, 26)
	m.instFound = false
	m.instApp = device.InstalledApp{Package: m.instPkg}
	m.deviceChecks.setupOK = false
	m.deviceChecks.rateOK = false
	all := components.StripANSI(strings.Join(m.deviceLines(m.width), "\n"))
	if strings.Contains(all, "not installed") || !strings.Contains(all, "unknown (package manager not reachable)") {
		t.Fatal(all)
	}
	// A stale result for a different package cannot be displayed as this app.
	m.instFound = true
	m.instApp = device.InstalledApp{Package: "com.old.app", VersionName: "old-version", Queryable: true}
	all = components.StripANSI(strings.Join(m.deviceLines(m.width), "\n"))
	if strings.Contains(all, "old-version") {
		t.Fatal(all)
	}
}

func TestDeviceMiddleAndTabOrdering(t *testing.T) {
	m := richDeviceModel(t, 120, 30)
	view := components.StripANSI(m.View())
	lines := strings.Split(view, "\n")
	var row string
	for _, l := range lines {
		if strings.Contains(l, "Pixel 8") {
			row = l
			break
		}
	}
	if strings.Index(row, "Pixel 8") < 30 {
		t.Fatalf("device content not centered: %q", row)
	}
	for _, tc := range []struct {
		key   string
		panel PanelID
	}{{"4", PanelDownloads}, {"5", PanelDevice}} {
		mm, _ := m.Update(key(tc.key))
		if mm.(Model).focus != tc.panel {
			t.Fatalf("%s did not select %v", tc.key, tc.panel)
		}
	}
	labels, _ := m.tabLabels()
	if strings.Index(lines[1], labels[PanelDownloads]) > strings.Index(lines[1], labels[PanelDevice]) {
		t.Fatalf("wrong tab order: %s", lines[1])
	}
}

func TestInstallAndDownloadFollowEveryTheme(t *testing.T) {
	withTrueColor(t)
	for _, name := range theme.Order {
		t.Run(name, func(t *testing.T) {
			m := buildModel(t, 80, 26)
			m.applyTheme(name)
			mm, _ := m.Update(key("i"))
			m = mm.(Model)
			plan := m.planBox()
			buttons := ""
			for _, line := range strings.Split(plan, "\n") {
				if strings.Contains(line, "Download only") {
					buttons = line
					break
				}
			}
			if buttons == "" {
				t.Fatal("buttons not found")
			}
			activeBG := backgroundAt(t, buttons, "Install")
			wantBG := backgroundAt(t, m.st.ButtonFocus.Padding(0, 0).Render("BUTTON"), "BUTTON")
			if activeBG != wantBG {
				t.Fatalf("install uses %q, theme uses %q", activeBG, wantBG)
			}
			if m.downloadStyles().P != m.st.P {
				t.Fatal("download palette differs from active theme")
			}
			j := &downloadJob{got: 50, total: 100}
			bar := m.downloadBar(j, 10, m.downloadStyles())
			want := components.StyleSeq(lipgloss.NewStyle().Foreground(m.st.P.Accent))
			if !strings.Contains(bar, want) {
				t.Fatalf("bar not themed: %q", bar)
			}
		})
	}
}

func TestBoxedListUsesActiveTheme(t *testing.T) {
	withTrueColor(t)
	for _, name := range theme.Order {
		st := theme.New(theme.Get(name))
		list := components.List{Cursor: 1, MaxShown: 3, Items: []components.Item{{ID: 1, Label: "selected-only"}, {ID: 2, Label: "cursor-only"}, {ID: 3, Label: "plain"}}}
		got := renderBoxedList(list, st, 40, 5, true, func(it components.Item) bool { return it.ID == 1 })
		selectedWant := backgroundAt(t, lipgloss.NewStyle().Background(st.P.Accent).Render("selected"), "selected")
		cursorWant := backgroundAt(t, lipgloss.NewStyle().Background(st.P.Selection).Render("cursor"), "cursor")
		if selected := backgroundAt(t, got, "selected-only"); selected != selectedWant {
			t.Errorf("%s selected: %s, want %s", name, selected, selectedWant)
		}
		if cursor := backgroundAt(t, got, "cursor-only"); cursor != cursorWant {
			t.Errorf("%s cursor: %s, want %s", name, cursor, cursorWant)
		}
	}
}
