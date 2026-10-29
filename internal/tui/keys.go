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
	KeyTheme       = "t"
	KeyDoctor      = "D"
	KeySuspend     = "ctrl+z"
)

// FocusOrder is the tab-cycle order. Artifacts are a focus within Builds,
// not a separate screen.
var FocusOrder = []PanelID{
	PanelRepos, PanelWorkflows, PanelRuns, PanelDownloads, PanelDevice, PanelSettings,
}

// PanelID identifies a dashboard panel.
type PanelID int

const (
	PanelRepos PanelID = iota
	PanelWorkflows
	PanelRuns
	PanelArtifacts
	PanelDevice
	PanelDownloads
	PanelSettings
	NumPanels
)

// PanelNames is used as panel titles.
var PanelNames = map[PanelID]string{
	PanelRepos:     "Repositories",
	PanelWorkflows: "Workflows",
	PanelRuns:      "Builds",
	PanelArtifacts: "Artifacts",
	PanelDevice:    "Device",
	PanelDownloads: "Downloads",
	PanelSettings:  "Settings",
}
