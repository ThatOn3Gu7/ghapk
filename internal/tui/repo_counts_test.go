package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"
)

func assertRepoTitles(t *testing.T, m Model, repos, branches int) {
	t.Helper()
	view := components.StripANSI(m.View())
	for _, title := range []string{fmt.Sprintf("Repos · %d", repos), fmt.Sprintf("Branches · %d", branches)} {
		if m.asciiEnabled() {
			title = asciiInterface(title)
		}
		if !strings.Contains(view, title) {
			t.Fatalf("missing %q in %s", title, view)
		}
	}
	lines := strings.Split(view, "\n")
	lineWidths(t, m.width, m.height, lines)
}

func TestRepoAndBranchOptionCounts(t *testing.T) {
	for _, size := range [][2]int{{32, 18}, {40, 24}, {80, 26}, {120, 40}} {
		for _, ascii := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/ascii=%v", size[0], size[1], ascii), func(t *testing.T) {
				t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
				m := buildModel(t, size[0], size[1])
				m.focus = PanelRepos
				if ascii {
					m.settings.Glyphs = "ascii"
					m.applyTheme(m.settings.Theme)
				}
				m.repos.SetItems([]components.Item{
					{ID: "acme/rocket", Label: "acme/rocket"},
					{ID: "acme/other", Label: "acme/other"},
					{ID: "team/app", Label: "team/app"},
				})
				m = seedBranches(t, m)
				// Three named branches plus the selectable "all branches" option.
				assertRepoTitles(t, m, 3, 4)

				// Filtering updates the number of selectable rows, including zero.
				m.repos.Filter = "acme"
				assertRepoTitles(t, m, 2, 4)
				m.repos.Filter = "no-match"
				assertRepoTitles(t, m, 0, 4)
				m.repos.Filter = ""
				m.branchList.Filter = "feature"
				assertRepoTitles(t, m, 3, 1)
				m.branchList.Filter = "no-match"
				assertRepoTitles(t, m, 3, 0)
				m.branchList.Filter = ""

				// Adding/removing rows and asynchronous branch responses update counts.
				m.repos.SetItems(m.repos.Items[:2])
				mm, _ := m.Update(BranchesMsg{Repo: m.settings.Repo, List: []ghapi.Branch{{Name: "main"}, {Name: "new"}}})
				m = mm.(Model)
				assertRepoTitles(t, m, 2, 3)
				mm, _ = m.Update(LocalBranchesMsg{Repo: m.settings.Repo, Names: []string{"main", "local"}})
				m = mm.(Model)
				assertRepoTitles(t, m, 2, 4) // duplicated main is not a second option
			})
		}
	}
}

func TestEmptyRepoAndBranchBoxesShowZero(t *testing.T) {
	for _, size := range [][2]int{{32, 18}, {80, 26}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
			m := buildModel(t, size[0], size[1])
			m.focus = PanelRepos
			m.repos.SetItems(nil)
			m.branchList.SetItems(nil)
			m.settings.Repo = ""
			m.branchLoading = false
			assertRepoTitles(t, m, 0, 0)
			if !strings.Contains(components.StripANSI(m.View()), "no recent repos") {
				t.Fatal("empty repository guidance disappeared")
			}
		})
	}
}

func TestBranchCountFitsOnSmallScreenWhileLoading(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := buildModel(t, 32, 18)
	m.focus = PanelRepos
	m.branchLoading = true
	items := make([]components.Item, 1001)
	for i := range items {
		items[i] = components.Item{ID: fmt.Sprint(i), Label: fmt.Sprint(i)}
	}
	m.branchList.SetItems(items)
	assertRepoTitles(t, m, len(m.repos.Filtered()), 1001)
	// The optional loading label must not hide the more important count.
	for _, line := range strings.Split(components.StripANSI(m.View()), "\n") {
		if strings.Contains(line, "Branches · 1001") && strings.Contains(line, "loading") {
			t.Fatal("loading label should yield to the count on narrow screens")
		}
	}
}

func TestBranchTouchWorksWhileRepoFilterHasNoMatches(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := seedBranches(t, buildModel(t, 40, 24))
	m.focus = PanelRepos
	m.repos.Filter = "no-match"
	m.branchList.SelectByID("feature/test")
	assertRepoTitles(t, m, 0, 4)
	r := m.panelRects()[PanelRepos]
	banner, repoH, _ := reposBranchSplit(r.h, len(m.repos.Items), len(m.branchList.Items), r.w)
	row := r.y + 1 + banner + repoH + 1 + m.branchList.Cursor - m.branchList.Offset
	m, cmd := tapBranch(t, m, r.x+3, row)
	if cmd == nil || !m.repoBranchFocused || m.branchSel != "feature/test" {
		t.Fatalf("visible branch did not respond to touch: focus=%v branch=%q", m.repoBranchFocused, m.branchSel)
	}
}

func TestRepoOptionCountTracksKeyboardFiltering(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := seedBranches(t, buildModel(t, 40, 24))
	m.focus = PanelRepos
	m.repos.SetItems([]components.Item{{ID: "org/one", Label: "org/one"}, {ID: "org/two", Label: "org/two"}})
	assertRepoTitles(t, m, 2, 4)
	m = updateKey(t, m, "/")
	for _, ch := range "one" {
		m = updateKey(t, m, string(ch))
	}
	assertRepoTitles(t, m, 1, 4)
	m = updateKey(t, m, "z")
	assertRepoTitles(t, m, 0, 4)
}

func TestRepoTitleTracksFocus(t *testing.T) {
	withTrueColor(t)
	for _, name := range theme.Order {
		t.Run(name, func(t *testing.T) {
			t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
			m := seedBranches(t, buildModel(t, 40, 24))
			m.applyTheme(name)
			m.focus = PanelRepos
			for _, focused := range []bool{true, false} {
				m.repoBranchFocused = !focused
				view := m.View()
				cells := cellbuf.NewBuffer(m.width, m.height)
				cellbuf.SetContent(cells, view)
				found := false
				for y, line := range strings.Split(view, "\n") {
					plain := components.StripANSI(line)
					i := strings.Index(plain, "Repos · ")
					if i < 0 {
						continue
					}
					x := components.VisibleWidth(plain[:i])
					borderColor := m.st.P.Border
					titleStyle := m.st.PanelTitle
					if focused {
						borderColor = m.st.P.BorderFocus
						titleStyle = m.st.PanelTitleFoc
					}
					if !sameColor(cells.Cell(x-2, y).Style.Fg, renderedForeground(lipgloss.NewStyle().Foreground(borderColor).Render("X"))) ||
						!sameColor(cells.Cell(x, y).Style.Fg, renderedForeground(titleStyle.Render("X"))) {
						t.Fatalf("repository title/border has wrong style (focused=%v)", focused)
					}
					found = true
					break
				}
				if !found {
					t.Fatal("repository title missing")
				}
			}
		})
	}
}
