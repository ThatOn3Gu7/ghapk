package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/theme"
	"github.com/charmbracelet/lipgloss"
)

func humanSize(b int64) string { return ghapi.HumanBytes(b) }

// centerEmpty floats a short message (loading spinner, empty state) in the
// middle of a panel content area. Lists are top-anchored for consistency;
// short singleton messages look better centered.
func centerEmpty(s string, w, h int) string {
	return components.CenterBlock(s, w-4, h-2)
}

// emptyState renders a clean, friendly, centered empty state with an icon,
// title, descriptive message, and an action hint.
func (m Model) emptyState(icon, title, desc, hint string, w, h int) string {
	st := m.st
	var b strings.Builder
	if icon != "" {
		b.WriteString(st.Accent.Render(icon) + "\n\n")
	}
	if title != "" {
		b.WriteString(st.Bold.Render(title) + "\n")
	}
	if desc != "" {
		b.WriteString(st.Dim.Render(desc) + "\n")
	}
	if hint != "" {
		b.WriteString("\n" + st.Dim.Render(hint) + "\n")
	}
	return centerEmpty(b.String(), w, h)
}

// reposSplit calculates the banner height and repo list box height.
// When there is ample vertical space, it introduces top padding so the logo and
// repo selection box sit comfortably in the optical center rather than sticking
// to the top border.
func reposSplit(h, count int, w ...int) (bannerH, listH int) {
	ih := h - 2
	avail := 40
	if len(w) > 0 {
		avail = w[0] - 4
	}
	if ih < 10 || avail < 16 {
		return 0, ih
	}

	useFull := avail >= 43 && ih >= 16
	var baseBannerH int
	if useFull {
		baseBannerH = 8 // 6 lines logo + 1 subtitle + 1 blank line
	} else if avail >= 21 {
		baseBannerH = 5 // 3 lines logo + 1 subtitle + 1 blank line
	} else {
		baseBannerH = 3 // 1 line title + 1 subtitle + 1 blank line
	}

	neededListH := count + 2
	if neededListH < 3 {
		neededListH = 3
	}

	minNeeded := baseBannerH + neededListH
	spare := ih - minNeeded
	topPad := 0
	if spare > 0 {
		topPad = spare / 3
		if topPad > 4 {
			topPad = 4
		}
	}

	bannerH = topPad + baseBannerH
	maxListH := ih - bannerH
	if neededListH > maxListH {
		listH = maxListH
	} else {
		listH = neededListH
	}
	return bannerH, listH
}

// reposBranchSplit reserves a visible branch list below the repository list.
func reposBranchSplit(h, repoCount, _ int, w int) (bannerH, repoH, branchH int) {
	inner := max(0, h-2)
	// Keep the box fixed while async branches or runs arrive; otherwise its
	// rows can move between the first and second touch.
	branchH = max(3, min(8, inner/3))
	bannerH, repoH = reposSplit(h-branchH, repoCount, w)
	return
}

// ANSI Shadow block plus pipe style (as in Santui reference)
var logoBlockWide = []string{
	` ██████╗ ██╗  ██╗   █████╗ ██████╗ ██╗  ██╗`,
	`██╔════╝ ██║  ██║  ██╔══██╗██╔══██╗██║ ██╔╝`,
	`██║  ███╗███████║  ███████║██████╔╝█████╔╝ `,
	`██║   ██║██╔══██║  ██╔══██║██╔═══╝ ██╔═██╗ `,
	`╚██████╔╝██║  ██║  ██║  ██║██║     ██║  ██╗`,
	` ╚═════╝ ╚═╝  ╚═╝  ╚═╝  ╚═╝╚═╝     ╚═╝  ╚═╝`,
}

// Compact 3-line block plus pipe style for narrow desktop panels
var logoBlockNarrow = []string{
	`███╗ █║█║  ▄█▄╗ ███╗ █║█╗`,
	`█▀█║ ███║  ███║ ██╔╝ ██╔╝`,
	`╚══╝ █║█║  █║█║ █║   █║█╗`,
}

