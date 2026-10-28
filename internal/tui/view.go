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

	switch m.modal {
	case ModalHelp:
		return m.overlayModal("Help", m.helpVP.View())
	case ModalSettings:
		if m.settingsUI != nil {
			return m.overlayModal("Settings", m.settingsUI.view(&m))
		}
	case ModalPickRepo:
		if m.repoPicker != nil {
			return m.overlayModal("Repository", m.repoPicker.view(&m))
		}
	case ModalPickBranch:
		if m.branchPicker != nil {
			return m.overlayModal("Branch", m.branchPicker.view(&m))
		}
		body := m.branchList.Render(m.st, 44)
		return m.overlayModal("Branch", body)
	case ModalPickWorkflow:
		if m.workflowPicker != nil {
			return m.overlayModal("Workflow", m.workflowPicker.view(&m))
		}
	case ModalEditRepo:
		body := m.editInput.View() + "\n\n" +
			m.st.Dim.Render("enter saves as owner/repo · esc cancels")
		return m.overlayModal("Edit repository", body)
	case ModalConfirm:
		return m.confirm.Render(m.st, m.width, m.height)
	case ModalTask:
		if m.task != nil {
			device_CanInstall = m.dev.CanInstall
			return m.overlayModal(ellipsisTitle(m.task.Title), m.task.view(&m))
		}
	case ModalDoctor:
		if m.doctor != nil {
			device_CanInstall = m.dev.CanInstall
			return m.overlayModal("Doctor", m.doctor.view(&m))
		}
	}
	return base
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

// overlayModal centers a box on the terminal's background.
func (m Model) overlayModal(title, body string) string {
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
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
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
