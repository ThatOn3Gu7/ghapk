package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
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
