package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/apk"
	"github.com/ghapk/ghapk/internal/tui/components"
	"github.com/ghapk/ghapk/internal/tui/theme"
)

// Match Claude's default cyan/amber/green visual language on these screens,
// while preserving the user's explicitly selected alternative themes.
func (m Model) downloadStyles() theme.Styles {
	st := m.st
	if m.settings.Theme == "action" || m.settings.Theme == "" {
		p := st.P
		p.Accent = "#56CCFF"
		p.BorderFocus = p.Accent
		p.OnAccent = "#0C1018"
		p.Success = "#78E696"
		p.Warning = "#FFBE5A"
		p.Error = "#FF7882"
		p.Muted = "#828C9B"
		p.Dim = p.Muted
		p.Text = "#E1E8F0"
		p.Border = "#485669"
		p.Selection = "#3A4454"
		st = theme.New(p)
	}
	st.BadgeStyle = st.Dim.Padding(0, 1)
	return st
}

func (m Model) jobGlyph(j *downloadJob, st theme.Styles) (string, lipgloss.Style) {
	switch j.status {
	case "running", "cancelling":
		spins := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		if st.G.OK == "+" {
			spins = []string{"|", "/", "-", "\\"}
		}
		return spins[m.shimmerPos%len(spins)], st.Warn
	case "asking":
		return "?", st.Warn.Bold(true)
	case "done", "installed":
		return st.G.OK, st.OK
	case "launched":
		return "!", st.Warn
	case "cancelled":
		if st.G.OK == "+" {
			return "-", st.Dim
		}
		return "–", st.Dim
	default:
		return st.G.Fail, st.Err
	}
}
func (m Model) renderDownloads() string {
	r := m.downloadRects()
	if r.jobs.w == 0 {
		return m.downloadDetailPanel(r.detail)
	}
	st := m.downloadStyles()
	jobs := m.jobList
	jobs.Focused = m.downloadFocus == 0
	jobs.MaxShown = max(1, r.jobs.h-2)
	jobs.Items = append([]components.Item(nil), jobs.Items...)
	for i := range jobs.Items {
		id, _ := jobs.Items[i].ID.(int)
		j := m.jobByID(id)
		if j == nil {
			continue
		}
		g, style := m.jobGlyph(j, st)
		name := j.spec.choice.name()
		if j.spec.savedPath != "" {
			name = filepath.Base(j.spec.savedPath)
		}
		jobs.Items[i].Rendered = style.Render(g) + " " + st.Text.Render(safeJobText(name))
		right := "…"
		switch j.status {
		case "running":
			if j.phase == "Download" && j.total > 0 {
				right = fmt.Sprintf("%d%%", min(int64(100), j.got*100/j.total))
			}
		case "asking":
			right = "asks"
		case "cancelling":
			right = "…"
		default:
			right = reltime(j.started)
		}
		jobs.Items[i].Detail = right
		reserve := components.VisibleWidth(right) + 2
		if len(jobs.Items) > jobs.MaxShown {
			reserve++
		}
		jobs.Items[i].Rendered = components.Clamp(jobs.Items[i].Rendered, max(1, r.jobs.w-4-reserve))
	}
	jobs.Empty = "  Nothing yet.\n  Pick a build to get started."
	saved := m.savedList
	saved.Focused = m.downloadFocus == 1
	saved.MaxShown = max(1, r.saved.h-2)
	saved.Empty = "  None kept yet."
	saved.Items = append([]components.Item(nil), saved.Items...)
	for i := range saved.Items {
		right := saved.Items[i].Detail
		available := r.saved.w - 4 - components.VisibleWidth(right) - 2
		if len(saved.Items) > saved.MaxShown {
			available--
		}
		available = max(1, available)
		saved.Items[i].Rendered = components.Clamp("  "+saved.Items[i].Label, available)
	}
	var total int64
	for _, f := range m.savedFiles {
		total += f.size
	}
	note := ""
	if len(m.savedFiles) > 0 {
		note = humanSize(total)
	}
	jobContent := renderDownloadList(jobs, st, r.jobs.w-4)
	savedContent := renderDownloadList(saved, st, r.saved.w-4)
	left := lipgloss.JoinVertical(lipgloss.Left, components.Panel{Title: "Jobs", Badge: fmt.Sprint(len(m.jobs)), Focused: jobs.Focused, Width: r.jobs.w, Height: r.jobs.h, Content: jobContent}.Render(st), components.Panel{Title: "Saved APKs", Badge: note, Focused: saved.Focused, Width: r.saved.w, Height: r.saved.h, Content: savedContent}.Render(st))
	if r.detail.w == 0 {
		return left
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, m.downloadDetailPanel(r.detail))
}

