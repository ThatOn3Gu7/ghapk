package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"
)

// Overlay places an opaque, centered dialog over a dimmed terminal screen.
// A cell buffer preserves ANSI styles and wide glyphs, and replaces every
// dialog cell (including spaces) so text behind it cannot leak through.
func Overlay(base, box string, width, height int) string {
	boxWidth, boxHeight := lipgloss.Width(box), lipgloss.Height(box)
	// Preserve existing behavior for dialogs larger than the viewport.
	canvasWidth, canvasHeight := max(width, boxWidth), max(height, boxHeight)
	if canvasWidth <= 0 || canvasHeight <= 0 {
		return base
	}
	screen := cellbuf.NewBuffer(canvasWidth, canvasHeight)
	cellbuf.SetContent(screen, base)
	if lipgloss.ColorProfile() != termenv.Ascii {
		for y := 0; y < canvasHeight; y++ {
			for x := 0; x < canvasWidth; x++ {
				c := screen.Cell(x, y)
				// Blank cells need no foreground dimming. Cell returns the
				// stored cell for text, so style it in place without disturbing
				// a wide glyph's continuation cells.
				if c == nil || c.Width == 0 || c.Rune == ' ' {
					continue
				}
				c.Style.Attrs |= cellbuf.FaintAttr
			}
		}
	}
	// lipgloss.Place centers with the extra odd cell on the right/bottom.
	x, y := max(0, (width-boxWidth)/2), max(0, (height-boxHeight)/2)
	cellbuf.SetContentRect(screen, box, cellbuf.Rect(x, y, boxWidth, boxHeight))
	lines := make([]string, canvasHeight)
	for y := range lines {
		lines[y] = overlayLine(screen, y, canvasWidth)
	}
	return strings.Join(lines, "\n")
}

// Render full styles at transitions: cellbuf's differential renderer emits
// SGR 22 when bold ends, which also cancels faint even when faint continues.
func overlayLine(screen *cellbuf.Buffer, y, width int) string {
	var b strings.Builder
	var style cellbuf.Style
	var link cellbuf.Link
	for x := 0; x < width; x++ {
		c := screen.Cell(x, y)
		if c == nil || c.Width == 0 {
			continue
		}
		if !c.Style.Equal(&style) {
			if !style.Empty() {
				b.WriteString(ansi.ResetStyle)
			}
			if !c.Style.Empty() {
				b.WriteString(c.Style.Sequence())
			}
			style = c.Style
		}
		if c.Link != link {
			if !link.Empty() {
				b.WriteString(ansi.ResetHyperlink())
			}
			if !c.Link.Empty() {
				b.WriteString(ansi.SetHyperlink(c.Link.URL, c.Link.Params))
			}
			link = c.Link
		}
		b.WriteString(c.String())
	}
	if !link.Empty() {
		b.WriteString(ansi.ResetHyperlink())
	}
	if !style.Empty() {
		b.WriteString(ansi.ResetStyle)
	}
	return b.String()
}
