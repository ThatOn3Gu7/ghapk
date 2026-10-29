package tui

import (
	"fmt"
	"os"
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
		return m, m.cancelJobsAndQuit()
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

	if m.focus == PanelSettings && (m.settingsUI.editing || key == "up" || key == "down" || key == "j" || key == "k" || key == "pgup" || key == "pgdown" || key == "ctrl+u" || key == "ctrl+d" || key == "g" || key == "G" || key == "s" || key == "d" || key == "enter" || key == " " || key == "left" || key == "right" || key == "h" || key == "l") {
		return m.settingsUI.handleKey(msg, &m)
	}
	if m.focus == PanelDevice {
		switch key {
		case "j", "down":
			m.scrollDevice(1)
			return m, nil
		case "k", "up":
			m.scrollDevice(-1)
			return m, nil
		case "ctrl+d", "pgdown":
			m.scrollDevice(6)
			return m, nil
		case "ctrl+u", "pgup":
			m.scrollDevice(-6)
			return m, nil
		case "g":
			m.deviceScroll = 0
			return m, nil
		case "G":
			m.deviceScroll = 999999
			m.scrollDevice(0)
			return m, nil
		case "r":
			m.deviceChecking = true
			m.deviceCheckSeq++
			m.deviceHistorySeq++
			m.deviceHistoryLoaded = false
			m.deviceHistoryLoading = false
			m.toastf("re-checking device…")
			return m, probeDeviceCmd()
		}
	}
	if m.focus == PanelDownloads {
		switch key {
		case "s", "c", "x", "X", "i", "enter", "esc":
			return m, m.downloadAction(key)
		case "j", "down", "ctrl+d", "pgdown", "k", "up", "ctrl+u", "pgup":
			delta := 1
			if key == "k" || key == "up" || key == "ctrl+u" || key == "pgup" {
				delta = -1
			}
			if strings.HasPrefix(key, "ctrl+") || strings.HasPrefix(key, "pg") {
				delta *= 6
			}
			if m.downloadDetail {
				m.downloadScroll = max(0, m.downloadScroll+delta)
			} else if m.downloadFocus == 1 {
				m.savedList.MoveBy(delta)
			} else {
				m.jobList.MoveBy(delta)
				m.downloadScroll = 0
			}
			return m, nil
		}
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
		for _, j := range m.jobs {
			if j.status == "running" || j.status == "asking" || j.status == "cancelling" {
				m.confirmKind = confirmQuitJobs
				m.confirm = components.Confirm{Title: "Quit with active jobs?", Body: []string{"Active jobs will be cancelled.", "An open installer cannot be undone."}, Options: []string{"Quit", "Cancel"}, Cursor: 1}
				m.modal = ModalConfirm
				return m, nil
			}
		}
		m.quitting = true
		return m, tea.Quit

	case "ctrl+z":
		return m, tea.Suspend

	case "?":
		m.modal = ModalHelp
		m.helpDragging = false
		m.helpVP, m.helpLineEntries = m.helpLayout()
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
			m.refreshSecs = m.settings.AutoRefreshSecs
			m.toastf("watch on — faster while builds are active")
			m.refreshSeq++
			return m, tea.Batch(append(m.refreshAll(), refreshTickCmd(m.effectiveRefreshSecs(), m.refreshSeq))...)
		}
		m.refreshSecs = m.settings.AutoRefreshSecs
		m.toastf("watch off")
		m.refreshSeq++
		return m, refreshTickCmd(m.effectiveRefreshSecs(), m.refreshSeq)

	case "r":
		if m.settings.Repo == "" {
			m.toastf("no repository — press R")
			return m, nil
		}
		m.toastf("refreshing…")
		// clear artifact cache so counts/detail always match fresh data
		m.runCache = map[int64][]ghapi.Artifact{}
		m.artifactErrors = map[int64]string{}
		return m, tea.Batch(m.refreshAll()...)

	case "tab":
		m.cycleFocus(1)
		return m, nil
	case "shift+tab":
		m.cycleFocus(-1)
		return m, nil

	case "g", "G":
		if l := m.focusedListOrNil(); l != nil {
			if key == "g" {
				l.Home()
			} else {
				l.End()
			}
			if m.focus == PanelRuns {
				m.syncBuildSelection()
				return m, m.fetchArtifactsForSelection()
			}
			if m.focus == PanelArtifacts {
				m.syncArtifactSelection()
			}
		}
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

	case "esc", "left", "h":
		if m.focus == PanelRepos && m.repoBranchFocused {
			m.repoBranchFocused = false
			return m, nil
		}
		if m.focus == PanelArtifacts {
			m.focus = PanelRuns
			m.buildDetail = false
			m.updateListBounds()
			return m, nil
		}
		return m, nil

	case "right", "l", "v":
		if m.focus == PanelRepos && !m.repoBranchFocused && m.settings.Repo != "" && key != "v" {
			m.repoBranchFocused = true
			return m, nil
		}
		if m.focus == PanelRuns {
			m.focus = PanelArtifacts
			m.buildDetail = true
			m.updateListBounds()
			return m, nil
		}
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
		if m.activeTab() == PanelRuns && m.usingReleaseAssets() {
			if a, ok := m.selectedReleaseAsset(); ok {
				return m, openURL(a.HTMLURL)
			}
		}
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
		if m.focus == PanelRepos && !m.repoBranchFocused {
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
		if m.focus == PanelRepos && !m.repoBranchFocused {
			if sel, ok := m.repos.Selected(); ok {
				if repo, _ := sel.ID.(string); repo != "" {
					m.openRepoEditor(repo)
				}
			}
		}
		return m, nil

	case "x":
		// remove the highlighted repo entry from the config
		if m.focus == PanelRepos && !m.repoBranchFocused {
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
				m.buildDetail = false
				m.updateListBounds()
			}
		}
		return m, nil
	}
}

