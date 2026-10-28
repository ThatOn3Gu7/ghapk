// Package components holds the small reusable widgets the dashboard is
// assembled from: framed panels, scrollable selection lists, a status bar,
// modal dialogs and text input overlays.
package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/tui/theme"
)

// Panel is a bordered box with a title and fixed outer size.
// The frame is drawn manually (not via lipgloss borders) so the title can
// sit on the border exactly like lazygit's windows.
type Panel struct {
	Title   string
	Focused bool
	Width   int  // outer width including borders
	Height  int  // outer height including border lines
	Center  bool // float the content block in the middle of the panel
	Badge   string
	ScrollY int // line offset of scrolled content (adds a scroll indicator)
	Content string
}

// InnerWidth/InnerHeight give the usable content area.
func (p Panel) InnerWidth() int {
	if p.Width < 3 {
		return 0
	}
	return p.Width - 4 // border + padding each side
}

func (p Panel) InnerHeight() int {
	if p.Height < 3 {
		return 0
	}
	return p.Height - 2
}

// Render draws "╭─ Title ─────╮ / padded content lines / ╰──────╯".
func (p Panel) Render(st theme.Styles) string {
	w := p.Width
	h := p.Height
	if w < 8 || h < 3 {
		return ""
	}
	iw, ih := p.InnerWidth(), p.InnerHeight()

	borderCol := st.P.Border
	titleSt := st.PanelTitle
	if p.Focused {
		borderCol = st.P.BorderFocus
		titleSt = st.PanelTitleFoc
	}
	bs := lipgloss.NewStyle().Foreground(borderCol)

	title := titleSt.Render(" " + p.Title + " ")
	badge := ""
	if p.Badge != "" {
		badge = st.BadgeStyle.Render(p.Badge)
	}
	// top border: ╭─ title ─..─ badge? ╮
	// width budget: ╭─ is 2 cells, ╮ is 1 → dashes get what is left of w-3
	gap := w - 3 - VisibleWidth(title) - VisibleWidth(badge)
	if gap < 1 {
		avail := w - 3 - VisibleWidth(badge) - 2
		if avail < 0 {
			avail = 0
		}
		title = titleSt.Render(" " + Clamp(p.Title, avail) + " ")
		gap = w - 3 - VisibleWidth(title) - VisibleWidth(badge)
		if gap < 0 {
			gap = 0
		}
	}
	top := bs.Render("╭─") + title + badge + bs.Render(strings.Repeat("─", gap)) + bs.Render("╮")

	content := p.Content
	if p.Center {
		content = CenterBlock(content, iw, ih)
	}
	lines := Lines(content, iw, ih)
	var b strings.Builder
	b.WriteString(top)
	b.WriteByte('\n')
	for i, ln := range lines {
		ind := ""
		if p.ScrollY > 0 && i == ih-1 && ih > 2 {
			ind = st.Dim.Render(" ↓")
			if len(ln) >= iw {
				ln = Clamp(ln, iw-3) + ind
			}
		}
		// Preserve styling on highlighted content without painting the panel.
		b.WriteString(bs.Render("│") + " " + ln + " " + bs.Render("│"))
		if i < ih-1 {
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')
	b.WriteString(bs.Render("╰" + strings.Repeat("─", w-2) + "╯"))
	return b.String()
}

// CenterBlock pads content so the text block floats in the middle of a w×h
// area. Every line gets the SAME left padding, so internal alignment (cursor
// columns, key/value pairs) is preserved — the block itself is centered, not
// each line individually.
func CenterBlock(s string, w, h int) string {
	raw := strings.Split(s, "\n")
	for len(raw) > 0 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	if len(raw) == 0 {
		return s
	}
	maxW := 0
	for _, ln := range raw {
		if vw := VisibleWidth(ln); vw > maxW {
			maxW = vw
		}
	}
	if maxW < w {
		lp := strings.Repeat(" ", (w-maxW)/2)
		for i, ln := range raw {
			if ln != "" {
				raw[i] = lp + ln
			}
		}
	}
	if len(raw) < h {
		off := (h - len(raw)) / 2
		raw = append(make([]string, off), raw...)
	}
	return strings.Join(raw, "\n")
}
