package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/tui/components"
)

// rect is an outer panel rectangle in screen cells.
type rect struct {
	x, y, w, h int
}

func (r rect) contains(px, py int) bool {
	return px >= r.x && px < r.x+r.w && py >= r.y && py < r.y+r.h
}

// panelRects computes the exact geometry the dashboard renders with, so
// mouse hit-testing can never drift from what is on screen. The layout
// Builds uses the same geometry for rendering and hit-testing.
func (m Model) panelRects() map[PanelID]rect {
	if m.activeTab() == PanelRuns {
		l := m.buildsLayout()
		out := map[PanelID]rect{}
		if l.runs.w > 0 {
			out[PanelRuns] = l.runs
		}
		if l.arts.w > 0 {
			out[PanelArtifacts] = l.arts
		}
		return out
	}
	return map[PanelID]rect{m.focus: {0, 2, m.width, m.height - 3}}
}

// compactTabRects gives the clickable spans of the compact tab bar.
// It mirrors renderCompact: each tab is " label " plus one join space,
// where label comes from compactTabLabel (hotkey + name + live count).
func (m Model) compactTabRects() map[PanelID]rect {
	out := map[PanelID]rect{}
	labels, padded := m.tabLabels()
	x := 0
	for _, p := range FocusOrder {
		w := components.VisibleWidth(labels[p])
		if padded {
			w += 2
		}
		out[p] = rect{x, 1, w, 1}
		x += w + 1
	}
	return out
}

// listOf returns the model's list for a panel, or nil for non-list panels.
func (m *Model) listOf(p PanelID) *components.List {
	switch p {
	case PanelRepos:
		return &m.repos
	case PanelWorkflows:
		return &m.workflows
	case PanelRuns:
		return &m.runs
	case PanelArtifacts:
		return &m.arts
	}
	return nil
}

// hintKey translates a visible hint into exactly the same message as a keypress.
func hintKey(action string) tea.KeyMsg {
	switch action {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(action)}
	}
}

// handleMouse implements click-to-focus, click-to-select,
// click-again-to-activate, and wheel scrolling of the hovered panel.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.screen != ScreenDashboard {
		return m, nil
	}
	if m.modal != ModalNone {
		return m.handleModalMouse(msg)
	}

	if msg.Type == tea.MouseLeft && msg.Action == tea.MouseActionPress && m.width >= minWidth && m.height >= minHeight {
		if _, repo := m.headerLayout(); repo.contains(msg.X, msg.Y) {
			return m, m.openRepoPicker()
		}
		if msg.Y == m.height-1 {
			sb := m.statusBar()
			_, spans := sb.Layout(m.st)
			for _, span := range spans {
				if msg.X >= span.Start && msg.X < span.End {
					return m.handleKey(hintKey(sb.Hints[span.Index].Action))
				}
			}
			return m, nil
		}
		for p, r := range m.compactTabRects() {
			if r.contains(msg.X, msg.Y) {
				m.focus = p
				m.wfYAMLFocused = false
				m.buildDetail = false
				m.updateListBounds()
				return m, nil
			}
		}
	}
	if m.activeTab() == PanelRuns {
		return m.handleBuildMouse(msg)
	}

	switch msg.Type {

	case tea.MouseWheelUp, tea.MouseWheelDown:
		d := 3
		if msg.Type == tea.MouseWheelUp {
			d = -3
		}
		for p, r := range m.panelRects() {
			if !r.contains(msg.X, msg.Y) {
				continue
			}
			// In Workflows, scroll the YAML preview if hovering over the sub-box
			if p == PanelWorkflows {
				listH, subH := flowsSplit(r.h, len(m.workflows.Items))
				subBoxTop := r.y + 1 + listH
				if subH > 1 && msg.Y >= subBoxTop {
					m.scrollWorkflowYAML(d)
					return m, nil
				}
			}
			if l := m.listOf(p); l != nil {
				l.MoveBy(d)
				// mouse scroll does not steal keyboard focus
				switch p {
				case PanelRuns:
					return m, m.fetchArtifactsForSelection()
				case PanelWorkflows:
					m.wfScroll = 0
					return m, m.fetchWorkflowForSelection()
				}
			}
			return m, nil
		}
		return m, nil

	case tea.MouseLeft:
		if m.compact {
			for p, r := range m.compactTabRects() {
				if r.contains(msg.X, msg.Y) {
					m.focus = p
					m.wfYAMLFocused = false
					return m, nil
				}
			}
		}
		for p, r := range m.panelRects() {
			if !r.contains(msg.X, msg.Y) {
				continue
			}
			m.focus = p
			l := m.listOf(p)
			if l == nil {
				m.wfYAMLFocused = false
				return m, nil
			}

			if p == PanelRepos {
				bannerH, listH := reposSplit(r.h, len(m.repos.Items), r.w)
				listInnerH := listH - 2
				if listInnerH < 1 {
					listInnerH = 1
				}
				row := msg.Y - (r.y + 2 + bannerH)
				if row < 0 || row >= listInnerH {
					return m, nil
				}
				idx := l.Offset + row
				fs := l.Filtered()
				if idx < 0 || idx >= len(fs) {
					return m, nil
				}
				same := l.Cursor == idx
				l.Cursor = idx
				l.MoveBy(0)
				if same {
					return m, m.activate()
				}
				return m, nil
			}

			if p == PanelWorkflows {
				listH, subH := flowsSplit(r.h, len(m.workflows.Items))
				subBoxTop := r.y + 1 + listH
				if subH > 1 && msg.Y >= subBoxTop && msg.Y < subBoxTop+subH {
					m.wfYAMLFocused = true
					return m, nil
				}
				m.wfYAMLFocused = false
				listInnerH := listH - 2
				if listInnerH < 1 {
					listInnerH = 1
				}
				row := msg.Y - (r.y + 2)
				if row < 0 || row >= listInnerH {
					return m, nil
				}
				idx := l.Offset + row
				fs := l.Filtered()
				if idx < 0 || idx >= len(fs) {
					return m, nil
				}
				same := l.Cursor == idx
				l.Cursor = idx
				l.MoveBy(0)
				if same {
					return m, m.activate()
				}
				m.wfScroll = 0
				return m, m.fetchWorkflowForSelection()
			}

			if p == PanelRuns {
				m.wfYAMLFocused = false
				listH, _ := runsSplit(r.h, len(m.runs.Items))
				listInnerH := listH - 2
				if listInnerH < 1 {
					listInnerH = 1
				}
				row := msg.Y - (r.y + 2)
				if row < 0 || row >= listInnerH {
					return m, nil
				}
				idx := l.Offset + row
				fs := l.Filtered()
				if idx < 0 || idx >= len(fs) {
					return m, nil
				}
				same := l.Cursor == idx
				l.Cursor = idx
				l.MoveBy(0)
				if same {
					return m, m.activate()
				}
				return m, m.fetchArtifactsForSelection()
			}

			if p == PanelArtifacts {
				m.wfYAMLFocused = false
				count := 0
				if rRun, ok := m.selectedRun(); ok {
					count = len(m.runCache[rRun.ID])
				}
				listH, _ := artSplit(r.h, count)
				listInnerH := listH - 2
				if listInnerH < 1 {
					listInnerH = 1
				}
				row := msg.Y - (r.y + 2)
				if row < 0 || row >= listInnerH {
					return m, nil
				}
				idx := l.Offset + row
				fs := l.Filtered()
				if idx < 0 || idx >= len(fs) {
					return m, nil
				}
				same := l.Cursor == idx
				l.Cursor = idx
				l.MoveBy(0)
				if same {
					return m, m.activate()
				}
				return m, nil
			}

			return m, nil
		}
		return m, nil
	}
	return m, nil
}

