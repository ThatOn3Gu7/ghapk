package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
		if p == m.activeTab() {
			w += 2
		} // active header style adds padding
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
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(action)}
	}
}

// handleMouse implements click-to-focus, click-to-select,
// click-again-to-activate, and wheel scrolling of the hovered panel.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Complete an outside tap on release, so a swipe or a drag across the
	// border never dismisses an overlay or activates the dashboard beneath it.
	if m.modalOutsidePress {
		if msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease {
			m.modalOutsidePress = false
			if !m.modalOutsideMoved && m.modal == m.modalOutsideKind && m.modal != ModalNone &&
				(m.confirmKind != confirmJobSafety || m.jobQuestionID == m.modalOutsideQuestionID) &&
				!m.modalBounds().contains(msg.X, msg.Y) {
				return m.dismissModalOutside()
			}
			return m, nil
		}
		if msg.Action == tea.MouseActionMotion {
			if msg.X != m.modalOutsideX || msg.Y != m.modalOutsideY {
				m.modalOutsideMoved = true
			}
			return m, nil
		}
		if msg.Action == tea.MouseActionPress && (msg.Type == tea.MouseLeft || msg.Button == tea.MouseButtonLeft) {
			m.modalOutsidePress = false // terminals that omit release events
		}
	}
	if m.modal != ModalNone {
		m.deviceDrag = false
		m.branchTouchActive = false
		if m.width >= minWidth && m.height >= minHeight && msg.Action == tea.MouseActionPress &&
			(msg.Type == tea.MouseLeft || msg.Button == tea.MouseButtonLeft) {
			box := m.modalBox()
			if box != "" && !m.modalBounds().contains(msg.X, msg.Y) {
				m.modalOutsidePress, m.modalOutsideMoved = true, false
				m.modalOutsideX, m.modalOutsideY = msg.X, msg.Y
				m.modalOutsideKind, m.modalOutsideQuestionID = m.modal, m.jobQuestionID
				return m, nil
			}
		}
		return m.handleModalMouse(msg)
	}
	if m.screen != ScreenDashboard {
		return m, nil
	}
	if m.settingsUI != nil && m.settingsUI.drag {
		if msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease {
			s := m.settingsUI
			s.drag = false
			if !s.moved && m.focus == PanelSettings {
				line := s.offset + msg.Y - 3
				lines := settingsLines()
				if line >= 0 && line < len(lines) && lines[line].row >= 0 {
					row := lines[line].row
					if row == s.cursor {
						return s.handleKey(hintKey("enter"), &m)
					}
					s.cursor = row
					s.adjust(&m)
				}
			}
			return m, nil
		}
		if msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft {
			s := m.settingsUI
			delta := s.dragY - msg.Y
			if delta != 0 {
				s.moved = true
				s.move(delta)
				s.adjust(&m)
			}
			s.dragY = msg.Y
			return m, nil
		}
	}
	if m.deviceDrag {
		if msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease {
			m.deviceDrag = false
			return m, nil
		}
		if msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft {
			m.scrollDevice(m.deviceDragY - msg.Y)
			m.deviceDragY = msg.Y
			return m, nil
		}
	}
	if m.branchTouchActive {
		if msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease {
			m.branchTouchActive = false
			if !m.branchTouchMoved && m.focus == PanelRepos && m.repoBranchFocused && m.settings.Repo == m.branchTouchRepo {
				r := m.panelRects()[PanelRepos]
				banner, repoH, branchH := reposBranchSplit(r.h, len(m.repos.Items), len(m.branchList.Items), r.w)
				top := r.y + 1 + banner + repoH
				if msg.X >= r.x+2 && msg.X < r.x+r.w-2 && msg.Y > top && msg.Y < top+branchH-1 {
					idx := m.branchList.Offset + msg.Y - top - 1
					fs := m.branchList.Filtered()
					if idx >= 0 && idx < len(fs) && fs[idx].ID == m.branchTouchValue {
						m.branchList.Cursor = idx
						return m, m.activate()
					}
				}
			}
			return m, nil
		}
		if msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft {
			delta := m.branchTouchLastY - msg.Y
			if delta != 0 {
				m.branchTouchMoved = true
				m.branchList.MoveBy(delta)
			}
			m.branchTouchLastY = msg.Y
			return m, nil
		}
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
				if m.focus == PanelSettings && p != PanelSettings && m.settingsUI.editing {
					m.settingsUI.editing = false
					m.settingsUI.input.Blur()
				}
				m.focus = p
				m.wfYAMLFocused = false
				m.buildDetail = false
				m.updateListBounds()
				return m, nil
			}
		}
	}
	if m.activeTab() == PanelSettings && !m.settingsUI.editing {
		s := m.settingsUI
		if msg.Y >= 3 && msg.Y < 3+s.listHeight(&m) && (m.width < 96 || msg.X < m.width*58/100) {
			switch msg.Type {
			case tea.MouseWheelUp:
				s.move(-3)
				s.adjust(&m)
				return m, nil
			case tea.MouseWheelDown:
				s.move(3)
				s.adjust(&m)
				return m, nil
			case tea.MouseLeft:
				if msg.Action == tea.MouseActionPress {
					s.drag = true
					s.moved = false
					s.dragY = msg.Y
				}
				return m, nil
			}
		}
	}
	if m.activeTab() == PanelDevice {
		r := m.panelRects()[PanelDevice]
		if r.contains(msg.X, msg.Y) {
			switch msg.Type {
			case tea.MouseWheelUp:
				m.scrollDevice(-3)
				return m, nil
			case tea.MouseWheelDown:
				m.scrollDevice(3)
				return m, nil
			case tea.MouseLeft:
				if msg.Action == tea.MouseActionPress {
					m.deviceDrag = true
					m.deviceDragY = msg.Y
				}
				return m, nil
			}
		}
	}
	if m.activeTab() == PanelRuns {
		return m.handleBuildMouse(msg)
	}
	if m.activeTab() == PanelDownloads {
		return m.handleDownloadMouse(msg)
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
			if p == PanelRepos {
				if len(m.repos.Items) == 0 || len(m.repos.Filtered()) == 0 {
					return m, nil
				}
				banner, repoH, branchH := reposBranchSplit(r.h, len(m.repos.Items), len(m.branchList.Items), r.w)
				branchTop := r.y + 1 + banner + repoH
				if msg.Y >= branchTop && msg.Y < branchTop+branchH {
					m.branchList.MoveBy(d)
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
				if len(m.repos.Items) == 0 || len(m.repos.Filtered()) == 0 {
					return m, nil
				}
				if msg.Action != tea.MouseActionPress {
					return m, nil
				}
				bannerH, repoH, branchH := reposBranchSplit(r.h, len(m.repos.Items), len(m.branchList.Items), r.w)
				repoTop := r.y + 1 + bannerH
				branchTop := repoTop + repoH
				if msg.X < r.x+2 || msg.X >= r.x+r.w-2 {
					return m, nil
				}
				if msg.Y == branchTop {
					m.repoBranchFocused = true
					return m, nil
				}
				if msg.Y > branchTop && msg.Y < branchTop+branchH-1 {
					l := &m.branchList
					idx := l.Offset + msg.Y - branchTop - 1
					fs := l.Filtered()
					if idx < 0 || idx >= len(fs) {
						return m, nil
					}
					m.repoBranchFocused = true
					l.Cursor = idx
					l.MoveBy(0)
					value, ok := fs[idx].ID.(string)
					if !ok {
						return m, nil
					}
					// Wait for release so a swipe scrolls without selecting.
					m.branchTouchActive = true
					m.branchTouchMoved = false
					m.branchTouchLastY = msg.Y
					m.branchTouchValue = value
					m.branchTouchRepo = m.settings.Repo
					return m, nil
				}
				if msg.Y <= repoTop || msg.Y >= repoTop+repoH-1 {
					return m, nil
				}
				l := &m.repos
				idx := l.Offset + msg.Y - repoTop - 1
				fs := l.Filtered()
				if idx < 0 || idx >= len(fs) {
					return m, nil
				}
				same := !m.repoBranchFocused && l.Cursor == idx
				m.repoBranchFocused = false
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

// modalBounds uses the same dimensions and centering as components.Overlay.
func (m Model) modalBounds() rect {
	box := m.modalBox()
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	return rect{x: max(0, (m.width-w)/2), y: max(0, (m.height-h)/2), w: w, h: h}
}

// Outside clicks have the same cancellation semantics as Escape, including
// rejecting queued install questions and cancelling in-flight preflight work.
func (m Model) dismissModalOutside() (tea.Model, tea.Cmd) {

	if m.modal == ModalTask && m.task != nil && m.task.running {
		m.pendingInstall = false
		if m.task.cancel != nil {
			m.task.cancel()
		}
		m.taskSeq++ // ignore late results from the dismissed operation
		m.task = nil
		m.modal = ModalNone
		m.toastf("cancelled; an Android installer already opened may remain open")
		return m, nil
	}
	return m.handleModalKey(tea.KeyMsg{Type: tea.KeyEsc})
}

// handleModalMouse captures input before it can reach the dashboard.
func (m Model) handleModalMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.modal == ModalConfirm && m.confirmKind == confirmJobSafety {
		return m.handleJobQuestionMouse(msg)
	}
	if m.modal == ModalPlan {
		return m.handlePlanMouse(msg)
	}
	if m.modal == ModalHelp {
		return m.handleHelpMouse(msg), nil
	}
	if m.modal == ModalPickRepo && m.repoPicker != nil {
		return m.handleRepoPickerMouse(msg)
	}
	if msg.Type != tea.MouseLeft {
		return m, nil
	}
	var buttons []string
	switch m.modal {
	case ModalConfirm:
		if m.safetyDialogTooLarge() {
			buttons = []string{"Cancel"}
		} else {
			buttons = m.confirm.Options
		}
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
		if m.safetyDialogTooLarge() {
			m.resetConfirm()
			return m, nil
		}
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

// repoPickerBounds matches the centered overlay and its input, divider and list.
func (m Model) repoPickerBounds() (rect, rect) {
	box := m.modalBox()
	x := max(0, (m.width-lipgloss.Width(box))/2) + 3
	y := max(0, (m.height-lipgloss.Height(box))/2) + 3
	w := max(1, m.modalWindowWidth()-4)
	rows := m.repoPicker.list.MaxShown
	return rect{x: x, y: y, w: w, h: rows}, rect{x: x, y: y + rows, w: w, h: 1}
}

func (m Model) handleRepoPickerMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	rp := m.repoPicker
	if m.width < minWidth || m.height < minHeight {
		rp.touchActive = false
		return m, nil
	}
	bounds, status := m.repoPickerBounds()
	if msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease {
		active := rp.touchActive
		rp.touchActive = false
		if active && !rp.touchMoved && bounds.contains(msg.X, msg.Y) {
			idx := rp.list.Offset + msg.Y - bounds.y
			items := rp.list.Filtered()
			if idx >= 0 && idx < len(items) && items[idx].ID == rp.touchValue {
				rp.list.Cursor = idx
				return rp.handleKey(tea.KeyMsg{Type: tea.KeyEnter}, &m)
			}
		}
		return m, nil
	}
	if rp.touchActive && msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft {
		delta := rp.touchLastY - msg.Y
		if delta != 0 {
			rp.touchMoved = true
			rp.list.MoveBy(delta)
		}
		rp.touchLastY = msg.Y
		return m, nil
	}
	if (msg.Type == tea.MouseWheelUp || msg.Button == tea.MouseButtonWheelUp) && bounds.contains(msg.X, msg.Y) {
		rp.list.MoveBy(-3)
		return m, nil
	}
	if (msg.Type == tea.MouseWheelDown || msg.Button == tea.MouseButtonWheelDown) && bounds.contains(msg.X, msg.Y) {
		rp.list.MoveBy(3)
		return m, nil
	}
	if msg.Type == tea.MouseLeft && msg.Action == tea.MouseActionPress {
		hintY := status.y + 1
		chooseWidth := components.VisibleWidth(repoChooseHint)
		cancelX := status.x + chooseWidth + components.VisibleWidth(repoHintSeparator)
		cancelWidth := components.VisibleWidth(repoCancelHint)
		if msg.Y == hintY && msg.X >= status.x && msg.X < status.x+chooseWidth {
			rp.touchActive = false
			return rp.handleKey(tea.KeyMsg{Type: tea.KeyEnter}, &m)
		}
		if msg.Y == hintY && msg.X >= cancelX && msg.X < cancelX+cancelWidth {
			rp.touchActive = false
			return rp.handleKey(tea.KeyMsg{Type: tea.KeyEsc}, &m)
		}
		if status.contains(msg.X, msg.Y) && rp.errMsg != "" {
			return m, m.fetchUserRepos()
		}
		if bounds.contains(msg.X, msg.Y) {
			idx := rp.list.Offset + msg.Y - bounds.y
			items := rp.list.Filtered()
			if idx >= 0 && idx < len(items) {
				if value, ok := items[idx].ID.(string); ok {
					rp.list.Cursor = idx
					rp.touchActive = true
					rp.touchMoved = false
					rp.touchLastY = msg.Y
					rp.touchValue = value
				}
			}
		}
	}
	return m, nil
}

// Confirm buttons can wrap onto multiple lines on phones. Hit-test the
// rendered labels rather than assuming every action is on the bottom row.
func (m Model) confirmButtonHit(px, py int, buttons []string) int {
	lines := strings.Split(m.modalLayer(), "\n")
	bottom := -1
	for y := len(lines) - 1; y >= 0; y-- {
		p := components.StripANSI(lines[y])
		if strings.Contains(p, "╰─") || strings.Contains(p, "+-") {
			bottom = y
			break
		}
	}
	footer := 0
	if m.confirmKind == confirmJobSafety {
		footer = 1
	}
	if py < bottom-len(buttons)-footer || py >= bottom-footer || py < 0 || py >= len(lines) {
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
	lines := strings.Split(m.modalLayer(), "\n")
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

// Help accepts both terminal wheel events and press/drag/release gestures.
func (m Model) handleHelpMouse(msg tea.MouseMsg) Model {
	if m.width < minWidth || m.height < minHeight {
		m.helpDragging = false
		return m
	}
	bounds := m.helpBounds()
	if msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease {
		m.helpDragging = false
		return m
	}
	if msg.Type == tea.MouseWheelUp || msg.Button == tea.MouseButtonWheelUp {
		if bounds.contains(msg.X, msg.Y) {
			m.helpVP.LineUp(3)
		}
		return m
	}
	if msg.Type == tea.MouseWheelDown || msg.Button == tea.MouseButtonWheelDown {
		if bounds.contains(msg.X, msg.Y) {
			m.helpVP.LineDown(3)
		}
		return m
	}
	if msg.Action == tea.MouseActionPress && (msg.Type == tea.MouseLeft || msg.Button == tea.MouseButtonLeft) {
		if m.helpWrapBounds().contains(msg.X, msg.Y) {
			m.toggleHelpWrap()
			return m
		}
		m.helpDragging = bounds.contains(msg.X, msg.Y)
		m.helpDragLastY = msg.Y
		start, size := m.helpThumb()
		m.helpDragScrollbar = m.helpDragging && size > 0 && msg.X == bounds.x+bounds.w-1
		if m.helpDragScrollbar {
			row := msg.Y - bounds.y
			if row >= start && row < start+size {
				// Grabbing the thumb must not make it jump.
				m.helpDragThumbOffset = row - start
			} else {
				m.helpDragThumbOffset = size / 2
				m.helpScrollToBar(msg.Y, bounds, size)
			}
		}
		return m
	}
	if msg.Action == tea.MouseActionMotion && m.helpDragging && msg.Button == tea.MouseButtonLeft {
		if m.helpDragScrollbar {
			_, size := m.helpThumb()
			if size > 0 {
				m.helpScrollToBar(msg.Y, bounds, size)
			}
		} else {
			m.helpVP.SetYOffset(m.helpVP.YOffset + m.helpDragLastY - msg.Y)
		}
		m.helpDragLastY = msg.Y
	}
	return m
}

func (m *Model) helpScrollToBar(y int, bounds rect, thumbHeight int) {
	travel := bounds.h - thumbHeight
	if travel <= 0 {
		return
	}
	thumbY := min(travel, max(0, y-bounds.y-m.helpDragThumbOffset))
	scrollMax := max(0, m.helpVP.TotalLineCount()-m.helpVP.Height)
	m.helpVP.SetYOffset((thumbY*scrollMax + travel/2) / travel)
}
