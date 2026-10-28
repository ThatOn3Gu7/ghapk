package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/tui/components"
)

// Builds has a full-height list on the left and artifacts/details on the right.
// A narrow terminal drills into the right side instead of squeezing the columns.
type buildsRects struct{ runs, arts, details rect }

func (m Model) activeTab() PanelID {
	if m.focus == PanelArtifacts {
		return PanelRuns
	}
	return m.focus
}

func (m Model) buildsLayout() buildsRects {
	h := m.height - 3 // header, tabs, status
	if h < 6 {
		h = 6
	}
	var l buildsRects
	if m.width < 64 {
		if !m.buildDetail {
			l.runs = rect{0, 2, m.width, h}
			return l
		}
		ah := h * 2 / 5
		if ah < 3 {
			ah = 3
		}
		if ah > 12 {
			ah = 12
		}
		l.arts = rect{0, 2, m.width, ah}
		l.details = rect{0, 2 + ah, m.width, h - ah}
		return l
	}
	lw := m.width * 42 / 100
	if lw < 38 {
		lw = 38
	}
	ah := h * 2 / 5
	if ah < 3 {
		ah = 3
	}
	if ah > 12 {
		ah = 12
	}
	l.runs = rect{0, 2, lw, h}
	l.arts = rect{lw, 2, m.width - lw, ah}
	l.details = rect{lw, 2 + ah, m.width - lw, h - ah}
	return l
}

func (m Model) renderBuilds() string {
	r := m.buildsLayout()
	if r.runs.w == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, m.buildArtifacts(r.arts), m.buildDetails(r.details))
	}
	left := m.buildRuns(r.runs)
	if r.arts.w == 0 {
		return left
	}
	right := lipgloss.JoinVertical(lipgloss.Left, m.buildArtifacts(r.arts), m.buildDetails(r.details))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (m Model) buildRuns(r rect) string {
	l := m.runs
	l.Focused = m.focus == PanelRuns
	l.MaxShown = max(1, r.h-2)
	items := make([]components.Item, 0, len(l.Items))
	for _, it := range l.Items {
		rr, ok := it.ID.(runRow)
		if !ok {
			continue
		}
		run := rr.run
		icon, _ := m.runIcon(run)
		if run.Active() {
			icon = m.spin.View()
		}
		title := firstLineS(run.DisplayTitle)
		if title == "" {
			title = run.Name
		}
		it.Rendered = fmt.Sprintf("%s %s %s", icon, m.st.Dim.Render(fmt.Sprintf("#%d", run.RunNumber)), title)
		it.Detail = reltime(run.CreatedAt)
		if it.Detail != "" {
			it.Rendered = components.Clamp(it.Rendered, max(1, r.w-5-components.VisibleWidth(it.Detail)))
		}
		items = append(items, it)
	}
	l.Items = items
	l.Empty = "No builds found. Press R to choose a repository."
	if m.settings.Repo != "" && !m.runsLoaded {
		l.Empty = m.spin.View() + " loading builds…"
	}
	badge := listBadge("Builds", &l, 0)
	if l.Filter != "" {
		badge = "/" + l.Filter + " " + badge
	}
	return components.Panel{Title: "Builds", Badge: badge, Focused: m.focus == PanelRuns, Width: r.w, Height: r.h, Content: l.Render(m.st, r.w-4)}.Render(m.st)
}

