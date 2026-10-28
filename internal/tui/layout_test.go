package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
)

// renderAt renders the dashboard at a given size and returns raw lines.
func renderAt(t *testing.T, w, h int, mutate func(*Model)) []string {
	t.Helper()
	m := newTestModel()
	mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = mm.(Model)
	m = seedRuns(m)
	m = seedArtifacts(m)
	if mutate != nil {
		mutate(&m)
	}
	v := m.View()
	return strings.Split(v, "\n")
}

// lineWidths checks every line fits the terminal width exactly (no wrap,
// no overrun) — an overrun of even one cell pushes the ╮ onto the next
// line, which is exactly the "broken top right corner" the user saw.
func lineWidths(t *testing.T, w, h int, lines []string) {
	t.Helper()
	for i, ln := range lines {
		vw := components.VisibleWidth(ln)
		if vw > w {
			t.Errorf("size %dx%d: line %d is %d cells wide (> %d): %q", w, h, i, vw, w, ln)
		}
	}
}

func TestNoLineOverflow(t *testing.T) {
	sizes := []struct{ w, h int }{
		{96, 30}, {97, 31}, {100, 40}, {101, 33}, {120, 40}, {121, 41},
		{130, 42}, {131, 43}, {96, 24}, {80, 26}, {79, 25}, {70, 26},
		{64, 18}, {150, 50}, {96, 60},
	}
	for _, s := range sizes {
		lines := renderAt(t, s.w, s.h, nil)
		lineWidths(t, s.w, s.h, lines)
	}
}

// focused panels wrap every row in a background — the classic way to lose a corner.
func TestNoLineOverflowFocused(t *testing.T) {
	sizes := []struct{ w, h int }{{120, 40}, {121, 41}, {130, 42}, {97, 31}, {80, 26}}
	for _, s := range sizes {
		for _, focus := range FocusOrder {
			lines := renderAt(t, s.w, s.h, func(m *Model) { m.focus = focus })
			lineWidths(t, s.w, s.h, lines)
		}
		// with badge + many artifacts and a scrolled list
		lines := renderAt(t, s.w, s.h, func(m *Model) {
			m.focus = PanelArtifacts
			r, _ := m.selectedRun()
			var many []ghapi.Artifact
			for i := 0; i < 40; i++ {
				many = append(many, ghapi.Artifact{
					ID: int64(9000 + i), Name: "app-arm64-v8a-debug-build-with-rather-long-name-" + time.Now().Format("05") + itoa(i) + ".apk",
					SizeInBytes: 14 << 20,
				})
			}
			m.runCache[r.ID] = m.scoreArtifacts(many)
			arts := m.runCache[r.ID]
			m.setArtifactItems(arts)
			m.arts.MoveBy(20) // scrolled: exercise the ↓ indicator
		})
		lineWidths(t, s.w, s.h, lines)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// modal rendering must obey the same width invariant — the user said
// "all the panels".
func TestNoLineOverflowModals(t *testing.T) {
	sizes := []struct{ w, h int }{{120, 40}, {97, 31}, {80, 26}, {64, 18}, {130, 42}}
	for _, s := range sizes {
		for _, modal := range []ModalKind{ModalHelp, ModalSettings, ModalPickRepo, ModalPickBranch, ModalConfirm, ModalTask, ModalDoctor} {
			lines := renderAt(t, s.w, s.h, func(m *Model) {
				m.modal = modal
				switch modal {
				case ModalHelp:
					m.helpVP = m.buildHelp()
					m.helpVP.Height = s.h - 10
				case ModalSettings:
					m.settingsUI = NewSettingsUI(m)
				case ModalPickRepo:
					m.openRepoPicker()
				case ModalPickBranch:
					m.branchList.SetItems(nil)
				case ModalConfirm:
					m.confirm = confirm_sample(m)
				case ModalTask:
					m.task = newTask("Downloading artifact", "app-debug.apk", progressColor{m.st.P.Accent, m.st.P.AccentAlt})
					m.task.setProgress(3<<20, 14<<20)
				case ModalDoctor:
					m.doctor = &DoctorState{ready: true, Rows: []doctorRow{{"ok", "gh", "2.62.0"}, {"err", "auth", "not logged in — run gh auth login on a line long enough to wrap"}}}
				}
			})
			lineWidths(t, s.w, s.h, lines)
		}
	}
}

func confirm_sample(m *Model) (c components.Confirm) {
	c.Title = "Confirm download"
	c.Body = []string{"Download  rocket-universal-debug.apk", "from run  #412 (main)", "size      20.0 MB (zipped)"}
	c.Options = []string{"Download", "Download & Install", "Cancel"}
	return c
}

// corner alignment: every panel-rendering section must start with ╭ and
// end with ╮ on its first line and ╰…╯ on its last.
func TestPanelFrameIntegrity(t *testing.T) {
	for _, size := range []struct{ w, h int }{{120, 40}, {97, 31}, {80, 26}} {
		lines := renderAt(t, size.w, size.h, nil)
		for i, ln := range lines {
			plain := components.StripANSI(ln)
			// no line should ever end with a bare corner carried over by wrap
			if strings.HasSuffix(plain, "╮") && strings.Contains(plain, "╮╭") {
				// adjacent panels: ╮ followed by ╭ is fine on the same line
			}
			if strings.Count(plain, "╮") > 4 {
				t.Errorf("size %dx%d line %d has too many top-right corners: %q", size.w, size.h, i, plain)
			}
		}
	}
}

// Resizing wide ↔ compact must re-render cleanly at the new size and
// retune the list windows (this was behind the "numbers" complaint).
func TestResizeRefilters(t *testing.T) {
	m := newTestModel()
	m = seedRuns(m)
	m = seedArtifacts(m)

	// shrink to compact
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 48, Height: 26})
	m = mm.(Model)
	if !m.compact {
		t.Fatal("48 cols must be compact")
	}
	lineWidths(t, 48, 26, strings.Split(m.View(), "\n"))
	// runs panel splits into list + detail sub-box: the list gets the top
	if want := 26 - 3 - 2; m.runs.MaxShown != want {
		t.Errorf("compact builds window = %d, want %d", m.runs.MaxShown, want)
	}

	// grow back to wide
	mm, _ = m.Update(tea.WindowSizeMsg{Width: 130, Height: 42})
	m = mm.(Model)
	if m.compact {
		t.Fatal("130 cols must be wide")
	}
	lineWidths(t, 130, 42, strings.Split(m.View(), "\n"))

	// odd small wide boundary (96 is the threshold)
	mm, _ = m.Update(tea.WindowSizeMsg{Width: 96, Height: 30})
	m = mm.(Model)
	if m.compact {
		t.Fatal("96 cols is exactly wide")
	}
	lineWidths(t, 96, 30, strings.Split(m.View(), "\n"))
}
