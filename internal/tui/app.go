// Package tui is the interactive terminal interface of ghapk — a lazygit-style
// full-screen app built on Bubble Tea. It runs in the alternate screen
// buffer, so when you quit, your terminal is exactly as you left it.
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/apk"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/score"
	"github.com/ghapk/ghapk/internal/tui/components"
	"github.com/ghapk/ghapk/internal/tui/theme"
)

// Screen is the top-level view.
type Screen int

const (
	ScreenBoot  Screen = iota
	ScreenSetup        // gh missing or unauthenticated
	ScreenDashboard
)

// ModalKind identifies the overlay open on top of the dashboard.
type ModalKind int

const (
	ModalNone ModalKind = iota
	ModalHelp
	ModalSettings
	ModalPickRepo
	ModalPickBranch
	ModalPickWorkflow
	ModalEditRepo
	ModalConfirm
	ModalTask
	ModalDoctor
)

// Options customises a session (flags from the CLI).
type Options struct {
	Settings  config.Settings
	StartRepo string
	PickRun   bool
}

// Model is the root Bubble Tea model.
type Model struct {
	screen Screen
	opts   Options

	st theme.Styles

	width, height int
	compact       bool
	buildDetail   bool // narrow-screen drill-down from builds into artifacts

	// environment
	ghVersion string
	authUser  string
	authErr   error

	// data
	settings config.Settings
	dev      device.Info
	repo     ghapi.Repo

	repos     components.List // recents + detected
	workflows components.List
	runs      components.List
	arts      components.List

	focus  PanelID
	filter string // legacy; per-list filters used instead
	// filterMode routes keystrokes into the focused list's filter.
	filterMode bool

	workflowSel       string // "" = any (name from workflow list)
	branchSel         string
	navRepo           string // repository explicitly selected during this session
	repoBranchFocused bool
	navBranch         string
	navBranchSet      bool
	apiBranches       []ghapi.Branch
	localBranches     []string
	recentBranches    []string
	branchLoading     bool
	branchTouchActive bool
	branchTouchMoved  bool
	branchTouchLastY  int
	branchTouchValue  string
	branchTouchRepo   string
	navWorkflow       string
	navWorkflowSet    bool

	runCache       map[int64][]ghapi.Artifact // scored+sorted artifacts per run
	fetching       map[int64]bool
	artifactErrors map[int64]string
	runsLoaded     bool

	wfPaths       map[string]string // workflow name → repo path (for previews)
	wfCache       map[string]string // path → YAML text (nil LRU, workflows are small)
	wfFetching    map[string]bool
	wfMissToasted bool // one-shot toast per repo for a stale workflow filter
	wfScroll      int  // line scroll offset for workflow YAML preview
	wfYAMLFocused bool // true when YAML viewport is focused for scrolling

	selectedRunID int64 // currently chosen run (green background)
	selectedArtID int64 // currently chosen artifact (green background)

	// release fallback
	releases []ghapi.Release

	// device panel
	instApp   device.InstalledApp
	instFound bool
	instPkg   string

	// detail pane + log
	detailVP viewport.Model
	logLines []string

	// spinners & busy state
	spin    spinner.Model
	busy    bool
	busyMsg string

	// toasts
	toast      string
	toastUntil time.Time

	// modal state
	modal ModalKind

	helpVP              viewport.Model
	helpWrapOff         bool // session preference; wrap is enabled by default
	helpLineEntries     []int
	helpDragging        bool
	helpDragScrollbar   bool
	helpDragLastY       int
	helpDragThumbOffset int

	settingsUI *SettingsUI

	repoPicker       *RepoPicker
	branchPicker     *FilterPicker
	workflowPicker   *FilterPicker
	cachedUserRepos  []ghapi.Repo
	userReposLoaded  bool
	userReposLoading bool
	userReposSeq     int
	wfCatalog        []ghapi.Workflow

	branchList components.List

	// edit/remove repo entries from the config
	editInput     textinput.Model
	editingRepo   string // slug being edited (renamed on save)
	confirmKind   confirmAction
	confirmTarget string // repo slug for confirmRemoveRepo

	confirm components.Confirm

	task *TaskState
	// task progress channel (kept so we can re-arm waitProgress)
	progCh chan tea.Msg
	logCh  chan tea.Msg

	doctor *DoctorState

	watchTicker bool // whether refresh ticks are running
	refreshSecs int

	// debounce for repo search
	searchSeq int

	gitDetected string

	// shimmer position for skeleton logo animation
	shimmerPos int

	// remembered artifacts to install after download completes
	pendingInstall       bool
	downloadedAPK        ghapi.ExtractedAPK
	haveDownload         bool
	safetyInstallPath    string
	safetyAllowDowngrade bool

	quitting bool
}