// Reserve a column for the scrollbar only when a list actually overflows.
func renderDownloadList(list components.List, st theme.Styles, width int) string {
	count := len(list.Filtered())
	if count <= list.MaxShown || width < 3 {
		return list.Render(st, width)
	}
	lines := strings.Split(list.Render(st, width-1), "\n")
	height := list.MaxShown
	thumbSize := max(1, height*height/count)
	thumbStart := list.Offset * height / count
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i := range lines {
		indicator := "│"
		sty := st.Dim
		if i >= thumbStart && i < thumbStart+thumbSize {
			indicator = "┃"
			sty = st.Accent
		}
		if st.G.OK == "+" {
			indicator = "|"
			if i >= thumbStart && i < thumbStart+thumbSize {
				indicator = "#"
			}
		}
		lines[i] = components.Fit(lines[i], width-1) + sty.Render(indicator)
	}
	return strings.Join(lines, "\n")
}

// Progress is genuinely indeterminate when GitHub didn't provide a total.
func (m Model) downloadBar(j *downloadJob, width int, st theme.Styles) string {
	width = max(1, min(width, 50))
	full, empty := "█", "░"
	partials := []string{"▏", "▎", "▍", "▌", "▋", "▊", "▉"}
	if st.G.OK == "+" || os.Getenv("GHAPK_ASCII") == "1" {
		full, empty = "#", "-"
		partials = nil
	}
	fill := lipgloss.NewStyle().Foreground(st.P.Accent)
	if m.settings.Theme == "action" || m.settings.Theme == "" {
		fill = lipgloss.NewStyle().Foreground(lipgloss.Color("#E1F5FF"))
	}
	track := lipgloss.NewStyle().Foreground(st.P.Border)
	if j.total <= 0 {
		seg := max(2, width/4)
		pos := (m.shimmerPos/2 + seg) % (width + seg)
		var b strings.Builder
		for i := 0; i < width; i++ {
			if i >= pos-seg && i < pos {
				b.WriteString(fill.Render(full))
			} else {
				b.WriteString(track.Render(empty))
			}
		}
		return b.String()
	}
	cells := float64(j.got) / float64(j.total) * float64(width)
	if cells < 0 {
		cells = 0
	}
	if cells > float64(width) {
		cells = float64(width)
	}
	n := int(cells)
	var b strings.Builder
	b.WriteString(fill.Render(strings.Repeat(full, n)))
	if n < width && len(partials) > 0 {
		e := int((cells - float64(n)) * 8)
		if e > 0 {
			b.WriteString(fill.Render(partials[e-1]))
			n++
		}
	}
	b.WriteString(track.Render(strings.Repeat(empty, width-n)))
	return b.String()
}
func (m Model) progressStats(j *downloadJob) (string, string) {
	bytes := humanSize(j.got) + " / unknown"
	if j.total > 0 {
		bytes = fmt.Sprintf("%d%%  %s / %s", min(int64(100), j.got*100/j.total), humanSize(j.got), humanSize(j.total))
	}
	rate := j.rate
	if rate <= 0 {
		seconds := time.Since(j.started).Seconds()
		if seconds > 0 {
			rate = float64(j.got) / seconds
		}
	}
	speed := ""
	if rate > 0 {
		speed = humanSize(int64(rate)) + "/s"
		if j.total > j.got {
			speed += "  eta " + (time.Duration(float64(j.total-j.got)/rate) * time.Second).Round(time.Second).String()
		}
	}
	return bytes, speed
}

type detailRow struct {
	text     string
	style    lipgloss.Style
	rendered bool
}

