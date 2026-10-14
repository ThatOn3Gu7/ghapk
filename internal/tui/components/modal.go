package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/tui/theme"
)

// StatusBar is the single bottom line with key hints and a message.
type StatusBar struct {
	Width   int
	Hints   []Hint
	Message string
	Busy    bool
	BusyMsg string
	Spinner string // current spinner frame, if busy
}

// Hint is one "key: action" pair.
type Hint struct{ Key, Desc string }

// Render draws the bar.
func (sb StatusBar) Render(st theme.Styles) string {
	var left strings.Builder
	if sb.Busy {
		left.WriteString(st.StatusKey.Render(sb.Spinner + " " + sb.BusyMsg))
		left.WriteString(st.StatusText.Render("   "))
	}
	for i, h := range sb.Hints {
		left.WriteString(st.StatusKey.Render(h.Key))
		left.WriteString(st.StatusText.Render(":" + h.Desc))
		if i < len(sb.Hints)-1 {
			left.WriteString(st.StatusText.Render("  "))
		}
	}
	msg := ""
	if sb.Message != "" {
		msg = st.Toast.Render(" " + sb.Message + " ")
	}
	gap := sb.Width - VisibleWidth(left.String()) - VisibleWidth(msg)
	if gap < 1 {
		gap = 1
	}
	bar := left.String() + strings.Repeat(" ", gap) + msg
	bar = Clamp(bar, sb.Width)
	return st.StatusBarBg.Render(Pad(bar, sb.Width))
}

// Confirm is a yes/no style dialog.
type Confirm struct {
	Title   string
	Body    []string // pre-styled lines
	Options []string // e.g. {"Download", "Install", "Cancel"}
	Cursor  int
}

// Modal renders content centered in a bordered box on a dim background.
func Modal(st theme.Styles, width, height int, title string, body string) string {
	bw := width * 3 / 4
	if bw < 40 {
		bw = min(width-4, 40)
	}
	if bw > width-4 {
		bw = width - 4
	}
	bh := lipgloss.Height(body) + 4
	titleSt := st.PanelTitleFoc.Render(" " + title + " ")
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(st.P.BorderFocus).
		Padding(0, 2)
	box := border.Width(bw - 6).Render(body)
	lines := strings.Split(box, "\n")
	if len(lines) > 0 {
		lines[0] = overlayCenter(lines[0], titleSt)
	}
	box = strings.Join(lines, "\n")
	_ = bh
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(st.P.Bg))
}

func overlayCenter(borderLine, content string) string {
	plain := StripANSI(borderLine)
	runes := []rune(plain)
	cw := VisibleWidth(content)
	start := (len(runes) - cw) / 2
	if start < 1 || start+cw >= len(runes)-1 {
		return borderLine
	}
	color := extractFirstColor(borderLine)
	st := lipgloss.NewStyle()
	if color != "" {
		st = st.Foreground(lipgloss.Color(color))
	}
	left := string(runes[:start])
	right := string(runes[start+cw:])
	return st.Render(left) + content + st.Render(right)
}

// StripANSI removes ANSI escape sequences, returning printable text.
func StripANSI(s string) string {
	var b strings.Builder
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
		b.WriteRune(r)
	}
	return b.String()
}

func extractFirstColor(s string) string {
	i := strings.Index(s, "\x1b[38;2;")
	if i < 0 {
		// fallback: 256-color or 16-color
		i = strings.Index(s, "\x1b[38;5;")
		if i < 0 {
			// might be simple color like \x1b[32m
			j := strings.Index(s, "\x1b[")
			if j < 0 {
				return ""
			}
			k := strings.IndexByte(s[j:], 'm')
			if k < 0 {
				return ""
			}
			return "\x1b[" + s[j+2:j+k] + "m"
		}
	}
	j := strings.IndexByte(s[i:], 'm')
	if j < 0 {
		return ""
	}
	seq := s[i+2 : i+j]
	parts := strings.Split(seq, ";")
	if len(parts) == 5 {
		return "#" + hex(parts[2]) + hex(parts[3]) + hex(parts[4])
	}
	if len(parts) == 3 {
		return "\x1b[38;5;" + parts[2] + "m"
	}
	return ""
}

func hex(d string) string {
	n := 0
	for _, c := range d {
		n = n*10 + int(c-'0')
	}
	const h = "0123456789abcdef"
	if n < 0 || n > 255 {
		return "00"
	}
	return string([]byte{h[n/16], h[n%16]})
}

// RenderConfirm draws a Confirm dialog with buttons.
func (c Confirm) Render(st theme.Styles, width, height int) string {
	var b strings.Builder
	for _, ln := range c.Body {
		b.WriteString(ln + "\n")
	}
	b.WriteString("\n")
	var btns []string
	for i, opt := range c.Options {
		if i == c.Cursor {
			btns = append(btns, st.ButtonFocus.Render(opt))
		} else {
			btns = append(btns, st.ButtonBlur.Render(opt))
		}
	}
	b.WriteString(strings.Join(btns, "  "))
	return Modal(st, width, height, c.Title, b.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