// renderShimmerLogo renders a "block plus pipe" style logo with a skeleton loading
// shimmer animation that slides an illuminated wave of light from left to right.
func (m Model) renderShimmerLogo(w int, totalBannerH ...int) string {
	st := m.st
	innerW := w
	if innerW < 10 {
		return ""
	}

	useFull := innerW >= 43 && (len(totalBannerH) == 0 || totalBannerH[0] >= 8)
	var baseBannerH int
	var logoLines []string
	var logoW int
	var splitCol int
	var maxShimmer int

	if useFull {
		logoLines = logoBlockWide
		logoW = 43
		splitCol = 18
		baseBannerH = 8
		maxShimmer = 55
	} else if innerW >= 21 {
		logoLines = logoBlockNarrow
		logoW = 25
		splitCol = 10
		baseBannerH = 5
		maxShimmer = 35
	} else {
		// Narrow fallback badge when width is under 21 columns
		title := "◆ GH APK ◆"
		sub := "APK Manager"
		vwT := components.VisibleWidth(title)
		spT := (innerW - vwT) / 2
		if spT < 0 {
			spT = 0
		}
		vwS := components.VisibleWidth(sub)
		spS := (innerW - vwS) / 2
		if spS < 0 {
			spS = 0
		}
		line1 := strings.Repeat(" ", spT) + st.Accent.Bold(true).Render(title)
		line2 := strings.Repeat(" ", spS) + st.Dim.Render(sub)
		return line1 + "\n" + line2 + "\n"
	}

	topPad := 0
	if len(totalBannerH) > 0 && totalBannerH[0] > baseBannerH {
		topPad = totalBannerH[0] - baseBannerH
	}

	sp := (innerW - logoW) / 2
	if sp < 0 {
		sp = 0
	}
	leftPad := strings.Repeat(" ", sp)

	shineBright := st.Bold
	shineSoft := st.Muted
	shineEdge := lipgloss.NewStyle().Foreground(st.P.Accent)

	var lines []string
	for i := 0; i < topPad; i++ {
		lines = append(lines, "")
	}

	for _, rawLine := range logoLines {
		var b strings.Builder
		b.WriteString(leftPad)
		for col, ch := range []rune(rawLine) {
			if ch == ' ' {
				b.WriteRune(' ')
				continue
			}

			dist := col - (m.shimmerPos - 4)
			if dist < 0 {
				dist = -dist
			}

			if m.shimmerPos < maxShimmer {
				switch dist {
				case 0:
					b.WriteString(shineBright.Render(string(ch)))
				case 1:
					b.WriteString(shineSoft.Render(string(ch)))
				case 2:
					b.WriteString(shineEdge.Render(string(ch)))
				default:
					if col < splitCol {
						b.WriteString(st.AccentAlt.Render(string(ch)))
					} else {
						b.WriteString(st.Accent.Render(string(ch)))
					}
				}
			} else {
				if col < splitCol {
					b.WriteString(st.AccentAlt.Render(string(ch)))
				} else {
					b.WriteString(st.Accent.Render(string(ch)))
				}
			}
		}
		lines = append(lines, b.String())
	}

	var sub string
	if innerW >= 32 {
		sub = "GitHub Actions APK Manager"
	} else if innerW >= 22 {
		sub = "GitHub Actions APK"
	} else {
		sub = "APK Manager"
	}

	vwSub := components.VisibleWidth(sub)
	spSub := (innerW - vwSub) / 2
	if spSub < 0 {
		spSub = 0
	}
	subLine := strings.Repeat(" ", spSub)
	if m.busy {
		subLine += m.spin.View() + " " + st.Accent.Render("Syncing…")
	} else {
		subLine += st.Dim.Render(sub)
	}
	lines = append(lines, subLine)
	lines = append(lines, "") // blank separator line

	return strings.Join(lines, "\n")
}

// ───────────────────────────── Repos ─────────────────────────────

func (m Model) reposContent(w, h int, mm *Model) string {
	l := m.repos
	if len(l.Items) == 0 {
		return m.emptyState("📂", "No repositories added", "Pick or add a repository", "press "+m.keyName("dashboard", "R"), w, h)
	}
	if len(l.Filtered()) == 0 {
		return m.emptyState("🔍 ∅", "No matching repositories", fmt.Sprintf("No repos match %q", l.Filter), "press "+m.keyName("filter", "esc")+" to clear filter", w, h)
	}

	bannerH, listH, branchH := reposBranchSplit(h, len(l.Items), len(m.branchList.Items), w)
	l.MaxShown = listH - 2
	l.Focused = m.focus == PanelRepos && !m.repoBranchFocused
	st := m.st

	var items []components.Item
	for _, it := range l.Items {
		repo, _ := it.ID.(string)
		row := it
		name := repo
		if repo == m.gitDetected {
			name = repo + st.Dim.Render(" (git)")
		}
		row.Rendered = "  " + name
		row.Label = repo
		items = append(items, row)
	}
	l.Items = items

	list := renderBoxedList(l, st, w-4, listH, m.focus == PanelRepos && !m.repoBranchFocused, func(it components.Item) bool {
		repo, _ := it.ID.(string)
		return repo != "" && repo == m.settings.Repo
	})

	branches := m.branchList
	branches.MaxShown = max(1, branchH-2)
	branches.Focused = m.focus == PanelRepos && m.repoBranchFocused
	branchBox := renderBoxedList(branches, st, w-4, branchH, branches.Focused, func(it components.Item) bool { value, ok := it.ID.(string); return ok && value == m.branchSel })
	lines := strings.Split(branchBox, "\n")
	if len(lines) > 0 {
		title := "Branches"
		if m.branchLoading {
			title = "Branches · loading"
		}
		lines[0] = putTitle(lines[0], st, title, branches.Focused)
	}
	branchBox = strings.Join(lines, "\n")
	if bannerH > 0 {
		banner := m.renderShimmerLogo(w-4, bannerH)
		return banner + "\n" + list + "\n" + branchBox
	}
	return list + "\n" + branchBox
}

