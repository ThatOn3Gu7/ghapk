package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/tui/components"
)

// renderDashboard draws header + panels + status bar.
func (m Model) renderDashboard() string {
	header := m.renderHeader()
	status := m.renderStatusBar()
	bodyH := m.height - 2 // header + status

	var body string
	body = m.renderCompact(bodyH)
	return header + "\n" + body + "\n" + status
}

// ───────────────────────────── header/status ─────────────────────────────

func (m Model) renderHeader() string {
	st := m.st
	w := m.width
	if w < 20 {
		w = 20
	}

	// Right side: "@user theme" — first thing to shrink on narrow terminals.
	right := ""
	if m.authUser != "" {
		right += st.Muted.Render("@" + m.authUser)
	}
	if th := m.settings.Theme; th != "" {
		if right != "" {
			right += " "
		}
		right += st.Dim.Render(th)
	}
	rw := components.VisibleWidth(right)

	// Left side pieces, in order of importance: logo, repo, branch, wf, busy.
	logo := st.Header.Render(" ghapk ")
	repo := m.settings.Repo
	if repo == "" {
		repo = "(no repo)"
	}
	busy := ""
	if m.watchActive() {
		busy = " " + st.BadgeStyle.Render("RUNNING")
	}

	build := func(repoW, branchW, wfW int) string {
		out := logo
		out += "  " + st.HeaderAccent.Render(compMiddleEll(repo, repoW))
		if branchW > 0 {
			bname := m.branchSel
			if bname == "" {
				bname = "all branches"
			}
			out += "  " + st.Muted.Render(strings.TrimSpace(st.G.Branch)+" "+compMiddleEll(bname, branchW))
		}
		if wfW > 0 && m.settings.Workflow != "" {
			out += "  " + st.Muted.Render("wf:"+compMiddleEll(m.settings.Workflow, wfW))
		}
		out += busy
		return out
	}

	bar := build(40, 24, 40)
	if components.VisibleWidth(bar)+rw+1 > w {
		bar = build(28, 16, 24) // trim values
	}
	if components.VisibleWidth(bar)+rw+1 > w {
		bar = build(24, 0, 14) // branch disappears whole, never mangled
	}
	if components.VisibleWidth(bar)+rw+1 > w {
		bar = build(18, 0, 0) // repo only
	}
	if components.VisibleWidth(bar)+rw+1 > w {
		right = "" // right side disappears last
		rw = 0
	}
	bar = components.Clamp(bar, max(w-rw-1, 8))
	gap := w - components.VisibleWidth(bar) - rw
	if gap < 0 {
		gap = 0
	}
	return lipgloss.NewStyle().Background(st.P.BgPanel).Render(components.Pad(bar+strings.Repeat(" ", gap)+right, w))
}

// compMiddleEll ellipsizes the CENTER of s ("termux-x11-un…-wrapp") so the
// ends of identifiers stay visible at narrow widths.
func compMiddleEll(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if len(s) <= w {
		return s
	}
	if w <= 4 {
		return s[:w-1] + "…"
	}
	head := (w+1)/2 - 1
	tail := w - head - 1
	return s[:head] + "…" + s[len(s)-tail:]
}

func (m Model) renderStatusBar() string {
	hints := []components.Hint{}
	add := func(k, d string) { hints = append(hints, components.Hint{Key: k, Desc: d}) }
	if m.filterMode {
		add("type", "filter")
		add("esc", "clear")
		add("enter", "keep")
	} else {
		switch m.focus {
		case PanelRepos:
			add("enter", "switch")
			add("R", "picker")
			add("e", "edit")
			add("x", "remove")
			add("f", "star")
		case PanelWorkflows:
			if m.wfYAMLFocused {
				add("↑/↓", "scroll")
				add("esc", "list")
			} else {
				add("enter", "filter runs")
				add("→/v", "view yaml")
			}
		case PanelArtifacts:
			add("d", "download")
			add("i", "install")
		case PanelRuns:
			add("→", "artifacts")
			add("d", "download")
			add("i", "install")
			add("o", "open")
		}
		add("tab", "panels")
		add("/", "filter")
		add("s", "settings")
		add("?", "help")
		add("q", "quit")
	}
	sb := components.StatusBar{
		Width:   m.width,
		Hints:   hints,
		Message: m.toastIfFresh(),
		Busy:    m.busy,
		BusyMsg: m.busyMsg,
		Spinner: m.spin.View(),
	}
	return sb.Render(m.st)
}

func (m Model) toastIfFresh() string {
	if m.toast != "" && time.Now().Before(m.toastUntil) {
		return m.toast
	}
	return ""
}

// ───────────────────────────── layouts ─────────────────────────────

