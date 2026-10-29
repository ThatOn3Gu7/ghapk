package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/ghapk/ghapk/internal/tui/components"
)

func (m Model) buildHelp() viewport.Model {
	vp, _ := m.helpLayout()
	return vp
}

// helpLayout returns the display lines and their logical help-entry IDs. The
// IDs let a resize or wrap toggle keep the reader at the same entry.
func (m Model) helpLayout() (viewport.Model, []int) {
	st := m.st
	var b strings.Builder
	var lineEntries []int
	entry := 0
	appendLine := func(line string) { b.WriteString(line + "\n"); lineEntries = append(lineEntries, entry) }

	// A narrow dialog stacks the key above its description; larger dialogs
	// keep two columns, with wrapped continuation lines under the description.
	width := max(1, m.modalWindowWidth()-6)
	wrapped := !m.helpWrapOff
	writeText := func(text string, indent int, render func(...string) string) {
		pad := strings.Repeat(" ", indent)
		for _, line := range strings.Split(text, "\n") {
			if wrapped {
				line = ansi.Wrap(line, max(1, width-indent), "")
			}
			for _, part := range strings.Split(line, "\n") {
				appendLine(pad + render(part))
			}
		}
	}
	h1 := func(t string) {
		defer func() { entry++ }()
		appendLine("")
		appendLine(st.PanelTitleFoc.Render(t))
		appendLine("")
	}
	row := func(k, d string) {
		defer func() { entry++ }()
		if !wrapped {
			appendLine(fmt.Sprintf("  %s %s", st.Hotkey.Render(fmt.Sprintf("%-12s", k)), st.Muted.Render(d)))
			return
		}
		if width < 42 || components.VisibleWidth(k) > 15 {
			writeText(k, 2, st.Hotkey.Render)
			writeText(d, 4, st.Muted.Render)
			return
		}
		const keyWidth = 15
		const descIndent = keyWidth + 3
		lines := strings.Split(ansi.Wrap(d, max(1, width-descIndent), ""), "\n")
		appendLine("  " + st.Hotkey.Render(components.Pad(k, keyWidth)) + " " + st.Muted.Render(lines[0]))
		for _, line := range lines[1:] {
			appendLine(strings.Repeat(" ", descIndent) + st.Muted.Render(line))
		}
	}

	writeText(" ghapk · GitHub Actions artifact APK installer ", 0, st.Header.Render)
	entry++
	writeText("finds the newest build of your app in GitHub Actions,\nscores its artifacts for your device, downloads and installs it.", 0, st.Muted.Render)
	entry++

	keys := func(original, ctx string, actions ...string) string {
		var names []string
		changed := false
		for _, a := range actions {
			n := m.keyName(ctx, a)
			names = append(names, n)
			if n != a {
				changed = true
			}
		}
		if !changed {
			return original
		}
		return strings.Join(names, " / ")
	}
	h1("Moving around")
	row(keys("tab / shift+tab", "dashboard", "tab", "shift+tab"), "cycle panels")
	row(keys("1 … 7", "dashboard", "1", "2", "3", "4", "5", "6", "7"), "jump to Repos, Flows, Builds, Downloads, Settings, Device, Log")
	row(keys("enter", "dashboard", "enter"), "select a repo, branch or workflow; "+m.keyName("dashboard", "enter")+" again to move on")
	row(keys("j / k · ↑ / ↓", "dashboard", "j", "k", "up", "down"), "move in a list")
	row(keys("ctrl+d / ctrl+u", "dashboard", "ctrl+d", "ctrl+u"), "page a list")
	row(keys("g / G", "dashboard", "g", "G"), "first / last row")
	row(keys("enter / →", "dashboard", "enter", "right"), "open a build’s artifacts; "+m.keyName("dashboard", "enter")+" installs the selected artifact")
	row(keys("← / esc", "dashboard", "left", "esc"), "return to the build list")
	row("mouse", "click focuses & selects, click again opens, wheel scrolls")
	row("touch", "tap the repo name or bottom hints; swipe to scroll help")

	h1("Doing things")
	row(keys("d", "dashboard", "d"), "download the selected artifact")
	row(keys("i", "dashboard", "i"), "check the device and choose Install or Download only")
	row(keys("o", "dashboard", "o"), "open the selected run in the browser")
	row(keys("O", "dashboard", "O"), "open the download folder")
	row(keys("r", "dashboard", "r"), "refresh runs, workflows, releases")
	row(keys("w", "dashboard", "w"), "watch mode — auto-refresh while builds run")
	row(keys("/", "dashboard", "/"), "filter the focused list")
	row(keys("f", "dashboard", "f"), "star/unstar a recent repository")
	row(keys("e", "dashboard", "e"), "edit the highlighted repo entry")
	row(keys("x", "dashboard", "x"), "remove it from the recents file")

	h1("Downloads")
	row(keys("enter", "downloads", "enter"), "view job detail, or install a selected saved APK")
	row(keys("s / esc", "downloads", "s", "esc"), "focus saved APKs / return to jobs")
	row(keys("c", "downloads", "c"), "cancel the selected job (installer handoff cannot be undone)")
	row(keys("x / X", "downloads", "x", "X"), "remove a finished job / remove all finished jobs")
	row(keys("x", "downloads", "x"), "delete the selected saved APK after confirmation")
	row("touch", "tap rows and hints; swipe lists or job details")

	h1("Device")
	row(keys("j / k · ↑ / ↓", "device", "j", "k", "up", "down"), "scroll the device and setup details")
	row(keys("r", "device", "r"), "re-check device, tools and GitHub rate limit")
	row("touch", "swipe the device details to scroll")

	h1("Pickers & screens")
	row(keys("R", "dashboard", "R"), "repository picker (recents, yours, GitHub search)")
	row(keys("B", "dashboard", "B"), "branch picker (filter + default/protected/PR branches)")
	row(keys("W", "dashboard", "W"), "workflow picker outside Help (filter by name or .yml)")
	row("Settings pane", fmt.Sprintf("%s changes, %s resets, %s switches save scope", m.keyName("settings", "enter"), m.keyName("settings", "d"), m.keyName("settings", "s")))
	row("Log pane", fmt.Sprintf("scroll with %s / %s or swipe; %s follows, %s copies recent entries", m.keyName("log", "up"), m.keyName("log", "down"), m.keyName("log", "f"), m.keyName("log", "y")))
	row(keys("t", "dashboard", "t"), "cycle theme")
	row(keys("D", "dashboard", "D"), "doctor — environment health check")
	row(keys("?", "dashboard", "?"), "this help")

	row("Bindings", "Screen-specific keys are listed in keybinding.json; restart after editing.")
	h1("Sessions")
	row(keys("ctrl+z", "dashboard", "ctrl+z"), "suspend to the shell, fg to come back")
	row(keys("q / ctrl+c", "dashboard", "q")+func() string {
		if m.keyName("dashboard", "q") != "q" {
			return " / ctrl+c"
		}
		return ""
	}(), "quit — your terminal is restored, nothing is cleared")

	h1("How artifacts are picked")
	writeText(`Every artifact of a run is scored for your device:
variant (debug/release) + arch (arm64/armv7/x86/universal)
+ android-N API hints, minus symbols/mapping/reports.
The best score gets a ★ — exactly the script's algorithm.`, 2, st.Muted.Render)

	// Leave a gap and a column for the scrollbar.
	height := max(1, m.height-10)
	if m.height <= 20 {
		height = max(1, m.height-6)
	}
	vp := viewport.New(width, height)
	vp.SetContent(strings.TrimSuffix(b.String(), "\n"))
	return vp, lineEntries
}

