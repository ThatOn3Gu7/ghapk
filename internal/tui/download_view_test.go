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
	"github.com/ThatOn3Gu7/ghapk/internal/config"
	"github.com/ThatOn3Gu7/ghapk/internal/device"
	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
)

func TestDownloadsProgressBarAndResponsiveLayout(t *testing.T) {
	t.Setenv("GHAPK_ASCII", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	withTrueColor(t)
	for _, size := range [][2]int{{32, 18}, {40, 24}, {80, 26}, {120, 40}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := buildModel(t, size[0], size[1])
			m.startJob(jobSpec{repo: "one/repo", choice: planChoice{art: ghapi.Artifact{Name: "android-debug", SizeInBytes: 1000}}})
			j := m.jobs[0]
			j.phase = "Download"
			j.got = 500
			j.total = 1000
			j.started = time.Now().Add(-5 * time.Second)
			j.rate = 100
			if size[0] < 80 && !m.downloadDetail {
				t.Fatal("phone should open detail so progress is visible")
			}
			view := m.View()
			plain := components.StripANSI(view)
			if !strings.Contains(plain, "50%") || !strings.Contains(plain, "█") || !strings.Contains(plain, "░") {
				t.Fatalf("progress bar missing: %s", plain)
			}
			if !strings.Contains(plain, "Download") || !strings.Contains(plain, "/s") {
				t.Fatalf("transfer details missing: %s", plain)
			}
			lineWidths(t, size[0], size[1], strings.Split(view, "\n"))
			j.total = 0
			plain = components.StripANSI(m.View())
			if strings.Contains(plain, "100%") || !strings.Contains(plain, "unknown") {
				t.Fatalf("unknown total must be indeterminate: %s", plain)
			}
			if size[0] >= 80 && m.downloadRects().jobs.w != 38 {
				t.Fatal("wide layout should use Claude's 38-column sidebar")
			}
			j.cancel()
		})
	}
}
func TestPlanStyleAndButtonsAreTouchSafe(t *testing.T) {
	t.Setenv("GHAPK_ASCII", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	withTrueColor(t)
	for _, size := range [][2]int{{32, 18}, {40, 24}, {80, 26}, {120, 40}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := buildModel(t, size[0], size[1])
			mm, _ := m.Update(key("i"))
			m = mm.(Model)
			mm, _ = m.Update(planReadyMsg{seq: m.plan.seq, dev: device.Info{OS: "android", CanInstall: true, ABIs: []string{"arm64-v8a"}}})
			m = mm.(Model)
			box := m.planBox()
			plain := components.StripANSI(box)
			for _, label := range []string{"Build", "Device", "Installed", "Version", "Artifact", "Install", "Download only", "Cancel"} {
				if !strings.Contains(plain, label) {
					t.Fatalf("%q missing from plan:\n%s", label, plain)
				}
			}
			if !strings.Contains(plain, "◉") {
				t.Fatal("selected artifact has no radio mark")
			}
			if components.VisibleWidth(strings.Split(box, "\n")[0]) > size[0] || lipgloss.Height(box) > size[1]-2 {
				t.Fatalf("plan overflows: %s", plain)
			}
			layout := m.plan.layout(&m)
			left := (m.width-lipgloss.Width(box))/2 + 2
			top := (m.height-lipgloss.Height(box))/2 + 1
			// Cancel must always be individually clickable, even when buttons share a row.
			hit := layout.buttons[2]
			mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: left + hit.x, Y: top + hit.y})
			m = mm.(Model)
			if cmd != nil || m.modal != ModalNone || len(m.jobs) != 0 {
				t.Fatal("cancel tap was not isolated")
			}
		})
	}
}
func TestDestructiveConfirmUsesRedFrame(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	withTrueColor(t)
	m := buildModel(t, 40, 24)
	m.askRemoveRepo("one/repo")
	if !m.confirm.Danger {
		t.Fatal("repo removal must be marked destructive")
	}
	red := components.StyleSeq(lipgloss.NewStyle().Foreground(m.st.P.Error))
	if red == "" || !strings.Contains(m.modalBox(), red) {
		t.Fatalf("delete frame is not red: %q", m.modalBox())
	}
	m.confirm.Cursor = 0
	buttonRed := components.StyleSeq(m.st.ButtonFocus.Background(m.st.P.Error).Padding(0, 0))
	if !strings.Contains(m.modalBox(), buttonRed) {
		t.Fatal("selected destructive action is not red")
	}
}

func TestPhoneTabLabelsAndHitboxesMatch(t *testing.T) {
	for _, focus := range FocusOrder {
		m := buildModel(t, 32, 18)
		m.focus = focus
		labels, _ := m.tabLabels()
		line := components.StripANSI(strings.Split(m.View(), "\n")[1])
		for _, tab := range FocusOrder {
			text := labels[tab]
			i := strings.Index(line, text)
			if i < 0 {
				t.Fatalf("focus %v: tab %q clipped in %q", focus, text, line)
			}
			x := components.VisibleWidth(line[:i]) + components.VisibleWidth(text)/2
			r := m.compactTabRects()[tab]
			if !r.contains(x, 1) {
				t.Fatalf("focus %v tab %v: visible cell %d not inside %+v", focus, tab, x, r)
			}
			mm, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x, Y: 1})
			if mm.(Model).focus != tab {
				t.Fatalf("focus %v: tap %q chose %v", focus, text, mm.(Model).focus)
			}
		}
	}
}

func TestDeleteOverlayTouchOnPhone(t *testing.T) {
	root, _ := jobTestEnv(t)
	path := filepath.Join(root, "cache", "apk", "saved.apk")
	jobTestAPK(t, path, 10)
	for _, width := range []int{32, 40} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := New(Options{Settings: config.Defaults()})
			m.width = width
			m.height = 24
			m.screen = ScreenDashboard
			m.focus = PanelDownloads
			m.refreshSaved()
			m.downloadFocus = 1
			mm, _ := m.Update(key("x"))
			m = mm.(Model)
			if m.modal != ModalConfirm || !m.confirm.Danger {
				t.Fatal("delete must open the red confirmation")
			}
			tap := func(m Model, label string) Model {
				for y, line := range strings.Split(m.modalLayer(), "\n") {
					plain := components.StripANSI(line)
					if pos := strings.Index(plain, label); pos >= 0 {
						x := components.VisibleWidth(plain[:pos])
						mm, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x, Y: y})
						return mm.(Model)
					}
				}
				t.Fatalf("%s not visible", label)
				return m
			}
			m = tap(m, "Cancel")
			if m.modal != ModalNone {
				t.Fatal("cancel did not dismiss")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("cancel deleted the APK")
			}
		})
	}
}
