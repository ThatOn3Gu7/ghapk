package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// VisibleWidth is the terminal cell width of a string, ignoring ANSI escapes.
func VisibleWidth(s string) int {
	w := 0
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if alpha(r) {
				inEsc = false
			}
			continue
		}
		w += runewidth.RuneWidth(r)
	}
	return w
}

func alpha(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// Clamp truncates s to w visible cells, adding an ellipsis when it cuts.
// ANSI styles inside the string are preserved; a reset is appended on cut.
func Clamp(s string, w int) string {
	if VisibleWidth(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	var b strings.Builder
	cols := 0
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		if inEsc {
			b.WriteRune(r)
			if alpha(r) {
				inEsc = false
			}
			continue
		}
		rw := runewidth.RuneWidth(r)
		if cols+rw > w-1 {
			break
		}
		cols += rw
		b.WriteRune(r)
	}
	b.WriteString("\x1b[0m…")
	return b.String()
}

// Pad pads s on the right with spaces to exactly w visible cells.
func Pad(s string, w int) string {
	d := w - VisibleWidth(s)
	if d <= 0 {
		return s
	}
	return s + strings.Repeat(" ", d)
}

// Fit forces s to exactly w visible cells (truncate or pad).
func Fit(s string, w int) string { return Pad(Clamp(s, w), w) }

// Lines splits and fits every line of s to w, returning exactly h lines
// (truncating extras, padding with blank lines).
func Lines(s string, w, h int) []string {
	ls := strings.Split(s, "\n")
	if len(ls) > h {
		ls = ls[:h]
	}
	out := make([]string, h)
	for i := range out {
		if i < len(ls) {
			out[i] = Fit(ls[i], w)
		} else {
			out[i] = strings.Repeat(" ", w)
		}
	}
	return out
}

// StyleSeq extracts the SGR prefix a lipgloss style would emit, by rendering
// a sentinel and looking at what comes before it. Going through lipgloss
// (instead of hand-writing ESC[48;2;… escapes) keeps the terminal's color
// profile honored and is a no-op in NO_COLOR/ASCII modes.
func StyleSeq(st lipgloss.Style) string {
	const sent = "Q"
	r := st.Render(sent)
	i := strings.Index(r, sent)
	if i <= 0 {
		return ""
	}
	return r[:i]
}

// ReStyle surrounds s with seq: every inner styled run ends with ESC[0m,
// which would kill the surrounding effect (background, bold, …) for the
// remainder of the line and leave an unpainted "strip" behind. ReStyle
// re-emits seq after every reset so the effect spans the whole line,
// and appends one final reset at the end.
func ReStyle(s, seq string) string {
	if seq == "" {
		return s
	}
	return seq + strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+seq) + "\x1b[0m"
}
