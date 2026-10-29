package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// asciiInterface maps UI decoration to single-cell ASCII while keeping ANSI
// sequences and non-UI text intact. Width is preserved so touch hitboxes and
// centered overlay geometry remain exactly where they were before conversion.
func asciiInterface(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			start := i
			i++
			if i < len(s) && s[i] == '[' {
				i++
				for i < len(s) {
					c := s[i]
					i++
					if c >= 0x40 && c <= 0x7e {
						break
					}
				}
			} else if i < len(s) && s[i] == ']' {
				i++
				for i < len(s) {
					if s[i] == 7 {
						i++
						break
					}
					if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			} else if i < len(s) {
				i++
			}
			out.WriteString(s[start:i])
			continue
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		if repl, ok := asciiDecoration(r); ok {
			width := runewidth.RuneWidth(r)
			if width <= 0 {
				continue
			}
			out.WriteString(repl)
			if len(repl) < width {
				out.WriteString(strings.Repeat(" ", width-len(repl)))
			}
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
func asciiDecoration(r rune) (string, bool) {
	switch r {
	case '─', '━', '═', '–', '—':
		return "-", true
	case '│', '┃', '║':
		return "|", true
	case '╭', '╮', '╰', '╯', '╔', '╗', '╚', '╝', '┌', '┐', '└', '┘', '├', '┤', '┬', '┴', '┼', '╠', '╣', '╦', '╩', '╬':
		return "+", true
	case '█', '▄', '▀', '▉', '▊', '▋', '▌', '▍', '▎', '▏':
		return "#", true
	case '░', '▒', '▓':
		return ".", true
	case '✓':
		return "+", true
	case '✗', '⊗', '⊘':
		return "x", true
	case '★', '☆', '◆', '◇', '◉', '○', '◌', '◐', '•', '·':
		return "*", true
	case '↑':
		return "^", true
	case '↓':
		return "v", true
	case '→', '▶', '↵', '↗':
		return ">", true
	case '←', '‹':
		return "<", true
	case '…':
		return ".", true
	case '“', '”':
		return "\"", true
	case '’':
		return "'", true
	case '⌫':
		return "<", true
	case '≠':
		return "!", true
	case '∅':
		return "-", true
	case '⚙', '⚡', '🔍', '📁', '📂', '📦', '🤖', '📱', '🍎', '⊞', '🐧', '🌐', '📄':
		return "*", true
	}
	if r >= 0x2800 && r <= 0x28ff {
		return "*", true
	}
	if r == 0xfe0f || r == 0xfe0e || r == 0x200d {
		return "", true
	}
	return "", false
}