func (m Model) buildArtifacts(r rect) string {
	l := m.arts
	l.Focused = m.focus == PanelArtifacts
	l.MaxShown = max(1, r.h-2)
	l.Empty = "No artifacts for this build."
	if run, ok := m.selectedRun(); ok && m.fetching[run.ID] {
		l.Empty = m.spin.View() + " loading artifacts…"
	}
	if m.settings.Repo == "" {
		l.Empty = "Choose a repository first (R)."
	}
	if run, ok := m.selectedRun(); ok && m.artifactErrors[run.ID] != "" {
		l.Empty = "Couldn't load artifacts. Press r to retry."
	}

	if run, ok := m.selectedRun(); ok && run.Active() && len(l.Items) == 0 && !m.fetching[run.ID] && m.artifactErrors[run.ID] == "" {
		l.Empty = "Still building; artifacts appear when ready."
	}
	if m.usingReleaseAssets() {
		l.Items = append([]components.Item(nil), l.Items...)
		for i := range l.Items {
			for _, ra := range m.rankedReleases() {
				if l.Items[i].ID == -ra.Asset.ID {
					mark := "·"
					if i == 0 && !ra.Asset.Rejected {
						mark = "✓"
					}
					if ra.Asset.Rejected {
						mark = "–"
					}
					l.Items[i].Rendered = mark + " " + ra.Asset.Name
					l.Items[i].Detail = humanSize(ra.Asset.Size)
					l.Items[i].Rendered = components.Clamp(l.Items[i].Rendered, max(1, r.w-5-components.VisibleWidth(l.Items[i].Detail)))
				}
			}
		}
	} else if run, ok := m.selectedRun(); ok {
		// Copy before adding display-only fields; the model's list is shared by value.
		l.Items = append([]components.Item(nil), l.Items...)
		for i := range l.Items {
			for _, a := range m.runCache[run.ID] {
				if l.Items[i].ID == a.ID {
					mark := "·"
					if i == 0 && !a.Rejected {
						mark = "✓"
					}
					if a.Rejected {
						mark = "–"
					}
					l.Items[i].Rendered = mark + " " + a.Name
					l.Items[i].Detail = humanSize(a.SizeInBytes)
					l.Items[i].Rendered = components.Clamp(l.Items[i].Rendered, max(1, r.w-5-components.VisibleWidth(l.Items[i].Detail)))
				}
			}
		}
	}
	title := "Artifacts"
	if m.usingReleaseAssets() {
		title = "Release assets"
	}
	if m.width < 64 {
		title = "‹ Builds / " + title
	}
	badge := listBadge(title, &l, 0)
	if l.Filter != "" {
		badge = "/" + l.Filter + " " + badge
	}
	return components.Panel{Title: title, Badge: badge, Focused: m.focus == PanelArtifacts, Width: r.w, Height: r.h, Content: l.Render(m.st, r.w-4)}.Render(m.st)
}

type buildActionHit struct {
	start, end int
}

// Each action is drawn as its own contrasting chip, and the very same cell
// ranges are used for touch hit-testing.
func (m Model) buildActionChips(width int) (string, []buildActionHit) {
	labels := []string{"Install", "Download", "Open"}
	if os.Getenv("NO_COLOR") != "" {
		// Keep the actions distinct from ordinary details even without color.
		for i := range labels {
			labels[i] = strings.ToUpper(labels[i])
		}
	}
	pad := 0
	if width >= 27 {
		pad = 1
	}
	primary := m.st.ButtonFocus.Padding(0, pad)
	secondary := lipgloss.NewStyle().Foreground(m.st.P.Text).Background(m.st.P.Selection).Bold(true).Padding(0, pad)
	var rendered []string
	var hits []buildActionHit
	x := 0
	for i, label := range labels {
		style := secondary
		if i == 0 {
			style = primary
		}
		part := style.Render(label)
		w := components.VisibleWidth(part)
		hits = append(hits, buildActionHit{x, x + w})
		rendered = append(rendered, part)
		x += w + 1
	}
	return strings.Join(rendered, " "), hits
}

func (m Model) buildDetails(r rect) string {
	ih := r.h - 2
	lines := []string{}
	add := func(s string) {
		if len(lines) < ih-1 {
			lines = append(lines, s)
		}
	}
	if m.branchSel != "" && m.usingReleaseAssets() {
		add(m.st.Warn.Render("Releases: all branches"))
	}
	if run, ok := m.selectedRun(); ok {
		title := firstLineS(run.DisplayTitle)
		if title == "" {
			title = run.Name
		}
		add(m.st.Bold.Render(title))
		status := run.Conclusion
		if run.Active() || status == "" {
			status = run.Status
		}
		icon, _ := m.runIcon(run)
		add(m.st.Dim.Render("Result    ") + icon + " " + strings.ReplaceAll(status, "_", " "))
		attempt := run.RunAttempt
		if attempt < 1 {
			attempt = 1
		}
		add(m.st.Dim.Render("Build     ") + fmt.Sprintf("#%d  %s · attempt %d", run.RunNumber, run.Event, attempt))
		add(m.st.Dim.Render("Branch    ") + run.HeadBranch + "  " + shortSHA(run.HeadSHA))
		if run.Name != "" {
			add(m.st.Dim.Render("Workflow  ") + run.Name)
		}
		if run.Actor.Login != "" {
			add(m.st.Dim.Render("By        ") + run.Actor.Login + "  " + reltime(run.CreatedAt) + " ago")
		} else if !run.CreatedAt.IsZero() {
			add(m.st.Dim.Render("Started   ") + run.CreatedAt.Local().Format("2006-01-02 15:04"))
		}
		if !run.CreatedAt.IsZero() {
			end := run.UpdatedAt
			if run.Active() {
				end = time.Now()
			}
			if !end.IsZero() && !end.Before(run.CreatedAt) {
				add(m.st.Dim.Render("Time      ") + shortDur(end.Sub(run.CreatedAt)))
			}
		}
	} else if !m.usingReleaseAssets() {
		add(m.st.Dim.Render("Select a build to see its details."))
	}
	if m.usingReleaseAssets() {
		if a, ok := m.selectedReleaseAsset(); ok {
			add(m.st.Dim.Render("Ships     ") + a.Asset.Name)
			add(m.st.Dim.Render("Release   ") + a.ReleaseTag)
		}
	} else if a, ok := m.selectedArtifact(); ok {
		add(m.st.Dim.Render("Ships     ") + a.Name)
		add(m.st.Dim.Render("Match     ") + fmt.Sprintf("score %d · %s", a.Score, humanSize(a.SizeInBytes)))
	}
	if m.instFound {
		add(m.st.Dim.Render("Phone     ") + m.instApp.Package + " " + m.instApp.VersionName)
	}
	for len(lines) < ih-1 {
		lines = append(lines, "")
	}
	if ih > 0 {
		chips, _ := m.buildActionChips(r.w - 4)
		lines = append(lines, chips)
	}
	return components.Panel{Title: "Details", Width: r.w, Height: r.h, Content: strings.Join(lines, "\n")}.Render(m.st)
}