// New builds the root model.
func New(opts Options) Model {
	th := theme.New(theme.Get(opts.Settings.Theme))
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = th.Accent

	m := Model{
		screen:         ScreenBoot,
		opts:           opts,
		settings:       opts.Settings,
		st:             th,
		spin:           sp,
		runCache:       map[int64][]ghapi.Artifact{},
		fetching:       map[int64]bool{},
		artifactErrors: map[int64]string{},
		repos:          components.List{Empty: "  no recent repos — press R"},
		workflows:      components.List{Empty: "  no workflows found"},
		runs:           components.List{Empty: "  no runs yet"},
		wfPaths:        map[string]string{},
		wfCache:        map[string]string{},
		wfFetching:     map[string]bool{},
		arts:           components.List{Empty: "  select a run with artifacts"},
		branchList:     components.List{Empty: "  no branches"},
		refreshSecs:    opts.Settings.AutoRefreshSecs,
		branchSel:      opts.Settings.Branch,
		instPkg:        opts.Settings.PackageName,
		progCh:         make(chan tea.Msg, 64),
		logCh:          make(chan tea.Msg, 64),
	}
	m.loadRecents("")
	m.updateListBounds()
	return m
}

// updateListBounds sizes each list's window to the current layout so
// scrolling (List.Offset) is computed against the real panel heights.
// Repos is carded: every row also reserves the blank line the card borders
// draw into, so its MaxShown is counted in ROWS (k cards), not lines.
// Builds and artifacts use their actual visible box heights.
func (m *Model) updateListBounds() {
	if m.width == 0 || m.height == 0 {
		return
	}
	h := m.height - 3
	_, repoH, branchH := reposBranchSplit(h, len(m.repos.Items), len(m.branchList.Items), m.width)
	m.repos.MaxShown = max(1, repoH-2)
	m.branchList.MaxShown = max(1, branchH-2)
	m.repos.MoveBy(0)
	m.branchList.MoveBy(0)
	m.workflows.MaxShown = max(1, h-4)
	layout := m.buildsLayout()
	m.runs.MaxShown = max(1, layout.runs.h-2)
	m.arts.MaxShown = max(1, layout.arts.h-2)
	m.runs.MoveBy(0)
	m.arts.MoveBy(0)
}

func (m *Model) loadRecents(current string) {
	items := []components.Item{}
	if current != "" {
		items = append(items, components.Item{
			ID:    current,
			Label: current,
		})
	}
	for _, r := range config.LoadRecents() {
		if r.Repo == current {
			continue
		}
		lbl := r.Repo
		items = append(items, components.Item{ID: r.Repo, Label: lbl})
	}
	if m.gitDetected != "" && m.gitDetected != current {
		items = append([]components.Item{{ID: m.gitDetected, Label: m.gitDetected}}, items...)
	}
	m.repos.SetItems(items)
}

// Init kicks off environment probes.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		checkAuthCmd(),
		probeDeviceCmd(),
		detectGitRepoCmd(),
		m.spin.Tick,
		shimmerTickCmd(),
	)
}

