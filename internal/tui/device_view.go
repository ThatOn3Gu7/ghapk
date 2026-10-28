package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/tui/components"
)

// deviceLines keeps the original hardware/session details and adds the
// reference's setup, GitHub, app and file checks. Values wrap on phones.
func (m Model) deviceLines(w int) []string {
	st, d, c := m.st, m.dev, m.deviceChecks
	width := max(16, min(68, w-6))
	const labelWidth = 14
	var lines []string
	section := func(s string) {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, st.AccentAlt.Render(s))
	}
	row := func(k, v string, style lipgloss.Style) {
		if strings.TrimSpace(v) == "" {
			return
		}
		v = safeJobText(v)
		parts := strings.Split(ansi.Wrap(v, max(1, width-labelWidth), ""), "\n")
		for i, p := range parts {
			label := strings.Repeat(" ", labelWidth)
			if i == 0 {
				label = components.Pad(k, labelWidth)
			}
			lines = append(lines, st.Dim.Render(label)+style.Render(p))
		}
	}
	section("Device")
	name := d.Model
	if d.Manufacturer != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(d.Manufacturer)) {
		name = strings.TrimSpace(d.Manufacturer + " " + name)
	}
	if name != "" {
		row("model", name, st.Bold)
	}
	system := d.Release
	if system == "" {
		system = d.OS
	}
	if d.SDK > 0 {
		system += fmt.Sprintf(" (API %d)", d.SDK)
	}
	row("system", system, st.Text)
	row("kernel", d.Kernel, st.Text)
	row("uptime", d.Uptime, st.Text)
	cpu := d.CPUModel
	if d.Cores > 0 {
		if cpu != "" {
			cpu += fmt.Sprintf(" (%d cores)", d.Cores)
		} else {
			cpu = fmt.Sprintf("%d cores", d.Cores)
		}
	}
	row("cpu", cpu, st.Text)
	if d.MemTotalMB > 0 {
		row("ram", fmt.Sprintf("%.1f GB (%.1f GB used)", float64(d.MemTotalMB)/1024, float64(d.MemTotalMB-d.MemFreeMB)/1024), st.Text)
	}
	if d.DiskTotalGB > 0 {
		row("storage", fmt.Sprintf("%.0f GB (%.0f GB free)", d.DiskTotalGB, d.DiskFreeGB), st.Text)
	}
	if len(d.ABIs) > 0 {
		label := "abis"
		if d.OS != "android" && !d.ADBConnected {
			label = "host abis"
		}
		row(label, strings.Join(d.ABIs, ", "), st.Text)
	}

	section("Session")
	row("terminal", d.Terminal, st.Text)
	row("shell", d.Shell, st.Text)
	row("theme", m.settings.Theme, st.Text)
	section("GitHub")
	gh := "not found"
	ghStyle := st.Warn
	if p, err := exec.LookPath("gh"); err == nil {
		gh = p
		ghStyle = st.Text
		if m.ghVersion != "" {
			gh += " (" + m.ghVersion + ")"
		}
	}
	row("gh CLI", gh, ghStyle)
	if m.authUser != "" {
		row("signed in", "@"+m.authUser, st.OK)
	} else if m.authErr != nil {
		row("signed in", "not authenticated", st.Warn)
	} else {
		row("signed in", "unknown", st.Dim)
	}
	if m.deviceChecking {
		row("rate limit", "checking…", st.Dim)
	} else if c.rateOK {
		remaining := time.Until(c.rate.Reset).Round(time.Minute)
		if remaining < 0 {
			remaining = 0
		}
		sty := st.Text
		if c.rate.Remaining < c.rate.Limit/10 {
			sty = st.Warn
		}
		row("rate limit", fmt.Sprintf("%d / %d left; resets in %s", c.rate.Remaining, c.rate.Limit, remaining), sty)
	} else {
		row("rate limit", "unknown", st.Dim)
	}

	section("Install")
	route := "none — download or transfer APK"
	routeStyle := st.Warn
	switch d.InstallVia {
	case "termux-open":
		route = "termux-open (installer prompt)"
		routeStyle = st.Text
	case "am":
		route = "am (installer prompt)"
		routeStyle = st.Text
	case "adb":
		if d.ADBConnected {
			route = "adb (connected)"
			routeStyle = st.Text
		} else {
			route = "adb (no device connected)"
		}
	}
	row("method", route, routeStyle)
	if m.deviceChecking {
		row("tools", "checking…", st.Dim)
		row("pkg manager", "checking…", st.Dim)
	} else if !c.setupOK {
		row("tools", "unknown", st.Dim)
		row("pkg manager", "unknown", st.Dim)
		row("Shizuku", "unknown", st.Dim)
		row("notifications", "unknown", st.Dim)
	} else {
		tools := strings.Join(c.setup.Installers, ", ")
		if tools == "" {
			tools = "none found"
		}
		row("tools", tools, st.Text)
		if c.setup.PackageManager != "" {
			row("pkg manager", "reachable ("+c.setup.PackageManager+")", st.OK)
		} else {
			row("pkg manager", "not reachable; installed version may be unknown", st.Warn)
		}
		rish := "not found (optional)"
		rishStyle := st.Dim
		if c.setup.RishPresent {
			rish = "found, not responding"
			rishStyle = st.Warn
			if d.HasRish {
				rish = "ready"
				rishStyle = st.OK
			}
		}
		row("Shizuku", rish, rishStyle)
		notice := "termux-api missing (optional)"
		noticeStyle := st.Dim
		if c.setup.Notifications {
			notice = "termux-api available"
			noticeStyle = st.OK
		}
		row("notifications", notice, noticeStyle)
	}
	section("App")
	if m.instPkg == "" {
		row("package", "not known yet; set in settings", st.Dim)
	} else {
		row("package", m.instPkg, st.Bold)
		if m.instFound && m.instApp.Package == m.instPkg {
			ver := m.instApp.VersionName
			if ver == "" {
				ver = "unknown version"
			}
			row("installed", fmt.Sprintf("%s (code %d)", ver, m.instApp.VersionCode), st.OK)
			if m.instApp.CertSHA256 != "" {
				row("signing key", m.instApp.CertSHA256, st.Text)
			}
		} else if m.instApp.Package == m.instPkg && m.instApp.Queryable {
			row("installed", "not installed", st.Text)
		} else {
			row("installed", "unknown (package manager not reachable)", st.Warn)
		}
	}
	if m.deviceHistoryLoaded && m.deviceHistoryOK && m.deviceHistoryRepo == m.settings.Repo && m.deviceHistory.Installs > 0 {
		history := m.deviceHistory
		when := "unknown time"
		if history.When > 0 {
			when = time.Unix(history.When, 0).Format("Jan 02 15:04")
		}
		run := ""
		if history.Run > 0 {
			run = fmt.Sprintf("run %d · ", history.Run)
		}
		row("last handoff", fmt.Sprintf("%s%s · %d total", run, when, history.Installs), st.Text)
	}
	section("Files")
	row("config", config.Dir(), st.Text)
	cache := config.CacheDir()
	if c.cacheOK {
		cache += " (" + humanSize(c.cacheBytes) + ")"
	} else if !m.deviceChecking {
		cache += " (size unknown)"
	}
	row("cache", cache, st.Text)
	if c.freeOK {
		sty := st.Text
		if c.freeBytes < 300<<20 {
			sty = st.Warn
		}
		row("free space", humanSize(c.freeBytes), sty)
	} else if m.deviceChecking {
		row("free space", "checking…", st.Dim)
	} else {
		row("free space", "unknown", st.Dim)
	}
	row("log", "session only (not saved to a file)", st.Dim)
	return lines
}