// Reflow for width or wrap changes while keeping the same help entry visible.
func (m *Model) resizeHelp() {
	progress := m.helpVP.ScrollPercent()
	atTop, atBottom := m.helpVP.AtTop(), m.helpVP.AtBottom()
	oldOffset := m.helpVP.YOffset
	entry := -1
	if len(m.helpLineEntries) == m.helpVP.TotalLineCount() && oldOffset >= 0 && oldOffset < len(m.helpLineEntries) {
		entry = m.helpLineEntries[oldOffset]
	}
	m.helpVP, m.helpLineEntries = m.helpLayout()
	switch {
	case atTop:
	case atBottom:
		m.helpVP.GotoBottom()
	case entry >= 0:
		for i, v := range m.helpLineEntries {
			if v == entry {
				m.helpVP.SetYOffset(i)
				break
			}
		}
	default:
		m.helpVP.SetYOffset(int(progress*float64(max(0, m.helpVP.TotalLineCount()-m.helpVP.Height)) + 0.5))
	}
	m.helpDragging = false
}

// helpThumb uses the same proportional thumb and track as the YAML preview.
func (m Model) helpThumb() (start, size int) {
	h, total := m.helpVP.Height, m.helpVP.TotalLineCount()
	if h <= 0 || total <= h {
		return 0, 0
	}
	size = max(1, h*h/total)
	start = (h - size) * m.helpVP.YOffset / (total - h)
	return start, size
}

func (m Model) renderHelp() string {
	lines := strings.Split(m.helpVP.View(), "\n")
	start, size := m.helpThumb()
	if size == 0 {
		return strings.Join(lines, "\n")
	}
	for i := range lines {
		marker := m.st.Dim.Render("│")
		if i >= start && i < start+size {
			marker = m.st.Accent.Render("█")
		}
		lines[i] = components.Fit(lines[i], m.helpVP.Width) + " " + marker
	}
	return strings.Join(lines, "\n")
}

// helpBounds matches the centered modal's padded content, including the bar.
func (m Model) helpBounds() rect {
	boxWidth := m.modalWindowWidth() + 2
	boxHeight := m.helpVP.Height + 2
	return rect{x: max(0, (m.width-boxWidth)/2) + 3, y: max(0, (m.height-boxHeight)/2) + 1, w: max(1, boxWidth-6), h: m.helpVP.Height}
}

func (m Model) helpWrapHint() string {
	state := "on"
	if m.helpWrapOff {
		state = "off"
	}
	hint := m.keyName("help", "W") + ":wrap " + state
	if components.VisibleWidth("Help · "+hint) > max(1, m.modalWindowWidth()-4) {
		return "wrap " + state
	}
	return hint
}
func (m Model) helpTitle() string { return "Help · " + m.helpWrapHint() }

func (m *Model) toggleHelpWrap() {
	m.helpWrapOff = !m.helpWrapOff
	m.resizeHelp()
}

// The visible title hint is also a touch target for phones without a keyboard.
func (m Model) helpWrapBounds() rect {
	box := m.modalBox()
	first := components.StripANSI(strings.Split(box, "\n")[0])
	hint := m.helpWrapHint()
	idx := strings.Index(first, hint)
	if idx < 0 {
		return rect{}
	}
	width := components.VisibleWidth(hint)
	return rect{x: max(0, (m.width-lipgloss.Width(box))/2) + components.VisibleWidth(first[:idx]), y: max(0, (m.height-lipgloss.Height(box))/2), w: width, h: 1}
}
