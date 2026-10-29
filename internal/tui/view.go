package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/tui/components"
	"github.com/ghapk/ghapk/internal/tui/theme"
)

const (
	minWidth  = 32
	minHeight = 18
)

// View renders the whole UI.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		return m.st.Text.Render("  ghapk starting…")
	}
	if m.width < minWidth || m.height < minHeight {
		return m.renderTooSmall()
	}

	var base string
	switch m.screen {
	case ScreenSetup:
		base = m.renderSetup()
	default:
		base = m.renderDashboard()
	}

	if box := m.modalBox(); box != "" {
		return components.Overlay(base, box, m.width, m.height)
	}
	return base
}

// modalBox is shared by rendering and modal mouse hit-testing.
func (m Model) modalBox() string {
	switch m.modal {
	case ModalHelp:
		return m.modalWindow(m.helpTitle(), m.renderHelp())
	case ModalSettings:
		if m.settingsUI != nil {
			return m.modalWindow("Settings", m.settingsUI.view(&m))
		}
	case ModalPickRepo:
		if m.repoPicker != nil {
			title := "Repository"
			if m.userReposLoaded {
				title = fmt.Sprintf("Repository · %d", len(m.cachedUserRepos))
			}
			return m.modalWindow(title, m.repoPicker.view(&m))
		}
	case ModalPickBranch:
		if m.branchPicker != nil {
			return m.modalWindow("Branch", m.branchPicker.view(&m))
		}
		body := m.branchList.Render(m.st, 44)
		return m.modalWindow("Branch", body)
	case ModalPickWorkflow:
		if m.workflowPicker != nil {
			return m.modalWindow("Workflow", m.workflowPicker.view(&m))
		}
	case ModalEditRepo:
		body := m.editInput.View() + "\n\n" +
			m.st.Dim.Render("enter saves as owner/repo · esc cancels")
		return m.modalWindow("Edit repository", body)
	case ModalConfirm:
		if m.confirmKind == confirmJobSafety {
			return m.jobQuestionBox()
		}
		if m.safetyDialogTooLarge() {
			return (components.Confirm{Title: "Safety check", Body: []string{"Not enough room to show the safety warnings.", "Zoom out or rotate to see them."}, Options: []string{"Cancel"}}).Box(m.st, m.width)
		}
		return m.confirm.Box(m.st, m.width)
	case ModalTask:
		if m.task != nil {
			device_CanInstall = m.dev.CanInstall
			return m.modalWindow(ellipsisTitle(m.task.Title), m.task.view(&m))
		}
	case ModalPlan:
		if m.plan != nil {
			return m.planBox()
		}
	case ModalDoctor:
		if m.doctor != nil {
			device_CanInstall = m.dev.CanInstall
			return m.modalWindow("Doctor", m.doctor.view(&m))
		}
	}
	return ""
}

// Never offer an install override when the full warning cannot fit onscreen.
func (m Model) safetyDialogTooLarge() bool {
	if m.modal != ModalConfirm || m.confirmKind != confirmSafetyOverride {
		return false
	}
	if m.width < minWidth || m.height < minHeight {
		return true
	}
	box := m.confirm.Box(m.st, m.width)
	return lipgloss.Width(box) > m.width || lipgloss.Height(box) > m.height
}

func ellipsisTitle(s string) string { return s }

func (m Model) renderTooSmall() string {
	w := m.width
	if w < 20 {
		w = 20
	}
	// adaptive copy so it never mid-word clips on tiny screens
	full := fmt.Sprintf("terminal too small (%dx%d) — need at least %dx%d",
		m.width, m.height, minWidth, minHeight)
	hint := "resize, zoom out, or rotate your phone / font"
	if components.VisibleWidth(full) > w-2 {
		full = fmt.Sprintf("too small: %dx%d", m.width, m.height)
		hint = fmt.Sprintf("need %dx%d — zoom out or rotate", minWidth, minHeight)
	}
	if components.VisibleWidth(hint) > w-2 {
		hint = fmt.Sprintf("need %dx%d", minWidth, minHeight)
	}
	body := "\n  " + components.Clamp(full, w-2) + "\n\n  " + components.Clamp(hint, w-2) + "\n"
	return lipgloss.NewStyle().Foreground(m.st.P.Warning).Render(body)
}

// modalWindowWidth is the shared width used by dialog layout and hit-testing.
func (m Model) modalWindowWidth() int {
	w := m.width * 3 / 4
	if w < 50 {
		w = m.width - 8
	}
	if w > m.width-4 {
		w = m.width - 4
	}
	if w < 30 {
		w = m.width - 2
	}
	return w
}

// modalWindow renders the dialog without the surrounding whitespace.
func (m Model) modalWindow(title, body string) string {
	w := m.modalWindowWidth()
	{
		ls := strings.Split(body, "\n")
		for i := range ls {
			ls[i] = components.Clamp(ls[i], w)
		}
		body = strings.Join(ls, "\n")
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.st.P.BorderFocus).
		Padding(0, 2).
		Width(w)
	box := border.Render(body)
	// draw the title into the top border
	lines := strings.Split(box, "\n")
	if len(lines) > 0 && title != "" {
		lines[0] = putTitle(lines[0], m.st, title)
		box = strings.Join(lines, "\n")
	}
	return box
}

// modalLayer gives the dialog in its screen position, without the dashboard.
// Mouse hit-testing must use this layer, never the borders behind it.
func (m Model) modalLayer() string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.modalBox())
}

// overlayModal is the isolated, centered dialog rendering.
func (m Model) overlayModal(title, body string) string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.modalWindow(title, body))
}

func putTitle(borderLine string, st theme.Styles, title string) string {
	plain := components.StripANSI(borderLine)
	runes := []rune(plain)
	tt := st.PanelTitleFoc.Render(" " + title + " ")
	cw := components.VisibleWidth(tt)
	start := 3
	if start+cw >= len(runes) {
		return borderLine
	}
	left := string(runes[:start])
	right := string(runes[start+cw:])
	bc := lipgloss.NewStyle().Foreground(st.P.BorderFocus)
	return bc.Render(left) + tt + bc.Render(right)
}
