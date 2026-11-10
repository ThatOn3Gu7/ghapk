package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ThatOn3Gu7/ghapk/internal/config"
	"github.com/ThatOn3Gu7/ghapk/internal/device"
	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
	"github.com/ThatOn3Gu7/ghapk/internal/version"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	// What changed since the last install ("N commits since your last
	// install"), and a newer run that is still building. Both are courtesies
	// computed beside the device check; the plan works without them.
	changes      []ghapi.Commit
	changesAhead int
	lastInstall  time.Time
	building     string
}
type planReadyMsg struct {
	seq     int
	dev     device.Info
	pkg     string
	app     device.InstalledApp
	found   bool
	changes []ghapi.Commit
	ahead   int
	since   time.Time
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
	if !m.usingReleaseAssets() {
		p.building = m.newerActiveRun(p.run)
	}
	m.planSeq++
	p.seq = m.planSeq
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	m.plan = p
	m.modal = ModalPlan
	settings, seq := p.settings, p.seq
	headSHA, isRelease := p.run.HeadSHA, m.usingReleaseAssets()
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
		msg := planReadyMsg{seq: seq, dev: dev, pkg: pkg, app: app, found: found}
		if h, ok := config.History(settings.Repo); ok && !isRelease && h.SHA != "" && headSHA != "" && h.SHA != headSHA {
			if ahead, commits, err := ghapi.CompareCommits(ctx, settings.Repo, h.SHA, headSHA); err == nil && len(commits) > 0 {
				msg.changes, msg.ahead = commits, max(ahead, len(commits))
				if h.When > 0 {
					msg.since = time.Unix(h.When, 0)
				}
			}
		}
		return msg
	}
}

// newerActiveRun names a run that started after run and is still building, so
// the plan can warn that a fresher APK may be minutes away.
func (m *Model) newerActiveRun(run ghapi.Run) string {
	var best ghapi.Run
	for _, r := range m.allRuns {
		if !r.Active() || !r.CreatedAt.After(run.CreatedAt) {
			continue
		}
		if best.ID == 0 || r.CreatedAt.After(best.CreatedAt) {
			best = r
		}
	}
	if best.ID == 0 {
		return ""
	}
	if best.RunNumber > 0 {
		return fmt.Sprintf("#%d", best.RunNumber)
	}
	return fmt.Sprintf("#%d", best.ID)
}