// renderBoxedList draws a list of options inside a single static bordered box
// stretched out to width x height. Cursor and selected rows use distinct
// highlights from the active theme.
// Moving through options never shifts the layout or characters.
func renderBoxedList(l components.List, st theme.Styles, width, height int, focused bool, isSelected func(it components.Item) bool) string {
	if width < 6 || height < 3 {
		return ""
	}
	innerW := width - 2
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}
	l.MaxShown = innerH
	l.CachedClamp()

	borderSt := lipgloss.NewStyle().Foreground(st.P.Border)
	if focused {
		borderSt = lipgloss.NewStyle().Foreground(st.P.BorderFocus)
	}

	// Cursor, selected and cursor-on-selection stay distinct using this theme's palette.
	cursorFg := st.P.Text
	if !focused {
		cursorFg = st.P.Muted
	}
	graySeq := components.StyleSeq(lipgloss.NewStyle().Background(st.P.Selection).Foreground(cursorFg))
	greenSeq := components.StyleSeq(lipgloss.NewStyle().Background(st.P.Accent).Foreground(st.P.OnAccent).Bold(true))
	bothSeq := components.StyleSeq(lipgloss.NewStyle().Background(st.P.AccentAlt).Foreground(st.P.OnAccent).Bold(true))

	fs := l.Filtered()
	var lines []string
	lines = append(lines, borderSt.Render("╭"+strings.Repeat("─", innerW)+"╮"))

	if len(fs) == 0 {
		msg := l.Empty
		if msg == "" {
			msg = "  (empty)"
		}
		if l.Filter != "" {
			msg = "  no match for /" + l.Filter
		}
		lines = append(lines, borderSt.Render("│")+components.Fit(st.Dim.Render(msg), innerW)+borderSt.Render("│"))
		for i := 1; i < innerH; i++ {
			lines = append(lines, borderSt.Render("│")+strings.Repeat(" ", innerW)+borderSt.Render("│"))
		}
	} else {
		for i := 0; i < innerH; i++ {
			idx := l.Offset + i
			if idx < len(fs) {
				it := fs[idx]
				isCursor := (idx == l.Cursor)
				isSel := false
				if isSelected != nil {
					isSel = isSelected(it)
				}

				row := it.Rendered
				if row == "" {
					row = "  " + it.Label
				}

				if it.Detail != "" {
					d := st.Dim.Render(it.Detail)
					if isSel {
						d = lipgloss.NewStyle().Foreground(st.P.OnAccent).Render(it.Detail)
					} else if isCursor {
						d = st.Text.Render(it.Detail)
					}
					fitW := innerW - components.VisibleWidth(d) - 1
					if fitW < 1 {
						fitW = 1
					}
					row = components.Clamp(row, fitW)
					room := innerW - components.VisibleWidth(row) - components.VisibleWidth(d)
					if room < 1 {
						room = 1
					}
					row += strings.Repeat(" ", room) + d
				} else {
					row = components.Fit(row, innerW)
				}

				switch {
				case isSel && isCursor:
					row = components.ReStyle(row, bothSeq)
				case isSel:
					row = components.ReStyle(row, greenSeq)
				case isCursor:
					row = components.ReStyle(row, graySeq)
				}

				lines = append(lines, borderSt.Render("│")+row+borderSt.Render("│"))
			} else {
				lines = append(lines, borderSt.Render("│")+strings.Repeat(" ", innerW)+borderSt.Render("│"))
			}
		}
	}

	lines = append(lines, borderSt.Render("╰"+strings.Repeat("─", innerW)+"╯"))
	return strings.Join(lines, "\n")
}

// ───────────────────────────── Workflows ─────────────────────────────

// flowsSplit divides a workflows-panel OUTER height into the list part and
// the YAML preview sub-box (borders included). The list part sizes dynamically
// to the number of workflows (plus one empty line), pushing the YAML preview up
// so it gets maximum space.
func flowsSplit(h int, count ...int) (listH, subH int) {
	ih := h - 2
	if ih < 10 {
		return ih, 0
	}
	minSubH := 8
	if ih >= 20 {
		minSubH = 10
	}
	maxListH := ih - minSubH
	if maxListH < 4 {
		maxListH = 4
	}
	if len(count) == 0 {
		return maxListH, ih - maxListH
	}
	c := count[0]
	neededListH := c + 3 // items + 1 empty line + 2 borders
	if neededListH < 4 {
		neededListH = 4
	}
	if neededListH > maxListH {
		listH = maxListH
	} else {
		listH = neededListH
	}
	subH = ih - listH
	return listH, subH
}

