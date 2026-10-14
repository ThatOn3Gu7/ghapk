package tui

// KeyHints renders "[k]ey action" pairs for the status bar.
type KeyHint struct {
	Key  string
	Desc string
}

// Dashboard global key bindings (documented in the help screen too).
var (
	KeyQuit        = "q / ctrl+c"
	KeyHelp        = "?"
	KeyRefresh     = "r"
	KeyDownload    = "d"
	KeyInstall     = "i"
	KeyOpenBrowser = "o"
	KeyFilter      = "/"
	KeyRepoPicker  = "R"
	KeyBranch      = "B"
	KeyWatch       = "w"
	KeySettings    = "s"
	KeyTheme       = "t"
	KeyDoctor      = "D"
	KeySuspend     = "ctrl+z"
)

// FocusOrder is the tab-cycle order of dashboard panels. Runs carries its
// own detail sub-pane, so the old separate Details panel is gone.
var FocusOrder = []PanelID{
	PanelRepos, PanelWorkflows, PanelRuns, PanelArtifacts, PanelDevice,
}

// PanelID identifies a dashboard panel.
type PanelID int

const (
	PanelRepos PanelID = iota
	PanelWorkflows
	PanelRuns
	PanelArtifacts
	PanelDevice
	NumPanels
)

// PanelNames is used as panel titles.
var PanelNames = map[PanelID]string{
	PanelRepos:     "Repositories",
	PanelWorkflows: "Workflows",
	PanelRuns:      "Runs",
	PanelArtifacts: "Artifacts",
	PanelDevice:    "Device",
}