// ───────────────────────────── UPDATE ─────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.WindowSizeMsg:
		m.branchTouchActive = false
		m.width = msg.Width
		m.height = msg.Height
		m.compact = m.width < 64
		m.updateListBounds()
		if m.modal == ModalPickRepo && m.repoPicker != nil {
			m.repoPicker.resize(&m)
		}
		if m.modal == ModalHelp {
			m.resizeHelp()
		}
		if m.screen == ScreenBoot && m.authErr == nil && m.ghVersion != "" {
			m.screen = ScreenDashboard
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		// Only repaint for the spinner when something is visibly moving.
		// Idle dashboards used to redraw ten times a second for no reason,
		// which made every repo switch on a phone feel jerky.
		if m.busy || len(m.fetching) > 0 || m.modal == ModalTask || (m.modal == ModalPickRepo && m.repoPicker != nil && m.repoPicker.loading) || m.watchActive() {
			return m, cmd
		}
		return m, nil

	case ShimmerTickMsg:
		m.shimmerPos++
		if m.shimmerPos >= 70 {
			m.shimmerPos = 0
		}
		if m.screen == ScreenDashboard && (m.focus == PanelRepos || !m.compact) {
			return m, shimmerTickCmd()
		}
		return m, tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg { return ShimmerTickMsg{At: t} })

	case AuthCheckedMsg:
		oldPicker := m.repoPicker
		changedAccount := msg.Err == nil && m.authUser != "" && m.authUser != msg.User
		if changedAccount {
			m.cachedUserRepos = nil
			m.userReposLoaded = false
			m.userReposLoading = false
			m.userReposSeq++
		}
		m.ghVersion = msg.Version
		m.authUser = msg.User
		m.authErr = msg.Err
		if msg.Err == nil {
			m.screen = ScreenDashboard
			cmd := m.bootstrapDashboard()
			if changedAccount && m.modal == ModalPickRepo && m.repoPicker == oldPicker {
				cmd = tea.Batch(cmd, m.openRepoPicker())
			}
			return m, cmd
		}
		if ghapiErr(msg.Err) == ghapi.ErrNoGH || m.ghVersion == "" && msg.Err != nil && !ghapi.Available() {
			m.screen = ScreenSetup
		} else {
			m.screen = ScreenSetup
		}
		return m, nil

	case DeviceMsg:
		m.dev = msg.Info
		if m.instPkg != "" {
			return m, queryPackageCmd(m.instPkg)
		}
		return m, nil

	case PackageQueryMsg:
		m.instApp = msg.App
		m.instFound = msg.Found
		return m, nil

	case GitRemoteMsg:
		if msg.Repo != "" {
			m.gitDetected = msg.Repo
			m.loadRecents(m.settings.Repo)
			if m.modal == ModalPickRepo && m.repoPicker != nil {
				m.repoPicker.rebuild(&m)
			}
		}
		return m, nil

	case RepoInfoMsg:
		if msg.RequestedRepo != "" && msg.RequestedRepo != m.settings.Repo {
			return m, nil
		}
		if msg.Err != nil {
			m.toastf("repo: %s", shortErr(msg.Err))
			return m, nil
		}
		m.repo = msg.Repo
		m.rebuildBranchList()
		// NOTE: an empty branchSel means ALL branches (the script's
		// behaviour) — never silently restrict to the default branch,
		// it hides nearly every run on repos that build from others.
		return m, nil

	case WorkflowsMsg:
		if msg.Repo != "" && msg.Repo != m.settings.Repo {
			return m, nil
		}
		if msg.Err != nil {
			if m.modal == ModalPickWorkflow && m.workflowPicker != nil {
				m.workflowPicker.setOptions(&m, m.buildWorkflowOptions(), msg.Err)
			}
			m.toastf("workflows: %s", shortErr(msg.Err))
			return m, nil
		}
		m.wfCatalog = msg.List
		items := []components.Item{{ID: "", Label: "Any workflow", Rendered: m.st.AccentAlt.Render("◆ Any workflow")}}
		for _, w := range msg.List {
			items = append(items, components.Item{ID: w.Name, Label: w.Name})
			m.wfPaths[w.Name] = strings.TrimPrefix(w.Path, "/")
		}
		m.workflows.SetItems(items)
		m.workflows.SelectByID(m.settings.Workflow)
		if m.modal == ModalPickWorkflow && m.workflowPicker != nil {
			m.workflowPicker.setOptions(&m, m.buildWorkflowOptions(), nil)
		}
		if wf := m.settings.Workflow; wf != "" {
			found := false
			for _, it := range m.workflows.Filtered() {
				if n, _ := it.ID.(string); n == wf {
					found = true
					break
				}
			}
			if !found && !m.wfMissToasted {
				m.wfMissToasted = true
				m.toastf("workflow %q not in this repo — showing all runs", wf)
			}
		}
		return m, m.fetchWorkflowForSelection()

	case WorkflowFileMsg:
		if msg.Repo != "" && msg.Repo != m.settings.Repo {
			return m, nil
		}
		delete(m.wfFetching, msg.Path)
		if msg.Err == nil {
			m.wfCache[msg.Path] = msg.YAML
		} else {
			m.wfCache[msg.Path] = "__ERR__" + msg.Err.Error()
		}
		return m, nil

	case RunsMsg:
		if msg.Repo != "" && (msg.Repo != m.settings.Repo || msg.Workflow != m.settings.Workflow || msg.Branch != m.branchSel) {
			return m, nil
		}
		m.runsLoaded = true
		if msg.Err != nil {
			m.toastf("runs: %s", shortErr(msg.Err))
			m.runs.SetItems(nil)
			m.busy = false
			m.busyMsg = ""
			if m.usingReleaseAssets() {
				m.syncReleaseArtifactItems()
			}
			return m, nil
		}
		m.setRuns(msg.List)
		m.rememberRunBranches(msg.List)
		m.rebuildBranchList()
		m.syncBuildSelection()
		return m, m.fetchArtifactsForSelection()

	case ArtifactsMsg:
		if msg.Repo != "" && msg.Repo != m.settings.Repo {
			return m, nil
		}
		if msg.Err != nil {
			m.artifactErrors[msg.RunID] = shortErr(msg.Err)
			m.toastf("artifacts: %s", shortErr(msg.Err))
			delete(m.fetching, msg.RunID)
			return m, nil
		}
		delete(m.fetching, msg.RunID)
		delete(m.artifactErrors, msg.RunID)
		arts := m.scoreArtifacts(msg.List)
		m.runCache[msg.RunID] = arts
		if sel, ok := m.selectedRun(); ok && sel.ID == msg.RunID {
			if m.usingReleaseAssets() {
				m.syncReleaseArtifactItems()
			} else {
				m.setArtifactItems(arts)
			}
		}
		return m, nil

	case ReleasesMsg:
		if msg.Repo != "" && msg.Repo != m.settings.Repo {
			return m, nil
		}
		if msg.Err == nil {
			m.releases = msg.List
			if m.usingReleaseAssets() {
				m.syncReleaseArtifactItems()
			}
		}
		return m, nil

	case BranchesMsg:
		if msg.Repo != "" && !strings.EqualFold(msg.Repo, m.settings.Repo) {
			return m, nil
		}
		m.branchLoading = false
		if msg.Err != nil {
			if m.modal == ModalPickBranch && m.branchPicker != nil {
				m.branchPicker.setOptions(&m, m.buildBranchOptions(m.apiBranches), msg.Err)
			}
			m.toastf("branches: %s", shortErr(msg.Err))
			return m, nil
		}
		m.apiBranches = msg.List
		m.rebuildBranchList()
		if m.modal == ModalPickBranch && m.branchPicker != nil {
			m.branchPicker.setOptions(&m, m.buildBranchOptions(msg.List), nil)
		}
		return m, nil

	case LocalBranchesMsg:
		if !strings.EqualFold(msg.Repo, m.settings.Repo) {
			return m, nil
		}
		m.localBranches = msg.Names
		m.rebuildBranchList()
		if m.modal == ModalPickBranch && m.branchPicker != nil {
			m.branchPicker.setOptions(&m, m.buildBranchOptions(m.apiBranches), nil)
			m.branchPicker.loading = m.branchLoading
		}
		return m, nil

	case SearchResultsMsg:
		if m.modal == ModalPickRepo && m.repoPicker != nil && (msg.Seq == 0 || msg.Seq == m.searchSeq) {
			m.repoPicker.setSearch(&m, msg.Query, msg.Hits)
		}
		return m, nil

	case UserReposMsg:
		if msg.Seq != 0 && msg.Seq != m.userReposSeq {
			return m, nil
		}
		m.userReposLoading = false
		if msg.Err == nil {
			m.cachedUserRepos = msg.List
			m.userReposLoaded = true
		}
		if m.modal == ModalPickRepo && m.repoPicker != nil {
			m.repoPicker.setUserRepos(&m, m.cachedUserRepos, msg.Err)
		}
		return m, nil

	case DownloadProgressMsg:
		if m.task != nil {
			m.task.setProgress(msg.Received, msg.Total)
		}
		return m, waitProgress(m.progCh)

	case DownloadDoneMsg:
		if msg.Err != nil {
			m.pendingInstall = false
			if m.task != nil {
				m.task.fail(msg.Err)
			} else {
				m.toastf("download failed: %s", shortErr(msg.Err))
			}
			m.busy = false
			return m, nil
		}
		m.downloadedAPK = msg.APK
		m.haveDownload = true
		m.busy = false
		m.detailLog(fmt.Sprintf("saved %s (%s)", msg.APK.Path, ghapi.HumanBytes(msg.APK.Size)))
		if m.pendingInstall {
			m.pendingInstall = false
			cmd := m.startInstall(msg.APK.Path)
			return m, cmd
		}
		if m.task != nil {
			m.task.downloadDone(msg, m.dev.CanInstall)
		}
		return m, nil

	case InstallPreparedMsg:
		if msg.QueryPkg != "" && (msg.Found || msg.App.Queryable) {
			if m.instPkg == "" || m.instPkg == msg.QueryPkg {
				m.instPkg = msg.QueryPkg
				m.instApp = msg.App
				m.instFound = msg.Found
			}
		}
		cmd := m.finishStartInstall(msg.Path, msg.Meta, msg.App, msg.Found)
		return m, cmd

	case InstallLogMsg:
		if m.task != nil {
			m.task.addLog(msg.Line)
		}
		m.detailLog(msg.Line)
		return m, waitInstallLog(m.logCh)

	case InstallDoneMsg:
		if m.task != nil {
			m.task.installDone(msg.Result, msg.Err)
		}
		var cmds []tea.Cmd
		meta := m.downloadedAPK.Meta
		pkg := m.instPkg
		if pkg == "" && meta.HaveInfo {
			pkg = meta.Info.Package
			m.instPkg = pkg
		}
		switch {
		case msg.Err != nil || msg.Result.Outcome == device.OutcomeFailed:
			if msg.Err != nil {
				m.toastf("install failed: %s", shortErr(msg.Err))
			} else {
				m.toastf("install failed")
			}
		case msg.Result.Outcome == device.OutcomeLaunched:
			m.toastf("installer launched (%s) — confirm on device", msg.Result.Via)
			if m.settings.VerifyInstall && pkg != "" && meta.HaveInfo {
				cmds = append(cmds, verifyInstallCmd(pkg, meta.Info.VersionCode, 30*time.Second))
			}
		default:
			m.toastf("installed via %s ✓", msg.Result.Via)
			m.recordAPKHistory(meta)
		}
		if pkg != "" {
			cmds = append(cmds, queryPackageCmd(pkg))
		}
		return m, tea.Batch(cmds...)

	case InstallVerifyMsg:
		if msg.Result.Outcome == device.VerifyConfirmed {
			m.instPkg = msg.Pkg
			m.instApp = msg.Result.App
			m.instFound = true
			m.recordAPKHistory(m.downloadedAPK.Meta)
			if m.task != nil && m.task.phase == "launched" {
				m.task.installRes.Outcome = device.OutcomeInstalled
				m.task.phase = "done"
				m.task.addLog(msg.Result.Detail)
			}
			m.toastf("verified %s on device ✓", msg.Pkg)
		} else if m.task != nil && m.task.phase == "launched" && msg.Result.Detail != "" {
			m.task.addLog(msg.Result.Detail)
		}
		return m, nil

	case repoSearchDebounced:
		if m.modal == ModalPickRepo && m.repoPicker != nil && msg.seq == m.searchSeq {
			return m, searchReposCmd(msg.query, msg.seq)
		}
		return m, nil

	case DoctorDoneMsg:
		if m.modal == ModalDoctor && m.doctor != nil {
			m.doctor.Rows = msg.Rows
			m.doctor.ready = true
		}
		return m, nil

	case RefreshTickMsg:
		if m.screen != ScreenDashboard || m.modal != ModalNone {
			return m, refreshTickCmd(m.effectiveRefreshSecs())
		}
		cmds := []tea.Cmd{refreshTickCmd(m.effectiveRefreshSecs())}
		if m.settings.Repo != "" {
			cmds = append(cmds, fetchRunsCmd(m.settings.Repo, m.settings.Workflow, m.branchSel, m.settings.RunLimit))
		}
		return m, tea.Batch(cmds...)

	case ClearToastMsg:
		if time.Now().After(m.toastUntil) {
			m.toast = ""
		}
		return m, nil

	case FatalErrMsg:
		m.toastf("%s: %s", msg.Context, shortErr(msg.Err))
		return m, nil
	}

	// route remaining messages into the active modal sub-component
	if m.modal == ModalEditRepo {
		var cmd tea.Cmd
		m.editInput, cmd = m.editInput.Update(msg)
		return m, cmd
	}
	if m.modal == ModalPickRepo && m.repoPicker != nil {
		cmd := m.repoPicker.update(&m, msg)
		return m, cmd
	}
	if m.modal == ModalPickBranch && m.branchPicker != nil {
		return m, m.branchPicker.update(msg)
	}
	if m.modal == ModalPickWorkflow && m.workflowPicker != nil {
		return m, m.workflowPicker.update(msg)
	}
	if m.modal == ModalTask && m.task != nil {
		return m, m.task.update(msg)
	}
	return m, nil
}

