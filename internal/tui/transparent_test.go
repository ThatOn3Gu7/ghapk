package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
	"github.com/ghapk/ghapk/internal/tui/theme"
	"github.com/muesli/termenv"
)

var sgrPattern = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// backgroundAt reports the effective SGR background when a visible label is
// written. This checks actual terminal output, including nested style resets.
func backgroundAt(t *testing.T, output, label string) string {
	t.Helper()
	end := strings.Index(output, label)
	if end < 0 {
		t.Fatalf("%q missing from rendered output", label)
	}
	background := ""
	for _, match := range sgrPattern.FindAllStringSubmatch(output[:end], -1) {
		codes := strings.Split(match[1], ";")
		for i := 0; i < len(codes); i++ {
			code, _ := strconv.Atoi(codes[i])
			switch {
			case code == 0 || code == 49:
				background = ""
			case code >= 40 && code <= 47 || code >= 100 && code <= 107:
				background = codes[i]
			case (code == 38 || code == 48) && i+1 < len(codes):
				length := 0
				switch codes[i+1] {
				case "2":
					length = 5
				case "5":
					length = 3
				}
				if length > 0 && i+length <= len(codes) {
					if code == 48 {
						background = strings.Join(codes[i:i+length], ";")
					}
					i += length - 1
				}
			}
		}
	}
	return background
}

func withTrueColor(t *testing.T) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

func TestPanelsAndStatusLeaveTerminalBackground(t *testing.T) {
	withTrueColor(t)
	for _, name := range theme.Order {
		t.Run(name, func(t *testing.T) {
			st := theme.New(theme.Get(name))
			for _, focused := range []bool{false, true} {
				panel := components.Panel{Title: "Panel", Focused: focused, Width: 50, Height: 5, Content: "ordinary panel text"}.Render(st)
				if strings.Contains(panel, "48;2;") {
					t.Fatalf("panel paints a background: %q", panel)
				}
			}
			status := (components.StatusBar{Width: 80, Hints: []components.Hint{{Key: "tab", Desc: "panels"}}}).Render(st)
			if strings.Contains(status, "48;2;") {
				t.Fatalf("status bar paints a background: %q", status)
			}
			panel := components.Panel{Title: "Panel", Width: 50, Height: 5, Content: "ordinary " + st.ButtonFocus.Padding(0, 0).Render("CLICK") + " plain afterward"}.Render(st)
			if bg := backgroundAt(t, panel, "CLICK"); bg == "" {
				t.Fatal("button highlight was removed")
			}
			if bg := backgroundAt(t, panel, "plain afterward"); bg != "" {
				t.Fatalf("button background leaked into panel: %s", bg)
			}
		})
	}
}

func TestRepoAndWorkflowRowsKeepTheirHighlights(t *testing.T) {
	withTrueColor(t)
	for _, name := range theme.Order {
		st := theme.New(theme.Get(name))
		list := components.List{Cursor: 1, MaxShown: 3, Items: []components.Item{
			{ID: 1, Label: "active-selection"},
			{ID: 2, Label: "current-cursor"},
			{ID: 3, Label: "ordinary-row"},
		}}
		content := renderBoxedList(list, st, 48, 5, true, func(item components.Item) bool { return item.ID == 1 })
		view := (components.Panel{Title: "Rows", Width: 52, Height: 7, Focused: true, Content: content}).Render(st)
		for _, label := range []string{"active-selection", "current-cursor"} {
			if bg := backgroundAt(t, view, label); bg == "" {
				t.Errorf("%s: %s lost highlight", name, label)
			}
		}
		if bg := backgroundAt(t, view, "ordinary-row"); bg != "" {
			t.Errorf("%s: unselected row background: %s", name, bg)
		}
	}
}

func TestDashboardUsesTerminalBackgroundAndKeepsHighlights(t *testing.T) {
	withTrueColor(t)
	for _, name := range theme.Order {
		for _, width := range []int{40, 64, 100} {
			t.Run(fmt.Sprintf("%s/%d", name, width), func(t *testing.T) {
				m := buildModel(t, width, 30)
				m.applyTheme(name)
				if width < 64 {
					m.focus = PanelArtifacts
					m.buildDetail = true
					m.updateListBounds()
				}
				view := m.View()
				for _, label := range []string{"acme/rocket", "Details"} {
					if bg := backgroundAt(t, view, label); bg != "" {
						t.Errorf("ordinary %q has background %s", label, bg)
					}
				}
				for _, label := range []string{"Install", "Download", "Open"} {
					detailView := m.buildDetails(m.buildsLayout().details)
					if bg := backgroundAt(t, detailView, label); bg == "" {
						t.Errorf("action %q is not highlighted", label)
					}
				}
				if width >= 64 {
					if bg := backgroundAt(t, view, "fix login crash"); bg == "" {
						t.Error("selected build row lost its highlight")
					}
				}
				lines := strings.Split(view, "\n")
				lineWidths(t, width, 30, lines)
			})
		}
	}
}

func TestSetupAndModalPaddingUseTerminalBackground(t *testing.T) {
	withTrueColor(t)
	for _, name := range theme.Order {
		t.Run(name, func(t *testing.T) {
			m := newTestModel()
			m.applyTheme(name)
			st := m.st
			modal := m.overlayModal("Test", "ordinary modal text")
			if strings.Contains(modal, "48;2;") {
				t.Fatalf("overlay paints background: %q", modal)
			}
			generic := components.Modal(st, 100, 30, "Test", "ordinary modal text")
			if strings.Contains(generic, "48;2;") {
				t.Fatalf("confirm padding paints background: %q", generic)
			}
			confirm := (components.Confirm{Title: "Safety", Body: []string{"ordinary warning text"}, Options: []string{"Proceed", "Cancel"}}).Render(st, 100, 30)
			if bg := backgroundAt(t, confirm, "ordinary warning text"); bg != "" {
				t.Fatalf("warning body background: %s", bg)
			}
			if bg := backgroundAt(t, confirm, "Proceed"); bg == "" {
				t.Fatal("selected confirmation button lost highlight")
			}
			if bg := backgroundAt(t, confirm, "Cancel"); bg != "" {
				t.Fatalf("unselected button background: %s", bg)
			}
			if strings.Contains(strings.Split(confirm, "\n")[0], "48;2;") {
				t.Fatal("confirmation outer padding painted")
			}

			m.screen = ScreenSetup
			m.authErr = ghapi.ErrNoGH
			setup := m.View()
			if bg := backgroundAt(t, setup, "the GitHub CLI"); bg != "" {
				t.Fatalf("setup body background: %s", bg)
			}
			if strings.Contains(strings.Split(setup, "\n")[0], "48;2;") {
				t.Fatal("setup outer padding painted")
			}
		})
	}
}
