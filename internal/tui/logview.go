package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/ThatOn3Gu7/ghapk/internal/activity"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
)

type logViewState struct {
	offset           int
	follow, dragging bool
	dragY            int
	cacheVersion     uint64
	cacheWidth       int
	cacheRows        []string
}
type logTickMsg struct{ seq int }
type logCopiedMsg struct{ err error }

func logTickCmd(seq int) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return logTickMsg{seq: seq} })
}

// logRows groups events by local capture date; explicit session start/end rows
// separate runs of the TUI, including sessions crossing midnight.
func (m Model) logRows() []string {
	if m.activity == nil {
		if m.logError != "" {
			return []string{"Activity log unavailable:", m.logError}
		}
		return []string{"Activity log is not available."}
	}
	version := m.activity.Version()
	w := max(8, m.width-5)
	if m.logState != nil && m.logState.cacheRows != nil && m.logState.cacheVersion == version && m.logState.cacheWidth == w {
		return m.logState.cacheRows
	}
	entries := m.activity.Entries()
	if len(entries) == 0 {
		return []string{"Nothing logged yet."}
	}
	var rows []string
	date, session := "", ""
	for _, e := range entries {
		d := e.At.Local().Format("2006-01-02")
		if d != date {
			date = d
			rows = append(rows, "--- "+date+" ---")
		}
		if e.Session != session {
			session = e.Session
			rows = append(rows, "-- Session "+e.At.Local().Format("15:04:05")+" --")
		}
		text := fmt.Sprintf("%s [%s] %s", e.At.Local().Format("15:04:05"), e.Kind, e.Text)
		rows = append(rows, strings.Split(ansi.Wrap(text, w, ""), "\n")...)
	}
	if m.logState != nil {
		m.logState.cacheVersion = version
		m.logState.cacheWidth = w
		m.logState.cacheRows = rows
	}
	return rows
}
func (m *Model) scrollLog(delta int) {
	rows := m.logRows()
	h := max(1, m.height-5)
	last := max(0, len(rows)-h)
	if m.logState.follow {
		m.logState.offset = last
	}
	m.logState.follow = false
	m.logState.offset = min(last, max(0, m.logState.offset+delta))
}
func (m Model) renderLog(h int) string {
	rows := m.logRows()
	height := max(1, h-2)
	width := max(1, m.width-4)
	last := max(0, len(rows)-height)
	offset := min(max(0, m.logState.offset), last)
	if m.logState.follow {
		offset = last
	}
	visible := make([]string, height)
	hasBar := last > 0
	thumbSize, thumbStart := 0, 0
	if hasBar {
		thumbSize = max(1, height*height/len(rows))
		thumbStart = offset * (height - thumbSize) / last
	}
	for i := 0; i < height; i++ {
		line := ""
		if offset+i < len(rows) {
			line = rows[offset+i]
		}
		style := m.st.Text
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "-- Session ") {
			style = m.st.PanelTitleFoc
		} else if strings.Contains(strings.ToLower(line), "failed") || strings.Contains(strings.ToLower(line), "error") {
			style = m.st.Warn
		}
		if hasBar {
			bar := "│"
			bs := m.st.Dim
			if i >= thumbStart && i < thumbStart+thumbSize {
				bar = "┃"
				bs = m.st.Accent
			}
			visible[i] = components.Fit(style.Render(line), width-1) + bs.Render(bar)
		} else {
			visible[i] = components.Fit(style.Render(line), width)
		}
	}
	badge := "paused"
	if m.logState.follow {
		badge = "following"
	}
	if m.activity != nil {
		if err := m.activity.Error(); err != nil {
			badge = "log error"
			visible[0] = components.Fit(m.st.Warn.Render("Log error: "+activity.Redact(err.Error())), width)
		}
	}
	return components.Panel{Title: "Log", Badge: badge, Focused: true, Width: m.width, Height: h, Content: strings.Join(visible, "\n")}.Render(m.st)
}
func (m *Model) logKey(key string) (bool, tea.Cmd) {
	switch key {
	case "j", "down":
		m.scrollLog(1)
	case "k", "up":
		m.scrollLog(-1)
	case "pgdown", "ctrl+d":
		m.scrollLog(max(1, m.height-5))
	case "pgup", "ctrl+u":
		m.scrollLog(-max(1, m.height-5))
	case "g":
		m.logState.follow = false
		m.logState.offset = 0
	case "G":
		m.logState.follow = true
	case "f":
		m.logState.follow = !m.logState.follow
		if !m.logState.follow {
			m.logState.offset = max(0, len(m.logRows())-max(1, m.height-5))
		}
	case "y":
		if m.activity == nil {
			m.toastf("log unavailable")
			return true, nil
		}
		entries := m.activity.Entries()
		if len(entries) > 60 {
			entries = entries[len(entries)-60:]
		}
		var b strings.Builder
		for _, e := range entries {
			fmt.Fprintf(&b, "%s [%s] %s\n", e.At.Local().Format("2006-01-02 15:04:05"), e.Kind, e.Text)
		}
		return true, copyLogCmd(b.String())
	default:
		return false, nil
	}
	return true, nil
}
func copyLogCmd(text string) tea.Cmd {
	return func() tea.Msg {
		options := [][]string{{"termux-clipboard-set"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}, {"pbcopy"}, {"clip.exe"}}
		for _, c := range options {
			if _, err := exec.LookPath(c[0]); err != nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			cmd := activity.CommandContext(ctx, c[0], c[1:]...)
			cmd.Stdin = strings.NewReader(text)
			err := cmd.Run()
			cancel()
			return logCopiedMsg{err}
		}
		return logCopiedMsg{fmt.Errorf("clipboard tool not found (install Termux:API)")}
	}
}
