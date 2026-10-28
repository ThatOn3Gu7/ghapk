package tui

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
)

func hintSpan(t *testing.T, m Model, action string) components.HintSpan {
	t.Helper()
	sb := m.statusBar()
	_, spans := sb.Layout(m.st)
	for _, span := range spans {
		if sb.Hints[span.Index].Action == action {
			return span
		}
	}
	t.Fatalf("hint %q not visible in %q", action, components.StripANSI(m.renderStatusBar()))
	return components.HintSpan{}
}

func tapHint(t *testing.T, m Model, action string) (Model, tea.Cmd) {
	t.Helper()
	span := hintSpan(t, m, action)
	mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: span.Start + (span.End-span.Start)/2, Y: m.height - 1})
	switch result := mm.(type) {
	case Model:
		return result, cmd
	case *Model:
		return *result, cmd
	default:
		t.Fatalf("unexpected model type %T", mm)
		return Model{}, nil
	}
}

func TestHeaderRepoTouch(t *testing.T) {
	for _, width := range []int{32, 40, 64, 100, 160} {
		for _, repo := range []string{"", "acme/rocket", "exceptionally-long-owner/exceptionally-long-repository-name"} {
			t.Run(fmt.Sprintf("%d/%s", width, repo), func(t *testing.T) {
				m := newTestModel()
				m.settings.Repo = repo
				m.width = width
				m.settings.Workflow = "very-long-workflow-name"
				m.branchSel = "very-long-branch-name"
				m.cachedUserRepos = []ghapi.Repo{{FullName: "acme/rocket"}}
				header, r := m.headerLayout()
				plain := components.StripANSI(header)
				if r.w <= 0 || r.x+r.w > width {
					t.Fatalf("bad repo hit rect %+v; header %q", r, plain)
				}
				if strings.TrimSpace(string([]rune(plain)[r.x:r.x+r.w])) == "" {
					t.Fatalf("empty header hit region %+v in %q", r, plain)
				}
				for _, x := range []int{r.x, r.x + r.w/2, r.x + r.w - 1} {
					mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: 0})
					got := mm.(Model)
					if got.modal != ModalPickRepo || got.repoPicker == nil || cmd != nil {
						t.Fatalf("repo tap x=%d: modal=%v picker=%v cmd=%v", x, got.modal, got.repoPicker != nil, cmd != nil)
					}
				}
				for _, x := range []int{r.x - 1, r.x + r.w} {
					mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x, Y: 0})
					if got := mm.(Model); got.modal != ModalNone || cmd != nil {
						t.Fatalf("outside repo x=%d activated picker", x)
					}
				}
			})
		}
	}
}

func TestVisibleHintGeometryAndSafeGaps(t *testing.T) {
	withTrueColor(t)
	for _, width := range []int{32, 40, 48, 64, 80, 120} {
		for _, focus := range []PanelID{PanelRepos, PanelWorkflows, PanelRuns, PanelArtifacts, PanelDevice} {
			for _, busy := range []bool{false, true} {
				m := seedArtifacts(seedRuns(newTestModel()))
				m.width = width
				m.focus = focus
				m.busy = busy
				m.busyMsg = "loading extremely long repository name"
				sb := m.statusBar()
				line, spans := sb.Layout(m.st)
				plain := components.StripANSI(line)
				if components.VisibleWidth(line) != width {
					t.Fatalf("width %d focus %v busy %v: rendered %d", width, focus, busy, components.VisibleWidth(line))
				}
				cells := []rune(plain)
				last := 0
				for _, span := range spans {
					if span.Start < last || span.End > width || span.Start >= span.End {
						t.Fatalf("invalid spans: %+v", spans)
					}
					h := sb.Hints[span.Index]
					want := h.Key + ":" + h.Desc
					if got := string(cells[span.Start:span.End]); got != want {
						t.Fatalf("span %+v got %q want %q", span, got, want)
					}
					last = span.End
				}
				// The cells between actions and at the end must not launch one.
				for x := 0; x < width; x++ {
					active := false
					for _, span := range spans {
						if x >= span.Start && x < span.End {
							active = true
							break
						}
					}
					if active {
						continue
					}
					mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x, Y: m.height - 1})
					got := mm.(Model)
					if cmd != nil || got.modal != ModalNone || got.focus != m.focus || got.quitting {
						t.Fatalf("width %d focus %v busy %v: inactive x=%d performed an action", width, focus, busy, x)
					}
				}
			}
		}
	}
}

