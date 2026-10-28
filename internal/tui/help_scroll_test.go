package tui

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/ghapk/ghapk/internal/tui/components"
	"github.com/muesli/termenv"
)

func openHelp(t *testing.T, w, h int) Model {
	t.Helper()
	m := buildModel(t, w, h)
	next, cmd := m.Update(key("?"))
	m = next.(Model)
	if cmd != nil || m.modal != ModalHelp {
		t.Fatal("help did not open")
	}
	return m
}

func helpMouse(t *testing.T, m Model, msg tea.MouseMsg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	if cmd != nil {
		t.Fatal("unexpected command from help mouse input")
	}
	return next.(Model)
}

func assertHelpBar(t *testing.T, m Model) (int, int) {
	t.Helper()
	r := m.helpBounds()
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Fatalf("help overflows %dx%d: %d lines", m.width, m.height, len(lines))
	}
	lineWidths(t, m.width, m.height, lines)
	canvas := cellbuf.NewBuffer(m.width, m.height)
	cellbuf.SetContent(canvas, view)
	start, size := m.helpThumb()
	for y := 0; y < r.h; y++ {
		want := '│'
		if y >= start && y < start+size {
			want = '█'
		}
		if got := canvas.Cell(r.x+r.w-1, r.y+y).Rune; got != want {
			t.Fatalf("bar row %d got %q want %q, bounds=%+v", y, got, want, r)
		}
	}
	return start, size
}

func TestHelpScrollIndicatorAndKeyboard(t *testing.T) {
	withTrueColor(t)
	for _, sz := range [][2]int{{32, 18}, {40, 24}, {64, 18}, {80, 26}, {100, 30}, {121, 41}} {
		t.Run(fmt.Sprint(sz), func(t *testing.T) {
			m := openHelp(t, sz[0], sz[1])
			start, size := assertHelpBar(t, m)
			if start != 0 || size < 1 {
				t.Fatalf("initial thumb=%d,%d", start, size)
			}
			mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
			m = mm.(Model)
			if m.helpVP.YOffset != 2 {
				t.Fatal("down arrow stopped working")
			}
			mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
			m = mm.(Model)
			if m.helpVP.YOffset <= 2 {
				t.Fatal("page down stopped working")
			}
			m.helpVP.GotoBottom()
			start, size = assertHelpBar(t, m)
			if start+size != m.helpVP.Height {
				t.Fatal("thumb did not reach bottom")
			}
			if !strings.Contains(components.StripANSI(m.helpVP.View()), "algorithm.") {
				t.Fatal("bottom of help not reachable")
			}
			mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
			m = mm.(Model)
			if m.helpVP.AtBottom() {
				t.Fatal("up arrow stopped working")
			}
			mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			m = mm.(Model)
			if m.modal != ModalNone {
				t.Fatal("escape did not close help")
			}
		})
	}
}

func TestHelpWheelAndTouchSwipeStayInsideModal(t *testing.T) {
	for _, sz := range [][2]int{{32, 18}, {40, 24}, {100, 30}} {
		t.Run(fmt.Sprint(sz), func(t *testing.T) {
			m := openHelp(t, sz[0], sz[1])
			r := m.helpBounds()
			x, y := r.x+1, r.y+r.h/2
			m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseWheelDown, X: 0, Y: 0})
			if m.helpVP.YOffset != 0 {
				t.Fatal("wheel outside modal scrolled help")
			}
			m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseWheelDown, X: x, Y: y})
			if m.helpVP.YOffset != 3 {
				t.Fatal("wheel down did not scroll")
			}
			m = helpMouse(t, m, tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, X: x, Y: y})
			if !m.helpVP.AtTop() {
				t.Fatal("wheel up did not scroll")
			}
			m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y})
			m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: x, Y: y - 3})
			if m.helpVP.YOffset != 3 {
				t.Fatalf("swipe up offset %d", m.helpVP.YOffset)
			}
			m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: x, Y: y - 1})
			if m.helpVP.YOffset != 1 {
				t.Fatalf("swipe down offset %d", m.helpVP.YOffset)
			}
			m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseRelease, Action: tea.MouseActionRelease, X: x, Y: y - 1})
			m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseMotion, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: x, Y: y - 4})
			if m.helpVP.YOffset != 1 {
				t.Fatal("motion after release scrolled help")
			}
			m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 0, Y: 0})
			m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseMotion, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: x, Y: y - 3})
			if m.helpVP.YOffset != 1 || m.modal != ModalHelp {
				t.Fatal("drag from outside affected modal or dashboard")
			}
			assertHelpBar(t, m)
		})
	}
}

