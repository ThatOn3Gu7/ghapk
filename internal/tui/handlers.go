package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
	"github.com/ghapk/ghapk/internal/tui/theme"
)

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// hard-global
	if key == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}

	// ── setup screen ──────────────────────────────────────────────
	if m.screen == ScreenSetup {
		switch key {
		case "q", "esc":
			m.quitting = true
			return m, tea.Quit
		case "r", "enter":
			m.busy = true
			m.busyMsg = "re-checking gh"
			return m, tea.Batch(checkAuthCmd(), m.spin.Tick)
		}
		return m, nil
	}

	// ── modals capture keys first ─────────────────────────────────
	if m.modal != ModalNone {
		return m.handleModalKey(msg)
	}

	// ── filter mode feeds the focused list ────────────────────────
	if m.filterMode {
		return m.handleFilterKey(msg)
	}

	// ── YAML preview scrolling inside workflow detail box ─────────
	if m.focus == PanelWorkflows && m.wfYAMLFocused {
		switch key {
		case "j", "down":
			m.scrollWorkflowYAML(1)
			return m, nil
		case "k", "up":
			m.scrollWorkflowYAML(-1)
			return m, nil
		case "ctrl+d", "pgdown":
			m.scrollWorkflowYAML(6)
			return m, nil
		case "ctrl+u", "pgup":
			m.scrollWorkflowYAML(-6)
			return m, nil
		case "g":
			m.wfScroll = 0
			return m, nil
		case "G":
			m.wfScroll = 999999
			m.scrollWorkflowYAML(0)
			return m, nil
		case "esc", "left", "h":
			m.wfYAMLFocused = false
			return m, nil
		case "tab":
			m.wfYAMLFocused = false
			m.cycleFocus(1)
			return m, nil
		case "shift+tab":
			m.wfYAMLFocused = false
			return m, nil
		}
	}

	switch key {
	case "q":
		m.quitting = true
		return m, tea.Quit

	case "ctrl+z":
		return m, tea.Suspend

	case "?":
		m.modal = ModalHelp
		h := m.buildHelp()
		h.Height = 24
		if m.height > 0 && m.height-10 > 10 {
			h.Height = m.height - 10
		}
		h.Width = 66
		m.helpVP = h
		return m, nil

	case "s":
		m.settingsUI = NewSettingsUI(&m)
		m.modal = ModalSettings
		return m, nil

	case "D":
		return m, m.openDoctor()

	case "R":
		return m, m.openRepoPicker()

	case "B":
		if m.settings.Repo == "" {
			m.toastf("set a repository first (R)")
			return m, nil
		}
		return m, m.openBranchPicker()

	case "W":
		if m.settings.Repo == "" {
			m.toastf("set a repository first (R)")
			return m, nil
		}
		return m, m.openWorkflowPicker()

	case "t":
		next := theme.Next(m.settings.Theme)
		m.applyTheme(next)
		m.toastf("theme: %s", next)
		return m, nil

	case "w":
		m.settings.Watch = !m.settings.Watch
		if m.settings.Watch {
			m.refreshSecs = 15
			m.toastf("watch on — refreshing every 15s")
			return m, tea.Batch(m.refreshAll()...)
		}
		m.refreshSecs = m.settings.AutoRefreshSecs
		m.toastf("watch off")
		return m, nil

	case "r":
		if m.settings.Repo == "" {
			m.toastf("no repository — press R")
			return m, nil
		}
		m.toastf("refreshing…")
		// clear artifact cache so counts/detail always match fresh data
		m.runCache = map[int64][]ghapi.Artifact{}
		return m, tea.Batch(m.refreshAll()...)

	case "tab":
		m.cycleFocus(1)
		return m, nil
	case "shift+tab":
		m.cycleFocus(-1)
		return m, nil

	case "g":
		m.focusedList().Home()
		return m, nil
	case "G":
		m.focusedList().End()
		return m, nil

	case "j", "down":
		return m, m.moveFocus(1)
	case "k", "up":
		return m, m.moveFocus(-1)
	case "ctrl+d", "pgdown":
		return m, m.moveFocus(8)
	case "ctrl+u", "pgup":
		return m, m.moveFocus(-8)

	case "/":
		if l := m.focusedListOrNil(); l != nil {
			m.filterMode = true
			return m, nil
		}
		return m, nil

	case "enter":
		return m, m.activate()

	case "right", "l", "v":
		if m.focus == PanelWorkflows && len(m.workflows.Items) > 0 {
			m.wfYAMLFocused = true
			return m, nil
		}
		return m, nil

	case "d":
		if m.focus == PanelArtifacts {
			if m.usingReleaseAssets() {
				if ra, ok := m.cursorReleaseAsset(); ok {
					m.selectedArtID = -ra.Asset.ID
				}
			} else if a, ok := m.cursorArtifact(); ok {
				m.selectedArtID = a.ID
			}
		}
		return m, m.requestDownload(false)
	case "i":
		if m.focus == PanelArtifacts {
			if m.usingReleaseAssets() {
				if ra, ok := m.cursorReleaseAsset(); ok {
					m.selectedArtID = -ra.Asset.ID
				}
			} else if a, ok := m.cursorArtifact(); ok {
				m.selectedArtID = a.ID
			}
		}
		return m, m.requestDownload(true)

	case "o":
		if r, ok := m.selectedRun(); ok {
			return m, openURL(r.HTMLURL)
		}
		return m, nil

	case "O":
		if m.haveDownload && m.downloadedAPK.Path != "" {
			p := m.downloadedAPK.Path
			if i := strings.LastIndex(p, "/"); i > 0 {
				p = p[:i]
			}
			return m, openURL("file://" + p)
		}
		return m, nil

	case "f":
		// favourite the highlighted recent repo
		if m.focus == PanelRepos {
			if sel, ok := m.repos.Selected(); ok {
				if repo, _ := sel.ID.(string); repo != "" {
					state := config.ToggleFavourite(repo)
					m.loadRecents(m.settings.Repo)
					if state {
						m.toastf("★ %s", repo)
					} else {
						m.toastf("☆ %s", repo)
					}
					return m, nil
				}
			}
		}
		return m, nil

	case "e":
		// edit the highlighted repo entry (fix a typo'd slug, rename…)
		if m.focus == PanelRepos {
			if sel, ok := m.repos.Selected(); ok {
				if repo, _ := sel.ID.(string); repo != "" {
					m.openRepoEditor(repo)
				}
			}
		}
		return m, nil

	case "x":
		// remove the highlighted repo entry from the config
		if m.focus == PanelRepos {
			if sel, ok := m.repos.Selected(); ok {
				if repo, _ := sel.ID.(string); repo != "" {
					m.askRemoveRepo(repo)
				}
			}
		}
		return m, nil

	default:
		// digit keys jump straight to a panel — FocusOrder is the single
		// source of truth, so digits always match the tab bar on screen.
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			if i := int(key[0] - '1'); i < len(FocusOrder) {
				m.wfYAMLFocused = false
				m.focus = FocusOrder[i]
			}
		}
		return m, nil
	}
}

