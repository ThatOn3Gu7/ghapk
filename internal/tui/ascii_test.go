package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ThatOn3Gu7/ghapk/internal/config"
	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
	tea "github.com/charmbracelet/bubbletea"
)

func TestASCIIInterfacePanelsAndDialogs(t *testing.T) {
	for _, sz := range [][2]int{{32, 18}, {40, 24}, {80, 26}, {120, 40}} {
		t.Run(fmt.Sprint(sz), func(t *testing.T) {
			m := buildModel(t, sz[0], sz[1])
			m.settings.Glyphs = "ascii"
			m.applyTheme(m.settings.Theme)
			check := func(label string) {
				v := components.StripANSI(m.View())
				lines := strings.Split(v, "\n")
				if len(lines) != sz[1] {
					t.Fatalf("%s height %d", label, len(lines))
				}
				lineWidths(t, sz[0], sz[1], lines)
				for _, r := range v {
					if r > 127 {
						t.Fatalf("%s still contains %q: %s", label, r, v)
					}
				}
			}
			for _, p := range FocusOrder {
				m.focus = p
				m.modal = ModalNone
				check(PanelNames[p])
			}
			m.focus = PanelRuns
			m.modal = ModalHelp
			m.helpVP = m.buildHelp()
			check("help")
			m.modal = ModalNone
			m.openRepoPicker()
			check("repository picker")
			m.modal = ModalNone
			m.confirm = components.Confirm{Title: "Safety", Body: []string{"Proceed?"}, Options: []string{"Cancel", "Proceed"}}
			m.modal = ModalConfirm
			check("confirm")
			m.openBranchPicker()
			check("branch picker")
			m.openWorkflowPicker()
			check("workflow picker")
			m.openRepoEditor("example/repo")
			check("repo editor")
			m.task = newTask("Download", "app.apk", progressColor{m.st.P.Accent, m.st.P.AccentAlt})
			m.modal = ModalTask
			check("task")
			m.doctor = &DoctorState{ready: true, Rows: []doctorRow{{"ok", "gh", "installed"}}}
			m.modal = ModalDoctor
			check("doctor")
			m.plan = &installPlan{choices: []planChoice{{art: ghapi.Artifact{ID: 1, Name: "app.apk"}}}}
			m.modal = ModalPlan
			check("plan")
			m.modal = ModalNone
			m.settings.Glyphs = "unicode"
			m.applyTheme(m.settings.Theme)
			if !strings.Contains(m.View(), "╭") {
				t.Fatal("Unicode mode did not restore borders")
			}
		})
	}
}
func TestASCIIKeepsCellGeometryAndUserText(t *testing.T) {
	input := "\x1b[31m╭─📁→…╮\x1b[0m Café 漢字"
	output := asciiInterface(input)
	if components.VisibleWidth(input) != components.VisibleWidth(output) {
		t.Fatalf("width changed: %d vs %d %q", components.VisibleWidth(input), components.VisibleWidth(output), output)
	}
	if !strings.Contains(output, "Café 漢字") || !strings.Contains(output, "\x1b[31m") {
		t.Fatalf("user content or color lost: %q", output)
	}
	m := buildModel(t, 40, 24)
	m.settings.Glyphs = "ascii"
	m.applyTheme(m.settings.Theme)
	r := m.compactTabRects()[PanelSettings]
	mm, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: r.x + r.w/2, Y: r.y})
	if mm.(Model).focus != PanelSettings {
		t.Fatal("ASCII tab hitbox moved")
	}
}
func TestSmallScreenRespectsASCII(t *testing.T) {
	m := buildModel(t, 40, 24)
	m.settings.Glyphs = "ascii"
	m.applyTheme(m.settings.Theme)
	m.width = 25
	m.height = 12
	for _, r := range components.StripANSI(m.View()) {
		if r > 127 {
			t.Fatalf("small screen has non-ASCII glyph %q", r)
		}
	}
}