// handleModalMouse supports clicking action buttons in ModalConfirm and ModalTask.
func (m Model) handleModalMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Type != tea.MouseLeft {
		return m, nil
	}
	var buttons []string
	switch m.modal {
	case ModalConfirm:
		buttons = m.confirm.Options
	case ModalTask:
		if m.task != nil && m.task.isDismissable() {
			buttons = m.task.buttons
		}
	}
	if len(buttons) == 0 {
		return m, nil
	}
	idx := -1
	if m.modal == ModalConfirm {
		idx = m.confirmButtonHit(msg.X, msg.Y, buttons)
	} else {
		idx = m.modalButtonHit(msg.X, msg.Y, buttons)
	}
	if idx < 0 {
		return m, nil
	}
	switch m.modal {
	case ModalConfirm:
		m.confirm.Cursor = idx
		cmd := m.confirmAccept()
		return m, cmd
	case ModalTask:
		m.task.btnIdx = idx
		cmd := m.task.activateFocused(&m)
		return m, cmd
	}
	return m, nil
}

// Confirm buttons can wrap onto multiple lines on phones. Hit-test the
// rendered labels rather than assuming every action is on the bottom row.
func (m Model) confirmButtonHit(px, py int, buttons []string) int {
	lines := strings.Split(m.View(), "\n")
	bottom := -1
	for y := len(lines) - 1; y >= 0; y-- {
		p := components.StripANSI(lines[y])
		if strings.Contains(p, "╰─") || strings.Contains(p, "+-") {
			bottom = y
			break
		}
	}
	if py < bottom-len(buttons) || py >= bottom || py < 0 || py >= len(lines) {
		return -1
	}
	line := components.StripANSI(lines[py])
	best, bestLen := -1, 0
	for i, label := range buttons {
		start := strings.Index(line, label)
		if start < 0 {
			continue
		}
		x := components.VisibleWidth(line[:start])
		w := components.VisibleWidth(label)
		if px >= x && px < x+w && w > bestLen {
			best, bestLen = i, w
		}
	}
	return best
}

// modalButtonHit returns the 0-based index of the modal button at screen cell
// (px, py), or -1 if (px, py) is outside the button strip.
func (m Model) modalButtonHit(px, py int, buttons []string) int {
	lines := strings.Split(m.View(), "\n")
	bottomY := -1
	for y := len(lines) - 1; y >= 1; y-- {
		plain := components.StripANSI(lines[y])
		if strings.Contains(plain, "╰─") || strings.Contains(plain, "+-") {
			bottomY = y
			break
		}
	}
	if bottomY < 1 || py != bottomY-1 {
		return -1
	}
	btnPlain := components.StripANSI(lines[bottomY-1])
	runes := []rune(btnPlain)
	borderCol := -1
	for i, r := range runes {
		if r == '│' || r == '|' {
			borderCol = components.VisibleWidth(string(runes[:i]))
			break
		}
	}
	if borderCol < 0 {
		return -1
	}
	curX := borderCol + 3 // 1 cell border + 2 cells left padding
	for i, b := range buttons {
		bw := components.VisibleWidth(b) + 4 // ButtonFocus/ButtonBlur PaddingLeft(2)+PaddingRight(2)
		if px >= curX && px < curX+bw {
			return i
		}
		curX += bw + 2 // "  " join separator
	}
	return -1
}