// renderCompact displays one screen beneath the tab bar. Builds has its own
// responsive three-box layout; the other screens retain their existing content.
func (m Model) renderCompact(h int) string {
	st := m.st
	labels, pad := m.tabLabels()
	var tabs []string
	for _, p := range FocusOrder {
		lbl := labels[p]
		if pad {
			lbl = " " + lbl + " "
		}
		if p == m.activeTab() {
			tabs = append(tabs, st.Header.Render(lbl))
		} else {
			tabs = append(tabs, st.Muted.Render(lbl))
		}
	}
	tabBar := components.Clamp(strings.Join(tabs, " "), m.width)

	bodyH := h - 1
	var content string
	switch m.focus {
	case PanelRepos:
		content = m.reposContent(m.width, bodyH, &m)
	case PanelWorkflows:
		content = m.workflowsContent(m.width, bodyH, &m)
	case PanelRuns:
		return tabBar + "\n" + m.renderBuilds()
	case PanelArtifacts:
		return tabBar + "\n" + m.renderBuilds()
	default:
		content = m.deviceContent(m.width, bodyH, &m)
	}
	p := m.panel(m.focus, m.width, bodyH, content)
	return tabBar + "\n" + p
}

// tabLabels keeps all tabs reachable even on a narrow portrait phone.
func (m Model) tabLabels() (map[PanelID]string, bool) {
	labels := map[PanelID]string{}
	total := len(FocusOrder) - 1
	for _, p := range FocusOrder {
		labels[p] = m.compactTabLabel(p)
		total += components.VisibleWidth(labels[p])
	}
	if total+2*len(FocusOrder) <= m.width {
		return labels, true
	}
	if total <= m.width {
		return labels, false
	}
	labels = map[PanelID]string{PanelRepos: "1 Repo", PanelRuns: "2 Build", PanelWorkflows: "3 Flow", PanelDevice: "4 Dev"}
	return labels, false
}

// compactTabLabel is the tab caption, e.g. "2 Builds (30)" — hotkey,
// short name, and the live item count. Device/Details carry no count.
func (m Model) compactTabLabel(p PanelID) string {
	n := ""
	switch p {
	case PanelRepos:
		if c := len(m.repos.Items); c > 0 {
			n = fmt.Sprint(c)
		}
	case PanelWorkflows:
		if c := len(m.workflows.Items) - 1; c > 0 {
			n = fmt.Sprint(c)
		}
	case PanelRuns:
		if c := len(m.runs.Items); c > 0 {
			n = fmt.Sprint(c)
		}
	case PanelArtifacts:
		if c := len(m.arts.Items); c > 0 {
			n = fmt.Sprint(c)
		}
	}
	i := 0
	for x, q := range FocusOrder {
		if q == p {
			i = x
		}
	}
	l := fmt.Sprintf("%d %s", i+1, panelShort(p))
	if n != "" {
		l += " (" + n + ")"
	}
	return l
}

func panelShort(p PanelID) string {
	switch p {
	case PanelRepos:
		return "Repos"
	case PanelWorkflows:
		return "Flows"
	case PanelRuns:
		return "Builds"
	case PanelArtifacts:
		return "Artif"
	default:
		return "Device"
	}
}

// listBadge renders the count shown on a panel's border. With an active
// filter it shows filtered/total so the number always matches the rows
// actually visible.
func listBadge(name string, l *components.List, excludeFirst int) string {
	total := len(l.Items) - excludeFirst
	if total <= 0 {
		return ""
	}
	if l.Filter != "" {
		shown := len(l.Filtered())
		if l.Filter == "" || excludeFirst == 0 {
			return fmt.Sprintf("%d/%d", shown, total)
		}
		return fmt.Sprintf("%d/%d", shown, total)
	}
	return fmt.Sprint(total)
}

// panel wraps content in a titled frame, marking focus.
func (m Model) panel(id PanelID, w, h int, content string) string {
	title := PanelNames[id]
	if id == PanelRuns && m.filterMode && m.focus == id {
		// no-op; filter shown in content
	}
	badge := ""
	switch id {
	case PanelRuns:
		badge = listBadge("Runs", &m.runs, 0)
	case PanelArtifacts:
		badge = listBadge("Artifacts", &m.arts, 0)
	case PanelWorkflows:
		badge = listBadge("Workflows", &m.workflows, 1)
	case PanelRepos:
		badge = listBadge("Repositories", &m.repos, 0)
	}
	return components.Panel{
		Title:   title,
		Center:  id == PanelDevice,
		Focused: m.focus == id,
		Width:   w,
		Height:  h,
		Badge:   badge,
		Content: content,
	}.Render(m.st)
}
