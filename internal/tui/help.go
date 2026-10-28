package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
	"github.com/ghapk/ghapk/internal/tui/components"
)

func (m Model) buildHelp() viewport.Model {
	st := m.st
	var b strings.Builder

	h1 := func(t string) { b.WriteString("\n" + st.PanelTitleFoc.Render(t) + "\n\n") }
	row := func(k, d string) {
		b.WriteString(fmt.Sprintf("  %s %s\n", st.Hotkey.Render(fmt.Sprintf("%-12s", k)), st.Muted.Render(d)))
	}

	b.WriteString(st.Header.Render(" ghapk · GitHub Actions artifact APK installer ") + "\n")
	b.WriteString(st.Muted.Render("finds the newest build of your app in GitHub Actions,\nscores its artifacts for your device, downloads and installs it.") + "\n")

	h1("Moving around")
	row("tab / shift+tab", "cycle panels")
	row("1 … 4", "jump to Repos, Flows, Builds, Device")
	row("enter", "select a repo or workflow; enter again to move on")
	row("j / k · ↑ / ↓", "move in a list")
	row("ctrl+d / ctrl+u", "page a list")
	row("g / G", "first / last row")
	row("enter / →", "open a build’s artifacts; enter installs the selected artifact")
	row("← / esc", "return to the build list")
	row("mouse", "click focuses & selects, click again opens, wheel scrolls")
	row("touch", "tap the repo name or bottom hints; swipe to scroll help")

	h1("Doing things")
	row("d", "download the selected artifact")
	row("i", "download and install it")
	row("o", "open the selected run in the browser")
	row("O", "open the download folder")
	row("r", "refresh runs, workflows, releases")
	row("w", "watch mode — auto-refresh while builds run")
	row("/", "filter the focused list")
	row("f", "star/unstar a recent repository")
	row("e", "edit the highlighted repo entry")
	row("x", "remove it from the recents file")

	h1("Pickers & screens")
	row("R", "repository picker (recents, yours, GitHub search)")
	row("B", "branch picker (filter + default/protected/PR branches)")
	row("W", "workflow picker (filter workflows by name or .yml)")
	row("s", "settings editor")
	row("t", "cycle theme")
	row("D", "doctor — environment health check")
	row("?", "this help")

	h1("Sessions")
	row("ctrl+z", "suspend to the shell, fg to come back")
	row("q / ctrl+c", "quit — your terminal is restored, nothing is cleared")

	h1("How artifacts are picked")
	b.WriteString(st.Muted.Render(`  Every artifact of a run is scored for your device:
  variant (debug/release) + arch (arm64/armv7/x86/universal)
  + android-N API hints, minus symbols/mapping/reports.
  The best score gets a ★ — exactly the script's algorithm.`))

	// Leave a gap and a column for the indicator so it remains visible on
	// phones, and wrap rather than clipping long help lines at narrow widths.
	width := max(1, m.modalWindowWidth()-6)
	height := max(1, m.height-10)
	if m.height <= 20 {
		height = max(1, m.height-6)
	}
	vp := viewport.New(width, height)
	vp.SetContent(ansi.Wrap(b.String(), width, ""))
	return vp
}

// Reflow for the new width and retain relative scroll position on rotation.
func (m *Model) resizeHelp() {
	progress := m.helpVP.ScrollPercent()
	atTop := m.helpVP.AtTop()
	m.helpVP = m.buildHelp()
	if !atTop {
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
