package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"
)

func TestOverlayDimsBackgroundAndKeepsDialogOpaque(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	selected := lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#005500")).Bold(true).Render("SELECTED")
	base := strings.Join([]string{"background " + selected, "abcdefghijklmnopqrstuvwxyz1234", "abcdefghijklmnopqrstuvwxyz1234", "background row four", "background row five"}, "\n")
	box := strings.Join([]string{"╭── Dialog ──╮", "│  CONTENT   │", "╰────────────╯"}, "\n")
	out := Overlay(base, box, 30, 5)
	rows := strings.Split(out, "\n")
	if len(rows) != 5 {
		t.Fatalf("height=%d", len(rows))
	}
	for _, row := range rows {
		if VisibleWidth(row) != 30 {
			t.Fatalf("wrong width: %d", VisibleWidth(row))
		}
	}
	cells := cellbuf.NewBuffer(30, 5)
	cellbuf.SetContent(cells, out)
	if c := cells.Cell(0, 0); c.Rune != 'b' || c.Style.Attrs&cellbuf.FaintAttr == 0 {
		t.Fatalf("background not dimmed: %+v", c)
	}
	if c := cells.Cell(11, 0); c.Rune != 'S' || c.Style.Attrs&cellbuf.FaintAttr == 0 || c.Style.Bg == nil || c.Style.Attrs&cellbuf.BoldAttr == 0 {
		t.Fatalf("background highlight lost: %+v", c)
	}
	boxX := (30 - lipgloss.Width(box)) / 2
	if c := cells.Cell(boxX, 1); c.Rune != '╭' || c.Style.Attrs&cellbuf.FaintAttr != 0 {
		t.Fatalf("modal border not fully bright: %+v", c)
	}
	if c := cells.Cell(boxX+3, 2); c.Rune != 'C' || c.Style.Attrs&cellbuf.FaintAttr != 0 {
		t.Fatalf("modal content dimmed: %+v", c)
	}
	if c := cells.Cell(boxX+1, 2); c.Rune != ' ' || c.Style.Bg != nil {
		t.Fatalf("background leaked through modal padding: %+v", c)
	}
	if c := cells.Cell(boxX+lipgloss.Width(box), 2); c.Rune == ' ' || c.Style.Attrs&cellbuf.FaintAttr == 0 {
		t.Fatalf("background next to dialog not visible/dim: %+v", c)
	}
}

func TestOverlayWideGlyphBoundaryAndNoColor(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(old)
	// The modal's left border intersects a wide character. Its exposed half
	// must become a blank rather than leaving a broken glyph or shifting text.
	base := "ab界XYZ-----\n0123456789ab\nABCDEFGHIJKL"
	box := "[box]\n[   ]\n[end]"
	out := Overlay(base, box, 12, 3)
	cells := cellbuf.NewBuffer(12, 3)
	cellbuf.SetContent(cells, out)
	if c := cells.Cell(3, 0); c.Rune != '[' {
		t.Fatalf("modal border shifted: %+v", c)
	}
	if c := cells.Cell(2, 0); c.Rune != ' ' {
		t.Fatalf("wide glyph split across modal: %+v", c)
	}
	if c := cells.Cell(0, 0); c.Rune != 'a' || c.Style.Attrs&cellbuf.FaintAttr != 0 {
		t.Fatalf("plain terminal background changed: %+v", c)
	}
	for _, line := range strings.Split(out, "\n") {
		if VisibleWidth(line) != 12 {
			t.Fatalf("bad width: %q", line)
		}
	}
}

func TestOverlayPreservesDimAcrossResets(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	base := "\x1b[1;31mA\x1b[22mB\x1b[0mC\x1b[38;2;4;5;6mD\x1b[39mE\nbackground\nbackground"
	box := "[\x1b[1;32mX\x1b[0m]"
	out := Overlay(base, box, 10, 3)
	cells := cellbuf.NewBuffer(10, 3)
	cellbuf.SetContent(cells, out)
	for x := 0; x < 5; x++ {
		c := cells.Cell(x, 0)
		if c.Style.Attrs&cellbuf.FaintAttr == 0 {
			t.Fatalf("faint cancelled at %c", c.Rune)
		}
	}
	if cells.Cell(0, 0).Style.Attrs&cellbuf.BoldAttr == 0 || cells.Cell(1, 0).Style.Attrs&cellbuf.BoldAttr != 0 {
		t.Fatal("bold reset not preserved")
	}
	c := cells.Cell(4, 1)
	if c.Rune != 'X' || c.Style.Attrs&cellbuf.FaintAttr != 0 || c.Style.Attrs&cellbuf.BoldAttr == 0 || c.Style.Fg == nil {
		t.Fatalf("modal styled text changed: %+v", c)
	}
}

func TestOverlayMatchesLipglossCentering(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(old)
	for _, size := range [][2]int{{10, 6}, {11, 7}, {12, 8}, {4, 2}} {
		box := "+---+\n| A |\n+---+"
		w, h := max(size[0], lipgloss.Width(box)), max(size[1], lipgloss.Height(box))
		got := cellbuf.NewBuffer(w, h)
		cellbuf.SetContent(got, Overlay("", box, size[0], size[1]))
		want := cellbuf.NewBuffer(w, h)
		cellbuf.SetContent(want, lipgloss.Place(size[0], size[1], lipgloss.Center, lipgloss.Center, box))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if got.Cell(x, y).String() != want.Cell(x, y).String() {
					t.Fatalf("%v at %d,%d: got %q want %q", size, x, y, got.Cell(x, y), want.Cell(x, y))
				}
			}
		}
	}
}