func (m *Model) cycleFocus(dir int) {
	m.wfYAMLFocused = false
	idx := 0
	for i, p := range FocusOrder {
		if p == m.activeTab() {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(FocusOrder)) % len(FocusOrder)
	m.focus = FocusOrder[idx]
	m.buildDetail = false
	m.updateListBounds()
}

func (m *Model) focusedList() *components.List {
	switch m.focus {
	case PanelRepos:
		if m.repoBranchFocused {
			return &m.branchList
		}
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
		return m.moveBuild(d)
	case PanelArtifacts:
		m.arts.MoveBy(d)
		m.syncArtifactSelection()
	case PanelRepos:
		if m.repoBranchFocused {
			m.branchList.MoveBy(d)
		} else {
			m.repos.MoveBy(d)
		}
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
		if m.repoBranchFocused {
			if sel, ok := m.branchList.Selected(); ok {
				if branch, ok := sel.ID.(string); ok {
					if m.navBranchSet && m.navBranch == branch && m.branchSel == branch {
						m.focus = PanelWorkflows
						m.updateListBounds()
						return m.fetchWorkflowForSelection()
					}
					m.navBranch, m.navBranchSet = branch, true
					return m.selectBranch(branch)
				}
			}
			return nil
		}
		if sel, ok := m.repos.Selected(); ok {
			if repo, ok := sel.ID.(string); ok && repo != "" {
				if m.navRepo == repo && m.settings.Repo == repo {
					m.repoBranchFocused = true
					m.updateListBounds()
					return nil
				}
				var saveErr error
				if repo != m.settings.Repo {
					saveErr = m.setRepo(repo, true)
				} else {
					config.TouchRecent(repo)
					saveErr = config.RememberSelection(repo, true, nil, nil)
				}
				m.repos.SelectByID(repo)
				m.navRepo = repo
				m.navWorkflowSet = false
				m.toastf("repo: %s", repo)
				if saveErr != nil {
					m.toastf("could not save selection: %s", shortErr(saveErr))
				}
				return tea.Batch(m.refreshAll()...)
			}
		}
	case PanelWorkflows:
		m.wfYAMLFocused = false
		m.wfScroll = 0
		if sel, ok := m.workflows.Selected(); ok {
			name, ok := sel.ID.(string)
			if !ok {
				return nil
			}
			if m.navWorkflowSet && m.navWorkflow == name && m.settings.Workflow == name {
				m.focus = PanelRuns
				m.buildDetail = false
				m.updateListBounds()
				return m.fetchArtifactsForSelection()
			}
			m.navWorkflow, m.navWorkflowSet = name, true
			m.settings.Workflow = name
			m.workflowSel = name
			m.runsLoaded = false
			m.runs.SetItems(nil)
			m.arts.SetItems(nil)
			m.selectedRunID, m.selectedArtID = 0, 0
			if name == "" {
				m.toastf("workflow: any")
			} else {
				m.toastf("workflow: %s", name)
			}
			m.saveSelection(nil, &name)
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
		return m.requestDownload(true)
	case PanelRuns:
		m.syncBuildSelection()
		m.focus = PanelArtifacts
		m.buildDetail = true
		m.updateListBounds()
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
		m.syncBuildSelection()
		return m, m.fetchArtifactsForSelection()
	}
	if m.focus == PanelArtifacts {
		m.syncArtifactSelection()
	}
	return m, nil
}

// ─────────────────── modal dispatch ───────────────────

func (m Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch m.modal {

	case ModalPlan:
		return m.handlePlanKey(msg)
	case ModalHelp:
		switch key {
		case "w", "W":
			m.toggleHelpWrap()
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
				m.branchPicker = nil
				m.branchList.Filter = ""
				m.navBranch, m.navBranchSet = val, true
				return m.selectBranch(val)
			})
		}
		m.modal = ModalNone
		return m, nil

	case ModalPickWorkflow:
		if m.workflowPicker != nil {
			return m.workflowPicker.handleKey(msg, &m, func(val string) tea.Cmd {
				m.settings.Workflow = val
				m.workflowSel = val
				m.selectWorkflowOption(val)
				m.runsLoaded = false
				m.runs.SetItems(nil)
				m.arts.SetItems(nil)
				m.workflowPicker = nil
				if val == "" {
					m.toastf("workflow: all")
				} else {
					m.toastf("workflow: %s", val)
				}
				m.saveSelection(nil, &val)
				return tea.Batch(m.refreshAll()...)
			})
		}
		m.modal = ModalNone
		return m, nil

	case ModalConfirm:
		if m.safetyDialogTooLarge() {
			switch key {
			case "esc", "q", "n", "enter", " ":
				m.resetConfirm()
			}
			return m, nil
		}
		if m.confirmKind == confirmJobSafety && len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			index := int(key[0] - '1')
			if index < len(m.confirm.Options) {
				m.confirm.Cursor = index
			}
			return m, nil // explicit Enter still required
		}
		if m.confirmKind == confirmJobSafety {
			switch key {
			case "pgdown":
				m.scrollJobQuestion(5)
				return m, nil
			case "pgup":
				m.scrollJobQuestion(-5)
				return m, nil
			}
		}
		switch key {
		case "esc", "q":
			m.resetConfirm()
			return m, nil
		case "left", "up", "k", "h", "shift+tab":
			if m.confirm.Cursor > 0 {
				m.confirm.Cursor--
			}
		case "right", "down", "j", "l":
			if m.confirm.Cursor < len(m.confirm.Options)-1 {
				m.confirm.Cursor++
			}
		case "tab":
			if len(m.confirm.Options) > 0 {
				m.confirm.Cursor = (m.confirm.Cursor + 1) % len(m.confirm.Options)
			}
		case "y":
			if m.confirmKind == confirmSafetyOverride || m.confirmKind == confirmJobSafety {
				return m, nil // safety overrides require deliberate selection + Enter
			}
			// Default to the first choice when the cursor is on Cancel.
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
	confirmNone confirmAction = iota
	confirmRemoveRepo
	confirmSafetyOverride
	confirmJobSafety
	confirmDeleteSaved
	confirmQuitJobs
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
		err := m.setRepo(new, true)
		m.toastf("repo: %s", new)
		if err != nil {
			m.toastf("could not save selection: %s", shortErr(err))
		}
		return tea.Batch(m.refreshAll()...)
	}
	m.loadRecents(m.settings.Repo)
	m.toastf("renamed to %s", new)
	return nil
}

