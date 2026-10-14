package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/tui/components"
)

// SettingRow is one editable entry in the settings screen.
type SettingRow struct {
	Key    string
	Label  string
	Hint   string
	Kind   string // enum | bool | text | int
	Values []string
}

var settingRows = []SettingRow{
	{Key: "theme", Label: "Theme", Hint: "color scheme — ask t on the dashboard to cycle too", Kind: "enum", Values: []string{"action", "mocha", "emerald", "paper"}},
	{Key: "arch", Label: "Arch preference", Hint: "which ABI the APK should match", Kind: "enum", Values: []string{"auto", "any", "arm64", "armv7", "x86_64", "x86", "universal"}},
	{Key: "variant", Label: "Variant", Hint: "debug vs release builds", Kind: "enum", Values: []string{"auto", "debug", "release", "any"}},
	{Key: "source", Label: "Source", Hint: "where APKs come from", Kind: "enum", Values: []string{"auto", "actions", "releases"}},
	{Key: "downgrade", Label: "Downgrade", Hint: "may the installer replace a newer app?", Kind: "enum", Values: []string{"auto", "allow", "never"}},
	{Key: "branch", Label: "Branch", Hint: "restrict runs to this branch (B for picker)", Kind: "text"},
	{Key: "workflow", Label: "Workflow", Hint: "restrict to one workflow name", Kind: "text"},
	{Key: "artifact-pattern", Label: "Artifact pattern", Hint: "glob for artifact names, e.g. *debug*", Kind: "text"},
	{Key: "file", Label: "File pattern", Hint: "glob for files inside the zip", Kind: "text"},
	{Key: "package", Label: "Package name", Hint: "com.example.app — tracks the installed version", Kind: "text"},
	{Key: "limit", Label: "Run limit", Hint: "how many workflow runs to scan", Kind: "int"},
	{Key: "auto-refresh", Label: "Auto refresh", Hint: "seconds between refreshes, 0 = off", Kind: "int"},
	{Key: "no-open", Label: "Never open installer", Hint: "download only, never hand off", Kind: "bool"},
	{Key: "keep-cache", Label: "Keep cache", Hint: "keep downloaded zips between runs", Kind: "bool"},
	{Key: "watch", Label: "Watch runs", Hint: "fast refresh while a run is in flight", Kind: "bool"},
}

// SettingsUI is the state of the settings modal.
type SettingsUI struct {
	cursor  int
	offset  int
	editing bool
	input   textinput.Model
	saved   string // transient notice
}

func NewSettingsUI(m *Model) *SettingsUI {
	ti := textinput.New()
	ti.CharLimit = 120
	ti.Width = 40
	return &SettingsUI{input: ti}
}