func (m Model) workflowsContent(w, h int, mm *Model) string {
	l := m.workflows
	st := m.st
	if m.settings.Repo == "" {
		return m.emptyState("📁", "No repository chosen", "Pick a repository to see workflows", "press "+m.keyName("dashboard", "R")+" to choose", w, h)
	}
	if len(l.Items) == 0 {
		return m.emptyState("⚙ ∅", "No workflows found", "No .github/workflows/*.yml found\nin this repository", "press "+m.keyName("dashboard", "r")+" to refresh", w, h)
	}
	if len(l.Filtered()) == 0 {
		return m.emptyState("🔍 ∅", "No matching workflows", fmt.Sprintf("No workflows match %q", l.Filter), "press "+m.keyName("filter", "esc")+" to clear filter", w, h)
	}

	listH, subH := flowsSplit(h, len(l.Items))
	if listH < 3 {
		listH = 3
	}
	l.MaxShown = listH - 2
	l.Focused = m.focus == PanelWorkflows && !m.wfYAMLFocused
	var items []components.Item
	for _, it := range l.Items {
		name, _ := it.ID.(string)
		row := it
		if name == "" {
			row.Rendered = "  " + st.AccentAlt.Render("◆ any workflow")
		} else {
			row.Rendered = "  " + name
		}
		items = append(items, row)
	}
	l.Items = items

	list := renderBoxedList(l, st, w-4, listH, m.focus == PanelWorkflows && !m.wfYAMLFocused, func(it components.Item) bool {
		name, _ := it.ID.(string)
		return name == m.settings.Workflow
	})
	if subH < 3 {
		return list
	}
	return list + "\n" + m.workflowDetailBox(w-4, subH)
}

// workflowDetailBox previews the selected workflow's YAML (downloaded on
// demand) so choosing a filter shows exactly what that workflow does.
func (m Model) workflowDetailBox(w, h int) string {
	st := m.st
	title := "workflow"
	var body string
	focused := m.focus == PanelWorkflows && m.wfYAMLFocused

	sel, ok := m.workflows.Selected()
	if !ok {
		body = st.Dim.Render("  no workflow selected")
		return components.Panel{Title: title, Focused: focused, Width: w, Height: h, Content: body}.Render(st)
	}
	name, _ := sel.ID.(string)
	if name == "" {
		title = "◆ any workflow"
		body = st.Dim.Render("  runs from every workflow are listed.\n  pick a workflow to preview its YAML and\n  filter the runs panel to it.")
	} else {
		path := m.wfPaths[name]
		title = components.Clamp(name, w-14)
		if path != "" && w >= 60 {
			branch := strings.TrimSpace(st.G.Branch)
			if branch != "" {
				branch += " "
			}
			title += " · " + branch + components.Clamp(path, w-14)
		}
		yaml, ok := m.wfCache[path]
		fetching := m.wfFetching[path]
		switch {
		case path == "":
			body = st.Dim.Render("  (no file path known for this workflow)")
		case fetching && !ok:
			body = m.spin.View() + st.Dim.Render(" fetching "+path+"…")
		case !ok:
			body = st.Dim.Render("  preview not fetched yet")
		default:
			if strings.HasPrefix(yaml, "__ERR__") {
				body = st.Warn.Render("  ") + st.Dim.Render("preview unavailable for this workflow")
			} else {
				header := st.Dim.Render(path)
				yamlLines := h - 3
				if yamlLines < 1 {
					yamlLines = 1
				}
				body = header + "\n" + m.scrollableYAML(yaml, w-4, yamlLines)
			}
		}
	}
	return components.Panel{Title: title, Focused: focused, Width: w, Height: h, Content: body}.Render(st)
}

// scrollableYAML renders the YAML text with syntax tinting, clamped to viewport
// height with scrolling and a vertical sidebar scrollbar indicator.
func (m Model) scrollableYAML(src string, w, maxLines int) string {
	st := m.st
	if maxLines < 1 {
		maxLines = 1
	}
	rawLines := strings.Split(src, "\n")
	for len(rawLines) > 0 && rawLines[len(rawLines)-1] == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}
	totalLines := len(rawLines)
	if totalLines == 0 {
		return ""
	}

	maxScroll := totalLines - maxLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := m.wfScroll
	if scroll < 0 {
		scroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}

	needsScrollbar := totalLines > maxLines
	contentW := w
	if needsScrollbar && contentW > 3 {
		contentW = w - 2 // leave room for space + scrollbar cell
	}

	thumbH := 1
	thumbY := 0
	if needsScrollbar && maxScroll > 0 {
		thumbH = maxLines * maxLines / totalLines
		if thumbH < 1 {
			thumbH = 1
		}
		if thumbH > maxLines {
			thumbH = maxLines
		}
		thumbY = (maxLines - thumbH) * scroll / maxScroll
		if thumbY+thumbH > maxLines {
			thumbY = maxLines - thumbH
		}
	}

	focused := m.focus == PanelWorkflows && m.wfYAMLFocused

	var b strings.Builder
	for i := 0; i < maxLines; i++ {
		lineIdx := scroll + i
		var lineStr string
		if lineIdx < totalLines {
			lineStr = m.tintYAMLLine(rawLines[lineIdx], contentW)
		}

		if needsScrollbar && contentW < w {
			fitted := components.Fit(lineStr, contentW)
			var bar string
			if i >= thumbY && i < thumbY+thumbH {
				if focused {
					bar = st.Accent.Render("█")
				} else {
					bar = st.Dim.Render("█")
				}
			} else {
				bar = st.Dim.Render("│")
			}
			b.WriteString(fitted + " " + bar)
		} else {
			b.WriteString(components.Fit(lineStr, w))
		}

		if i < maxLines-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m Model) tintYAMLLine(ln string, w int) string {
	st := m.st
	trim := strings.TrimSpace(ln)
	switch {
	case trim == "":
		return ""
	case strings.HasPrefix(trim, "#"):
		return st.Dim.Render(components.Clamp(ln, w))
	}
	k := strings.Index(ln, ":")
	keyPart := strings.TrimSpace(ln[:max(k, 0)])
	hasSpace := strings.ContainsAny(strings.TrimPrefix(keyPart, "-"), " \t")
	isKey := k > 0 && keyPart != "" && !hasSpace && !strings.HasSuffix(keyPart, "://")
	if isKey {
		left := ln[:k+1]
		right := ln[k+1:]
		res := st.AccentAlt.Render(components.Clamp(left, w))
		if right != "" {
			res += st.Muted.Render(components.Clamp(right, w-components.VisibleWidth(left)))
		}
		return res
	}
	return components.Clamp(ln, w)
}

