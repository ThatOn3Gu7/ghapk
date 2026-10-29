package tui

import (
	"strings"

	"github.com/ThatOn3Gu7/ghapk/internal/config"
	"github.com/ThatOn3Gu7/ghapk/internal/device"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type SettingRow struct {
	Key, Label, Hint, Kind, Group string
	Values                        []string
}

var settingRows = []SettingRow{
	{Key: "branch", Label: "Branch", Group: "Source", Kind: "text", Hint: "Restrict runs to this branch; blank means all branches."},
	{Key: "workflow", Label: "Workflow", Group: "Source", Kind: "text", Hint: "Restrict runs to a workflow; blank means any workflow."},
	{Key: "source", Label: "Source", Group: "Source", Kind: "enum", Values: []string{"auto", "actions", "releases"}, Hint: "Choose Actions, releases, or automatic fallback."},
	{Key: "limit", Label: "Run limit", Group: "Source", Kind: "int", Hint: "Maximum number of runs to fetch (1–200)."},
	{Key: "artifact", Label: "Artifact name", Group: "APK selection", Kind: "text", Hint: "Select an exact artifact name; blank allows patterns."},
	{Key: "artifact-pattern", Label: "Artifact pattern", Group: "APK selection", Kind: "text", Hint: "Comma-separated artifact name globs."},
	{Key: "file", Label: "File pattern", Group: "APK selection", Kind: "text", Hint: "Glob for APK files in the artifact or release."},
	{Key: "exclude", Label: "Exclude", Group: "APK selection", Kind: "text", Hint: "Comma-separated globs to exclude artifacts or files."},
	{Key: "arch", Label: "Arch preference", Group: "APK selection", Kind: "enum", Values: []string{"auto", "any", "arm64", "armv7", "x86_64", "x86", "universal"}, Hint: "Choose the preferred architecture for APK scoring."},
	{Key: "variant", Label: "Variant", Group: "APK selection", Kind: "enum", Values: []string{"auto", "debug", "release", "any"}, Hint: "Choose the preferred build variant."},
	{Key: "all-arch", Label: "All architectures", Group: "APK selection", Kind: "bool", Hint: "Offer APKs for every architecture, not only this device."},
	{Key: "out", Label: "Output folder", Group: "APK selection", Kind: "text", Hint: "Also copy downloaded APKs to this folder; blank disables the extra copy."},
	{Key: "package", Label: "Package name", Group: "Install & safety", Kind: "text", Hint: "Expected installed package for safety checks."},
	{Key: "downgrade", Label: "Downgrade", Group: "Install & safety", Kind: "enum", Values: []string{"auto", "allow", "never"}, Hint: "Ask about, allow, or block version downgrades."},
	{Key: "pkg-check", Label: "Package check", Group: "Install & safety", Kind: "bool", Hint: "Warn when the APK package differs from the expected package."},
	{Key: "sig-check", Label: "Signature check", Group: "Install & safety", Kind: "bool", Hint: "Warn when the APK signing key differs from the installed app."},
	{Key: "identical-check", Label: "Same-version check", Group: "Install & safety", Kind: "bool", Hint: "Ask before reinstalling an already installed version."},
	{Key: "verify-digest", Label: "Verify SHA-256", Group: "Install & safety", Kind: "bool", Hint: "Verify downloaded artifacts against GitHub's digest when available."},
	{Key: "verify-install", Label: "Verify install", Group: "Install & safety", Kind: "bool", Hint: "Check the package version after installer handoff when possible."},
	{Key: "install-wait", Label: "Install wait", Group: "Install & safety", Kind: "int", Hint: "Seconds to wait for installation verification (1–3600)."},
	{Key: "keep", Label: "Keep cached APKs", Group: "Install & safety", Kind: "int", Hint: "Number of recent downloaded APKs to retain."},
	{Key: "keep-cache", Label: "Keep zip cache", Group: "Install & safety", Kind: "bool", Hint: "Keep downloaded archives for verified reuse."},
	{Key: "auto-refresh", Label: "Refresh seconds", Group: "Watching", Kind: "int", Hint: "Seconds between automatic refreshes; zero disables them."},
	{Key: "watch", Label: "Watch active runs", Group: "Watching", Kind: "bool", Hint: "Refresh faster while a run is in progress."},
	{Key: "notify-on", Label: "Notifications", Group: "Notifications", Kind: "enum", Values: []string{"always", "success", "failure", "never"}, Hint: "Choose which job outcomes trigger Termux notifications."},
	{Key: "speak", Label: "Speak outcome", Group: "Notifications", Kind: "bool", Hint: "Speak job outcomes using termux-tts-speak when installed."},
	{Key: "toast", Label: "Toast outcome", Group: "Notifications", Kind: "bool", Hint: "Show job outcomes using termux-toast when installed."},
	{Key: "glyphs", Label: "Glyphs", Group: "Interface", Kind: "enum", Values: []string{"auto", "unicode", "ascii"}, Hint: "Use automatic, Unicode, or ASCII symbols."},
	{Key: "mouse", Label: "Mouse and touch", Group: "Interface", Kind: "bool", Hint: "Enable mouse and touch; use the keyboard to turn this back on."},
	{Key: "exit-summary", Label: "Exit summary", Group: "Interface", Kind: "bool", Hint: "Print the saved APK path when leaving the app."},
	{Key: "theme", Label: "Theme", Group: "Interface", Kind: "enum", Values: []string{"action", "mocha", "emerald", "paper"}, Hint: "Choose the app's existing color scheme."},
}

type settingsLine struct {
	header string
	row    int
}
type SettingsUI struct {
	cursor, offset       int
	editing, drag, moved bool
	dragY                int
	input                textinput.Model
	scopeOverrides       map[string]bool // true means global; per setting for this session
}

func NewSettingsUI(m *Model) *SettingsUI {
	t := textinput.New()
	t.CharLimit = 512
	t.Width = 28
	return &SettingsUI{input: t}
}
func settingsLines() []settingsLine {
	var lines []settingsLine
	group := ""
	for i, r := range settingRows {
		if r.Group != group {
			group = r.Group
			lines = append(lines, settingsLine{header: group, row: -1})
		}
		lines = append(lines, settingsLine{row: i})
	}
	return lines
}
func (s *SettingsUI) lineIndex() int {
	for i, l := range settingsLines() {
		if l.row == s.cursor {
			return i
		}
	}
	return 0
}

// Scope defaults follow what the setting describes: repo-specific selection
// rules stay with a repo; interface, notifications, and policies are global.
func defaultGlobalScope(key string) bool {
	switch key {
	case "branch", "workflow", "source", "artifact", "artifact-pattern", "file", "exclude", "package":
		return false
	default:
		return true
	}
}
func (s *SettingsUI) globalFor(m *Model, key string) bool {
	if m.settings.Repo == "" {
		return true
	}
	if global, ok := s.scopeOverrides[key]; ok {
		return global
	}
	return defaultGlobalScope(key)
}
func (s *SettingsUI) scopeFor(m *Model, key string) string {
	if s.globalFor(m, key) {
		return "global"
	}
	return "this repo"
}
func (s *SettingsUI) scope(m *Model) string {
	return s.scopeFor(m, settingRows[s.cursor].Key)
}
func (s *SettingsUI) targetValue(m *Model, row SettingRow) string {
	repo := ""
	if !s.globalFor(m, row.Key) {
		repo = m.settings.Repo
		// Picker choices are the current per-repo branch/workflow value.
		if (row.Key == "branch" || row.Key == "workflow") && s.source(m, row.Key) != "env" && s.source(m, row.Key) != "cli" {
			return config.Value(m.settings, row.Key)
		}
	}
	return config.Value(config.LoadSavedScope(repo), row.Key)
}
func (s *SettingsUI) source(m *Model, key string) string {
	if _, ok := m.opts.CLIOverrides[key]; ok {
		return "cli"
	}
	return config.SettingSource(m.settings.Repo, key)
}
func (s *SettingsUI) value(m *Model, row SettingRow) string {
	v := config.Value(m.settings, row.Key)
	if row.Kind == "bool" {
		if v == "1" {
			return "on"
		}
		return "off"
	}
	if v == "" {
		return "—"
	}
	return v
}
func (s *SettingsUI) move(n int) { s.cursor = max(0, min(len(settingRows)-1, s.cursor+n)) }
func (s *SettingsUI) listHeight(m *Model) int {
	h := m.height - 6
	if m.width < 96 && h >= 12 {
		h -= 6
	}
	return max(1, h)
}
func (s *SettingsUI) adjust(m *Model) {
	h := s.listHeight(m)
	i := s.lineIndex()
	if i < s.offset {
		s.offset = i
	}
	if i >= s.offset+h {
		s.offset = i - h + 1
	}
	lines := settingsLines()
	if i > 0 && lines[i-1].row < 0 && s.offset == i {
		s.offset = i - 1
	}
}
func (s *SettingsUI) change(m *Model, raw *string) tea.Cmd {
	return s.changeRow(m, settingRows[s.cursor], raw)
}
func (s *SettingsUI) changeRow(m *Model, row SettingRow, raw *string) tea.Cmd {
	scope := ""
	if s.scopeFor(m, row.Key) == "this repo" {
		scope = m.settings.Repo
	}
	if raw != nil {
		tmp := m.settings
		if err := config.SetValue(&tmp, row.Key, *raw); err != nil {
			m.toastf("invalid value: %s", err)
			return nil
		}
	}
	op := "changed"
	if raw == nil {
		op = "reset"
	}
	if err := config.SaveSetting(scope, row.Key, raw); err != nil {
		m.auditSetting(row.Key, s.scopeFor(m, row.Key), "save failed")
		m.toastf("save failed: %s", err)
		return nil
	}
	m.auditSetting(row.Key, s.scopeFor(m, row.Key), op)
	isFilter := row.Key == "branch" || row.Key == "workflow"
	preserveSelection := isFilter && scope == "" && config.HasRepoSetting(m.settings.Repo, row.Key)
	if isFilter && m.settings.Repo != "" && !preserveSelection {
		if err := config.ClearRememberedFilter(m.settings.Repo, row.Key); err != nil {
			m.toastf("saved, but could not clear remembered choice: %s", shortErr(err))
			return nil
		}
	}
	effective := config.LoadForRepo(m.settings.Repo)
	value := config.Value(effective, row.Key)
	if preserveSelection {
		value = config.Value(m.settings, row.Key)
	}
	if v, ok := m.opts.CLIOverrides[row.Key]; ok {
		value = v
	}
	if err := config.SetValue(&m.settings, row.Key, value); err != nil {
		m.toastf("setting saved, but could not apply: %s", err)
		return nil
	}
	src := s.source(m, row.Key)
	if (scope == "" && src == "repo") || src == "env" || src == "cli" {
		m.toastf("saved; %s override is active", src)
	} else {
		m.toastf("saved %s (%s)", row.Label, s.scopeFor(m, row.Key))
	}
	switch row.Key {
	case "theme", "glyphs":
		m.applyTheme(m.settings.Theme)
	case "mouse":
		if m.settings.Mouse {
			return tea.EnableMouseCellMotion
		}
		return tea.DisableMouse
	case "branch":
		m.branchSel = m.settings.Branch
		m.rebuildBranchList()
		m.branchList.SelectByID(m.branchSel)
	case "workflow":
		m.workflowSel = m.settings.Workflow
		m.selectWorkflowOption(m.workflowSel)
	case "package":
		if m.instPkg != m.settings.PackageName {
			m.instPkg = m.settings.PackageName
			m.instApp = device.InstalledApp{}
			m.instFound = false
		}
		return queryPackageCmd(m.instPkg)
	case "auto-refresh", "watch":
		m.refreshSecs = m.settings.AutoRefreshSecs
		m.refreshSeq++
		return refreshTickCmd(m.effectiveRefreshSecs(), m.refreshSeq)
	}
	switch row.Key {
	case "branch", "workflow", "source", "limit":
		return tea.Batch(m.refreshAll()...)
	case "artifact", "artifact-pattern", "file", "exclude", "arch", "variant", "all-arch":
		m.invalidateArtifacts()
		return m.fetchArtifactsForSelection()
	}
	return nil
}
func (s *SettingsUI) handleKey(msg tea.KeyMsg, m *Model) (tea.Model, tea.Cmd) {
	if s.editing {
		switch msg.String() {
		case "esc":
			s.editing = false
			s.input.Blur()
			return *m, nil
		case "enter":
			v := s.input.Value()
			tmp := m.settings
			if err := config.SetValue(&tmp, settingRows[s.cursor].Key, v); err != nil {
				m.toastf("invalid value: %s", err)
				return *m, nil
			}
			s.editing = false
			s.input.Blur()
			cmd := s.change(m, &v)
			return *m, cmd
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return *m, cmd
	}
	switch msg.String() {
	case "up", "k":
		s.move(-1)
	case "down", "j":
		s.move(1)
	case "pgup", "ctrl+u":
		s.move(-6)
	case "pgdown", "ctrl+d":
		s.move(6)
	case "g":
		s.cursor = 0
	case "G":
		s.cursor = len(settingRows) - 1
	case "s":
		if m.settings.Repo == "" {
			m.toastf("select a repo for repo-specific settings")
		} else {
			key := settingRows[s.cursor].Key
			if s.scopeOverrides == nil {
				s.scopeOverrides = make(map[string]bool)
			}
			s.scopeOverrides[key] = !s.globalFor(m, key)
			m.toastf("%s: save to %s", settingRows[s.cursor].Label, s.scope(m))
		}
	case "d":
		cmd := s.change(m, nil)
		return *m, cmd
	case "enter", " ", "right", "l", "left", "h":
		row := settingRows[s.cursor]
		cur := s.targetValue(m, row)
		if row.Kind == "text" || row.Kind == "int" {
			s.editing = true
			s.input.SetValue(cur)
			s.input.Placeholder = row.Label
			s.input.Focus()
			s.input.CursorEnd()
			return *m, textinput.Blink
		}
		v := "0"
		if row.Kind == "bool" {
			if cur != "1" {
				v = "1"
			}
		} else {
			idx := 0
			for i, x := range row.Values {
				if x == cur {
					idx = i
				}
			}
			dir := 1
			if msg.String() == "left" || msg.String() == "h" {
				dir = -1
			}
			v = row.Values[(idx+dir+len(row.Values))%len(row.Values)]
		}
		cmd := s.change(m, &v)
		return *m, cmd
	}
	s.adjust(m)
	return *m, nil
}

func (s *SettingsUI) render(m *Model, h int) string {
	w := m.width
	innerW := max(1, w-4)
	listH := h
	wide := w >= 96
	detailH := 0
	if wide {
		listH = h
	} else if h >= 14 {
		detailH = 6
		listH = h - detailH
	}
	listW := w
	if wide {
		listW = w * 58 / 100
	}
	lw := max(1, listW-4)
	s.adjust(m)
	lines := settingsLines()
	sources := config.SettingSources(m.settings.Repo)
	for key := range m.opts.CLIOverrides {
		sources[key] = "cli"
	}
	configured := config.LoadForRepo(m.settings.Repo)
	for _, key := range []string{"branch", "workflow"} {
		if sources[key] != "cli" && sources[key] != "env" && config.Value(configured, key) != config.Value(m.settings, key) {
			sources[key] = "selected"
		}
	}
	var list []string
	for i := s.offset; i < len(lines) && len(list) < max(1, listH-2); i++ {
		l := lines[i]
		if l.row < 0 {
			list = append(list, m.st.PanelTitleFoc.Render(components.Clamp(" "+l.header, lw)))
			continue
		}
		row := settingRows[l.row]
		value := s.value(m, row)
		source := sources[row.Key]
		if source == "" {
			source = "default"
		}
		suffix := " [" + source + "]"
		labelWidth := min(19, max(8, (lw-len(suffix)-2)/2))
		valueWidth := max(1, lw-1-labelWidth-1-len(suffix))
		line := components.Pad(components.Clamp(row.Label, labelWidth), labelWidth) + " " + components.Pad(components.Clamp(value, valueWidth), valueWidth) + suffix
		if s.editing && l.row == s.cursor {
			s.input.Width = max(6, lw-labelWidth-2)
			line = components.Pad(components.Clamp(row.Label, labelWidth), labelWidth) + " " + s.input.View()
		}
		line = components.Fit(components.Clamp(" "+line, lw), lw)
		if l.row == s.cursor {
			line = m.st.RowSel.Render(line)
		}
		list = append(list, line)
	}
	listBox := components.Panel{Title: "Settings", Badge: "save: " + s.scope(m), Focused: true, Width: listW, Height: listH, Content: strings.Join(list, "\n")}.Render(m.st)
	row := settingRows[s.cursor]
	target := s.targetValue(m, row)
	if target == "" {
		target = "—"
	}
	source := sources[row.Key]
	if source == "" {
		source = "default"
	}
	summary := "  save " + s.scope(m) + ": " + target + "\n  active [" + source + "]: " + s.value(m, row)
	detail := "  " + m.st.Bold.Render(row.Label) + "\n" + summary + "\n" + ansi.Wrap("  "+row.Hint, max(1, innerW-2), "") + "\n  default: " + config.Value(config.Defaults(), row.Key)
	if len(row.Values) > 0 {
		detail += "\n  choices: " + strings.Join(row.Values, ", ")
	}
	if wide {
		dw := w - listW
		detail = "  " + row.Label + "\n" + summary + "\n" + ansi.Wrap("  "+row.Hint, max(1, dw-4), "") + "\n  default: " + config.Value(config.Defaults(), row.Key)
		if len(row.Values) > 0 {
			detail += "\n" + ansi.Wrap("  choices: "+strings.Join(row.Values, ", "), max(1, dw-4), "")
		}
		box := components.Panel{Title: "About", Width: dw, Height: h, Content: detail}.Render(m.st)
		return lipgloss.JoinHorizontal(lipgloss.Top, listBox, box)
	}
	if detailH > 0 {
		box := components.Panel{Title: "About", Width: w, Height: detailH, Content: detail}.Render(m.st)
		return listBox + "\n" + box
	}
	return listBox
}
