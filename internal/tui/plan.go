package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
)

type planChoice struct {
	art                     ghapi.Artifact
	asset                   ghapi.ReleaseAsset
	release                 bool
	releaseTag, releaseName string
}

func (c planChoice) name() string {
	if c.release {
		return c.asset.Name
	}
	return c.art.Name
}

type installPlan struct {
	seq                      int
	repo                     string
	settings                 config.Settings
	run                      ghapi.Run
	choices                  []planChoice
	selected, offset, button int
	loading                  bool
	cancel                   context.CancelFunc
	touchActive              bool
	touchLastY               int
	dev                      device.Info
	pkg                      string
	app                      device.InstalledApp
	found                    bool
}
type planReadyMsg struct {
	seq   int
	dev   device.Info
	pkg   string
	app   device.InstalledApp
	found bool
}

func (m *Model) beginPlan() tea.Cmd {
	p := &installPlan{repo: m.settings.Repo, settings: m.settings, button: 0, loading: true}
	if m.usingReleaseAssets() {
		for _, ra := range m.rankedReleases() {
			p.choices = append(p.choices, planChoice{asset: ra.Asset, release: true, releaseTag: ra.ReleaseTag, releaseName: ra.ReleaseName})
			if -ra.Asset.ID == m.selectedArtID {
				p.selected = len(p.choices) - 1
			}
		}
	} else if run, ok := m.selectedRun(); ok {
		p.run = run
		for _, a := range m.runCache[run.ID] {
			p.choices = append(p.choices, planChoice{art: a})
			if a.ID == m.selectedArtID {
				p.selected = len(p.choices) - 1
			}
		}
	}
	if len(p.choices) == 0 {
		m.toastf("select an available artifact in Builds")
		return nil
	}
	m.planSeq++
	p.seq = m.planSeq
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	m.plan = p
	m.modal = ModalPlan
	settings, seq := p.settings, p.seq
	return func() tea.Msg {
		dev := device.ProbeContext(ctx)
		pkg := settings.PackageName
		if pkg == "" {
			if h, ok := config.History(settings.Repo); ok {
				pkg = h.Pkg
			}
		}
		var app device.InstalledApp
		var found bool
		if pkg != "" {
			app, found = device.QueryPackageContext(ctx, pkg)
		}
		return planReadyMsg{seq, dev, pkg, app, found}
	}
}

type planLayout struct {
	lines            []string
	choices, buttons []rect
}

func (m Model) planWidth() int { return min(78, m.width-2) }

