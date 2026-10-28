package tui

import (
	"fmt"
	"strings"
	"time"

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
	header, _ := m.headerLayout()
	return header
}

// headerLayout shares the drawn repository span with mouse hit-testing.
func (m Model) headerLayout() (string, rect) {
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

	repoStart := components.VisibleWidth(logo) + 2
	repoEnd := repoStart
	build := func(repoW, branchW, wfW int) string {
		out := logo
		repoText := compMiddleEll(repo, repoW)
		repoEnd = repoStart + components.VisibleWidth(repoText)
		out += "  " + st.HeaderAccent.Render(repoText)
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
	barLimit := max(w-rw-1, 8)
	if components.VisibleWidth(bar) > barLimit && repoEnd > barLimit {
		repoEnd = barLimit
	}
	bar = components.Clamp(bar, barLimit)
	repoEnd = min(repoEnd, components.VisibleWidth(bar))
	gap := w - components.VisibleWidth(bar) - rw
	if gap < 0 {
		gap = 0
	}
	repoEnd = min(repoEnd, w)
	if repoEnd < repoStart {
		repoEnd = repoStart
	}
	return components.Pad(bar+strings.Repeat(" ", gap)+right, w), rect{x: repoStart, y: 0, w: repoEnd - repoStart, h: 1}
}

// compMiddleEll ellipsizes the CENTER of s ("termux-x11-un…-wrapp") so the
// ends of identifiers stay visible at narrow widths.
func compMiddleEll(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if components.VisibleWidth(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	// Work in terminal cells and whole runes: repository/branch names can
	// contain multibyte or wide characters, especially on non-English phones.
	head := (w+1)/2 - 1
	if w <= 4 {
		head = w - 1
	}
	tail := w - head - 1
	rs := []rune(s)
	leftEnd, used := 0, 0
	for leftEnd < len(rs) {
		rw := components.VisibleWidth(string(rs[leftEnd]))
		if used+rw > head {
			break
		}
		used += rw
		leftEnd++
	}
	rightStart := len(rs)
	used = 0
	for rightStart > leftEnd {
		rw := components.VisibleWidth(string(rs[rightStart-1]))
		if used+rw > tail {
			break
		}
		used += rw
		rightStart--
	}
	return string(rs[:leftEnd]) + "…" + string(rs[rightStart:])
}

func (m Model) renderStatusBar() string {
	return m.statusBar().Render(m.st)
}

// statusBar is the single source for displayed hints and their key actions.
func (m Model) statusBar() components.StatusBar {
	hints := []components.Hint{}
	add := func(k, d, action string) { hints = append(hints, components.Hint{Key: k, Desc: d, Action: action}) }
	if m.filterMode {
		add("⌫", "delete", "backspace")
		add("esc", "clear", "esc")
		add("enter", "keep", "enter")
	} else {
		switch m.focus {
		case PanelRepos:
			add("enter", "select / next", "enter")
			if m.repoBranchFocused {
				add("←", "repos", "left")
				add("B", "picker", "B")
			} else {
				add("R", "picker", "R")
				add("e", "edit", "e")
				add("x", "remove", "x")
				add("f", "star", "f")
			}
		case PanelWorkflows:
			if m.wfYAMLFocused {
				add("↑", "up", "up")
				add("↓", "down", "down")
				add("esc", "list", "esc")
			} else {
				add("enter", "select / next", "enter")
				if len(m.workflows.Items) > 0 {
					add("→/v", "view yaml", "right")
				}
			}
		case PanelDownloads:
			if !m.downloadDetail && m.width < 80 && m.downloadFocus == 0 {
				add("↵", "detail", "enter")
			}
			if m.downloadDetail {
				add("esc", "back", "esc")
			}
			if m.downloadFocus == 1 {
				add("i", "install", "i")
				add("x", "delete", "x")
				add("esc", "jobs", "esc")
			} else {
				add("c", "cancel", "c")
				add("x", "remove", "x")
				add("s", "saved", "s")
			}
		case PanelArtifacts:
			add("d", "download", "d")
			add("i", "install", "i")
		case PanelRuns:
			add("→", "artifacts", "right")
			add("d", "download", "d")
			add("i", "install", "i")
			add("o", "open", "o")
		}
		add("tab", "panels", "tab")
		if m.focusedListOrNil() != nil {
			add("/", "filter", "/")
		}
		if m.focus != PanelDownloads {
			add("s", "settings", "s")
		}
		add("?", "help", "?")
		add("q", "quit", "q")
	}
	return components.StatusBar{Width: m.width, Hints: hints, Message: m.toastIfFresh(), Busy: m.busy, BusyMsg: m.busyMsg, Spinner: m.spin.View()}
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
	case PanelDownloads:
		return tabBar + "\n" + m.renderDownloads()
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
	if total+2*len(FocusOrder)+2 <= m.width {
		return labels, true
	}
	if total+2 <= m.width {
		return labels, false
	}
	labels = map[PanelID]string{PanelRepos: "1 Repo", PanelRuns: "3 Build", PanelWorkflows: "2 Flow", PanelDownloads: "5 DL", PanelDevice: "4 Dev"}
	if m.width < 35 {
		labels = map[PanelID]string{PanelRepos: "1 Rep", PanelWorkflows: "2 Flo", PanelRuns: "3 Bld", PanelDevice: "4 Dev", PanelDownloads: "5 DL"}
	}
	return labels, false
}

// compactTabLabel is the tab caption, e.g. "3 Builds (30)" — hotkey,
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
	case PanelDownloads:
		active := 0
		asking := false
		for _, j := range m.jobs {
			if j.status == "running" || j.status == "cancelling" || j.status == "asking" {
				active++
			}
			if j.status == "asking" {
				asking = true
			}
		}
		if asking {
			n = "?"
		} else if active > 0 {
			n = fmt.Sprint(active)
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
	case PanelDownloads:
		return "Downloads"
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
