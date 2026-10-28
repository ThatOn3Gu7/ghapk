package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/ghapk/ghapk/internal/tui/components"
	"github.com/ghapk/ghapk/internal/tui/theme"
)

func TestModalOverlaysPreserveDimDashboard(t *testing.T) {
	withTrueColor(t)
	for _, name := range theme.Order {
		for _, kind := range []ModalKind{ModalHelp, ModalSettings, ModalPickRepo, ModalPickBranch, ModalPickWorkflow, ModalEditRepo, ModalConfirm, ModalTask, ModalDoctor} {
			t.Run(fmt.Sprintf("%s/%d", name, kind), func(t *testing.T) {
				m := buildModel(t, 120, 50)
				m.applyTheme(name)
				switch kind {
				case ModalHelp:
					m.helpVP = m.buildHelp()
					m.helpVP.Height = 10
				case ModalSettings:
					m.settingsUI = NewSettingsUI(&m)
				case ModalPickRepo:
					m.openRepoPicker()
				case ModalPickBranch:
					m.openBranchPicker()
				case ModalPickWorkflow:
					m.openWorkflowPicker()
				case ModalEditRepo:
					m.openRepoEditor(m.settings.Repo)
				case ModalConfirm:
					m.confirm = components.Confirm{Title: "Safety", Body: []string{"Safety warning"}, Options: []string{"Install anyway", "Cancel"}, Cursor: 1}
					m.confirmKind = confirmSafetyOverride
				case ModalTask:
					m.task = newTask("Downloading", "test.apk", progressColor{m.st.P.Accent, m.st.P.AccentAlt})
				case ModalDoctor:
					m.doctor = &DoctorState{ready: true, Rows: []doctorRow{{"ok", "gh", "installed"}}}
				}
				m.modal = ModalNone
				before := m.View()
				m.modal = kind
				layer := m.modalLayer()
				view := m.View()
				beforeLines, lines, layerLines := strings.Split(before, "\n"), strings.Split(view, "\n"), strings.Split(layer, "\n")
				if len(lines) != m.height {
					t.Fatalf("height %d, want %d", len(lines), m.height)
				}
				lineWidths(t, m.width, m.height, lines)
				if components.StripANSI(lines[0]) != components.StripANSI(beforeLines[0]) {
					t.Fatal("header not preserved behind modal")
				}
				screen := cellbuf.NewBuffer(m.width, m.height)
				cellbuf.SetContent(screen, view)
				_, r := m.headerLayout()
				if c := screen.Cell(r.x, 0); c.Style.Attrs&cellbuf.FaintAttr == 0 {
					t.Fatalf("header not dimmed: %+v", c)
				}
				if c := screen.Cell(r.x, 0); c.Style.Bg != nil {
					t.Fatal("terminal background replaced")
				}
				topY, topX := -1, -1
				for y, line := range layerLines {
					plain := components.StripANSI(line)
					if p := strings.Index(plain, "╭"); p >= 0 {
						topY = y
						topX = components.VisibleWidth(plain[:p])
						break
					}
				}
				if topY < 0 {
					t.Fatal("modal border not found")
				}
				if c := screen.Cell(topX, topY); c.Rune != '╭' || c.Style.Attrs&cellbuf.FaintAttr != 0 {
					t.Fatalf("modal border dimmed: %+v", c)
				}
				if m.modal != ModalConfirm && m.modal != ModalTask {
					mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: r.x, Y: 0})
					if mm.(Model).modal != kind || cmd != nil {
						t.Fatal("background header clicked through modal")
					}
				}
			})
		}
	}
}

func TestModalDismissRestoresDashboard(t *testing.T) {
	withTrueColor(t)
	m := buildModel(t, 100, 30)
	before := m.View()
	m.openRepoPicker()
	during := m.View()
	if !strings.Contains(components.StripANSI(during), "Repository") {
		t.Fatal("missing picker")
	}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mm.(Model)
	if m.modal != ModalNone || m.View() != before {
		t.Fatal("dashboard not restored after dismiss")
	}
}

func TestModalHitTestsIgnoreBackgroundBorders(t *testing.T) {
	m := buildModel(t, 100, 30)
	m.modal = ModalConfirm
	m.confirmKind = confirmSafetyOverride
	m.confirm = components.Confirm{Title: "Safety", Body: []string{"warning"}, Options: []string{"Install anyway", "Cancel"}, Cursor: 1}
	layer := strings.Split(m.modalLayer(), "\n")
	var x, y int
	found := false
	for i, line := range layer {
		plain := components.StripANSI(line)
		if p := strings.Index(plain, "Cancel"); p >= 0 {
			x = components.VisibleWidth(plain[:p]) + 2
			y = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("cancel missing")
	}
	if m.confirmButtonHit(x, y, m.confirm.Options) != 1 {
		t.Fatal("modal cancel is not hittable")
	}
	if m.confirmButtonHit(x, m.height-2, m.confirm.Options) >= 0 {
		t.Fatal("background border incorrectly interpreted as modal")
	}
	mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x, Y: y})
	got := mm.(Model)
	if got.modal != ModalNone || cmd != nil {
		t.Fatal("cancel did not dismiss safety confirmation")
	}
}

