package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// A line built from separately styled segments must keep its background
// end-to-end: resets inside the line used to leave terminal-default strips.
func TestReStyleKeepsBackground(t *testing.T) {
	bg := lipgloss.NewStyle().Background(lipgloss.Color("#112233"))
	seq := StyleSeq(bg)
	if seq == "" {
		t.Skip("colorless environment")
	}
	inner := lipgloss.NewStyle().Bold(true).Render("label")
	line := ReStyle(" "+inner+" padded tail ", seq)
	if !strings.HasPrefix(line, seq) {
		t.Error("line should start with the bg sequence")
	}
	// the reset emitted by the bold segment must be followed by the bg seq again
	if !strings.Contains(line, "\x1b[0m"+seq) {
		t.Error("bg sequence not re-emitted after an inner reset")
	}
	if !strings.HasSuffix(line, "\x1b[0m") {
		t.Error("line should end with exactly one reset")
	}
}

// NO_COLOR must make painting a complete no-op.
func TestReStyleNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	s := ReStyle("plain", StyleSeq(lipgloss.NewStyle().Background(lipgloss.Color("#112233"))))
	if s != "plain" {
		t.Errorf("NO_COLOR leak: %q", s)
	}
}