// deviceContent centres the information block; when it overflows, only the
// visible slice is shown, with a scrollbar and touch/keyboard scrolling.
func (m Model) deviceContent(w, h int, _ *Model) string {
	if m.dev.OS == "" {
		return centerEmpty(m.spin.View()+m.st.Dim.Render(" probing device…"), w, h)
	}
	lines := m.deviceLines(w)
	ih := max(1, h-2)
	iw := max(1, w-4)
	if len(lines) <= ih {
		return strings.Join(lines, "\n")
	}
	off := min(max(0, m.deviceScroll), len(lines)-ih)
	blockW := 0
	for _, line := range lines {
		blockW = max(blockW, components.VisibleWidth(line))
	}
	blockW = min(blockW, iw-1)
	left := max(0, (iw-1-blockW)/2)
	thumbSize := max(1, ih*ih/len(lines))
	thumbStart := off * (ih - thumbSize) / (len(lines) - ih)
	visible := make([]string, ih)
	for i := range visible {
		bar := "│"
		style := m.st.Dim
		if i >= thumbStart && i < thumbStart+thumbSize {
			bar = "┃"
			style = m.st.Accent
		}
		if m.st.G.OK == "+" {
			bar = "|"
			if i >= thumbStart && i < thumbStart+thumbSize {
				bar = "#"
			}
		}
		visible[i] = components.Fit(strings.Repeat(" ", left)+lines[off+i], iw-1) + style.Render(bar)
	}
	return strings.Join(visible, "\n")
}
func (m *Model) scrollDevice(delta int) {
	maxOffset := max(0, len(m.deviceLines(m.width))-(m.height-5))
	m.deviceScroll = min(max(0, m.deviceScroll+delta), maxOffset)
}