func (s *SettingsUI) valueOf(m *Model, key string) string {
	st := &m.settings
	switch key {
	case "theme":
		return st.Theme
	case "arch":
		return st.Arch
	case "variant":
		return st.Variant
	case "source":
		return st.Source
	case "downgrade":
		return st.Downgrade
	case "branch":
		return orDefault(st.Branch, "(default)")
	case "workflow":
		return orDefault(st.Workflow, "(any)")
	case "artifact-pattern":
		return orDefault(st.ArtifactPattern, "(none)")
	case "file":
		return orDefault(st.FilePattern, "(any .apk)")
	case "package":
		return orDefault(st.PackageName, "(unset)")
	case "limit":
		return fmt.Sprint(st.RunLimit)
	case "auto-refresh":
		if st.AutoRefreshSecs == 0 {
			return "off"
		}
		return fmt.Sprintf("%ds", st.AutoRefreshSecs)
	case "no-open":
		return onOff(!st.NoOpen)
	case "keep-cache":
		return onOff(st.KeepCache)
	case "watch":
		return onOff(st.Watch)
	}
	return ""
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func (s *SettingsUI) setValue(m *Model, key, raw string) {
	st := &m.settings
	setting := raw
	switch key {
	case "theme":
		st.Theme = setting
		m.applyTheme(setting)
	case "arch":
		st.Arch = setting
	case "variant":
		st.Variant = setting
	case "source":
		st.Source = setting
	case "downgrade":
		st.Downgrade = setting
	case "branch":
		if setting == "(default)" {
			setting = ""
		}
		st.Branch = setting
		m.branchSel = setting
	case "workflow":
		if setting == "(any)" {
			setting = ""
		}
		st.Workflow = setting
	case "artifact-pattern":
		if setting == "(none)" {
			setting = ""
		}
		st.ArtifactPattern = setting
	case "file":
		if strings.HasPrefix(setting, "(") {
			setting = ""
		}
		st.FilePattern = setting
	case "package":
		if strings.HasPrefix(setting, "(") {
			setting = ""
		}
		st.PackageName = setting
		m.instPkg = setting
	case "limit":
		if n, err := strconv.Atoi(strings.TrimSuffix(setting, "s")); err == nil && n > 0 {
			st.RunLimit = n
		}
	case "auto-refresh":
		if setting == "off" {
			st.AutoRefreshSecs = 0
			break
		}
		if n, err := strconv.Atoi(strings.TrimSuffix(setting, "s")); err == nil {
			st.AutoRefreshSecs = n
			m.refreshSecs = n
		}
	case "no-open":
		st.NoOpen = setting != "on"
	case "keep-cache":
		st.KeepCache = setting == "on"
	case "watch":
		st.Watch = setting == "on"
		m.refreshSecs = m.effectiveRefreshSecs()
	}
}

func (s *SettingsUI) cycle(m *Model, dir int) {
	row := settingRows[s.cursor]
	cur := s.valueOf(m, row.Key)
	curNorm := strings.TrimSuffix(cur, "s")
	switch row.Kind {
	case "enum":
		idx := 0
		for i, v := range row.Values {
			if v == curNorm {
				idx = i
			}
		}
		idx = (idx + dir + len(row.Values)) % len(row.Values)
		s.setValue(m, row.Key, row.Values[idx])
	case "bool":
		if cur == "on" {
			s.setValue(m, row.Key, "off")
		} else {
			s.setValue(m, row.Key, "on")
		}
	case "int":
		n, err := strconv.Atoi(curNorm)
		if err != nil {
			n = 0
		}
		if row.Key == "limit" {
			n += 5 * dir
			if n < 5 {
				n = 5
			}
			if n > 100 {
				n = 100
			}
			s.setValue(m, row.Key, fmt.Sprint(n))
		} else {
			steps := []int{0, 30, 60, 120, 300}
			idx := 0
			for i, v := range steps {
				if v == n {
					idx = i
				}
			}
			idx = (idx + dir + len(steps)) % len(steps)
			if steps[idx] == 0 {
				s.setValue(m, row.Key, "off")
			} else {
				s.setValue(m, row.Key, fmt.Sprintf("%d", steps[idx]))
			}
		}
	case "text":
		// open the inline editor
		s.editing = true
		cur2 := s.valueOf(m, row.Key)
		if strings.HasPrefix(cur2, "(") {
			cur2 = ""
		}
		s.input.SetValue(cur2)
		s.input.Placeholder = row.Label
		s.input.Focus()
		s.input.CursorEnd()
	}
	if row.Key == "arch" || row.Key == "variant" || row.Key == "artifact-pattern" {
		m.invalidateArtifacts()
	}
}

func (s *SettingsUI) handleKey(msg tea.KeyMsg, m *Model) (tea.Model, tea.Cmd) {
	if s.editing {
		switch msg.String() {
		case "enter":
			row := settingRows[s.cursor]
			s.setValue(m, row.Key, s.input.Value())
			s.editing = false
			s.input.Blur()
			return *m, queryPackageCmd(m.instPkg)
		case "esc":
			s.editing = false
			s.input.Blur()
		}
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		return *m, cmd
	}

	switch msg.String() {
	case "esc", "q", "s":
		m.modal = ModalNone
		m.invalidateArtifacts()
		return *m, m.fetchArtifactsForSelection()
	case "j", "down":
		if s.cursor < len(settingRows)-1 {
			s.cursor++
		}
	case "k", "up":
		if s.cursor > 0 {
			s.cursor--
		}
	case "enter", " ", "right", "l":
		s.cycle(m, 1)
		if row := settingRows[s.cursor]; row.Key == "arch" || row.Key == "variant" || row.Key == "artifact-pattern" || row.Key == "file" {
			m.invalidateArtifacts()
			return *m, m.fetchArtifactsForSelection()
		}
	case "left", "h":
		s.cycle(m, -1)
	case "ctrl+g":
		if err := config.SaveGlobal(m.settings); err != nil {
			m.toastf("save failed: %s", shortErr(err))
		} else {
			s.saved = "saved globally  ✓"
			m.toastf("saved to %s", "~/.config/ghapk/config")
		}
	case "ctrl+r":
		if m.settings.Repo == "" {
			m.toastf("no repo selected")
			break
		}
		if err := config.SaveRepo(m.settings); err != nil {
			m.toastf("save failed: %s", shortErr(err))
		} else {
			s.saved = "saved for this repo  ✓"
			m.toastf("saved per-repo config")
		}
	}
	return *m, nil
}

// view draws the settings table.
func (s *SettingsUI) view(m *Model) string {
	st := m.st
	var b strings.Builder
	b.WriteString(st.Muted.Render("changes apply immediately · ctrl+g save for every repo · ctrl+r save for this repo") + "\n\n")
	h := 16
	if s.offset > s.cursor {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+h {
		s.offset = s.cursor - h + 1
	}
	end := s.offset + h
	if end > len(settingRows) {
		end = len(settingRows)
	}
	for i := s.offset; i < end; i++ {
		row := settingRows[i]
		val := s.valueOf(m, row.Key)
		line := fmt.Sprintf("%-20s %s", row.Label, st.Accent.Render(val))
		if i == s.cursor {
			if s.editing {
				line = fmt.Sprintf("%-20s %s", row.Label, s.input.View())
			}
			line = st.RowSel.Render(components.Fit(" "+line, 64))
			b.WriteString(line + "\n")
			b.WriteString("    " + st.Dim.Render(row.Hint) + "\n")
		} else {
			b.WriteString(" " + line + "\n")
		}
	}
	if s.saved != "" {
		b.WriteString("\n" + st.OK.Render(s.saved) + "\n")
	}
	return b.String()
}
