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
	art     ghapi.Artifact
	asset   ghapi.ReleaseAsset
	release bool
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
			p.choices = append(p.choices, planChoice{asset: ra.Asset, release: true})
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
func (p *installPlan) lines(m *Model) ([]string, []int, []int) {
	w := max(10, m.modalWindowWidth())
	lines := []string{}
	add := func(s string) { lines = append(lines, components.Clamp(safeJobText(s), w)) }
	add("Repo: " + p.repo)
	if p.loading {
		add("Checking device and installed app…")
		add("Please wait")
	} else {
		abi := strings.Join(p.dev.ABIs, ", ")
		if abi == "" {
			abi = "unknown"
		}
		label := "Device: "
		if p.dev.OS != "android" && !p.dev.ADBConnected {
			label = "Host ABI: "
		}
		if p.dev.SDK > 0 {
			abi += fmt.Sprintf(" · SDK %d", p.dev.SDK)
		}
		add(label + abi)
		installed := "unknown (package not known)"
		if p.pkg != "" {
			installed = "unknown (device not queryable)"
			if p.app.Queryable {
				installed = "not installed"
			}
			if p.found {
				installed = fmt.Sprintf("%s v%s (%d)", p.pkg, p.app.VersionName, p.app.VersionCode)
			}
		}
		add("Installed: " + installed)
	}
	add("APK details checked after download")
	add("Artifact (↑/↓):")
	rows := max(1, min(4, m.height-13))
	if p.selected < p.offset {
		p.offset = p.selected
	}
	if p.selected >= p.offset+rows {
		p.offset = p.selected - rows + 1
	}
	if p.offset < 0 {
		p.offset = 0
	}
	var choiceRows []int
	for i := p.offset; i < len(p.choices) && i < p.offset+rows; i++ {
		c := p.choices[i]
		marker := "  "
		if i == p.selected {
			marker = "> "
		}
		label := marker
		if (!c.release && c.art.Rejected) || (c.release && c.asset.Rejected) {
			label += "(mismatch) "
		}
		label += c.name()
		choiceRows = append(choiceRows, len(lines))
		add(label)
	}
	add("")
	var buttonRows []int
	for i, b := range []string{"Install", "Download only", "Cancel"} {
		buttonRows = append(buttonRows, len(lines))
		label := "  " + b
		if i == p.button {
			label = "> " + b
		}
		if i < 2 && p.loading {
			label += " (checking)"
		}
		add(label)
	}
	return lines, choiceRows, buttonRows
}
func (p *installPlan) view(m *Model) string { l, _, _ := p.lines(m); return strings.Join(l, "\n") }
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
	x := (m.width - lipgloss.Width(box)) / 2
	y := (m.height-lipgloss.Height(box))/2 + 1
	_, choices, buttons := p.lines(&m)
	if msg.X < x || msg.X >= x+lipgloss.Width(box) {
		return m, nil
	}
	for i, row := range choices {
		if msg.Y == y+row {
			p.selected = p.offset + i
			p.touchActive = true
			p.touchLastY = msg.Y
			return m, nil
		}
	}
	for i, row := range buttons {
		if msg.Y == y+row {
			p.button = i
			return m, m.acceptPlan()
		}
	}
	return m, nil
}