func (m *Model) cycleFocus(dir int) {
	m.wfYAMLFocused = false
	idx := 0
	for i, p := range FocusOrder {
		if p == m.focus {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(FocusOrder)) % len(FocusOrder)
	m.focus = FocusOrder[idx]
}

func (m *Model) focusedList() *components.List {
	switch m.focus {
	case PanelRepos:
		return &m.repos
	case PanelWorkflows:
		return &m.workflows
	case PanelRuns:
		return &m.runs
	case PanelArtifacts:
		return &m.arts
	}
	return &m.repos
}

func (m *Model) focusedListOrNil() *components.List {
	switch m.focus {
	case PanelRepos, PanelWorkflows, PanelRuns, PanelArtifacts:
		return m.focusedList()
	}
	return nil
}

func (m *Model) moveFocus(d int) tea.Cmd {
	switch m.focus {
	case PanelRuns:
		m.runs.MoveBy(d)
		return m.fetchArtifactsForSelection()
	case PanelArtifacts:
		m.arts.MoveBy(d)
	case PanelRepos:
		m.repos.MoveBy(d)
	case PanelWorkflows:
		m.wfScroll = 0
		m.workflows.MoveBy(d)
		return m.fetchWorkflowForSelection()
	case PanelDevice:
	}
	return nil
}

// activate handles <enter> per focused panel.
func (m *Model) activate() tea.Cmd {
	switch m.focus {
	case PanelRepos:
		if sel, ok := m.repos.Selected(); ok {
			if repo, _ := sel.ID.(string); repo != "" && repo != m.settings.Repo {
				m.setRepo(repo, true)
				m.toastf("repo: %s", repo)
				return tea.Batch(m.refreshAll()...)
			}
		}
	case PanelWorkflows:
		m.wfYAMLFocused = false
		m.wfScroll = 0
		if sel, ok := m.workflows.Selected(); ok {
			name, _ := sel.ID.(string)
			m.settings.Workflow = name
			m.runsLoaded = false
			m.runs.SetItems(nil)
			m.arts.SetItems(nil)
			if name == "" {
				m.toastf("workflow: any")
			} else {
				m.toastf("workflow: %s", name)
			}
			return tea.Batch(m.refreshAll()...)
		}
	case PanelArtifacts:
		if m.usingReleaseAssets() {
			if ra, ok := m.cursorReleaseAsset(); ok {
				m.selectedArtID = -ra.Asset.ID
			}
		} else if a, ok := m.cursorArtifact(); ok {
			m.selectedArtID = a.ID
		}
		return m.requestDownload(false)
	case PanelRuns:
		if r, ok := m.cursorRun(); ok {
			m.selectedRunID = r.ID
		}
		m.focus = PanelArtifacts
		return m.fetchArtifactsForSelection()
	case PanelDevice:
		if m.instPkg != "" {
			return queryPackageCmd(m.instPkg)
		}
		m.toastf("set a package name in settings to track it")
		return nil
	}
	return nil
}

func (m *Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	l := m.focusedList()
	switch msg.Type {
	case tea.KeyEsc:
		l.Filter = ""
		m.filterMode = false
	case tea.KeyEnter:
		m.filterMode = false
	case tea.KeyBackspace, tea.KeyDelete:
		l.BackspaceFilter()
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			l.AddFilterChar(r)
		}
	case tea.KeyUp:
		l.MoveBy(-1)
	case tea.KeyDown:
		l.MoveBy(1)
	}
	if m.focus == PanelRuns {
	}
	return m, nil
}

