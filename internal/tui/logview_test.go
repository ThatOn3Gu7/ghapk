package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ThatOn3Gu7/ghapk/internal/activity"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
)

func TestLogPanelHistoryFollowAndTouch(t *testing.T) {
	dir := t.TempDir()
	old, err := activity.Open(dir, activity.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	_ = old.Write("job", "previous session job")
	_ = old.Close()
	store, err := activity.Open(dir, activity.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := 0; i < 50; i++ {
		_ = store.Write("job", fmt.Sprintf("step %d", i))
	}
	for _, sz := range [][2]int{{32, 18}, {40, 24}, {80, 26}, {120, 40}} {
		m := buildModel(t, sz[0], sz[1])
		m.activity = store
		mm, cmd := m.Update(key("7"))
		m = mm.(Model)
		if m.focus != PanelLog || cmd == nil {
			t.Fatal("log panel not opened or refresh not scheduled")
		}
		v := components.StripANSI(m.View())
		lines := strings.Split(v, "\n")
		if len(lines) != sz[1] {
			t.Fatalf("%v line count %d", sz, len(lines))
		}
		lineWidths(t, sz[0], sz[1], lines)
		if !strings.Contains(v, "step 49") || !strings.Contains(v, "following") {
			t.Fatalf("log not following: %s", v)
		}
		mm, _ = m.Update(key("g"))
		m = mm.(Model)
		v = components.StripANSI(m.View())
		if !strings.Contains(strings.Join(m.logRows(), " "), "previous session job") || !strings.Contains(v, time.Now().Local().Format("2006-01-02")) {
			t.Fatalf("previous session/date missing: %s", v)
		}
		if m.logState.follow {
			t.Fatal("top should pause following")
		}
		mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseWheelDown, X: 5, Y: 5})
		m = mm.(Model)
		if m.logState.offset == 0 {
			t.Fatal("wheel did not scroll")
		}
		y := 7
		mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 5, Y: y})
		m = mm.(Model)
		prev := m.logState.offset
		mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseMotion, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: 5, Y: y - 2})
		m = mm.(Model)
		if m.logState.offset <= prev {
			t.Fatal("swipe did not scroll")
		}
		mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseRelease, Action: tea.MouseActionRelease, X: 5, Y: y - 2})
		m = mm.(Model)
		if m.logState.dragging {
			t.Fatal("drag stuck")
		}
		mm, _ = m.Update(key("G"))
		m = mm.(Model)
		if !m.logState.follow {
			t.Fatal("G did not follow")
		}
		_ = store.Write("job", "newest event")
		if !strings.Contains(components.StripANSI(m.View()), "newest event") {
			t.Fatal("new entry not followed")
		}
	}
}
func TestAuditCapturesActionsAndJobsWithoutTypedInput(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	store, err := activity.Open(t.TempDir(), activity.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	m := buildModel(t, 80, 26)
	m.activity = store
	mm, _ := m.Update(key("5"))
	m = mm.(Model)
	m.settingsUI.cursor = rowIndex("package")
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	for _, r := range "secret-input-12345" {
		mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(Model)
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mm.(Model)
	mm, _ = m.Update(jobEventMsg{id: 3, log: "Downloading 50% token=secret123456"})
	m = mm.(Model)
	mm, _ = m.Update(jobEventMsg{id: 3, log: "unlabelled-installer-output", transient: true})
	m = mm.(Model)
	mm, _ = m.Update(jobFinishedMsg{id: 3, status: "failed", err: fmt.Errorf("network failed")})
	m = mm.(Model)
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "secret-input-12345") || strings.Contains(text, "secret123456") || strings.Contains(text, "unlabelled-installer-output") || strings.Contains(text, "sensitive-paste") {
		t.Fatalf("private input leaked: %s", text)
	}
	for _, want := range []string{"Opened Settings", "job #3: Downloading", "network failed", "[redacted]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
}
func TestLogTickGenerationIgnoresStaleTimers(t *testing.T) {
	store, err := activity.Open(t.TempDir(), activity.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	m := buildModel(t, 40, 24)
	m.activity = store
	mm, _ := m.Update(key("7"))
	m = mm.(Model)
	seq := m.logSeq
	mm, _ = m.Update(key("5"))
	m = mm.(Model)
	mm, _ = m.Update(key("7"))
	m = mm.(Model)
	if seq == m.logSeq {
		t.Fatal("timer generation did not advance")
	}
	_, cmd := m.Update(logTickMsg{seq: seq})
	if cmd != nil {
		t.Fatal("stale timer restarted")
	}
}
func TestCopyLastSixtyLogEntries(t *testing.T) {
	dir := t.TempDir()
	clipboard := dir + "/clipboard"
	tool := dir + "/termux-clipboard-set"
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n/bin/cat > \"$LOG_CLIPBOARD\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("LOG_CLIPBOARD", clipboard)
	store, err := activity.Open(t.TempDir(), activity.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := 0; i < 80; i++ {
		_ = store.Write("job", fmt.Sprintf("entry-%02d", i))
	}
	m := buildModel(t, 40, 24)
	m.activity = store
	m.focus = PanelLog
	_, cmd := m.Update(key("y"))
	if cmd == nil {
		t.Fatal("copy command missing")
	}
	msg := cmd().(logCopiedMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	data, err := os.ReadFile(clipboard)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, "\n") != 60 || strings.Contains(text, "entry-00") || !strings.Contains(text, "entry-79") {
		t.Fatalf("wrong copied entries: %s", text)
	}
}
func TestLogErrorIsVisible(t *testing.T) {
	dir := t.TempDir()
	store, err := activity.Open(dir, activity.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(store.Path()); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(store.Path(), 0700); err != nil {
		t.Fatal(err)
	}
	if err = store.Write("test", "x"); err == nil {
		t.Fatal("expected write error")
	}
	m := buildModel(t, 80, 26)
	m.activity = store
	m.focus = PanelLog
	if v := components.StripANSI(m.View()); !strings.Contains(v, "Log error:") || !strings.Contains(v, "log error") {
		t.Fatalf("log error not visible: %s", v)
	}
}

func TestBrowsingLogDoesNotCreateScrollEntries(t *testing.T) {
	for _, size := range [][2]int{{32, 18}, {80, 26}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			store, err := activity.Open(t.TempDir(), activity.DefaultMaxBytes)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			for i := 0; i < 80; i++ {
				if err := store.Write("job", fmt.Sprintf("existing entry %d", i)); err != nil {
					t.Fatal(err)
				}
			}
			m := buildModel(t, size[0], size[1])
			m.activity = store
			mm, _ := m.Update(key("7"))
			m = mm.(Model)
			before, err := os.Stat(store.Path())
			if err != nil {
				t.Fatal(err)
			}
			count := len(store.Entries())
			apply := func(msg tea.Msg) { mm, _ := m.Update(msg); m = mm.(Model) }
			r := m.panelRects()[PanelLog]
			x, y := r.x+2, r.y+2
			apply(tea.MouseMsg{Type: tea.MouseWheelUp, X: x, Y: y})
			if m.logState.follow {
				t.Fatal("wheel failed to pause following")
			}
			start := m.logState.offset
			apply(tea.MouseMsg{Type: tea.MouseWheelUp, X: x, Y: y})
			if m.logState.offset >= start {
				t.Fatal("wheel failed to scroll upwards")
			}
			for i := 0; i < 120; i++ {
				apply(tea.MouseMsg{Type: tea.MouseWheelUp, X: x, Y: y})
			}
			if m.logState.offset != 0 {
				t.Fatalf("did not reach top: %d", m.logState.offset)
			}
			for i := 0; i < 120; i++ {
				apply(tea.MouseMsg{Type: tea.MouseWheelDown, X: x, Y: y})
			}
			bottom := m.logState.offset
			for i := 0; i < 20; i++ {
				apply(tea.MouseMsg{Type: tea.MouseWheelDown, X: x, Y: y})
			}
			if m.logState.offset != bottom {
				t.Fatal("scrolling past bottom moved offset")
			}
			for _, k := range []tea.KeyMsg{
				hintKey("g"), hintKey("G"), hintKey("f"), hintKey("up"), hintKey("down"), hintKey("j"), hintKey("k"),
				{Type: tea.KeyPgUp}, {Type: tea.KeyPgDown}, {Type: tea.KeyCtrlU}, {Type: tea.KeyCtrlD},
			} {
				apply(k)
			}
			apply(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y + 3})
			apply(tea.MouseMsg{Type: tea.MouseMotion, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: x, Y: y})
			apply(tea.MouseMsg{Type: tea.MouseRelease, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease, X: x, Y: y})
			sb := m.statusBar()
			_, spans := sb.Layout(m.st)
			for _, span := range spans {
				if logNavigationKey(sb.Hints[span.Index].Action) {
					apply(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: span.Start, Y: m.height - 1})
				}
			}
			after, err := os.Stat(store.Path())
			if err != nil {
				t.Fatal(err)
			}
			if len(store.Entries()) != count || after.Size() != before.Size() {
				t.Fatalf("browsing grew log: %d -> %d entries, %d -> %d bytes", count, len(store.Entries()), before.Size(), after.Size())
			}
			apply(jobEventMsg{id: 8, log: "real activity still recorded"})
			_, copyCmd := m.Update(key("y"))
			if copyCmd == nil {
				t.Fatal("copy no longer works")
			}
			apply(key("5"))
			data, err := os.ReadFile(store.Path())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "real activity still recorded") || !strings.Contains(string(data), "Opened Settings") || !strings.Contains(string(data), "key y") {
				t.Fatal("meaningful activity no longer logged")
			}
		})
	}
}

func TestWheelScrollOutsideLogIsNotRecorded(t *testing.T) {
	store, err := activity.Open(t.TempDir(), activity.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	m := buildModel(t, 80, 26)
	m.activity = store
	count := len(store.Entries())
	for i := 0; i < 50; i++ {
		mm, _ := m.Update(tea.MouseMsg{Type: tea.MouseWheelDown, X: 5, Y: 5})
		m = mm.(Model)
	}
	if len(store.Entries()) != count {
		t.Fatal("wheel scrolling outside Log generated entries")
	}
}