func ghapiErr(err error) error {
	if err == nil {
		return nil
	}
	if err == ghapi.ErrNoGH || strings.Contains(err.Error(), "not found") {
		return ghapi.ErrNoGH
	}
	return err
}

func shortErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if i := strings.Index(s, "gh api"); i >= 0 {
		// trim the noisy prefix
	}
	if len(s) > 90 {
		s = s[:87] + "…"
	}
	return s
}

// bootstrapDashboard runs once after auth succeeds.
func (m *Model) bootstrapDashboard() tea.Cmd {
	var cmds []tea.Cmd
	repo := m.settings.Repo
	if repo == "" {
		if r := config.LoadRecents(); len(r) > 0 {
			repo = r[0].Repo
		}
	}
	if repo != "" {
		m.setRepo(repo, false)
		cmds = append(cmds, m.refreshAll()...)
	}
	if repo == "" {
		// nothing to load — open the repo picker right away
		cmds = append(cmds, m.openRepoPicker())
	}
	if m.instPkg == "" && m.dev.OS == "android" {
		// leave empty; user can set package in settings
	}
	return tea.Batch(cmds...)
}

// setRepo switches the active repository, invalidating cached data.
// Per-repo config re-applies on top, but session-live choices
// (theme, watch) survive.
func (m *Model) setRepo(repo string, persist bool) {
	m.navRepo = ""
	m.repoBranchFocused = false
	m.branchTouchActive = false
	m.navBranch = ""
	m.navBranchSet = false
	m.apiBranches = nil
	m.localBranches = nil
	m.recentBranches = nil
	m.branchList.Filter = ""
	m.branchList.SetItems(nil)
	m.repo = ghapi.Repo{}
	m.navWorkflow = ""
	m.navWorkflowSet = false
	theme, watch, limit := m.settings.Theme, m.settings.Watch, m.settings.RunLimit
	m.settings = config.LoadForRepo(repo)
	m.settings.Repo = repo
	if theme != "" {
		m.settings.Theme = theme
	}
	m.settings.Watch = watch
	if limit > 0 {
		m.settings.RunLimit = limit
	}
	m.branchSel = pickNonEmpty(m.opts.Settings.Branch, m.settings.Branch)
	m.settings.Branch = m.branchSel
	m.branchLoading = true
	m.runCache = map[int64][]ghapi.Artifact{}
	m.wfPaths = map[string]string{}
	m.wfCache = map[string]string{}
	m.wfFetching = map[string]bool{}
	m.wfCatalog = nil
	m.wfMissToasted = false
	m.wfScroll = 0
	m.wfYAMLFocused = false
	m.buildDetail = false
	m.fetching = map[int64]bool{}
	m.artifactErrors = map[int64]string{}
	m.selectedRunID = 0
	m.selectedArtID = 0
	m.runsLoaded = false
	m.runs.SetItems(nil)
	m.arts.SetItems(nil)
	m.workflows.SetItems(nil)
	m.releases = nil
	m.rebuildBranchList()
	m.loadRecents(repo)
	if persist {
		config.TouchRecent(repo)
	}
}

func pickNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// refreshAll refetches the pieces that depend on the selected repo.
func (m *Model) refreshAll() []tea.Cmd {
	repo := m.settings.Repo
	if repo == "" {
		return nil
	}
	m.busy = true
	m.branchLoading = true
	m.busyMsg = "loading " + repo
	cfg := m.settings
	return []tea.Cmd{
		m.spin.Tick, // re-arm the gated spinner chain while work streams in
		fetchRepoCmd(repo),
		fetchBranchesCmd(repo),
		fetchLocalBranchesCmd(repo),
		fetchWorkflowsCmd(repo),
		fetchRunsCmd(repo, cfg.Workflow, m.branchSel, cfg.RunLimit),
		fetchReleasesCmd(repo),
		tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ClearToastMsg{} }),
	}
}

func (m *Model) effectiveRefreshSecs() int {
	// fast ticks while something is in flight, the configured cadence otherwise
	if m.watchActive() {
		return 15
	}
	if m.refreshSecs > 0 {
		return m.refreshSecs
	}
	return 0
}

// watchActive reports whether any listed run is still running.
func (m *Model) watchActive() bool {
	for _, it := range m.runs.Items {
		if r, ok := it.ID.(runRow); ok {
			if r.run.Active() {
				return true
			}
		}
	}
	return false
}