// agoText is "3h ago" for recent times and a plain date for old ones.
func agoText(t time.Time) string {
	r := reltime(t)
	switch {
	case r == "":
		return ""
	case r == "now":
		return "just now"
	case strings.Contains(r, "-"):
		return r
	}
	return r + " ago"
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
		meta := safeJobText(p.repo)
		if !choice.release {
			if when := agoText(p.run.CreatedAt); when != "" {
				who := ""
				if p.run.Actor.Login != "" {
					who = " · " + safeJobText(p.run.Actor.Login)
				}
				meta = when + who + " · " + meta
			}
		}
		add(st.Dim.Render(components.Clamp("          "+meta, w)))
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
	add(p.versionLine(m, label, choice))
	if note := p.versionNote(m, choice); note != "" {
		add(note)
	}
	if p.building != "" {
		add("          " + st.Warn.Render(st.G.Running+" "+safeJobText(p.building)+" is still building, so a newer APK may follow"))
	}
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
	if sec := p.commitLines(m, w, len(out.lines), buttonLines); len(sec) > 0 {
		add("")
		for _, ln := range sec {
			add(ln)
		}
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

// installedVersionText is what the device reports, "" when unknown.
func (p *installPlan) installedVersionText() string {
	if !p.found {
		return ""
	}
	if p.app.VersionName != "" {
		return p.app.VersionName
	}
	if p.app.VersionCode > 0 {
		return fmt.Sprint(p.app.VersionCode)
	}
	return ""
}

// versionLine shows the version in the artifact's name next to what is
// installed, e.g. "1.4.2  →  installed 1.4.1".
func (p *installPlan) versionLine(m *Model, label func(string) string, c planChoice) string {
	st := m.downloadStyles()
	ver := version.Extract(c.name())
	if ver == "" {
		return label("Version") + st.Dim.Render("unknown until downloaded")
	}
	line := label("Version") + st.Text.Render(ver)
	if in := p.installedVersionText(); in != "" {
		line += st.Dim.Render("  " + st.G.Arrow + "  installed " + safeJobText(in))
	}
	return line
}

// versionNote says whether installing moves forward, back or nowhere. It only
// speaks when both versions are known and comparable.
func (p *installPlan) versionNote(m *Model, c planChoice) string {
	st := m.downloadStyles()
	ver := version.Extract(c.name())
	in := version.Extract(p.installedVersionText())
	if p.loading || ver == "" || in == "" {
		return ""
	}
	pad := "          "
	switch version.Compare(ver, in) {
	case -1:
		return pad + st.Err.Render(st.G.Fail+" older than what is installed")
	case 0:
		return pad + st.Warn.Render(st.G.Dot+" same version as installed")
	}
	return pad + st.OK.Render(st.G.OK+" newer than installed")
}

// commitLines is the "since your last install" section, trimmed to the room
// the dialog has left so the buttons never fall off a short screen.
func (p *installPlan) commitLines(m *Model, w, used, buttonLines int) []string {
	if len(p.changes) == 0 || p.loading {
		return nil
	}
	st := m.downloadStyles()
	room := m.height - 4 - used - buttonLines - 3 // heading, spacer before and after
	if room < 2 {
		return nil
	}
	count := p.changesAhead
	if count < len(p.changes) {
		count = len(p.changes)
	}
	noun := "commits"
	if count == 1 {
		noun = "commit"
	}
	head := fmt.Sprintf("Since your last install  %s  %d %s", st.G.Dot, count, noun)
	if !p.lastInstall.IsZero() {
		head += " · " + agoText(p.lastInstall)
	}
	lines := []string{st.PanelTitleFoc.Render(components.Clamp(head, w))}
	show := p.changes
	// Newest first: the commit people care about is the latest one.
	rev := make([]ghapi.Commit, len(show))
	for i, c := range show {
		rev[len(show)-1-i] = c
	}
	limit := min(len(rev), min(6, room))
	more := len(rev) - limit
	if more > 0 && limit > 1 {
		limit--
		more++
	}
	for _, c := range rev[:limit] {
		subject := components.Clamp(safeJobText(c.Subject()), max(1, w-9))
		lines = append(lines, st.Dim.Render(c.Short()+" ")+st.Text.Render(subject))
	}
	if more > 0 {
		lines = append(lines, st.Dim.Render(fmt.Sprintf("%s…and %d more", "        ", more)))
	}
	return lines
}

// installNewest is the one-key path: pick the newest successful build, load its
// artifacts if needed, and open the same install preview as everywhere else.
// Nothing is installed without the preview's confirmation.
func (m *Model) installNewest() tea.Cmd {
	if m.settings.Repo == "" {
		m.toastf("pick a repository first")
		return nil
	}
	m.pendingPlanRun = 0
	if m.settings.Source == "releases" {
		m.focus = PanelRuns
		return m.beginPlan()
	}
	// In auto mode the cursor may sit on a run without artifacts, which makes
	// the screen show release assets. "Newest build" still means Actions first.
	for _, r := range m.allRuns {
		if !r.Finished() || r.Conclusion != "success" {
			continue
		}
		// Once the artifact index is known, skip good builds that shipped nothing.
		if m.artIndex != nil && m.artIndex[r.ID] == 0 && len(m.runCache[r.ID]) == 0 {
			continue
		}
		m.focus = PanelRuns
		// The newest good build may be hidden by a status filter; show it.
		if m.statusFilter != "" && !runMatchesStatus(r, m.statusFilter) {
			m.statusFilter = ""
			m.setRuns(m.allRuns)
		}
		m.selectedRunID, m.selectedArtID = r.ID, 0
		m.runs.SelectByID(runRowByID(m.runs.Items, r.ID))
		if len(m.runCache[r.ID]) > 0 {
			return m.beginPlan()
		}
		m.pendingPlanRun = r.ID
		m.fetching[r.ID] = true
		m.toastf("finding the newest build…")
		return fetchArtifactsCmd(m.settings.Repo, r.ID)
	}
	if m.usingReleaseAssets() {
		m.focus = PanelRuns
		return m.beginPlan() // no Actions build to offer: fall back to the release
	}
	if len(m.allRuns) == 0 && !m.runsLoaded {
		m.toastf("builds are still loading")
	} else {
		m.toastf("no successful build to install")
	}
	return nil
}

// toggleArmedRun marks the selected running build to be installed the moment it
// succeeds, or cancels that wait. The refresh tick polls while armed.
func (m *Model) toggleArmedRun() tea.Cmd {
	run, ok := m.selectedRun()
	if !ok {
		return nil
	}
	if m.armedRun == run.ID {
		m.armedRun = 0
		m.toastf("no longer waiting for #%d", run.RunNumber)
		return nil
	}
	if !run.Active() {
		m.toastf("that build is not running — use %s to install it", m.keyName("dashboard", "i"))
		return nil
	}
	m.armedRun = run.ID
	m.toastf("waiting for #%d — the install preview opens when it succeeds", run.RunNumber)
	m.refreshSeq++
	return refreshTickCmd(m.effectiveRefreshSecs(), m.refreshSeq)
}

// checkArmedRun runs after each runs refresh. Success opens the preview (the
// same one as everywhere, so nothing installs unconfirmed); a failed or
// cancelled run ends the wait and says so.
func (m *Model) checkArmedRun() tea.Cmd {
	if m.armedRun == 0 {
		return nil
	}
	for _, r := range m.allRuns {
		if r.ID != m.armedRun {
			continue
		}
		switch {
		case r.Active():
			return nil
		case r.Conclusion == "success":
			m.armedRun = 0
			if m.modal != ModalNone {
				m.toastf("#%d finished — press %s to install it", r.RunNumber, m.keyName("dashboard", "I"))
				return nil
			}
			m.statusFilter = ""
			m.setRuns(m.allRuns)
			m.selectedRunID, m.selectedArtID, m.focus = r.ID, 0, PanelRuns
			m.runs.SelectByID(runRowByID(m.runs.Items, r.ID))
			m.pendingPlanRun = r.ID
			m.fetching[r.ID] = true
			m.toastf("#%d finished", r.RunNumber)
			return fetchArtifactsCmd(m.settings.Repo, r.ID)
		default:
			m.armedRun = 0
			m.toastErrf("#%d ended as %s — nothing to install", r.RunNumber, strings.ReplaceAll(r.Conclusion, "_", " "))
			return nil
		}
	}
	m.armedRun = 0 // the run left the list
	return nil
}
