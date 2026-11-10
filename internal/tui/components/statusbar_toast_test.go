package components

import (
	"strings"
	"testing"

	"github.com/ThatOn3Gu7/ghapk/internal/tui/theme"
)

func TestLongToastIsNeverDropped(t *testing.T) {
	st := theme.New(theme.Get("action"))
	hints := []Hint{{"→", "artifacts", "right"}, {"d", "download", "d"}, {"i", "install", "i"}, {"o", "open", "o"},
		{"tab", "panels", "tab"}, {"/", "filter", "/"}, {"?", "help", "?"}, {"q", "quit", "q"}}
	for _, w := range []int{40, 60, 80, 110} {
		sb := StatusBar{Width: w, Hints: hints, MessageErr: true, Message: `workflow "Old Name" is not in this repo — showing all runs`}
		line, spans := sb.Layout(st)
		plain := StripANSI(line)
		if VisibleWidth(line) != w {
			t.Errorf("w=%d: bar is %d cells wide", w, VisibleWidth(line))
		}
		if !strings.Contains(plain, "workflow") {
			t.Errorf("w=%d: the toast was dropped: %q", w, plain)
		}
		for _, sp := range spans { // every clickable span must still sit on its own label
			if sp.End > w || sp.Start >= sp.End {
				t.Errorf("w=%d: bad span %+v", w, sp)
			}
		}
	}
}

func TestRoutineToastNeverDisplacesHints(t *testing.T) {
	st := theme.New(theme.Get("action"))
	hints := []Hint{{"↵", "change", "enter"}, {"s", "scope", "s"}, {"?", "help", "?"}, {"q", "quit", "q"}}
	for _, w := range []int{40, 60, 80} {
		bare := StatusBar{Width: w, Hints: hints}
		_, want := bare.Layout(st)
		sb := StatusBar{Width: w, Hints: hints, Message: "Theme: save to this repo and then some more words"}
		line, got := sb.Layout(st)
		if len(got) != len(want) || VisibleWidth(line) != w {
			t.Errorf("w=%d: hints %d -> %d, width %d", w, len(want), len(got), VisibleWidth(line))
		}
	}
}

func TestStatusBarWithoutToastKeepsAllHints(t *testing.T) {
	st := theme.New(theme.Get("action"))
	sb := StatusBar{Width: 110, Hints: []Hint{{"?", "help", "?"}, {"q", "quit", "q"}}}
	if _, spans := sb.Layout(st); len(spans) != 2 {
		t.Fatalf("spans = %d", len(spans))
	}
}