// runsSplit divides a runs-panel OUTER height into the scrolling list part
// and the detail sub-box part (both outer heights, borders included). Below
// a minimum height the detail box is dropped and the list takes everything.
func runsSplit(h int, count ...int) (listH, subH int) {
	ih := h - 2
	if ih < 10 {
		return ih, 0
	}
	subH = 9
	if ih >= 24 {
		subH = 10
	}
	if len(count) == 0 {
		legacySub := ih * 2 / 5
		if legacySub < 7 {
			legacySub = 7
		}
		if legacySub > 11 {
			legacySub = 11
		}
		return ih - legacySub, legacySub
	}
	c := count[0]
	maxListH := ih - subH
	if maxListH < 3 {
		maxListH = 3
	}
	neededListH := c + 2
	if neededListH < 3 {
		neededListH = 3
	}
	if neededListH > maxListH {
		listH = maxListH
	} else {
		listH = neededListH
	}
	return listH, subH
}

func (m Model) runsContent(w, h int, mm *Model) string {
	st := m.st
	if m.settings.Repo == "" {
		return m.emptyState("📁", "No repository chosen", "Pick a repository to see runs", "press "+m.keyName("dashboard", "R")+" to choose", w, h)
	}
	if !m.runsLoaded {
		return centerEmpty(m.spin.View()+" "+st.Accent.Render("Loading workflow runs…")+"\n\n"+st.Dim.Render("fetching from GitHub"), w, h)
	}
	l := m.runs
	if len(l.Items) == 0 {
		return m.emptyState("⚡ ∅", "No workflow runs yet", "Push a commit or trigger a workflow\non GitHub to see runs here", "press "+m.keyName("dashboard", "r")+" to refresh", w, h)
	}
	if len(l.Filtered()) == 0 {
		return m.emptyState("🔍 ∅", "No matching runs", fmt.Sprintf("No runs match filter %q", l.Filter), "press "+m.keyName("filter", "esc")+" to clear filter", w, h)
	}

	listH, subH := runsSplit(h, len(l.Items))
	if listH < 3 {
		listH = 3
	}
	l.MaxShown = listH - 2
	l.Focused = m.focus == PanelRuns
	var items []components.Item
	for _, it := range l.Items {
		rr, _ := it.ID.(runRow)
		r := rr.run
		icon, _ := m.runIcon(r)
		num := st.Dim.Render(fmt.Sprintf("#%-4d", r.RunNumber))
		inner := w - 6
		branch := r.HeadBranch
		bw := components.VisibleWidth(branch) + 1
		if bw > 14 {
			bw = 14
		}
		if branch == "" {
			bw = 0
		}
		// icon+num+branch+padding eat into the title budget
		tw := inner - 4 - 6 - bw - 1
		if tw < 8 {
			tw = 8
		}
		title := r.DisplayTitle
		if title == "" {
			title = r.Name
		}
		title = components.Clamp(title, tw)
		line := " " + icon + " " + num + " " + title
		if bw > 0 {
			line += st.Dim.Render(" " + components.Clamp(branch, 13))
		}
		if r.Active() {
			line = " " + m.spin.View() + " " + num + " " + title
			if bw > 0 {
				line += st.Dim.Render(" " + components.Clamp(branch, 13))
			}
		}
		row := it
		row.Rendered = line
		row.Detail = reltime(r.CreatedAt)
		items = append(items, row)
	}
	l.Items = items
	list := renderBoxedList(l, st, w-4, listH, m.focus == PanelRuns, func(it components.Item) bool {
		rr, ok := it.ID.(runRow)
		return ok && rr.run.ID == m.selectedRunID
	})
	if subH < 3 {
		return list
	}
	return list + "\n" + m.runDetailBox(w-4, subH)
}