// runRow is the payload of a Runs list item.
type runRow struct {
	run ghapi.Run
}

// setRuns rebuilds the runs list preserving cursor position by run id.
func (m *Model) setRuns(list []ghapi.Run) {
	prevID := int64(0)
	if sel, ok := m.runs.Selected(); ok {
		if rr, ok := sel.ID.(runRow); ok {
			prevID = rr.run.ID
		}
	}
	items := make([]components.Item, 0, len(list))
	for _, r := range list {
		items = append(items, components.Item{
			ID:    runRow{r},
			Label: fmt.Sprintf("#%d %s %s %s %s %s", r.RunNumber, firstLineS(r.DisplayTitle), r.HeadBranch, r.Name, r.Actor.Login, shortSHA(r.HeadSHA)),
		})
	}
	m.runs.SetItems(items)
	if prevID != 0 {
		m.runs.SelectByID(runRowByID(m.runs.Items, prevID))
	}
	if len(list) > 0 {
		found := false
		for _, r := range list {
			if r.ID == m.selectedRunID {
				found = true
				break
			}
		}
		if !found {
			m.selectedRunID = list[0].ID
		}
	} else {
		m.selectedRunID = 0
	}
	m.busy = false
	m.busyMsg = ""
}

func runRowByID(items []components.Item, id int64) runRow {
	for _, it := range items {
		if rr, ok := it.ID.(runRow); ok && rr.run.ID == id {
			return rr
		}
	}
	return runRow{}
}