// The first frame is rendered before GitHub authentication and before
// bootstrapDashboard reloads per-repository settings. A remembered repo's
// glyph setting must already be in effect on that frame.
func TestStartupGlyphsAreAppliedBeforeAuthentication(t *testing.T) {
	cases := []struct {
		name         string
		global       string
		repoGlyphs   string
		selection    string
		recent       string
		explicitRepo string
		startRepo    string
		envRepo      string
		envGlyphs    string
		cliGlyphs    string
		wantRepo     string
		wantGlyphs   string
	}{
		{name: "global ASCII, no repo", global: "ascii", wantGlyphs: "ascii"},
		{name: "remembered repo ASCII", repoGlyphs: "ascii", selection: "owner/repo", wantRepo: "owner/repo", wantGlyphs: "ascii"},
		{name: "recent repo ASCII", repoGlyphs: "ascii", recent: "owner/repo", wantRepo: "owner/repo", wantGlyphs: "ascii"},
		{name: "explicit repo overrides remembered and environment", repoGlyphs: "ascii", selection: "other/saved", envRepo: "other/env", explicitRepo: "owner/repo", wantRepo: "owner/repo", wantGlyphs: "ascii"},
		{name: "start repo ASCII", repoGlyphs: "ascii", startRepo: "owner/repo", wantRepo: "owner/repo", wantGlyphs: "ascii"},
		{name: "environment repo ASCII", repoGlyphs: "ascii", selection: "other/repo", envRepo: "owner/repo", wantRepo: "owner/repo", wantGlyphs: "ascii"},
		{name: "repo Unicode overrides global ASCII", global: "ascii", repoGlyphs: "unicode", selection: "owner/repo", wantRepo: "owner/repo", wantGlyphs: "unicode"},
		{name: "environment overrides repo", repoGlyphs: "ascii", selection: "owner/repo", envGlyphs: "unicode", wantRepo: "owner/repo", wantGlyphs: "unicode"},
		{name: "CLI Unicode overrides environment and repo", repoGlyphs: "ascii", selection: "owner/repo", envGlyphs: "ascii", cliGlyphs: "unicode", wantRepo: "owner/repo", wantGlyphs: "unicode"},
		{name: "CLI ASCII overrides repo", repoGlyphs: "unicode", selection: "owner/repo", cliGlyphs: "ascii", wantRepo: "owner/repo", wantGlyphs: "ascii"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
			t.Setenv("GHAPK_REPO", tc.envRepo)
			t.Setenv("GHAPK_GLYPHS", tc.envGlyphs)
			t.Setenv("GHAPK_ASCII", "")
			t.Setenv("TERM", "xterm-256color")
			if tc.global != "" {
				s := config.Defaults()
				s.Glyphs = tc.global
				if err := config.SaveGlobal(s); err != nil {
					t.Fatal(err)
				}
			}
			if tc.repoGlyphs != "" {
				s := config.Defaults()
				s.Repo, s.Glyphs = "owner/repo", tc.repoGlyphs
				if err := config.SaveRepo(s); err != nil {
					t.Fatal(err)
				}
			}
			if tc.selection != "" {
				if err := config.RememberSelection(tc.selection, true, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			if tc.recent != "" {
				config.TouchRecent(tc.recent)
			}
			opts := Options{Settings: config.Load(), StartRepo: tc.startRepo}
			if tc.explicitRepo != "" {
				opts.Settings.Repo, opts.RepoOverride = tc.explicitRepo, true
			}
			if tc.cliGlyphs != "" {
				opts.Settings.Glyphs = tc.cliGlyphs
				opts.CLIOverrides = map[string]string{"glyphs": tc.cliGlyphs}
			}
			opts = prepareStartupGlyphs(opts)
			m := New(opts)
			checkFrame := func(stage string) {
				t.Helper()
				if m.settings.Glyphs != tc.wantGlyphs {
					t.Fatalf("%s glyph mode = %q, want %q", stage, m.settings.Glyphs, tc.wantGlyphs)
				}
				v := components.StripANSI(m.View())
				if tc.wantGlyphs == "ascii" && strings.ContainsAny(v, "╭╮╰╯│─…") {
					t.Fatalf("%s has Unicode decorations: %q", stage, v)
				}
				if tc.wantGlyphs == "unicode" && m.width > 0 && !strings.Contains(v, "╭") {
					t.Fatalf("%s is missing Unicode borders: %q", stage, v)
				}
			}
			checkFrame("before terminal size")
			mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 26})
			m = mm.(Model)
			checkFrame("before authentication")
			mm, _ = m.Update(AuthCheckedMsg{Version: "2", User: "tester"})
			m = mm.(Model)
			if m.settings.Repo != tc.wantRepo {
				t.Fatalf("restored repo = %q, want %q", m.settings.Repo, tc.wantRepo)
			}
			checkFrame("after authentication")
		})
	}
}

func TestBootstrapKeepsCurrentRepoOnAuthRecheck(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	t.Setenv("GHAPK_REPO", "")
	m := startSelectionSession(t, Options{Settings: config.Settings{Repo: "initial/repo"}})
	if err := m.setRepo("current/repo", false); err != nil {
		t.Fatal(err)
	}
	mm, _ := m.Update(AuthCheckedMsg{Version: "2", User: "tester"})
	if got := mm.(Model).settings.Repo; got != "current/repo" {
		t.Fatalf("auth recheck changed repo to %q", got)
	}
}
