package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
)

func outsideTap(m Model, x, y int) (Model, tea.Cmd) {
	next, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y})
	m = next.(Model)
	next, cmd := m.Update(tea.MouseMsg{Type: tea.MouseRelease, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease, X: x, Y: y})
	return next.(Model), cmd
}

func TestOutsideClickDismissesEachOverlayWithoutClickThrough(t *testing.T) {
	for _, kind := range []ModalKind{ModalHelp, ModalPickRepo, ModalPickBranch, ModalPickWorkflow, ModalEditRepo, ModalConfirm, ModalTask, ModalDoctor, ModalPlan} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			m := buildModel(t, 100, 30)
			switch kind {
			case ModalHelp:
				m.helpVP = m.buildHelp()
			case ModalPickRepo:
				m.openRepoPicker()
			case ModalPickBranch:
				m.openBranchPicker()
			case ModalPickWorkflow:
				m.openWorkflowPicker()
			case ModalEditRepo:
				m.openRepoEditor("example/repo")
			case ModalConfirm:
				m.confirmKind = confirmSafetyOverride
				m.confirm = components.Confirm{Title: "Safety", Body: []string{"Warning"}, Options: []string{"Install anyway", "Cancel"}, Cursor: 0}
				m.safetyInstallPath = "/never/install.apk"
			case ModalTask:
				m.task = newTask("Done", "app.apk", progressColor{m.st.P.Accent, m.st.P.AccentAlt})
				m.task.running = false
			case ModalDoctor:
				m.doctor = &DoctorState{ready: true}
			case ModalPlan:
				m.plan = &installPlan{choices: []planChoice{{art: ghapi.Artifact{ID: 1, Name: "test.apk"}}}}
			}
			m.modal = kind
			beforeRepo, beforeFocus := m.settings.Repo, m.focus
			if m.modalBounds().contains(0, 0) {
				t.Fatalf("fixture covers outside: %+v", m.modalBounds())
			}
			m, cmd := outsideTap(m, 0, 0)
			if m.modal != ModalNone || m.settings.Repo != beforeRepo || m.focus != beforeFocus || len(m.jobs) != 0 {
				t.Fatalf("outside tap did not only dismiss: modal=%v focus=%v repo=%s", m.modal, m.focus, m.settings.Repo)
			}
			if cmd != nil {
				t.Fatalf("unexpected action command for %v", kind)
			}
			if kind == ModalConfirm && m.safetyInstallPath != "" {
				t.Fatal("safety override retained")
			}
			if kind == ModalTask && m.task != nil {
				t.Fatal("finished task retained")
			}
		})
	}
}

func TestOutsideClickCancelsPlanAndIgnoresLatePreflight(t *testing.T) {
	m := buildModel(t, 80, 24)
	ctx, cancel := context.WithCancel(context.Background())
	m.plan = &installPlan{seq: 37, loading: true, cancel: cancel, choices: []planChoice{{art: ghapi.Artifact{ID: 1, Name: "app.apk"}}}}
	m.modal = ModalPlan
	m, _ = outsideTap(m, 0, 0)
	if m.modal != ModalNone || m.plan != nil || ctx.Err() == nil {
		t.Fatal("plan not cancelled")
	}
	mm, cmd := m.Update(planReadyMsg{seq: 37})
	m = mm.(Model)
	if cmd != nil || m.modal != ModalNone || len(m.jobs) != 0 {
		t.Fatal("late plan result revived operation")
	}
}

func TestOutsideClickRejectsQueuedJobQuestions(t *testing.T) {
	m := buildModel(t, 40, 24)
	m.startJob(jobSpec{})
	m.startJob(jobSpec{})
	q1 := jobQuestionMsg{id: 1, title: "Signing key mismatch", body: []string{"Warning"}, options: []string{"Cancel", "Uninstall first"}, danger: 1, reply: make(chan int, 1)}
	q2 := jobQuestionMsg{id: 2, title: "Already installed", body: []string{"Same version"}, options: []string{"Keep APK", "Reinstall"}, danger: -1, reply: make(chan int, 1)}
	mm, _ := m.Update(q1)
	m = mm.(Model)
	mm, _ = m.Update(q2)
	m = mm.(Model)
	m, _ = outsideTap(m, 0, 0)
	select {
	case choice := <-q1.reply:
		if choice != 0 {
			t.Fatal("outside click accepted destructive option")
		}
	default:
		t.Fatal("first job not answered")
	}
	if m.modal != ModalConfirm || m.jobQuestionID != 2 {
		t.Fatal("next question was not queued")
	}
	m, _ = outsideTap(m, 0, 0)
	select {
	case choice := <-q2.reply:
		if choice != 0 {
			t.Fatal("outside click accepted reinstall")
		}
	default:
		t.Fatal("second job not answered")
	}
	for _, j := range m.jobs {
		j.cancel()
	}
}

