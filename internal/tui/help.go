package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
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
	row("1 … 5", "jump straight to a panel")
	row("j / k · ↑ / ↓", "move in a list")
	row("ctrl+d / ctrl+u", "page a list")
	row("g / G", "first / last row")
	row("enter", "activate: switch repo, filter by workflow, focus runs…")
	row("mouse", "click focuses & selects, click again opens, wheel scrolls")

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

	vp := viewport.New(60, 20)
	vp.SetContent(b.String())
	return vp
}
