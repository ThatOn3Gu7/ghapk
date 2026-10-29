package tui

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"
)

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func renderedForeground(s string) color.Color {
	cells := cellbuf.NewBuffer(1, 1)
	cellbuf.SetContent(cells, s)
	return cells.Cell(0, 0).Style.Fg
}

// Check the actual ANSI-rendered cells, not just the focus flag. The box title
// is inserted separately from its frame and used to color the entire top line
// as focused even while the rest of the branch box was unfocused.
func assertBranchTopColors(t *testing.T, m Model, focused bool) {
	t.Helper()
	view := m.View()
	rows := strings.Split(view, "\n")
	cells := cellbuf.NewBuffer(m.width, m.height)
	cellbuf.SetContent(cells, view)
	for y, line := range rows {
		plain := components.StripANSI(line)
		i := strings.Index(plain, "Branches")
		if i < 0 {
			continue
		}
		x := components.VisibleWidth(plain[:i])
		borderStyle := m.st.P.Border
		titleStyle := m.st.PanelTitle
		if focused {
			borderStyle = m.st.P.BorderFocus
			titleStyle = m.st.PanelTitleFoc
		}
		borderColor := renderedForeground(lipgloss.NewStyle().Foreground(borderStyle).Render("X"))
		titleColor := renderedForeground(titleStyle.Render("X"))
		title := "Branches"
		if m.branchLoading {
			title = "Branches · loading"
		}
		for _, dx := range []int{-2, components.VisibleWidth(title) + 2} {
			got := cells.Cell(x+dx, y)
			if got == nil || !sameColor(got.Style.Fg, borderColor) {
				t.Fatalf("focused=%v border x=%d: got %+v, want %v", focused, x+dx, got, borderColor)
			}
		}
		if got := cells.Cell(x, y); got == nil || !sameColor(got.Style.Fg, titleColor) {
			t.Fatalf("focused=%v title: got %+v, want %v", focused, got, titleColor)
		}
		return
	}
	t.Fatal("Branches title missing from dashboard")
}

func TestBranchBoxBorderTracksFocus(t *testing.T) {
	withTrueColor(t)
	for _, name := range theme.Order {
		for _, size := range [][2]int{{32, 18}, {64, 24}, {120, 40}} {
			for _, ascii := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%dx%d/ascii=%v", name, size[0], size[1], ascii), func(t *testing.T) {
					t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
					m := seedBranches(t, buildModel(t, size[0], size[1]))
					m.applyTheme(name)
					if ascii {
						m.settings.Glyphs = "ascii"
					}
					m.focus = PanelRepos
					m.repoBranchFocused = false
					m.repos.SelectByID(m.settings.Repo)
					assertBranchTopColors(t, m, false)
					mm, _ := m.Update(keyOf("enter"))
					m = mm.(Model)
					assertBranchTopColors(t, m, false)
					mm, _ = m.Update(keyOf("enter"))
					m = mm.(Model)
					if !m.repoBranchFocused {
						t.Fatal("second enter did not focus branches")
					}
					assertBranchTopColors(t, m, true)
					mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
					m = mm.(Model)
					if m.repoBranchFocused {
						t.Fatal("left did not return focus to repos")
					}
					assertBranchTopColors(t, m, false)
				})
			}
		}
	}
}

func TestBranchBorderTracksTouch(t *testing.T) {
	withTrueColor(t)
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := seedBranches(t, buildModel(t, 40, 24))
	m.focus = PanelRepos
	m.repoBranchFocused = false
	m.branchList.SelectByID("feature/test")
	assertBranchTopColors(t, m, false)
	r := m.panelRects()[PanelRepos]
	banner, repoH, _ := reposBranchSplit(r.h, len(m.repos.Items), len(m.branchList.Items), r.w)
	row := r.y + 1 + banner + repoH + 1 + m.branchList.Cursor - m.branchList.Offset
	m, _ = tapBranch(t, m, r.x+3, row)
	if !m.repoBranchFocused {
		t.Fatal("tapping a branch did not focus its box")
	}
	assertBranchTopColors(t, m, true)
}

// The loading title follows the same focus treatment as the normal title.
func TestLoadingBranchTitleTracksFocus(t *testing.T) {
	withTrueColor(t)
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := buildModel(t, 100, 30)
	m.focus = PanelRepos
	m.branchLoading = true
	m.repoBranchFocused = false
	assertBranchTopColors(t, m, false)
	m.repoBranchFocused = true
	assertBranchTopColors(t, m, true)
}

// Retain the focused appearance of dialog titles when sharing the title helper.
func TestModalTitleRemainsFocused(t *testing.T) {
	withTrueColor(t)
	m := newTestModel()
	line := strings.Split(m.modalWindow("Dialog", "text"), "\n")[0]
	cells := cellbuf.NewBuffer(100, 1)
	cellbuf.SetContent(cells, line)
	plain := components.StripANSI(line)
	i := strings.Index(plain, "Dialog")
	x := components.VisibleWidth(plain[:i])
	if !sameColor(cells.Cell(x-2, 0).Style.Fg, renderedForeground(lipgloss.NewStyle().Foreground(m.st.P.BorderFocus).Render("X"))) ||
		!sameColor(cells.Cell(x, 0).Style.Fg, renderedForeground(m.st.PanelTitleFoc.Render("X"))) {
		t.Fatal("modal border or title lost its focused style")
	}
}