func firstLineS(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// scoreArtifacts ranks a run's artifacts for this device and prefs,
// sorted best-first (mirrors select_candidates in the script).
func (m *Model) scoreArtifacts(list []ghapi.Artifact) []ghapi.Artifact {
	prefs := score.Prefs{Arch: m.settings.Arch, Variant: m.settings.Variant}
	dev := score.Device{ABIs: m.dev.ABIs, SDK: m.dev.SDK}
	out := make([]ghapi.Artifact, 0, len(list))
	for _, a := range list {
		if a.Expired {
			continue
		}
		if m.settings.ArtifactName != "" && !strings.EqualFold(a.Name, m.settings.ArtifactName) {
			continue
		}
		if m.settings.ArtifactName == "" {
			if m.settings.ArtifactPattern != "" && !globCIList(m.settings.ArtifactPattern, a.Name) {
				continue
			}
			if m.settings.ExcludePattern != "" && globCIList(m.settings.ExcludePattern, a.Name) {
				continue
			}
		}
		a.Score = score.ArtifactRank(a.Name, prefs, dev)
		a.Rejected = !score.AllowedByPolicy(a.Name, prefs, dev)
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rejected != out[j].Rejected {
			return !out[i].Rejected
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func globCIList(patterns, name string) bool {
	for _, p := range strings.Split(patterns, ",") {
		p = strings.TrimSpace(p)
		if p != "" && globCI(p, name) {
			return true
		}
	}
	return false
}

func globCI(pattern, name string) bool {
	p := strings.ToLower(pattern)
	n := strings.ToLower(name)
	if !strings.ContainsAny(p, "*?[") {
		return strings.Contains(n, p)
	}
	ok, err := matchPath(p, n)
	if err != nil {
		return strings.Contains(n, strings.Trim(p, "*"))
	}
	return ok
}

func matchPath(pattern, name string) (bool, error) {
	// path.Match without importing path for tiny helper
	return wildcardMatch(pattern, name), nil
}

// wildcardMatch supports * and ? only.
func wildcardMatch(pattern, name string) bool {
	p, n := []rune(pattern), []rune(name)
	pi, ni := 0, 0
	star := -1
	match := 0
	for ni < len(n) {
		if pi < len(p) && (p[pi] == '?' || p[pi] == n[ni]) {
			pi++
			ni++
		} else if pi < len(p) && p[pi] == '*' {
			star = pi
			match = ni
			pi++
		} else if star != -1 {
			pi = star + 1
			match++
			ni = match
		} else {
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// setArtifactItems rebuilds the Artifacts panel rows.
func (m *Model) setArtifactItems(arts []ghapi.Artifact) {
	items := make([]components.Item, 0, len(arts))
	for i, a := range arts {
		items = append(items, components.Item{
			ID:    a.ID,
			Label: a.Name,
		})
		_ = i
	}
	m.arts.SetItems(items)
	if len(arts) > 0 {
		found := false
		for _, a := range arts {
			if a.ID == m.selectedArtID {
				found = true
				break
			}
		}
		if !found {
			m.selectedArtID = arts[0].ID
		}
		m.arts.SelectByID(m.selectedArtID)
		m.syncArtifactSelection()
	} else {
		m.selectedArtID = 0
	}
}

// selectedRun returns the currently chosen run (for artifacts/details).
func (m *Model) selectedRun() (ghapi.Run, bool) {
	if m.selectedRunID != 0 {
		for _, it := range m.runs.Items {
			if rr, ok := it.ID.(runRow); ok && rr.run.ID == m.selectedRunID {
				return rr.run, true
			}
		}
	}
	return m.cursorRun()
}

// cursorRun returns the run currently under the cursor (moving through).
func (m *Model) cursorRun() (ghapi.Run, bool) {
	sel, ok := m.runs.Selected()
	if !ok {
		return ghapi.Run{}, false
	}
	rr, ok := sel.ID.(runRow)
	if !ok {
		return ghapi.Run{}, false
	}
	return rr.run, true
}

func (m *Model) scrollWorkflowYAML(d int) {
	sel, ok := m.workflows.Selected()
	if !ok {
		return
	}
	name, _ := sel.ID.(string)
	path := m.wfPaths[name]
	yaml, ok := m.wfCache[path]
	if !ok {
		return
	}
	rawLines := strings.Split(yaml, "\n")
	for len(rawLines) > 0 && rawLines[len(rawLines)-1] == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}
	totalLines := len(rawLines)
	var subH int
	if m.compact {
		h := m.height - 3
		if h < 5 {
			h = 5
		}
		_, subH = flowsSplit(h, len(m.workflows.Items))
	} else {
		rs := m.panelRects()
		_, subH = flowsSplit(rs[PanelWorkflows].h, len(m.workflows.Items))
	}
	yamlH := subH - 3
	if yamlH < 1 {
		yamlH = 1
	}
	maxScroll := totalLines - yamlH
	if maxScroll < 0 {
		maxScroll = 0
	}
	m.wfScroll += d
	if m.wfScroll < 0 {
		m.wfScroll = 0
	}
	if m.wfScroll > maxScroll {
		m.wfScroll = maxScroll
	}
}

// fetchWorkflowForSelection downloads the YAML for the highlighted workflow
// (once), so the preview pane under the list stays live.
func (m *Model) fetchWorkflowForSelection() tea.Cmd {
	sel, ok := m.workflows.Selected()
	if !ok {
		return nil
	}
	name, _ := sel.ID.(string)
	if name == "" || m.settings.Repo == "" {
		return nil // "◆ any workflow" shows the intro panel
	}
	path := m.wfPaths[name]
	if path == "" {
		return nil
	}
	if _, ok := m.wfCache[path]; ok {
		return nil
	}
	if m.wfFetching[path] {
		return nil
	}
	m.wfFetching[path] = true
	return fetchWorkflowFileCmd(m.settings.Repo, path)
}

// fetchArtifactsForSelection fetches artifacts for the highlighted run if needed.
func (m *Model) fetchArtifactsForSelection() tea.Cmd {
	if m.settings.Source == "releases" && len(m.rankedReleases()) > 0 {
		m.syncReleaseArtifactItems()
		return nil
	}
	r, ok := m.selectedRun()
	if !ok || m.settings.Repo == "" {
		if m.usingReleaseAssets() {
			m.syncReleaseArtifactItems()
		} else {
			m.arts.SetItems(nil)
			m.selectedArtID = 0
		}
		return nil
	}
	if cached, ok := m.runCache[r.ID]; ok {
		if m.usingReleaseAssets() {
			m.syncReleaseArtifactItems()
		} else {
			m.setArtifactItems(cached)
		}
		return nil
	}
	if m.fetching[r.ID] {
		return nil
	}
	m.fetching[r.ID] = true
	delete(m.artifactErrors, r.ID)
	m.arts.SetItems(nil)
	return tea.Batch(fetchArtifactsCmd(m.settings.Repo, r.ID), m.spin.Tick)
}

// invalidateArtifacts drops cached scoring so artifacts get re-ranked.
func (m *Model) invalidateArtifacts() {
	m.runCache = map[int64][]ghapi.Artifact{}
	m.arts.SetItems(nil)
}

func (m *Model) toastf(format string, a ...any) {
	m.toast = fmt.Sprintf(format, a...)
	m.toastUntil = time.Now().Add(4 * time.Second)
}

func (m *Model) detailLog(line string) {
	m.logLines = append(m.logLines, line)
	if len(m.logLines) > 100 {
		m.logLines = m.logLines[len(m.logLines)-100:]
	}
}

func (m *Model) selectedArtifact() (ghapi.Artifact, bool) {
	r, ok := m.selectedRun()
	if !ok {
		return ghapi.Artifact{}, false
	}
	arts, ok := m.runCache[r.ID]
	if !ok || len(arts) == 0 {
		return ghapi.Artifact{}, false
	}
	if m.selectedArtID != 0 {
		for _, a := range arts {
			if a.ID == m.selectedArtID {
				return a, true
			}
		}
	}
	return m.cursorArtifact()
}

func (m *Model) cursorArtifact() (ghapi.Artifact, bool) {
	r, ok := m.selectedRun()
	if !ok {
		return ghapi.Artifact{}, false
	}
	it, ok := m.arts.Selected()
	if !ok {
		return ghapi.Artifact{}, false
	}
	id, ok := it.ID.(int64)
	if !ok {
		return ghapi.Artifact{}, false
	}
	for _, a := range m.runCache[r.ID] {
		if a.ID == id {
			return a, true
		}
	}
	return ghapi.Artifact{}, false
}

// ───────────────────────────── styles helper ─────────────────────────────

func (m *Model) applyTheme(name string) {
	m.settings.Theme = name
	m.st = theme.New(theme.Get(name))
	m.spin.Style = m.st.Accent
}

func (m *Model) recordAPKHistory(meta apk.Inspection) {
	if m.settings.Repo == "" || (!meta.HaveInfo && meta.Cert.SHA256 == "") {
		return
	}
	entry := config.HistoryEntry{
		SHA: meta.LocalSHA256,
		Sig: meta.Cert.SHA256,
	}
	if meta.HaveInfo {
		entry.Pkg = meta.Info.Package
		entry.VCode = meta.Info.VersionCode
		entry.VName = meta.Info.VersionName
	}
	_ = config.RecordInstall(m.settings.Repo, entry)
}

func (m *Model) rankedReleases() []ghapi.RankedReleaseAsset {
	if len(m.releases) == 0 {
		return nil
	}
	prefs := score.Prefs{Arch: m.settings.Arch, Variant: m.settings.Variant}
	dev := score.Device{ABIs: m.dev.ABIs, SDK: m.dev.SDK}
	return ghapi.RankReleaseAssets(
		m.releases,
		m.settings.ArtifactPattern,
		m.settings.FilePattern,
		m.settings.ExcludePattern,
		prefs,
		dev,
	)
}

// usingReleaseAssets reports whether PanelArtifacts should present GitHub
// Release assets (either explicitly via source=releases, or as auto fallback
// when workflow artifacts are unavailable).
func (m *Model) usingReleaseAssets() bool {
	if m.settings.Source == "actions" {
		return false
	}
	rels := m.rankedReleases()
	if len(rels) == 0 {
		return false
	}
	if m.settings.Source == "releases" {
		return true
	}
	r, ok := m.selectedRun()
	if !ok {
		return m.runsLoaded
	}
	if m.fetching[r.ID] {
		return false
	}
	arts, cached := m.runCache[r.ID]
	return cached && len(arts) == 0
}

func (m *Model) syncReleaseArtifactItems() {
	rels := m.rankedReleases()
	items := make([]components.Item, 0, len(rels))
	for _, ra := range rels {
		items = append(items, components.Item{
			ID:    -ra.Asset.ID,
			Label: ra.Asset.Name,
		})
	}
	m.arts.SetItems(items)
	if len(rels) > 0 {
		found := false
		for _, ra := range rels {
			if -ra.Asset.ID == m.selectedArtID {
				found = true
				break
			}
		}
		if !found {
			m.selectedArtID = -rels[0].Asset.ID
		}
		m.arts.SelectByID(m.selectedArtID)
		m.syncArtifactSelection()
	} else {
		m.selectedArtID = 0
	}
}

func (m *Model) selectedReleaseAsset() (ghapi.RankedReleaseAsset, bool) {
	rels := m.rankedReleases()
	if len(rels) == 0 {
		return ghapi.RankedReleaseAsset{}, false
	}
	if m.selectedArtID < 0 {
		for _, ra := range rels {
			if -ra.Asset.ID == m.selectedArtID {
				return ra, true
			}
		}
	}
	return m.cursorReleaseAsset()
}

func (m *Model) cursorReleaseAsset() (ghapi.RankedReleaseAsset, bool) {
	it, ok := m.arts.Selected()
	if !ok {
		return ghapi.RankedReleaseAsset{}, false
	}
	id, ok := it.ID.(int64)
	if !ok {
		return ghapi.RankedReleaseAsset{}, false
	}
	for _, ra := range m.rankedReleases() {
		if -ra.Asset.ID == id {
			return ra, true
		}
	}
	return ghapi.RankedReleaseAsset{}, false
}