// Keep all three boxes and the selected artifact in sync as the build cursor moves.
func (m *Model) syncBuildSelection() {
	r, ok := m.cursorRun()
	id := int64(0)
	if ok {
		id = r.ID
	}
	if m.selectedRunID != id {
		m.selectedRunID = id
		m.selectedArtID = 0
		m.arts.Cursor, m.arts.Offset = 0, 0
		m.arts.Filter = ""
		m.arts.SetItems(nil)
	}
}

func (m *Model) syncArtifactSelection() {
	if it, ok := m.arts.Selected(); ok {
		if id, ok := it.ID.(int64); ok {
			m.selectedArtID = id
			return
		}
	}
	m.selectedArtID = 0
}

func (m *Model) moveBuild(d int) tea.Cmd {
	m.runs.MoveBy(d)
	m.syncBuildSelection()
	return m.fetchArtifactsForSelection()
}

func (m Model) handleBuildMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	l := m.buildsLayout()
	switch msg.Type {
	case tea.MouseWheelUp, tea.MouseWheelDown:
		d := 3
		if msg.Type == tea.MouseWheelUp {
			d = -3
		}
		if l.runs.contains(msg.X, msg.Y) {
			return m, m.moveBuild(d)
		}
		if l.arts.contains(msg.X, msg.Y) {
			m.arts.MoveBy(d)
			m.syncArtifactSelection()
		}
	case tea.MouseLeft:
		if l.runs.contains(msg.X, msg.Y) {
			m.focus = PanelRuns
			row := msg.Y - l.runs.y - 1
			if row < 0 || row >= l.runs.h-2 {
				return m, nil
			}
			idx := m.runs.Offset + row
			if idx >= len(m.runs.Filtered()) {
				return m, nil
			}
			same := idx == m.runs.Cursor
			m.runs.Cursor = idx
			m.runs.MoveBy(0)
			m.syncBuildSelection()
			cmd := m.fetchArtifactsForSelection()
			if same || m.width < 64 {
				m.focus = PanelArtifacts
				m.buildDetail = true
				m.updateListBounds()
			}
			return m, cmd
		}
		if l.arts.contains(msg.X, msg.Y) {
			if m.width < 64 && msg.Y == l.arts.y {
				m.focus = PanelRuns
				m.buildDetail = false
				m.updateListBounds()
				return m, nil
			}
			m.focus = PanelArtifacts
			row := msg.Y - l.arts.y - 1
			if row < 0 || row >= l.arts.h-2 {
				return m, nil
			}
			idx := m.arts.Offset + row
			if idx >= len(m.arts.Filtered()) {
				return m, nil
			}
			same := idx == m.arts.Cursor
			m.arts.Cursor = idx
			m.arts.MoveBy(0)
			m.syncArtifactSelection()
			if same {
				return m, m.requestDownload(true)
			}
			return m, nil
		}
		if l.details.contains(msg.X, msg.Y) && msg.Y == l.details.y+l.details.h-2 {
			x := msg.X - (l.details.x + 2)
			_, hits := m.buildActionChips(l.details.w - 4)
			for i, hit := range hits {
				if x < hit.start || x >= hit.end {
					continue
				}
				switch i {
				case 0:
					return m, m.requestDownload(true)
				case 1:
					return m, m.requestDownload(false)
				case 2:
					if m.usingReleaseAssets() {
						if a, ok := m.selectedReleaseAsset(); ok {
							return m, openURL(a.HTMLURL)
						}
					}
					if run, ok := m.selectedRun(); ok {
						return m, openURL(run.HTMLURL)
					}
				}
			}
		}
	}
	return m, nil
}