func TestFooterTouchesDispatchActions(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	cases := []struct {
		name   string
		focus  PanelID
		action string
		check  func(*testing.T, Model, tea.Cmd)
	}{
		{"repo picker", PanelRepos, "R", func(t *testing.T, m Model, c tea.Cmd) {
			if m.modal != ModalPickRepo || m.repoPicker == nil {
				t.Fatal("picker not opened")
			}
		}},
		{"edit repo", PanelRepos, "e", func(t *testing.T, m Model, c tea.Cmd) {
			if m.modal != ModalEditRepo {
				t.Fatal("editor not opened")
			}
		}},
		{"remove repo", PanelRepos, "x", func(t *testing.T, m Model, c tea.Cmd) {
			if m.modal != ModalConfirm || m.confirmKind != confirmRemoveRepo {
				t.Fatal("remove safety confirmation missing")
			}
		}},
		{"star repo", PanelRepos, "f", func(t *testing.T, m Model, c tea.Cmd) {
			if m.toast == "" {
				t.Fatal("star did not update")
			}
		}},
		{"select repo", PanelRepos, "enter", func(t *testing.T, m Model, c tea.Cmd) {
			if m.navRepo == "" || c == nil {
				t.Fatal("repo selection did not sync")
			}
		}},
		{"open artifacts", PanelRuns, "right", func(t *testing.T, m Model, c tea.Cmd) {
			if m.focus != PanelArtifacts {
				t.Fatal("artifacts not focused")
			}
		}},
		{"download", PanelRuns, "d", func(t *testing.T, m Model, c tea.Cmd) {
			if m.modal != ModalTask || m.pendingInstall || c == nil {
				t.Fatal("download did not start")
			}
		}},
		{"install", PanelRuns, "i", func(t *testing.T, m Model, c tea.Cmd) {
			if m.modal != ModalTask || !m.pendingInstall || c == nil {
				t.Fatal("install did not start")
			}
		}},
		{"open", PanelRuns, "o", func(t *testing.T, m Model, c tea.Cmd) {
			if c == nil || m.modal != ModalNone {
				t.Fatal("open command did not start")
			}
		}},
		{"tab", PanelRuns, "tab", func(t *testing.T, m Model, c tea.Cmd) {
			if m.focus != PanelDevice {
				t.Fatal("tab did not cycle")
			}
		}},
		{"filter", PanelRuns, "/", func(t *testing.T, m Model, c tea.Cmd) {
			if !m.filterMode {
				t.Fatal("filter not opened")
			}
		}},
		{"settings", PanelRuns, "s", func(t *testing.T, m Model, c tea.Cmd) {
			if m.modal != ModalSettings {
				t.Fatal("settings not opened")
			}
		}},
		{"help", PanelRuns, "?", func(t *testing.T, m Model, c tea.Cmd) {
			if m.modal != ModalHelp {
				t.Fatal("help not opened")
			}
		}},
		{"quit", PanelRuns, "q", func(t *testing.T, m Model, c tea.Cmd) {
			if !m.quitting || c == nil {
				t.Fatal("quit did not dispatch")
			}
		}},
		{"workflow preview", PanelWorkflows, "right", func(t *testing.T, m Model, c tea.Cmd) {
			if !m.wfYAMLFocused {
				t.Fatal("YAML preview not focused")
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := seedArtifacts(seedRuns(newTestModel()))
			m.width = 200
			m.focus = tc.focus
			m.updateListBounds()
			got, cmd := tapHint(t, m, tc.action)
			tc.check(t, got, cmd)
		})
	}
}

func TestFilterHintsAndPreviewScroll(t *testing.T) {
	for _, action := range []string{"backspace", "esc", "enter"} {
		m := seedRuns(newTestModel())
		m.width = 100
		m.focus = PanelWorkflows
		m.filterMode = true
		m.workflows.Filter = "ci"
		got, _ := tapHint(t, m, action)
		switch action {
		case "backspace":
			if got.workflows.Filter != "c" || !got.filterMode {
				t.Fatal("backspace did not edit filter")
			}
		case "esc":
			if got.workflows.Filter != "" || got.filterMode {
				t.Fatal("esc did not clear filter")
			}
		case "enter":
			if got.workflows.Filter != "ci" || got.filterMode {
				t.Fatal("enter did not keep filter")
			}
		}
	}
	m := seedRuns(newTestModel())
	m.width = 100
	m.focus = PanelWorkflows
	m.wfYAMLFocused = true
	m.wfPaths["CI"] = "ci.yml"
	m.wfCache["ci.yml"] = strings.Repeat("step\n", 100)
	m.workflows.SelectByID("CI")
	got, _ := tapHint(t, m, "down")
	if got.wfScroll != 1 {
		t.Fatalf("down scroll = %d", got.wfScroll)
	}
	got, _ = tapHint(t, got, "up")
	if got.wfScroll != 0 {
		t.Fatalf("up scroll = %d", got.wfScroll)
	}
	got, _ = tapHint(t, got, "esc")
	if got.wfYAMLFocused {
		t.Fatal("esc did not return to workflow list")
	}
}

func TestHeaderAndFooterRespectModalAndMouseMotion(t *testing.T) {
	m := seedRuns(newTestModel())
	m.width = 100
	m.focus = PanelRuns
	_, r := m.headerLayout()
	span := hintSpan(t, m, "d")
	m.modal = ModalConfirm
	m.confirmKind = confirmSafetyOverride
	m.confirm = components.Confirm{Title: "Safety", Options: []string{"Install anyway", "Cancel"}, Cursor: 1}
	for _, point := range [][2]int{{r.x, 0}, {span.Start, m.height - 1}} {
		mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: point[0], Y: point[1]})
		got := mm.(Model)
		if got.modal != ModalConfirm || cmd != nil {
			t.Fatal("touch bypassed modal")
		}
	}
	m.modal = ModalNone
	for _, point := range [][2]int{{r.x, 0}, {span.Start, m.height - 1}} {
		mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: point[0], Y: point[1]})
		got := mm.(Model)
		if got.modal != ModalNone || cmd != nil {
			t.Fatal("drag triggered an action")
		}
	}
	m.toast = "a message"
	m.toastUntil = time.Now().Add(time.Minute)
	_, spans := m.statusBar().Layout(m.st)
	for _, s := range spans {
		if s.Start < 0 || s.End > m.width {
			t.Fatal("toast overlapped a hint")
		}
	}
}