// ─────────────────── modal dispatch ───────────────────

func (m Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch m.modal {

	case ModalHelp:
		switch key {
		case "q", "esc", "?":
			m.modal = ModalNone
		case "j", "down":
			m.helpVP.LineDown(2)
		case "k", "up":
			m.helpVP.LineUp(2)
		case "ctrl+d", "pgdown":
			m.helpVP.HalfViewDown()
		case "ctrl+u", "pgup":
			m.helpVP.HalfViewUp()
		}
		return m, nil

	case ModalDoctor:
		if m.doctor != nil {
			switch key {
			case "q", "esc", "D":
				m.modal = ModalNone
				m.doctor = nil
			case "r":
				return m, m.openDoctor()
			case "j", "down":
				m.doctor.scroll(2)
			case "k", "up":
				m.doctor.scroll(-2)
			}
		}
		return m, nil

	case ModalPickBranch:
		if m.branchPicker != nil {
			return m.branchPicker.handleKey(msg, &m, func(val string) tea.Cmd {
				m.branchSel = val
				m.settings.Branch = val
				m.runsLoaded = false
				m.runs.SetItems(nil)
				m.arts.SetItems(nil)
				m.branchPicker = nil
				if val == "" {
					m.toastf("branch: all")
				} else {
					m.toastf("branch: %s", val)
				}
				return tea.Batch(m.refreshAll()...)
			})
		}
		m.modal = ModalNone
		return m, nil

	case ModalPickWorkflow:
		if m.workflowPicker != nil {
			return m.workflowPicker.handleKey(msg, &m, func(val string) tea.Cmd {
				m.settings.Workflow = val
				m.workflowSel = val
				m.workflows.SelectByID(val)
				m.runsLoaded = false
				m.runs.SetItems(nil)
				m.arts.SetItems(nil)
				m.workflowPicker = nil
				if val == "" {
					m.toastf("workflow: all")
				} else {
					m.toastf("workflow: %s", val)
				}
				return tea.Batch(m.refreshAll()...)
			})
		}
		m.modal = ModalNone
		return m, nil

	case ModalConfirm:
		switch key {
		case "esc", "q":
			m.resetConfirm()
			return m, nil
		case "left", "h", "shift+tab":
			if m.confirm.Cursor > 0 {
				m.confirm.Cursor--
			}
		case "right", "l":
			if m.confirm.Cursor < len(m.confirm.Options)-1 {
				m.confirm.Cursor++
			}
		case "tab":
			if len(m.confirm.Options) > 0 {
				m.confirm.Cursor = (m.confirm.Cursor + 1) % len(m.confirm.Options)
			}
		case "y":
			if m.confirmKind == confirmSafetyOverride {
				return m, nil // safety overrides require deliberate selection + Enter
			}
			// Preserve an already-highlighted non-Cancel choice (e.g. "Download & Install");
			// only default to option 0 if the cursor was sitting on Cancel.
			if m.confirm.Cursor < 0 || m.confirm.Cursor >= len(m.confirm.Options)-1 {
				m.confirm.Cursor = 0
			}
			cmd := m.confirmAccept()
			return m, cmd
		case "n":
			m.resetConfirm()
			return m, nil
		case "enter", " ":
			cmd := m.confirmAccept()
			return m, cmd
		}
		return m, nil

	case ModalTask:
		if m.task != nil {
			// task handles its own keys (buttons after completion etc.)
			finished := m.task.isDismissable()
			if key == "esc" && (finished || !m.task.running) {
				m.pendingInstall = false
				m.modal = ModalNone
				if finished {
					m.task = nil
				}
				return m, nil
			}
			if finished {
				switch key {
				case "left", "h", "shift+tab":
					m.task.moveButton(-1)
					return m, nil
				case "right", "l":
					m.task.moveButton(1)
					return m, nil
				case "tab":
					if len(m.task.buttons) > 1 {
						m.task.btnIdx = (m.task.btnIdx + 1) % len(m.task.buttons)
					}
					return m, nil
				case "enter", " ":
					cmd := m.task.activateFocused(&m)
					return m, cmd
				case "i":
					for idx, b := range m.task.buttons {
						if b == "Install" {
							m.task.btnIdx = idx
							cmd := m.task.activateFocused(&m)
							return m, cmd
						}
					}
					return m, nil
				case "q":
					m.pendingInstall = false
					m.modal = ModalNone
					m.task = nil
					return m, nil
				}
			}
			return m, nil
		}
		if key == "esc" || key == "q" {
			m.modal = ModalNone
		}
		return m, nil

	case ModalSettings:
		if m.settingsUI != nil {
			return m.settingsUI.handleKey(msg, &m)
		}
		if key == "esc" || key == "q" {
			m.modal = ModalNone
		}
		return m, nil

	case ModalPickRepo:
		if m.repoPicker != nil {
			return m.repoPicker.handleKey(msg, &m)
		}
		if key == "esc" {
			m.modal = ModalNone
		}
		return m, nil

	case ModalEditRepo:
		switch key {
		case "esc":
			m.modal = ModalNone
			m.editingRepo = ""
			return m, nil
		case "enter":
			return m, m.saveRepoEdit()
		}
		var cmd tea.Cmd
		m.editInput, cmd = m.editInput.Update(msg)
		return m, cmd
	}
	m.modal = ModalNone
	return m, nil
}