func TestPhoneSafetyDialogOverlayKeepsWarningsAndButtons(t *testing.T) {
	withTrueColor(t)
	for _, sz := range [][2]int{{32, 18}, {32, 24}, {40, 20}, {41, 21}} {
		t.Run(fmt.Sprint(sz), func(t *testing.T) {
			m := buildModel(t, sz[0], sz[1])
			m.modal = ModalConfirm
			m.confirmKind = confirmSafetyOverride
			m.confirm = components.Confirm{Title: "Safety check before install", Body: []string{"Downgrade warning", "The new version is older than the version installed on your device. Installing it might cause data loss.", "Signing key mismatch", "The APK has a different signing key from your installed application."}, Options: []string{"Install anyway", "Cancel"}, Cursor: 1}
			box := m.modalBox()
			layer := m.modalLayer()
			view := m.View()
			w, h := max(m.width, lipgloss.Width(box)), max(m.height, lipgloss.Height(box))
			actual := cellbuf.NewBuffer(w, h)
			cellbuf.SetContent(actual, view)
			expected := cellbuf.NewBuffer(w, h)
			cellbuf.SetContent(expected, layer)
			x, y := max(0, (m.width-lipgloss.Width(box))/2), max(0, (m.height-lipgloss.Height(box))/2)
			for row := y; row < y+lipgloss.Height(box); row++ {
				for col := x; col < x+lipgloss.Width(box); col++ {
					a, b := actual.Cell(col, row), expected.Cell(col, row)
					if a.String() != b.String() || !a.Style.Equal(&b.Style) {
						t.Fatalf("modal differs at %d,%d: got %q %+v, want %q %+v", col, row, a, a.Style, b, b.Style)
					}
				}
			}
			if m.safetyDialogTooLarge() {
				if strings.Contains(components.StripANSI(view), "Install anyway") || !strings.Contains(components.StripANSI(view), "Zoom out") {
					t.Fatal("oversized warning must prevent install and request resize")
				}
				if len(strings.Split(view, "\n")) != m.height {
					t.Fatal("fallback overflowed screen")
				}
				continueTestSafetyFallback(t, m)
				return
			}
			if !strings.Contains(components.StripANSI(view), "Install anyway") || !strings.Contains(components.StripANSI(view), "Cancel") {
				t.Fatal("missing confirmation buttons")
			}
			for row, line := range strings.Split(layer, "\n") {
				plain := components.StripANSI(line)
				for index, label := range m.confirm.Options {
					if p := strings.Index(plain, label); p >= 0 {
						col := components.VisibleWidth(plain[:p])
						if got := m.confirmButtonHit(col, row, m.confirm.Options); got != index {
							t.Fatalf("%s hit=%d want %d", label, got, index)
						}
					}
				}
			}
		})
	}
}

func TestSetupModalShowsSetupBehind(t *testing.T) {
	withTrueColor(t)
	m := buildModel(t, 100, 30)
	m.screen = ScreenSetup
	m.modal = ModalNone
	before := m.View()
	m.modal = ModalConfirm
	m.confirm = components.Confirm{Title: "Example", Body: []string{"Continue?"}, Options: []string{"OK", "Cancel"}}
	out := m.View()
	if components.StripANSI(strings.Split(before, "\n")[0]) != components.StripANSI(strings.Split(out, "\n")[0]) {
		t.Fatal("setup header not preserved")
	}
	if !strings.Contains(components.StripANSI(out), "Continue?") {
		t.Fatal("modal not visible")
	}
}

// A warning that doesn't fit cannot be accepted by keyboard or touch. Resizing
// restores the full warning, and tapping the fallback Cancel remains safe.
func continueTestSafetyFallback(t *testing.T, m Model) {
	t.Helper()
	resized := m
	resized.height = 100
	if resized.safetyDialogTooLarge() || !strings.Contains(components.StripANSI(resized.View()), "Install anyway") || !strings.Contains(components.StripANSI(resized.View()), "Downgrade warning") {
		t.Fatal("resizing did not restore the full warning")
	}
	m.confirm.Cursor = 0
	m.safetyInstallPath = "/tmp/never-install.apk"
	guarded := m
	if cmd := guarded.confirmAccept(); cmd != nil || guarded.task != nil || guarded.modal != ModalNone {
		t.Fatal("direct accept bypassed oversized safety warning")
	}
	for _, k := range []string{"left", "y", "enter"} {
		mm, cmd := m.Update(key(k))
		m = mm.(Model)
		if cmd != nil || m.task != nil {
			t.Fatal("oversized warning accepted install")
		}
	}
	if m.modal != ModalNone {
		t.Fatal("enter did not dismiss fallback")
	}
	m.modal = ModalConfirm
	m.confirmKind = confirmSafetyOverride
	m.confirm = components.Confirm{Title: "Safety", Body: []string{strings.Repeat("warning ", 200)}, Options: []string{"Install anyway", "Cancel"}, Cursor: 0}
	m.safetyInstallPath = "/tmp/never-install.apk"
	layer := strings.Split(m.modalLayer(), "\n")
	found := false
	for y, line := range layer {
		plain := components.StripANSI(line)
		p := strings.Index(plain, "Cancel")
		if p >= 0 {
			mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: components.VisibleWidth(plain[:p]) + 1, Y: y})
			got := mm.(Model)
			if cmd != nil || got.modal != ModalNone || got.task != nil {
				t.Fatal("fallback cancel was unsafe")
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("fallback cancel not found")
	}
}

func TestSafetyOverrideBlockedWhenScreenTooSmall(t *testing.T) {
	m := buildModel(t, 32, 18)
	m.width = 20
	m.height = 10
	m.modal = ModalConfirm
	m.confirmKind = confirmSafetyOverride
	m.confirm = components.Confirm{Title: "Safety", Body: []string{"warning"}, Options: []string{"Install anyway", "Cancel"}, Cursor: 0}
	m.safetyInstallPath = "/tmp/never-install.apk"
	mm, cmd := m.Update(key("enter"))
	got := mm.(Model)
	if cmd != nil || got.modal != ModalNone || got.task != nil {
		t.Fatal("unseen safety override accepted")
	}
}
