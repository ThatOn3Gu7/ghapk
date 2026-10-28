package ui

import (
	"fmt"
	"strings"
	"time"

	"ghapk/internal/term"
)

// Wrap breaks s into lines at most w columns wide, on word boundaries where it
// can, keeping explicit newlines. Words longer than a line are cut.
func Wrap(s string, w int) []string {
	if w < 1 {
		return nil
	}
	var out []string
	for _, para := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		indent := ""
		trim := strings.TrimLeft(para, " ")
		if n := len(para) - len(trim); n > 0 && n < w/2 {
			indent = para[:n]
		}
		line := indent
		lw := term.StringWidth(indent)
		for _, word := range strings.Fields(trim) {
			ww := term.StringWidth(word)
			for ww > w-lw && lw == term.StringWidth(indent) && ww > w { // hard cut a giant word
				cut := term.Clip(word, w-lw)
				out = append(out, line+cut)
				word = word[len(cut):]
				ww = term.StringWidth(word)
				line, lw = indent, term.StringWidth(indent)
			}
			switch {
			case lw == term.StringWidth(indent):
				line += word
				lw += ww
			case lw+1+ww <= w:
				line += " " + word
				lw += 1 + ww
			default:
				out = append(out, line)
				line, lw = indent+word, term.StringWidth(indent)+ww
			}
		}
		out = append(out, line)
	}
	return out
}

// Bytes formats a size: 4.2 MB.
func Bytes(n int64) string {
	const k = 1024.0
	f := float64(n)
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case f < k*k:
		return fmt.Sprintf("%.1f KB", f/k)
	case f < k*k*k:
		return fmt.Sprintf("%.1f MB", f/(k*k))
	}
	return fmt.Sprintf("%.2f GB", f/(k*k*k))
}

// Rate formats bytes/second.
func Rate(bps float64) string {
	if bps <= 0 {
		return "–"
	}
	return Bytes(int64(bps)) + "/s"
}

// Dur formats a duration compactly: 42s, 3m07s, 1h02m.
func Dur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// Ago formats how long ago t was: "just now", "5m", "3h", "2d", "4w", "Jan 02".
func Ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < 45*time.Second:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()+0.5))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()+0.5))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/24/7))
	}
	return t.Format("Jan 02")
}

// Plural returns "1 build" / "3 builds".
func Plural(n int, one string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %ss", n, one)
}

// FitLeftRight lays out a left and a right string in w columns: the left one is
// truncated first.
func FitLeftRight(left, right string, w int, ell string) (string, string) {
	rw := term.StringWidth(right)
	if rw >= w {
		return "", term.Truncate(right, w, ell)
	}
	room := w - rw - 1
	return term.Truncate(left, room, ell), right
}