// ─────────────────── confirm plumbing ───────────────────

type confirmAction int

const (
	confirmDownload confirmAction = iota
	confirmDownloadInstall
	confirmInstallExisting
	confirmRemoveRepo
	confirmSafetyOverride
)

// openRepoEditor opens the small modal that renames a repo entry.
func (m *Model) openRepoEditor(repo string) {
	ti := textinput.New()
	ti.SetValue(repo)
	ti.CursorEnd()
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40
	m.editInput = ti
	m.editingRepo = repo
	m.modal = ModalEditRepo
}

// saveRepoEdit applies the renamed slug.
func (m *Model) saveRepoEdit() tea.Cmd {
	old := m.editingRepo
	new := strings.TrimSpace(m.editInput.Value())
	m.editingRepo = ""
	if new == old {
		m.modal = ModalNone
		return nil
	}
	if !validRepoSlug(new) {
		m.toastf("need owner/repo — e.g. termux/termux-app")
		return nil
	}
	m.modal = ModalNone
	config.RenameRecent(old, new)
	if m.settings.Repo == old {
		m.setRepo(new, true)
		m.toastf("repo: %s", new)
		return tea.Batch(m.refreshAll()...)
	}
	m.loadRecents(m.settings.Repo)
	m.toastf("renamed to %s", new)
	return nil
}

