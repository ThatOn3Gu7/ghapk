package components

import (
	"strings"

	"github.com/ThatOn3Gu7/ghapk/internal/tui/theme"
	"github.com/charmbracelet/lipgloss"
)

// StatusBar is the single bottom line with key hints and a message.
type StatusBar struct {
	Width      int
	Hints      []Hint
	Message    string
	MessageErr bool // style the message as a failure
	Busy       bool
	BusyMsg    string
	Spinner    string // current spinner frame, if busy
}

// Hint is one "key: action" pair. Action is the keyboard action to dispatch
// when its visible label is tapped; it need not match the displayed glyph.
type Hint struct{ Key, Desc, Action string }

// HintSpan is a visible, complete hint's half-open cell range on the status bar.
type HintSpan struct{ Index, Start, End int }

// Layout draws the bar and returns the exact clickable ranges it rendered.
// Never draw a partial hint: a truncated label can suggest the wrong action.
func (sb StatusBar) Layout(st theme.Styles) (string, []HintSpan) {
	if sb.Width <= 0 {
		return "", nil
	}
	busy := ""
	if sb.Busy {
		busy = st.StatusKey.Render(sb.Spinner + " " + sb.BusyMsg)
		// On phones, keep the spinner but leave the line for tappable actions.
		if sb.Width < 48 {
			busy = st.StatusKey.Render(strings.TrimSpace(StripANSI(sb.Spinner)))
		} else {
			busy = Clamp(busy, max(1, sb.Width/3))
		}
	}
	busyW := VisibleWidth(busy)

	// place lays the hints out left to right inside budget cells, skipping any
	// that do not fit whole (a cut label could suggest the wrong action).
	place := func(budget int) (string, []HintSpan, int) {
		var left strings.Builder
		left.WriteString(busy)
		x := busyW
		var spans []HintSpan
		for i, h := range sb.Hints {
			label := st.StatusKey.Render(h.Key) + st.StatusText.Render(":"+h.Desc)
			w := VisibleWidth(label)
			gap := 0
			if x > 0 {
				gap = 2
			}
			if x+gap+w > budget {
				continue
			}
			left.WriteString(strings.Repeat(" ", gap))
			x += gap
			spans = append(spans, HintSpan{Index: i, Start: x, End: x + w})
			left.WriteString(label)
			x += w
		}
		return left.String(), spans, x
	}
	toast := func(maxW int) (string, int) {
		if sb.Message == "" || maxW < 6 {
			return "", 0
		}
		// the toast styles pad one cell each side, on top of the spaces below
		text := Clamp(sb.Message, maxW-4)
		style := st.Toast
		if sb.MessageErr {
			style = st.ToastErr
		}
		m := style.Render(" " + text + " ")
		return m, VisibleWidth(m)
	}

	msg, mw := toast(sb.Width)
	left, spans, x := place(sb.Width)
	if msg != "" && sb.Width-x-mw < 1 {
		if sb.MessageErr {
			// A failure must not vanish just because the hint row is long:
			// keep only the hints that fit beside it and, if even that is not
			// enough, shorten the message itself.
			left, spans, x = place(sb.Width - mw - 1)
			if sb.Width-x-mw < 1 {
				msg, mw = toast(sb.Width - x - 1)
			}
		} else {
			// Routine notices never push a tappable hint out; they take what
			// room is left, shortened, or nothing at all when that is too small.
			msg, mw = toast(sb.Width - x - 1)
			if mw < 14 {
				msg, mw = "", 0
			}
		}
	}
	if msg != "" {
		if gap := sb.Width - x - mw; gap >= 1 {
			left += strings.Repeat(" ", gap) + msg
		}
	}
	return Pad(left, sb.Width), spans
}

// Render draws the bar.
func (sb StatusBar) Render(st theme.Styles) string {
	line, _ := sb.Layout(st)
	return line
}

// Confirm is a yes/no style dialog.
type Confirm struct {
	Title       string
	Body        []string // pre-styled lines
	Options     []string // e.g. {"Download", "Install", "Cancel"}
	Cursor      int
	Danger      bool // destructive action: red frame and primary action
	DangerIndex int  // destructive choice in a question; -1 means none
}

// ModalBox renders a bordered dialog without the surrounding whitespace.
func ModalBox(st theme.Styles, width int, title string, body string) string {
	bw := width * 3 / 4
	if bw < 40 {
		bw = min(width-4, 40)
	}
	if bw > width-4 {
		bw = width - 4
	}
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
	return box
}

// Modal renders a dialog centered on the terminal background.
func Modal(st theme.Styles, width, height int, title, body string) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, ModalBox(st, width, title, body))
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

// Box draws the confirmation dialog and its buttons, without screen padding.
func (c Confirm) Box(st theme.Styles, width int) string {
	if c.Danger {
		st.P.BorderFocus = st.P.Error
		st.PanelTitleFoc = st.Err.Bold(true)
	}
	var b strings.Builder
	for _, ln := range c.Body {
		b.WriteString(ln + "\n")
	}
	b.WriteString("\n")

	var btns []string
	for i, opt := range c.Options {
		if i == c.Cursor {
			selected := st.ButtonFocus
			if c.Danger && i == 0 {
				selected = selected.Background(st.P.Error)
			}
			btns = append(btns, selected.Render(opt))
		} else {
			btns = append(btns, st.ButtonBlur.Render(opt))
		}
	}
	// Keep each choice intact on phones instead of letting the terminal wrap
	// a button's label across lines (which also makes touch hit-testing unsafe).
	bw := width * 3 / 4
	if bw < 40 {
		bw = min(width-4, 40)
	}
	if bw > width-4 {
		bw = width - 4
	}
	join := "  "
	if VisibleWidth(strings.Join(btns, join)) > bw-10 {
		join = "\n"
		btns = nil
		for i, opt := range c.Options {
			if i == c.Cursor {
				selected := st.ButtonFocus.Padding(0, 0)
				if c.Danger && i == 0 {
					selected = selected.Background(st.P.Error)
				}
				btns = append(btns, selected.Render(opt))
			} else {
				btns = append(btns, st.ButtonBlur.Padding(0, 0).Render(opt))
			}
		}
	}
	b.WriteString(strings.Join(btns, join))
	return ModalBox(st, width, c.Title, b.String())
}

// QuestionBox keeps numbered choices visible while the caller scrolls the body.
func QuestionBox(st theme.Styles, width int, title, body string) string {
	bw := min(width-2, 70)
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(st.P.BorderFocus).Padding(0, 1).Width(bw - 2).Render(body)
	lines := strings.Split(box, "\n")
	if len(lines) > 0 {
		lines[0] = overlayCenter(lines[0], st.PanelTitleFoc.Render(" "+title+" "))
	}
	return strings.Join(lines, "\n")
}

// Render draws a centered confirmation dialog.
func (c Confirm) Render(st theme.Styles, width, height int) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, c.Box(st, width))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