// planLayout keeps rows and their touch targets together, including when the
// action strip stacks on a narrow phone.
func (p *installPlan) layout(m *Model) planLayout {
	st := m.downloadStyles()
	w := max(1, m.planWidth()-4)
	var out planLayout
	add := func(line string) { out.lines = append(out.lines, components.Clamp(line, w)) }
	label := func(s string) string { return st.Dim.Render(fmt.Sprintf("%-9s ", s)) }
	choice := p.choices[p.selected]
	if choice.release {
		name := choice.releaseTag
		if name == "" {
			name = "release"
		}
		if choice.releaseName != "" && choice.releaseName != name {
			name += " · " + choice.releaseName
		}
		add(label("Release") + st.Text.Render(safeJobText(name)))
	} else {
		build := fmt.Sprintf("#%d", p.run.RunNumber)
		if p.run.RunNumber <= 0 {
			build = fmt.Sprintf("#%d", p.run.ID)
		}
		description := ""
		if p.run.HeadBranch != "" {
			description += " · " + p.run.HeadBranch
		}
		if p.run.HeadSHA != "" {
			sha := p.run.HeadSHA
			if len(sha) > 7 {
				sha = sha[:7]
			}
			description += " · " + sha
		}
		add(label("Build") + st.Accent.Render(build) + st.Dim.Render(safeJobText(description)))
	}
	if m.height >= 24 {
		title := p.run.DisplayTitle
		if choice.release {
			title = choice.releaseName
		}
		if title != "" {
			add(st.Text.Render("          " + safeJobText(title)))
		}
		add(st.Dim.Render("          " + safeJobText(p.repo)))
		add("")
	}
	if p.loading {
		add(label("Device") + st.Warn.Render("Checking device…"))
		add(label("Installed") + st.Dim.Render("checking…"))
	} else {
		abi := strings.Join(p.dev.ABIs, ", ")
		if abi == "" {
			abi = "unknown"
		}
		if p.dev.SDK > 0 {
			abi += fmt.Sprintf(" · SDK %d", p.dev.SDK)
		}
		deviceLabel := "Device"
		if p.dev.OS != "android" && !p.dev.ADBConnected {
			deviceLabel = "Host ABI"
		}
		add(label(deviceLabel) + st.Text.Render(safeJobText(abi)))
		installed := "unknown (package not known)"
		if p.pkg != "" {
			installed = "unknown (not queryable)"
			if p.app.Queryable {
				installed = "not installed"
			}
			if p.found {
				installed = p.app.VersionName
				if installed == "" {
					installed = fmt.Sprint(p.app.VersionCode)
				}
				installed += " · " + p.pkg
			}
		}
		add(label("Installed") + st.Text.Render(safeJobText(installed)))
	}
	add(label("Version") + st.Dim.Render("unknown until downloaded"))
	if m.height >= 24 {
		add("")
	}
	heading := len(out.lines)
	add(st.PanelTitleFoc.Render("Artifact"))
	buttons := []string{"Install", "Download only", "Cancel"}
	horizontal := w >= 32
	buttonLines := 3
	if horizontal {
		buttonLines = 1
	}
	maxRows := max(1, min(6, m.height-4-len(out.lines)-buttonLines-1))
	if len(p.choices) > maxRows {
		out.lines[heading] = st.PanelTitleFoc.Render("Artifact") + st.Dim.Render(fmt.Sprintf("  %d/%d", p.selected+1, len(p.choices)))
	}
	if p.selected < p.offset {
		p.offset = p.selected
	}
	if p.selected >= p.offset+maxRows {
		p.offset = p.selected - maxRows + 1
	}
	p.offset = max(0, p.offset)
	for i := p.offset; i < len(p.choices) && i < p.offset+maxRows; i++ {
		c := p.choices[i]
		dot := "○"
		if i == p.selected {
			dot = "◉"
		}
		if st.G.OK == "+" {
			dot = "[ ]"
			if i == p.selected {
				dot = "[x]"
			}
		}
		name := safeJobText(c.name())
		if (!c.release && c.art.Rejected) || (c.release && c.asset.Rejected) {
			name = "(mismatch) " + name
		}
		sz := c.art.SizeInBytes
		if c.release {
			sz = c.asset.Size
		}
		size := ""
		if sz > 0 {
			size = humanSize(sz)
		}
		prefix := dot + " "
		space := w - components.VisibleWidth(prefix) - components.VisibleWidth(size)
		if size != "" {
			space--
		}
		name = components.Clamp(name, max(1, space))
		padding := max(0, w-components.VisibleWidth(prefix)-components.VisibleWidth(name)-components.VisibleWidth(size))
		sty := st.Text
		if i == p.selected {
			sty = st.Accent
		}
		row := sty.Render(prefix+name) + strings.Repeat(" ", padding) + st.Dim.Render(size)
		out.choices = append(out.choices, rect{x: 0, y: len(out.lines), w: w, h: 1})
		add(row)
	}
	add("")
	if horizontal {
		var row strings.Builder
		x := 0
		for i, b := range buttons {
			if i > 0 {
				row.WriteString("  ")
				x += 2
			}
			txt := b
			if i == p.button {
				txt = " " + b + " "
				row.WriteString(st.ButtonFocus.Padding(0, 0).Render(txt))
			} else {
				row.WriteString(st.Dim.Render(txt))
			}
			bw := components.VisibleWidth(txt)
			out.buttons = append(out.buttons, rect{x: x, y: len(out.lines), w: bw, h: 1})
			x += bw
		}
		add(row.String())
	} else {
		for i, b := range buttons {
			txt := " " + b + " "
			if i == p.button {
				add(st.ButtonFocus.Padding(0, 0).Render(txt))
			} else {
				add(st.Dim.Render(txt))
			}
			out.buttons = append(out.buttons, rect{x: 0, y: len(out.lines) - 1, w: components.VisibleWidth(txt), h: 1})
		}
	}
	return out
}
func (m Model) planBox() string {
	if m.plan == nil {
		return ""
	}
	layout := m.plan.layout(&m)
	return components.Panel{Title: "Install this build?", Focused: true, Width: m.planWidth(), Height: len(layout.lines) + 2, Content: strings.Join(layout.lines, "\n")}.Render(m.downloadStyles())
}
func (m *Model) acceptPlan() tea.Cmd {
	p := m.plan
	if p == nil {
		return nil
	}
	if p.button == 2 {
		if p.cancel != nil {
			p.cancel()
		}
		m.modal = ModalNone
		m.plan = nil
		return nil
	}
	if p.loading {
		return nil
	}
	c := p.choices[p.selected]
	if p.button == 0 && !p.dev.CanInstall {
		m.toastf("no Android installer available")
		return nil
	}
	if p.cancel != nil {
		p.cancel()
	}
	m.modal = ModalNone
	m.plan = nil
	return m.startJob(jobSpec{repo: p.repo, settings: p.settings, dev: p.dev, run: p.run, choice: c, install: p.button == 0})
}
func (m Model) handlePlanKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.plan
	if p == nil {
		m.modal = ModalNone
		return m, nil
	}
	switch msg.String() {
	case "esc", "q":
		if p.cancel != nil {
			p.cancel()
		}
		m.modal = ModalNone
		m.plan = nil
	case "up", "k":
		p.selected = max(0, p.selected-1)
	case "down", "j":
		p.selected = min(len(p.choices)-1, p.selected+1)
	case "tab", "right", "l":
		p.button = (p.button + 1) % 3
	case "shift+tab", "left", "h":
		p.button = (p.button + 2) % 3
	case "enter", " ":
		return m, m.acceptPlan()
	}
	return m, nil
}
func (m Model) handlePlanMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	p := m.plan
	if p == nil {
		return m, nil
	}
	if p.touchActive {
		if msg.Type == tea.MouseRelease || msg.Action == tea.MouseActionRelease {
			p.touchActive = false
			return m, nil
		}
		if msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft {
			delta := p.touchLastY - msg.Y
			if delta != 0 {
				p.selected = min(len(p.choices)-1, max(0, p.selected+delta))
				p.touchLastY = msg.Y
			}
			return m, nil
		}
	}
	if msg.Type == tea.MouseWheelUp {
		p.selected = max(0, p.selected-1)
		return m, nil
	}
	if msg.Type == tea.MouseWheelDown {
		p.selected = min(len(p.choices)-1, p.selected+1)
		return m, nil
	}
	if msg.Type != tea.MouseLeft || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	box := m.modalBox()
	x := (m.width-lipgloss.Width(box))/2 + 2
	y := (m.height-lipgloss.Height(box))/2 + 1
	layout := p.layout(&m)
	for i, hit := range layout.choices {
		if hit.contains(msg.X-x, msg.Y-y) {
			p.selected = p.offset + i
			p.touchActive = true
			p.touchLastY = msg.Y
			return m, nil
		}
	}
	for i, hit := range layout.buttons {
		if hit.contains(msg.X-x, msg.Y-y) {
			p.button = i
			return m, m.acceptPlan()
		}
	}
	return m, nil
}