func validRepoSlug(s string) bool {
	if strings.ContainsAny(s, " \t\n") {
		return false
	}
	parts := strings.Split(s, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

// askRemoveRepo opens the confirm dialog before dropping a repo entry.
func (m *Model) askRemoveRepo(repo string) {
	m.confirmKind = confirmRemoveRepo
	m.confirmTarget = repo
	m.confirm = components.Confirm{
		Title: "Remove repo entry",
		Body: []string{
			fmt.Sprintf("Remove  %s", m.st.Bold.Render(repo)),
			m.st.Dim.Render("from the recent list (config file)"),
		},
		Options: []string{"Remove", "Cancel"},
		Cursor:  1,
	}
	m.modal = ModalConfirm
}

func (m *Model) resetConfirm() {
	m.modal = ModalNone
	m.confirm = components.Confirm{}
	m.confirmKind = confirmDownload
	m.confirmTarget = ""
	m.safetyInstallPath = ""
	m.safetyAllowDowngrade = false
}

// requestDownload asks what to do with the selected artifact or release asset.
func (m *Model) requestDownload(withInstall bool) tea.Cmd {
	if m.usingReleaseAssets() {
		ra, ok := m.selectedReleaseAsset()
		if !ok {
			if m.haveDownload && withInstall && m.dev.CanInstall {
				return m.startInstall(m.downloadedAPK.Path)
			}
			m.toastf("select a release asset first (Tab to Artifacts)")
			return nil
		}
		opts := []string{"Download", "Cancel"}
		body := []string{
			fmt.Sprintf("Download  %s", m.st.Bold.Render(ra.Asset.Name)),
			fmt.Sprintf("release   %s", ra.ReleaseTag),
			fmt.Sprintf("size      %s", humanSize(ra.Asset.Size)),
		}
		if ra.Asset.Rejected {
			body = append(body, m.st.Warn.Render("warning: this does not match your arch/variant policy"))
		}
		if m.dev.CanInstall {
			opts = []string{"Download", "Download & Install", "Cancel"}
		}
		m.confirmKind = confirmDownload
		m.confirmTarget = ""
		m.confirm = components.Confirm{
			Title:   "Confirm download",
			Body:    body,
			Options: opts,
		}
		if withInstall && m.dev.CanInstall && !m.settings.NoOpen {
			m.confirm.Cursor = 1
		}
		m.modal = ModalConfirm
		return nil
	}

	art, ok := m.selectedArtifact()
	if !ok {
		if m.haveDownload && withInstall && m.dev.CanInstall {
			// install the last download again
			return m.startInstall(m.downloadedAPK.Path)
		}
		m.toastf("select an artifact first (Tab to Artifacts)")
		return nil
	}
	opts := []string{"Download", "Cancel"}
	body := []string{
		fmt.Sprintf("Download  %s", m.st.Bold.Render(art.Name)),
		fmt.Sprintf("from run  %s", m.selectedRunLabel()),
		fmt.Sprintf("size      %s (zipped)", humanSize(art.SizeInBytes)),
	}
	if art.Rejected {
		body = append(body, m.st.Warn.Render("warning: this does not match your arch/variant policy"))
	}
	if m.dev.CanInstall {
		opts = []string{"Download", "Download & Install", "Cancel"}
	}
	m.confirmKind = confirmDownload
	m.confirmTarget = ""
	m.confirm = components.Confirm{
		Title:   "Confirm download",
		Body:    body,
		Options: opts,
	}
	if withInstall && m.dev.CanInstall && !m.settings.NoOpen {
		m.confirm.Cursor = 1
	}
	m.modal = ModalConfirm
	return nil
}

func (m *Model) selectedRunLabel() string {
	if r, ok := m.selectedRun(); ok {
		return fmt.Sprintf("#%d (%s)", r.RunNumber, r.HeadBranch)
	}
	return "?"
}

// confirmAccept runs after the user picked a confirm option.
func (m *Model) confirmAccept() tea.Cmd {
	choice := m.confirm.Cursor
	m.modal = ModalNone
	m.confirm = components.Confirm{}
	kind := m.confirmKind
	m.confirmKind = confirmDownload
	target := m.confirmTarget
	m.confirmTarget = ""
	if kind == confirmSafetyOverride {
		path := m.safetyInstallPath
		allowDg := m.safetyAllowDowngrade
		m.safetyInstallPath = ""
		m.safetyAllowDowngrade = false
		if choice == 0 && path != "" {
			return m.executeInstall(path, allowDg, []string{"user confirmed safety override"})
		}
		m.toastf("install cancelled")
		return nil
	}
	if kind == confirmRemoveRepo {
		if choice == 0 && target != "" {
			config.RemoveRecent(target)
			m.loadRecents(m.settings.Repo)
			m.toastf("removed %s from recents", target)
		}
		return nil
	}
	switch choice {
	case 0:
		return m.startDownload(false)
	case 1:
		if m.dev.CanInstall {
			return m.startDownload(true)
		}
		// second option is Cancel when install is unavailable
	}
	return nil
}