func TestHelpScrollbarTouchAndResize(t *testing.T) {
	m := openHelp(t, 40, 24)
	r := m.helpBounds()
	x := r.x + r.w - 1
	m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: r.y + r.h - 1})
	if !m.helpVP.AtBottom() {
		t.Fatal("tapping bottom of track did not scroll to bottom")
	}
	m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: x, Y: r.y})
	if !m.helpVP.AtTop() {
		t.Fatal("dragging thumb to top did not scroll to top")
	}
	m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseRelease, Action: tea.MouseActionRelease})
	m.helpVP.GotoBottom()
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 32, Height: 18})
	m = mm.(Model)
	if !m.helpVP.AtBottom() || m.helpDragging {
		t.Fatal("rotation lost scroll position or left drag active")
	}
	assertHelpBar(t, m)
	mm, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	if !m.helpVP.AtBottom() {
		t.Fatal("resize did not retain bottom")
	}
	assertHelpBar(t, m)
}

func TestHelpReflowsLongLinesAndHidesUnneededIndicator(t *testing.T) {
	m := openHelp(t, 32, 18)
	var all strings.Builder
	for {
		all.WriteString(components.StripANSI(m.helpVP.View()))
		if m.helpVP.AtBottom() {
			break
		}
		m.helpVP.PageDown()
	}
	text := strings.Join(strings.Fields(all.String()), " ")
	if !strings.Contains(text, "scores its artifacts for your device") || !strings.Contains(text, "swipe to scroll help") {
		t.Fatalf("help was truncated instead of wrapped: %s", text)
	}
	m.helpVP.SetContent("short help")
	m.helpVP.GotoTop()
	if _, size := m.helpThumb(); size != 0 {
		t.Fatal("indicator shown when content fits")
	}
	if strings.Contains(components.StripANSI(m.renderHelp()), "█") {
		t.Fatal("unneeded thumb rendered")
	}
}

// Exercise actual Bubble Tea SGR events, including a touch-style drag and wheel.
func TestLiveTeaHelpTouchScroll(t *testing.T) {
	m := openHelp(t, 40, 24)
	r := m.helpBounds()
	x, y := r.x+1, r.y+r.h/2
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	var out syncBuf
	updates := make(chan Model, 100)
	p := tea.NewProgram(touchHarness{model: m, updates: updates}, tea.WithInput(input), tea.WithOutput(&out), tea.WithMouseCellMotion(), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	defer p.Kill()
	p.Send(tea.WindowSizeMsg{Width: 40, Height: 24})
	waitFor := func(pred func(Model) bool) {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case state := <-updates:
				if pred(state) {
					return
				}
			case err := <-done:
				t.Fatalf("program exited: %v", err)
			case <-timer.C:
				t.Fatalf("timed out: %s", components.StripANSI(out.String()))
			}
		}
	}
	send := func(code, x, y int, end byte) {
		t.Helper()
		if _, err := fmt.Fprintf(writer, "\x1b[<%d;%d;%d%c", code, x+1, y+1, end); err != nil {
			t.Fatal(err)
		}
	}
	send(0, x, y, 'M')
	waitFor(func(m Model) bool { return m.helpDragging })
	send(32, x, y-3, 'M')
	waitFor(func(m Model) bool { return m.helpVP.YOffset == 3 })
	send(0, x, y-3, 'm')
	waitFor(func(m Model) bool { return !m.helpDragging })
	send(65, x, y, 'M')
	waitFor(func(m Model) bool { return m.helpVP.YOffset == 6 })
	wrap := m.helpWrapBounds()
	send(0, wrap.x, wrap.y, 'M')
	waitFor(func(m Model) bool { return m.helpWrapOff })
	send(0, wrap.x, wrap.y, 'm')
	if _, err := fmt.Fprint(writer, "W"); err != nil {
		t.Fatal(err)
	}
	waitFor(func(m Model) bool { return !m.helpWrapOff })
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

func TestHelpThumbGrabDoesNotJump(t *testing.T) {
	m := openHelp(t, 40, 24)
	m.helpVP.SetContent(strings.Repeat("line\n", m.helpVP.Height*2-1) + "last")
	m.helpVP.SetYOffset(m.helpVP.Height / 2)
	start, size := m.helpThumb()
	if size < 2 {
		t.Fatalf("test needs a multi-line thumb, got %d", size)
	}
	r := m.helpBounds()
	old := m.helpVP.YOffset
	x, y := r.x+r.w-1, r.y+start+size-1
	m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y})
	if m.helpVP.YOffset != old {
		t.Fatal("thumb jumped on press")
	}
	m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseMotion, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: x, Y: y + 1})
	if m.helpVP.YOffset <= old {
		t.Fatal("thumb did not drag down")
	}
}

func TestHelpIndicatorInPlainTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(old)
	m := openHelp(t, 40, 24)
	assertHelpBar(t, m)
	r := m.helpBounds()
	m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseWheelDown, X: r.x + 1, Y: r.y + 1})
	if m.helpVP.YOffset != 3 {
		t.Fatal("plain terminal wheel did not work")
	}
}
