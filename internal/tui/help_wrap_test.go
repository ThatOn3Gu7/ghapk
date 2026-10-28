package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/ghapk/ghapk/internal/tui/components"
)

// Read the whole underlying viewport without its usual clipping or scrolling.
func allHelpLines(m Model) []string {
	vp := m.helpVP
	vp.Height = vp.TotalLineCount()
	vp.Width = 500
	vp.SetYOffset(0)
	result := strings.Split(vp.View(), "\n")
	for i := range result {
		result[i] = strings.TrimRight(components.StripANSI(result[i]), " ")
	}
	return result
}

func TestHelpWordWrapPreservesTextAndFits(t *testing.T) {
	withTrueColor(t)
	for _, w := range []int{32, 33, 40, 48, 64, 80, 100, 120} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			m := openHelp(t, w, 24)
			if m.helpWrapOff {
				t.Fatal("wrapping must be on by default")
			}
			lines := allHelpLines(m)
			if len(lines) != len(m.helpLineEntries) {
				t.Fatalf("entry map length %d != lines %d", len(m.helpLineEntries), len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > m.helpVP.Width {
					t.Fatalf("wrapped line exceeds %d columns: %q", m.helpVP.Width, line)
				}
			}
			paragraphs := map[int][]string{}
			for i, line := range lines {
				paragraphs[m.helpLineEntries[i]] = append(paragraphs[m.helpLineEntries[i]], strings.Fields(line)...)
			}
			var entries []string
			for _, parts := range paragraphs {
				entries = append(entries, strings.Join(parts, " "))
			}
			all := strings.Join(entries, "\n")
			for _, want := range []string{"tab / shift+tab cycle panels", "ctrl+d / ctrl+u page a list", "touch tap the repo name or bottom hints; swipe to scroll help", "select a repo or workflow; enter again to move on"} {
				if !strings.Contains(all, want) {
					t.Errorf("lost or split words %q: %s", want, all)
				}
			}
			assertHelpBar(t, m)
		})
	}
}

func TestHelpWrapToggleKeyboardTouchAndResize(t *testing.T) {
	m := openHelp(t, 32, 18)
	if !strings.Contains(components.StripANSI(m.View()), "W:wrap on") {
		t.Fatal("missing visible wrap hint")
	}
	m.helpVP.SetYOffset(m.helpVP.TotalLineCount() / 2)
	entry := m.helpLineEntries[m.helpVP.YOffset]
	wrappedCount := m.helpVP.TotalLineCount()
	mm, cmd := m.Update(key("W"))
	m = mm.(Model)
	if cmd != nil || m.modal != ModalHelp || !m.helpWrapOff {
		t.Fatal("W did not turn wrap off")
	}
	if m.helpLineEntries[m.helpVP.YOffset] != entry {
		t.Fatal("toggle moved away from current help entry")
	}
	if m.helpVP.TotalLineCount() >= wrappedCount {
		t.Fatal("off mode did not remove wrapped lines")
	}
	if !strings.Contains(components.StripANSI(m.View()), "W:wrap off") {
		t.Fatal("off state not visible")
	}
	r := m.helpWrapBounds()
	canvas := cellbuf.NewBuffer(m.width, m.height)
	cellbuf.SetContent(canvas, m.View())
	if r.w <= 0 || canvas.Cell(r.x, r.y).Rune != 'W' {
		t.Fatalf("touch target does not match visible hint: %+v", r)
	}
	m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: r.x, Y: r.y})
	if !m.helpWrapOff {
		t.Fatal("hover toggled wrap")
	}
	m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: r.x, Y: r.y})
	if m.helpWrapOff || m.helpLineEntries[m.helpVP.YOffset] != entry {
		t.Fatal("tap did not restore wrap at same entry")
	}
	assertHelpBar(t, m)
	mm, _ = m.Update(key("w"))
	m = mm.(Model)
	if !m.helpWrapOff {
		t.Fatal("lowercase w did not toggle")
	}
	mm, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m = mm.(Model)
	if !m.helpWrapOff || m.helpLineEntries[m.helpVP.YOffset] != entry {
		t.Fatal("resize lost mode or current entry")
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mm.(Model)
	mm, _ = m.Update(key("?"))
	m = mm.(Model)
	if !m.helpWrapOff || !strings.Contains(components.StripANSI(m.View()), "W:wrap off") {
		t.Fatal("wrap preference not kept for session")
	}
	assertHelpBar(t, m)
}

func TestHelpWrapOffClipsRatherThanReflowing(t *testing.T) {
	m := openHelp(t, 32, 18)
	mm, _ := m.Update(key("W"))
	m = mm.(Model)
	lines := allHelpLines(m)
	target := -1
	for i, line := range lines {
		if strings.Contains(line, "select a repo or workflow; enter again to move on") {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("source line missing")
	}
	id := m.helpLineEntries[target]
	for i, lineID := range m.helpLineEntries {
		if i != target && lineID == id {
			t.Fatal("disabled wrap added continuation lines")
		}
	}
	m.helpVP.SetYOffset(target)
	shown := strings.Split(components.StripANSI(m.helpVP.View()), "\n")[0]
	if strings.Contains(shown, "workflow; enter again") || components.VisibleWidth(shown) != m.helpVP.Width {
		t.Fatalf("long text not clipped: %q", shown)
	}
	if !strings.HasPrefix(strings.TrimSpace(shown), "enter") {
		t.Fatalf("wrong clipped entry: %q", shown)
	}
	r := m.helpBounds()
	before := m.helpVP.YOffset
	m = helpMouse(t, m, tea.MouseMsg{Type: tea.MouseWheelDown, X: r.x + 1, Y: r.y + 1})
	if m.helpVP.YOffset <= before {
		t.Fatal("wheel stopped working with wrap off")
	}
	assertHelpBar(t, m)
}

func TestHelpToggleKeepsTopAndBottom(t *testing.T) {
	m := openHelp(t, 40, 24)
	mm, _ := m.Update(key("W"))
	m = mm.(Model)
	if !m.helpVP.AtTop() {
		t.Fatal("toggle moved away from top")
	}
	m.helpVP.GotoBottom()
	mm, _ = m.Update(key("W"))
	m = mm.(Model)
	if !m.helpVP.AtBottom() {
		t.Fatal("toggle moved away from bottom")
	}
}

func TestWrappedContinuationRetainsThemeColor(t *testing.T) {
	withTrueColor(t)
	m := openHelp(t, 32, 18)
	lines := allHelpLines(m)
	index := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "again to move on" {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatal("wrapped continuation not found")
	}
	m.helpVP.SetYOffset(index)
	cells := cellbuf.NewBuffer(m.helpVP.Width, m.helpVP.Height)
	cellbuf.SetContent(cells, m.helpVP.View())
	if c := cells.Cell(4, 0); c.Rune != 'a' || c.Style.Fg == nil {
		t.Fatalf("continuation lost its style: %+v", c)
	}
}