// runDetailBox is the boxed sub-pane under the runs list: everything you
// used to need the separate Details panel for, glued to the selection.
func (m Model) runDetailBox(w, h int) string {
	st := m.st
	title := "run"
	body := st.Dim.Render("  move the cursor over a run")
	r, ok := m.cursorRun()
	if !ok {
		r, ok = m.selectedRun()
	}
	if ok {
		status := r.Status
		if r.Conclusion != "" {
			status = r.Conclusion
		}
		if r.Active() {
			status = "running " + shortDur(time.Since(r.CreatedAt))
		} else if !r.UpdatedAt.IsZero() && !r.CreatedAt.IsZero() {
			status += " in " + shortDur(r.UpdatedAt.Sub(r.CreatedAt))
		}
		title = fmt.Sprintf("Run #%d · %s", r.RunNumber, status)
		body = m.runDetailSub(w, r)
	}
	return components.Panel{Title: title, Width: w, Height: h, Content: body}.Render(st)
}

func (m Model) runDetailSub(w int, r ghapi.Run) string {
	st := m.st
	icon, _ := m.runIcon(r)
	var b strings.Builder
	kv := func(k, v string) {
		if strings.TrimSpace(components.StripANSI(v)) == "" {
			return
		}
		b.WriteString(fmt.Sprintf("%s %s\n", st.Dim.Render(fmt.Sprintf("%-9s", k)), components.Clamp(v, w-12)))
	}
	t := r.DisplayTitle
	if t == "" {
		t = r.Name
	}
	b.WriteString(icon + " " + st.Bold.Render(components.Clamp(t, w-6)) + "\n")
	kv("workflow", r.Name)
	branch := r.HeadBranch
	if r.HeadSHA != "" {
		branch += "  " + st.Dim.Render("("+shortSHA(r.HeadSHA)+")")
	}
	kv("branch", branch)
	kv("event", r.Event)
	if !r.CreatedAt.IsZero() {
		kv("started", r.CreatedAt.Format("2006-01-02 15:04")+"  "+st.Dim.Render("("+reltime(r.CreatedAt)+" ago)"))
	}
	if arts, ok := m.runCache[r.ID]; ok && len(arts) > 0 {
		var total int64
		for _, a := range arts {
			total += a.SizeInBytes
		}
		kv("artifacts", fmt.Sprintf("%d · %s", len(arts), humanSize(total)))
	}
	if r.HTMLURL != "" {
		kv("url", st.Info.Render(r.HTMLURL))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// shortSHA takes the first seven chars when possible.
func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// shortDur renders durations tight: 42s, 4m12s, 2h05m.
func shortDur(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// runIcon picks a glyph + style for a run's outcome.
func (m Model) runIcon(r ghapi.Run) (string, bool) {
	st := m.st
	g := st.G
	if r.Active() {
		return st.Running.Render(g.Running), true
	}
	switch r.Conclusion {
	case "success":
		return st.OK.Render(g.OK), false
	case "failure":
		return st.Err.Render(g.Fail), false
	case "cancelled":
		return st.Warn.Render(g.Cancelled), false
	case "skipped":
		return st.Dim.Render(g.Skipped), false
	default:
		return st.Dim.Render(g.Neutral), false
	}
}

// futureShort renders a positive duration compactly ("29d", "4h").
func futureShort(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dm", int(d.Minutes())+1)
	}
}

// reltime renders "3h", "2d", "5m" style ages.
func reltime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

// artSplit divides an artifacts-panel OUTER height into list + detail box.
// The informational box has a fixed height, while the artifact list box
// sizes to fit the available artifacts without extra blank lines.
func artSplit(h int, count ...int) (listH, subH int) {
	ih := h - 2
	if ih < 10 {
		return ih, 0
	}
	subH = 9
	if ih >= 24 {
		subH = 10
	}
	maxListH := ih - subH
	if maxListH < 3 {
		maxListH = 3
	}
	if len(count) == 0 {
		return maxListH, subH
	}
	c := count[0]
	neededListH := c + 2
	if neededListH < 3 {
		neededListH = 3
	}
	if neededListH > maxListH {
		listH = maxListH
	} else {
		listH = neededListH
	}
	return listH, subH
}

func (m Model) artsContent(w, h int, mm *Model) string {
	st := m.st
	if m.settings.Repo == "" {
		return m.emptyState("📁", "No repository chosen", "Pick a repository to see artifacts", "press "+m.keyName("dashboard", "R")+" to choose", w, h)
	}
	if m.usingReleaseAssets() {
		rels := m.rankedReleases()
		l := m.arts
		if len(l.Items) != len(rels) {
			var syncItems []components.Item
			for _, ra := range rels {
				syncItems = append(syncItems, components.Item{ID: -ra.Asset.ID, Label: ra.Asset.Name})
			}
			l.Items = syncItems
		}
		if len(l.Filtered()) == 0 {
			return m.emptyState("🔍 ∅", "No matching release assets", fmt.Sprintf("No assets match %q", l.Filter), "press "+m.keyName("filter", "esc")+" to clear filter", w, h)
		}
		listH, subH := artSplit(h, len(rels))
		if listH < 3 {
			listH = 3
		}
		l.MaxShown = listH - 2
		l.Focused = m.focus == PanelArtifacts
		var items []components.Item
		bestScore := rels[0].Asset.Score
		for _, ra := range rels {
			a := ra.Asset
			star := "  "
			if !a.Rejected && a.Score == bestScore {
				star = st.Accent.Render(st.G.Star + " ")
			}
			style := st.Text
			if a.Rejected {
				style = st.Dim
			}
			line := star + style.Render(components.Clamp(a.Name, w-22))
			items = append(items, components.Item{
				ID:       -a.ID,
				Label:    a.Name,
				Rendered: line,
				Detail:   humanSize(a.Size),
			})
		}
		l.Items = items
		list := renderBoxedList(l, st, w-4, listH, m.focus == PanelArtifacts, func(it components.Item) bool {
			id, ok := it.ID.(int64)
			return ok && id == m.selectedArtID
		})
		if subH < 3 {
			return list
		}
		return list + "\n" + m.artifactDetailBox(w-4, subH)
	}

	r, ok := m.selectedRun()
	if !ok {
		return m.emptyState("📦 ⊘", "No run selected", "Select a workflow run in the Runs tab\nto view downloadable APKs", "press "+m.keyName("dashboard", "3")+" or "+m.keyName("dashboard", "tab")+" to switch to Runs", w, h)
	}
	if m.fetching[r.ID] {
		return centerEmpty(m.spin.View()+" "+st.Accent.Render("Loading artifacts…")+"\n\n"+st.Dim.Render(fmt.Sprintf("fetching for run #%d", r.RunNumber)), w, h)
	}
	arts, ok := m.runCache[r.ID]
	if !ok {
		return centerEmpty(m.spin.View()+" "+st.Accent.Render("Loading artifacts…")+"\n\n"+st.Dim.Render(fmt.Sprintf("fetching for run #%d", r.RunNumber)), w, h)
	}
	if len(arts) == 0 {
		return m.emptyState("📦 ∅", "No artifacts found", "This workflow run did not produce\nany downloadable APK artifacts", "check workflow run on GitHub", w, h)
	}
	l := m.arts
	if len(l.Filtered()) == 0 {
		return m.emptyState("🔍 ∅", "No matching artifacts", fmt.Sprintf("No artifacts match %q", l.Filter), "press "+m.keyName("filter", "esc")+" to clear filter", w, h)
	}
	listH, subH := artSplit(h, len(arts))
	if listH < 3 {
		listH = 3
	}
	l.MaxShown = listH - 2
	l.Focused = m.focus == PanelArtifacts
	var items []components.Item
	bestScore := arts[0].Score
	for i, a := range arts {
		star := "  "
		if !a.Rejected && a.Score == bestScore {
			star = st.Accent.Render(st.G.Star + " ")
		}
		name := a.Name
		style := st.Text
		if a.Rejected {
			style = st.Dim
		}
		line := star + style.Render(components.Clamp(name, w-22))
		row := components.Item{ID: a.ID, Label: a.Name, Rendered: line, Detail: humanSize(a.SizeInBytes)}
		if i < len(arts) {
			items = append(items, row)
		}
	}
	l.Items = items
	listW := w - 4
	list := renderBoxedList(l, st, listW, listH, m.focus == PanelArtifacts, func(it components.Item) bool {
		id, ok := it.ID.(int64)
		return ok && id == m.selectedArtID
	})
	if subH < 3 {
		return list
	}
	return list + "\n" + m.artifactDetailBox(w-4, subH)
}

// artifactDetailBox explains the selected artifact: how big, why it scored
// the way it did on THIS device, and what to do with it.
func (m Model) artifactDetailBox(w, h int) string {
	st := m.st
	title := "artifact"
	body := st.Dim.Render("  select an artifact")
	if m.usingReleaseAssets() {
		if ra, ok := m.selectedReleaseAsset(); ok {
			a := ra.Asset
			rels := m.rankedReleases()
			best := len(rels) > 0 && !a.Rejected && a.Score == rels[0].Asset.Score
			if best {
				title = components.Clamp(st.G.Star+" "+a.Name, w-14)
			} else {
				title = components.Clamp(a.Name, w-14)
			}
			var b strings.Builder
			kv := func(k, v string) {
				if v == "" {
					return
				}
				b.WriteString(fmt.Sprintf("%s %s\n", st.Dim.Render(fmt.Sprintf("%-8s", k)), components.Clamp(v, w-12)))
			}
			kv("source", "release "+ra.ReleaseTag)
			kv("size", humanSize(a.Size))
			kv("score", fmt.Sprint(a.Score)+"  "+st.Dim.Render("("+m.scoreWhy(a.Name)+")"))
			if a.Rejected {
				kv("policy", st.Warn.Render("rejected by arch/variant prefs"))
			} else {
				kv("policy", st.OK.Render("matches this device"))
			}
			if a.Digest != "" {
				kv("digest", st.OK.Render("sha256 published"))
			}
			if !ra.PublishedAt.IsZero() {
				kv("built", ra.PublishedAt.Format("2006-01-02 15:04"))
			}
			kv("", st.Dim.Render("d download · i download & install"))
			body = strings.TrimSuffix(b.String(), "\n")
		}
		return components.Panel{Title: title, Width: w, Height: h, Content: body}.Render(st)
	}
	if a, ok := m.selectedArtifact(); ok {
		best := false
		if r, ok2 := m.selectedRun(); ok2 {
			if arts, ok3 := m.runCache[r.ID]; ok3 && len(arts) > 0 {
				best = !a.Rejected && a.Score == arts[0].Score
			}
		}
		if best {
			title = components.Clamp(st.G.Star+" "+a.Name, w-14)
		} else {
			title = components.Clamp(a.Name, w-14)
		}
		var b strings.Builder
		kv := func(k, v string) {
			if v == "" {
				return
			}
			b.WriteString(fmt.Sprintf("%s %s\n", st.Dim.Render(fmt.Sprintf("%-8s", k)), components.Clamp(v, w-12)))
		}
		kv("size", humanSize(a.SizeInBytes)+" zipped")
		kv("score", fmt.Sprint(a.Score)+"  "+st.Dim.Render("("+m.scoreWhy(a.Name)+")"))
		if a.Rejected {
			kv("policy", st.Warn.Render("rejected by arch/variant prefs"))
		} else {
			kv("policy", st.OK.Render("matches this device"))
		}
		if r, ok := m.selectedRun(); ok {
			kv("run", fmt.Sprintf("#%d · %s", r.RunNumber, r.HeadBranch))
		}
		if !a.CreatedAt.IsZero() {
			kv("built", a.CreatedAt.Format("2006-01-02 15:04")+"  "+st.Dim.Render("("+reltime(a.CreatedAt)+" ago)"))
		}
		if !a.ExpiresAt.IsZero() {
			if a.Expired {
				kv("expires", st.Warn.Render("expired"))
			} else if d := time.Until(a.ExpiresAt); d > 0 {
				kv("expires", "in "+futureShort(d))
			} else {
				kv("expires", st.Warn.Render("expired"))
			}
		}
		kv("", st.Dim.Render("d download · i download & install"))
		body = strings.TrimSuffix(b.String(), "\n")
	}
	return components.Panel{Title: title, Width: w, Height: h, Content: body}.Render(st)
}

// scoreWhy explains which name hints drove the score on this device.
func (m Model) scoreWhy(name string) string {
	n := strings.ToLower(name)
	var parts []string
	pick := ""
	for _, arch := range []string{"arm64-v8a", "armeabi-v7a", "arm64", "armv7", "x86_64", "x86", "universal"} {
		if strings.Contains(n, arch) {
			pick = arch
			break
		}
	}
	if pick != "" {
		match := false
		if len(m.dev.ABIs) > 0 {
			for _, abi := range m.dev.ABIs {
				if abi == pick || strings.HasPrefix(abi, pick) || strings.HasPrefix(pick, abi) || pick == "universal" {
					match = true
					break
				}
			}
		}
		if match || len(m.dev.ABIs) == 0 {
			parts = append(parts, pick)
		} else {
			parts = append(parts, pick+" ≠ device")
		}
	}
	switch {
	case strings.Contains(n, "debug") && !strings.Contains(n, "release"):
		parts = append(parts, "debug")
	case strings.Contains(n, "release"):
		parts = append(parts, "release")
	}
	for _, junk := range []string{"mapping", "symbols", "report"} {
		if strings.Contains(n, junk) {
			parts = append(parts, "not the app ("+junk+")")
			break
		}
	}
	if len(parts) == 0 {
		return "no arch/variant hint in the name"
	}
	return strings.Join(parts, " · ")
}

// ───────────────────────────── setup screen ─────────────────────────────

func (m Model) renderSetup() string {
	st := m.st
	var b strings.Builder
	b.WriteString(st.Header.Render(" ghapk — setup ") + "\n\n")
	if m.authErr == ghapi.ErrNoGH {
		b.WriteString(st.Err.Render(" "+st.G.Fail+" the GitHub CLI (gh) is not installed") + "\n\n")
		b.WriteString("  ghapk talks to GitHub through gh, exactly like the\n  original script did.\n\n")
		b.WriteString(st.Bold.Render("  Install it:") + "\n")
		b.WriteString("    Termux          pkg install gh\n")
		b.WriteString("    Debian/Ubuntu   sudo apt install gh\n")
		b.WriteString("    Fedora          sudo dnf install gh\n")
		b.WriteString("    macOS           brew install gh\n")
		b.WriteString("    other           https://cli.github.com\n\n")
	} else {
		b.WriteString(st.Warn.Render(" "+st.G.Fail+" gh is installed but not authenticated") + "\n\n")
		b.WriteString("  Run in another terminal:\n\n")
		b.WriteString(st.Bold.Render("    gh auth login") + "\n\n")
	}
	b.WriteString(st.Muted.Render("  "+m.keyName("setup", "r")+"  re-check      "+m.keyName("setup", "q")+"  quit") + "\n")
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, b.String())
}