func validRepoSlug(s string) bool {
	if strings.ContainsAny(s, " \t\r\n") {
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
		Title:  "Remove repo entry",
		Danger: true,
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
	if m.confirmKind == confirmJobSafety {
		m.answerJobQuestion(0)
	}
	m.modal = ModalNone
	m.confirm = components.Confirm{}
	m.confirmKind = confirmNone
	m.confirmTarget = ""
	m.safetyInstallPath = ""
	m.safetyAllowDowngrade = false
}

// requestDownload starts the chosen action immediately. APK safety checks still
// run before installation, after the download has finished.
func (m *Model) requestDownload(withInstall bool) tea.Cmd {
	if withInstall {
		return m.beginPlan()
	}
	spec := jobSpec{repo: m.settings.Repo, settings: m.settings, dev: m.dev}
	if m.usingReleaseAssets() {
		a, ok := m.selectedReleaseAsset()
		if !ok {
			m.toastf("select a release asset in Builds")
			return nil
		}
		spec.choice = planChoice{asset: a.Asset, release: true}
	} else {
		a, ok := m.selectedArtifact()
		if !ok {
			m.toastf("select an artifact in Builds")
			return nil
		}
		spec.choice = planChoice{art: a}
		spec.run, _ = m.selectedRun()
	}
	return m.startJob(spec)
}

// confirmAccept runs after the user picked a confirm option.
func (m *Model) confirmAccept() tea.Cmd {
	if m.safetyDialogTooLarge() {
		m.resetConfirm()
		return nil
	}
	choice := m.confirm.Cursor
	if m.confirmKind == confirmJobSafety && choice != 0 && m.jobQuestionScroll < m.jobQuestionMaxScroll() {
		m.scrollJobQuestion(5)
		return nil
	}
	m.modal = ModalNone
	m.confirm = components.Confirm{}
	kind := m.confirmKind
	m.confirmKind = confirmNone
	target := m.confirmTarget
	m.confirmTarget = ""
	if kind == confirmJobSafety {
		m.answerJobQuestion(choice)
		return nil
	}
	if kind == confirmDeleteSaved {
		if choice == 0 && target != "" {
			busy := false
			for _, j := range m.jobs {
				if (j.status == "running" || j.status == "asking" || j.status == "cancelling") && (j.path == target || j.spec.savedPath == target || (j.spec.savedPath == "" && j.path == "")) {
					busy = true
				}
			}
			if busy {
				m.toastf("APK is in use by a job")
			} else if err := os.Remove(target); err != nil {
				m.toastf("delete: %s", err)
			} else {
				m.toastf("APK deleted")
				_ = os.Remove(target + ".origin")
				m.refreshSaved()
			}
		}
		return nil
	}
	if kind == confirmQuitJobs {
		if choice == 0 {
			return m.cancelJobsAndQuit()
		}
		return nil
	}
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
			if err := config.ForgetSelectedRepo(target); err != nil {
				m.toastf("could not save selection: %s", shortErr(err))
			}
		}
		return nil
	}
	return nil
}

// selectBranch changes the Actions run filter; release assets are not branch-scoped.
func (m *Model) selectBranch(branch string) tea.Cmd {
	m.branchSel = branch
	m.settings.Branch = branch
	m.navWorkflowSet = false
	m.runsLoaded = false
	m.runs.SetItems(nil)
	m.arts.SetItems(nil)
	m.selectedRunID, m.selectedArtID = 0, 0
	m.rebuildBranchList()
	m.branchList.SelectByID(branch)
	if m.settings.Source == "releases" {
		m.syncReleaseArtifactItems()
	}
	if branch == "" {
		m.toastf("branch: all")
	} else {
		m.toastf("branch: %s", branch)
	}
	m.saveSelection(&branch, nil)
	if m.settings.Repo == "" {
		return nil
	}
	return fetchRunsCmd(m.settings.Repo, m.settings.Workflow, branch, m.settings.RunLimit)
}
