package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
)

// Question body scrolls independently of its fixed, tappable choices. An
// action is not accepted until the complete warning has been made visible.
func (m Model) jobQuestionLines() ([]string, int) {
	width := min(m.width-2, 70) - 4
	var lines []string
	for _, line := range m.confirm.Body {
		lines = append(lines, strings.Split(ansi.Wrap(line, max(1, width), ""), "\n")...)
	}
	rows := max(1, m.height-2-len(m.confirm.Options)-3)
	return lines, rows
}
func (m Model) jobQuestionMaxScroll() int {
	lines, rows := m.jobQuestionLines()
	return max(0, len(lines)-rows)
}
func (m *Model) scrollJobQuestion(delta int) {
	m.jobQuestionScroll = max(0, min(m.jobQuestionMaxScroll(), m.jobQuestionScroll+delta))
}
func (m Model) jobQuestionBox() string {
	lines, rows := m.jobQuestionLines()
	start := max(0, min(m.jobQuestionScroll, m.jobQuestionMaxScroll()))
	end := min(len(lines), start+rows)
	var body []string
	body = append(body, lines[start:end]...)
	if len(lines) > rows {
		body = append(body, m.st.Dim.Render(fmt.Sprintf("%d/%d · swipe or %s", end, len(lines), m.keyName("confirm", "pgdown"))))
	} else {
		body = append(body, "")
	}
	body = append(body, "")
	for i, opt := range m.confirm.Options {
		label := m.questionOptionLabel(i, opt)
		style := m.st.ButtonBlur.Padding(0, 0)
		if i == m.confirm.Cursor {
			style = m.st.ButtonFocus.Padding(0, 0)
			if i == m.confirm.DangerIndex {
				style = style.Background(m.st.P.Error)
			}
		}
		body = append(body, style.Render(label))
	}
	choose, cancel := m.questionHints()
	body = append(body, m.st.Dim.Render(choose+" · "+cancel))
	return components.QuestionBox(m.st, m.width, m.confirm.Title, strings.Join(body, "\n"))
}

func (m Model) handleJobQuestionMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.MouseWheelUp || msg.Button == tea.MouseButtonWheelUp {
		m.scrollJobQuestion(-3)
		return m, nil
	}
	if msg.Type == tea.MouseWheelDown || msg.Button == tea.MouseButtonWheelDown {
		m.scrollJobQuestion(3)
		return m, nil
	}
	if m.jobQuestionTouch && msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft {
		delta := m.jobQuestionTouchY - msg.Y
		if delta != 0 {
			m.jobQuestionMoved = true
			m.scrollJobQuestion(delta)
		}
		m.jobQuestionTouchY = msg.Y
		return m, nil
	}
	if msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease {
		active, moved, choice := m.jobQuestionTouch, m.jobQuestionMoved, m.jobQuestionTouchChoice
		m.jobQuestionTouch = false
		if active && !moved && choice != -1 && m.jobQuestionHit(msg.X, msg.Y) == choice {
			if choice == -3 {
				m.resetConfirm()
				return m, nil
			}
			if choice >= 0 {
				m.confirm.Cursor = choice
			}
			return m, m.confirmAccept()
		}
		return m, nil
	}
	if msg.Type == tea.MouseLeft && msg.Action == tea.MouseActionPress {
		box := m.modalBox()
		x, y := (m.width-components.VisibleWidth(strings.Split(box, "\n")[0]))/2, (m.height-lipgloss.Height(box))/2
		if msg.X >= x && msg.X < x+lipgloss.Width(box) && msg.Y >= y && msg.Y < y+lipgloss.Height(box) {
			m.jobQuestionTouch = true
			m.jobQuestionMoved = false
			m.jobQuestionTouchY = msg.Y
			m.jobQuestionTouchChoice = m.jobQuestionHit(msg.X, msg.Y)
		}
	}
	return m, nil
}

// Hit whole option rows and both visible footer hints, including on phones.
func (m Model) jobQuestionHit(x, y int) int {
	lines := strings.Split(m.modalLayer(), "\n")
	if y < 0 || y >= len(lines) {
		return -1
	}
	plain := components.StripANSI(lines[y])
	box := m.modalBox()
	left := (m.width - lipgloss.Width(box)) / 2
	if x < left+1 || x >= left+lipgloss.Width(box)-1 {
		return -1
	}
	bottom := (m.height-lipgloss.Height(box))/2 + lipgloss.Height(box) - 1
	index := y - (bottom - 1 - len(m.confirm.Options))
	if index >= 0 && index < len(m.confirm.Options) && strings.Contains(plain, m.questionOptionLabel(index, m.confirm.Options[index])) {
		return index
	}
	if y != bottom-1 {
		return -1
	}
	choose, cancel := m.questionHints()
	for _, h := range []struct {
		label  string
		result int
	}{{choose, -2}, {cancel, -3}} {
		if pos := strings.Index(plain, h.label); pos >= 0 {
			col := components.VisibleWidth(plain[:pos])
			if x >= col && x < col+components.VisibleWidth(h.label) {
				return h.result
			}
		}
	}
	return -1
}

func (m Model) questionOptionLabel(i int, option string) string {
	key := m.keyName("confirm", fmt.Sprint(i+1))
	label := key + "  " + option
	// Long key names must not wrap a safety choice or move its touch target.
	if key != fmt.Sprint(i+1) && components.VisibleWidth(label) > max(1, min(m.width-2, 70)-6) {
		label = "*  " + option
	}
	return label
}
func (m Model) questionHints() (string, string) {
	choose, cancel := "↵ choose", "Esc cancel"
	if k := m.keyName("confirm", "enter"); k != "enter" {
		choose = k + " choose"
	}
	if k := m.keyName("confirm", "esc"); k != "esc" {
		cancel = k + " cancel"
	}
	if components.VisibleWidth(choose+" · "+cancel) > max(1, min(m.width-2, 70)-6) {
		return "choose", "cancel"
	}
	return choose, cancel
}