func TestOutsideClickCancelsPendingTaskAndIgnoresLateCompletion(t *testing.T) {
	m := buildModel(t, 80, 24)
	ctx, cancel := context.WithCancel(context.Background())
	m.taskSeq = 9
	m.task = newTask("Installing", "app.apk", progressColor{m.st.P.Accent, m.st.P.AccentAlt})
	m.task.Kind = "install"
	m.task.seq = 9
	m.task.cancel = cancel
	m.pendingInstall = true
	m.modal = ModalTask
	m, _ = outsideTap(m, 0, 0)
	if m.modal != ModalNone || m.task != nil || m.pendingInstall || ctx.Err() == nil {
		t.Fatal("pending task not cancelled")
	}
	mm, cmd := m.Update(InstallPreparedMsg{seq: 9, Path: "/not/an/apk"})
	m = mm.(Model)
	if cmd != nil || m.task != nil || m.modal != ModalNone {
		t.Fatal("late prepare started installer")
	}
	m.task = newTask("New task", "other.apk", progressColor{m.st.P.Accent, m.st.P.AccentAlt})
	m.task.seq = 11
	m.modal = ModalTask
	mm, cmd = m.Update(InstallDoneMsg{seq: 9})
	m = mm.(Model)
	if cmd != nil || m.task.phase != "downloading" || !m.task.running {
		t.Fatal("late completion modified a newer task")
	}
}

func TestOnlyOutsideTapsDismiss(t *testing.T) {
	m := buildModel(t, 40, 24)
	m.modal = ModalHelp
	m.helpVP = m.buildHelp()
	r := m.modalBounds()
	insideX, insideY := r.x, r.y
	m, _ = outsideTap(m, insideX, insideY)
	if m.modal != ModalHelp {
		t.Fatal("border click dismissed")
	}
	for _, msg := range []tea.MouseMsg{{Type: tea.MouseWheelDown, X: 0, Y: 0}, {Button: tea.MouseButtonRight, Action: tea.MouseActionPress, X: 0, Y: 0}, {Type: tea.MouseMotion, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: 0, Y: 0}} {
		mm, _ := m.Update(msg)
		m = mm.(Model)
		if m.modal != ModalHelp {
			t.Fatal("non-left-click dismissed")
		}
	}
	mm, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 0, Y: 0})
	m = mm.(Model)
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseMotion, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: 1, Y: 0})
	m = mm.(Model)
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseRelease, Action: tea.MouseActionRelease, X: 1, Y: 0})
	m = mm.(Model)
	if m.modal != ModalHelp {
		t.Fatal("outside swipe dismissed")
	}
}

func TestOutsideClickDismissesOverlayOnSetupScreen(t *testing.T) {
	m := buildModel(t, 80, 24)
	m.screen = ScreenSetup
	m.modal = ModalHelp
	m.helpVP = m.buildHelp()
	m, _ = outsideTap(m, 0, 0)
	if m.modal != ModalNone || m.quitting {
		t.Fatal("setup overlay did not dismiss safely")
	}
}

// Exercise actual SGR mouse input, not just synthetic Bubble Tea messages.
func TestLiveOutsideTapDismissesOverlay(t *testing.T) {
	m := buildModel(t, 80, 24)
	m.helpVP = m.buildHelp()
	m.modal = ModalHelp
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	var out syncBuf
	updates := make(chan Model, 100)
	p := tea.NewProgram(touchHarness{model: m, updates: updates}, tea.WithInput(input), tea.WithOutput(&out), tea.WithMouseCellMotion(), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	defer p.Kill()
	p.Send(tea.WindowSizeMsg{Width: 80, Height: 24})
	if _, err := fmt.Fprint(writer, "\x1b[<0;1;1M\x1b[<0;1;1m"); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case state := <-updates:
			if state.modal == ModalNone {
				if state.focus != m.focus || state.repoPicker != nil {
					t.Fatal("tap reached dashboard")
				}
				p.Quit()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-timer.C:
					t.Fatal("program did not exit")
				}
				return
			}
		case err := <-done:
			t.Fatalf("program exited early: %v", err)
		case <-timer.C:
			t.Fatalf("tap not recognized: %s", out.String())
		}
	}
}

func TestCancelledInstallerClosesLogWaiter(t *testing.T) {
	root, _ := jobTestEnv(t)
	path := filepath.Join(root, "app.apk")
	if err := os.WriteFile(path, []byte("apk"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	logs := make(chan tea.Msg, 64)
	done := make(chan struct{})
	go func() { installApkCmd(ctx, 5, path, false, logs)(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled installer did not stop")
	}
	// Cancellation may leave a final message in the buffer; draining it must
	// terminate rather than leaving a waiting Bubble Tea command behind.
	for range logs {
	}
	if msg := waitInstallLog(logs)(); msg != nil {
		t.Fatalf("closed log channel produced %#v", msg)
	}
}