// A real Bubble Tea input loop parses the same SGR mouse sequences sent by
// Termux, rather than calling the model's mouse handler directly.
type touchHarness struct {
	model   Model
	updates chan Model
}

func (h touchHarness) Init() tea.Cmd { return nil }
func (h touchHarness) View() string  { return h.model.View() }
func (h touchHarness) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := h.model.Update(msg)
	switch m := updated.(type) {
	case Model:
		h.model = m
	case *Model:
		h.model = *m
	}
	select {
	case h.updates <- h.model:
	default:
	}
	return h, cmd
}

func TestLiveTeaTouchHintsAndHeader(t *testing.T) {
	m := buildModel(t, 100, 30)
	m.cachedUserRepos = []ghapi.Repo{{FullName: "acme/rocket"}}
	span := hintSpan(t, m, "right")
	_, repo := m.headerLayout()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	var out syncBuf
	updates := make(chan Model, 100)
	p := tea.NewProgram(touchHarness{model: m, updates: updates}, tea.WithInput(input), tea.WithOutput(&out), tea.WithMouseCellMotion(), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	defer p.Kill()
	p.Send(tea.WindowSizeMsg{Width: 100, Height: 30})
	waitFor := func(predicate func(Model) bool) {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case state := <-updates:
				if predicate(state) {
					return
				}
			case err := <-done:
				t.Fatalf("program exited early: %v", err)
			case <-timer.C:
				t.Fatalf("timed out; output: %s", components.StripANSI(out.String()))
			}
		}
	}
	sendClick := func(x, y int) {
		t.Helper()
		if _, err := fmt.Fprintf(writer, "\x1b[<0;%d;%dM", x+1, y+1); err != nil {
			t.Fatal(err)
		}
	}
	sendClick(span.Start+1, m.height-1)
	waitFor(func(state Model) bool { return state.focus == PanelArtifacts })
	sendClick(repo.x+1, 0)
	waitFor(func(state Model) bool { return state.modal == ModalPickRepo && state.repoPicker != nil })
	p.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("program did not exit")
	}
}