func (m Model) downloadDetailPanel(r rect) string {
	st := m.downloadStyles()
	j := m.selectedJob()
	title := "Job"
	badge := ""
	w := max(1, r.w-4)
	var lines []detailRow
	add := func(style lipgloss.Style, text string) {
		text = safeJobText(text)
		if text == "" {
			lines = append(lines, detailRow{})
			return
		}
		var part strings.Builder
		cols := 0
		for _, ch := range text {
			rw := components.VisibleWidth(string(ch))
			if cols+rw > w && part.Len() > 0 {
				lines = append(lines, detailRow{text: part.String(), style: style})
				part.Reset()
				cols = 0
			}
			part.WriteRune(ch)
			cols += rw
		}
		lines = append(lines, detailRow{text: part.String(), style: style})
	}
	if j == nil {
		add(st.Dim, "Downloads and installs show up here, step by step.")
	} else {
		title = safeJobText(j.spec.choice.name())
		if j.spec.savedPath != "" {
			title = safeJobText(filepath.Base(j.spec.savedPath))
		}
		if title == "" {
			title = "Job"
		}
		badge = j.status
		size := j.spec.choice.art.SizeInBytes
		kind := "actions"
		if j.spec.choice.release {
			size = j.spec.choice.asset.Size
			kind = "release"
		}
		if j.spec.savedPath != "" {
			kind = "saved APK"
		}
		if size > 0 {
			add(st.Dim, humanSize(size)+"  "+kind)
			add(st.Dim, "")
		}
		phases := []string{"Download", "Checksum", "Unpack", "Validate APK", "Save", "Installer", "Confirm on device"}
		current := -1
		for i, p := range phases {
			if p == j.phase {
				current = i
			}
		}
		for i, p := range phases {
			label := p
			if p == "Checksum" {
				label = "Verify checksum"
			}
			if p == "Installer" {
				label = "Hand to installer"
			}
			mark, style := "○", st.Dim
			if st.G.OK == "+" {
				mark = "o"
			}
			done := i < current || (j.status == "done" && i <= 4) || j.status == "installed"
			if done {
				mark, style = st.G.OK, st.OK
			}
			if i == current && (j.status == "running" || j.status == "cancelling") {
				mark, style = m.jobGlyph(j, st)
			}
			if i == current && j.status == "asking" {
				mark, style = "?", st.Warn
			}
			if i == current && j.status == "launched" {
				mark, style = "!", st.Warn
			}
			if i == current && j.status == "failed" {
				mark, style = st.G.Fail, st.Err
			}
			if i == current && j.status == "cancelled" {
				mark, style = "–", st.Dim
				if st.G.OK == "+" {
					mark = "-"
				}
			}
			skipped := (!j.spec.install && i > 4) || (j.spec.savedPath != "" && (i == 0 || i == 1 || i == 2 || i == 4)) || (j.spec.choice.release && !strings.HasSuffix(strings.ToLower(j.spec.choice.asset.Name), ".zip") && i == 2)
			if skipped {
				mark, style = "–", st.Dim
				if st.G.OK == "+" {
					mark = "-"
				}
			}
			if i == 1 && j.meta.DigestStatus == apk.DigestUnverified {
				mark, style = "–", st.Dim
				if st.G.OK == "+" {
					mark = "-"
				}
				label += "  unverified"
			}
			add(style, mark+" "+label)
			if i == 0 && j.phase == "Download" && (j.status == "running" || j.status == "cancelling") {
				lines = append(lines, detailRow{text: "  " + m.downloadBar(j, max(1, w-4), st), rendered: true})
				bytes, speed := m.progressStats(j)
				if speed != "" && components.VisibleWidth(bytes+"  "+speed) <= w-2 {
					add(st.Dim, "  "+bytes+"  "+speed)
				} else {
					add(st.Dim, "  "+bytes)
					if speed != "" {
						add(st.Dim, "  "+speed)
					}
				}
			}
		}
		add(st.Dim, "")
		if j.path != "" {
			add(st.Text, "Saved     "+j.path)
		}
		if j.meta.HaveInfo {
			add(st.Text, "App       "+j.meta.Info.Package)
			add(st.Text, fmt.Sprintf("Version   %s (code %d)", j.meta.Info.VersionName, j.meta.Info.VersionCode))
		}
		if j.meta.LocalSHA256 != "" {
			if j.meta.DigestStatus == apk.DigestVerified {
				add(st.OK, "Checksum  "+st.G.OK+" matches GitHub SHA-256")
			} else {
				add(st.Warn, "Checksum  unverified")
			}
			add(st.Dim, "SHA-256   "+j.meta.LocalSHA256)
		}
		if j.meta.Cert.SHA256 != "" {
			add(st.Dim, "Signing key ("+j.meta.Cert.Scheme+")  "+j.meta.Cert.SHA256)
		}
		if j.meta.InfoErr != "" {
			add(st.Warn, "Manifest unavailable: "+j.meta.InfoErr)
		}
		switch j.status {
		case "installed":
			add(st.OK, "Phone     "+st.G.OK+" installation confirmed")
		case "launched":
			add(st.Warn, "Phone     installer launched · unconfirmed")
		case "cancelled":
			add(st.Dim, "Cancelled.")
		case "failed":
			add(st.Err, "Failed.")
		}
		if len(j.logs) > 0 {
			add(st.Dim, "")
			for _, log := range j.logs {
				add(st.Dim, log)
			}
		}
	}
	maxScroll := max(0, len(lines)-(r.h-2))
	off := min(max(0, m.downloadScroll), maxScroll)
	end := min(len(lines), off+max(0, r.h-2))
	visible := make([]string, 0, end-off)
	for _, line := range lines[off:end] {
		if line.rendered {
			visible = append(visible, line.text)
		} else {
			visible = append(visible, line.style.Render(line.text))
		}
	}
	if maxScroll > 0 && off > 0 {
		badge += fmt.Sprintf("  %d/%d", off+1, len(lines))
	}
	return components.Panel{Title: title, Badge: badge, Width: r.w, Height: r.h, Content: strings.Join(visible, "\n"), ScrollY: maxScroll - off}.Render(st)
}