func TestHeaderUnicodeEllipsisUsesWholeCells(t *testing.T) {
	input := "开发者/超长仓库名称📦repository"
	for width := 1; width <= 30; width++ {
		output := compMiddleEll(input, width)
		if !utf8.ValidString(output) || components.VisibleWidth(output) > width {
			t.Fatalf("width %d: %q (%d cells)", width, output, components.VisibleWidth(output))
		}
	}
	m := newTestModel()
	m.settings.Repo = input
	m.width = 32
	header, r := m.headerLayout()
	if !utf8.ValidString(header) || r.x+r.w > m.width || r.w == 0 {
		t.Fatalf("unicode header %q rect=%+v", header, r)
	}
	mm, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: r.x + r.w - 1, Y: 0})
	if mm.(Model).modal != ModalPickRepo {
		t.Fatal("unicode repo touch failed")
	}
}

func TestToastDoesNotMoveHintTargets(t *testing.T) {
	m := seedRuns(newTestModel())
	m.width = 100
	m.focus = PanelDevice
	_, before := m.statusBar().Layout(m.st)
	m.toast = "hello"
	m.toastUntil = time.Now().Add(time.Hour)
	line, during := m.statusBar().Layout(m.st)
	if !strings.Contains(components.StripANSI(line), "hello") {
		t.Fatal("toast should fit")
	}
	if !reflect.DeepEqual(before, during) {
		t.Fatal("toast shifted clickable hints")
	}
	x := strings.Index(components.StripANSI(line), "hello")
	mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x, Y: m.height - 1})
	if got := mm.(Model); got.modal != ModalNone || got.quitting || cmd != nil {
		t.Fatal("toast activated a hint")
	}
	m.toastUntil = time.Now().Add(-time.Second)
	_, after := m.statusBar().Layout(m.st)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("expired toast shifted clickable hints")
	}
}

func TestHintAndHeaderTouchWithoutColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := seedRuns(newTestModel())
	m.width = 100
	m.focus = PanelRuns
	got, _ := tapHint(t, m, "right")
	if got.focus != PanelArtifacts {
		t.Fatal("no-color hint failed")
	}
	_, r := m.headerLayout()
	mm, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: r.x, Y: 0})
	if mm.(Model).modal != ModalPickRepo {
		t.Fatal("no-color header touch failed")
	}
}

func TestPhoneWidthTouchActions(t *testing.T) {
	for _, width := range []int{32, 40} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := buildModel(t, width, 24)
			got, cmd := tapHint(t, m, "d")
			if got.modal != ModalTask || got.pendingInstall || cmd == nil {
				t.Fatal("phone download hint did not start")
			}
			m = buildModel(t, width, 24)
			m.focus, m.buildDetail = PanelArtifacts, true
			m.updateListBounds()
			got, cmd = tapHint(t, m, "i")
			if got.modal != ModalTask || !got.pendingInstall || cmd == nil {
				t.Fatal("phone install hint did not start")
			}
			m = newTestModel()
			m.width = width
			m.cachedUserRepos = []ghapi.Repo{{FullName: "acme/rocket"}}
			got, _ = tapHint(t, m, "R")
			if got.modal != ModalPickRepo {
				t.Fatal("phone repository hint did not open picker")
			}
		})
	}
}
