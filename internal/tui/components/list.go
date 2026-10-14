package components

import (
	"strings"

	"github.com/ghapk/ghapk/internal/tui/theme"
)

// Item is one row in a List.
type Item struct {
	Label    string // plain text used for filtering + fallback render
	Rendered string // pre-styled line (optional; Label rendered plainly if empty)
	ID       any    // caller-owned payload (run id, artifact index, repo name...)
	Disabled bool
	Detail   string // optional right-aligned dim text (size, time)
}

// List is a single-column scrollable, filterable selection list.
type List struct {
	Items    []Item
	Cursor   int // index into *filtered* items
	Offset   int
	Filter   string
	Focused  bool
	Empty    string // message when no items
	MaxShown int    // rows available
}

// Filtered returns items passing Filter (case-insensitive substring).
func (l *List) Filtered() []Item {
	if l.Filter == "" {
		return l.Items
	}
	f := strings.ToLower(l.Filter)
	var out []Item
	for _, it := range l.Items {
		if strings.Contains(strings.ToLower(it.Label), f) {
			out = append(out, it)
		}
	}
	return out
}

// Selected returns the highlighted item, if any.
func (l *List) Selected() (Item, bool) {
	fs := l.Filtered()
	if l.Cursor < 0 || l.Cursor >= len(fs) {
		return Item{}, false
	}
	return fs[l.Cursor], true
}

// SelectedIndex returns the cursor's index in the FILTERED slice.
func (l *List) SelectedIndex() int {
	fs := l.Filtered()
	if l.Cursor < 0 || l.Cursor >= len(fs) {
		return -1
	}
	return l.Cursor
}

// SetItems replaces items, keeping the cursor sane.
func (l *List) SetItems(items []Item) {
	l.Items = items
	l.clamp()
}

// SelectByID moves the cursor to the first item with a matching ID.
func (l *List) SelectByID(id any) {
	fs := l.Filtered()
	for i, it := range fs {
		if it.ID == id {
			l.Cursor = i
			l.clamp()
			return
		}
	}
}

// MoveBy moves the cursor by delta rows (filtered space).
func (l *List) MoveBy(d int) {
	l.Cursor += d
	l.clamp()
}

// Home/End jump to the edges.
func (l *List) Home() { l.Cursor = 0; l.clamp() }
func (l *List) End()  { l.Cursor = len(l.Filtered()) - 1; l.clamp() }

func (l *List) clamp() {
	n := len(l.Filtered())
	if n == 0 {
		l.Cursor = 0
		l.Offset = 0
		return
	}
	if l.Cursor < 0 {
		l.Cursor = 0
	}
	if l.Cursor >= n {
		l.Cursor = n - 1
	}
	if l.MaxShown < 1 {
		l.MaxShown = 1
	}
	if l.Cursor < l.Offset {
		l.Offset = l.Cursor
	}
	if l.Cursor >= l.Offset+l.MaxShown {
		l.Offset = l.Cursor - l.MaxShown + 1
	}
	if l.Offset > n-l.MaxShown && n >= l.MaxShown {
		l.Offset = n - l.MaxShown
	}
	if l.Offset < 0 {
		l.Offset = 0
	}
}

// AddFilterChar appends to the filter and re-clamps.
// CachedClamp exposes the window clamp for custom renderers (carded lists).
func (l *List) CachedClamp() { l.clamp() }

func (l *List) AddFilterChar(r rune) {
	l.Filter += string(r)
	l.Cursor = 0
	l.clamp()
}

// BackspaceFilter trims the filter.
func (l *List) BackspaceFilter() {
	if l.Filter != "" {
		r := []rune(l.Filter)
		l.Filter = string(r[:len(r)-1])
	}
	l.clamp()
}

// Render produces up to MaxShown rows.
func (l *List) Render(st theme.Styles, width int) string {
	fs := l.Filtered()
	if len(fs) == 0 {
		msg := l.Empty
		if msg == "" {
			msg = "  (empty)"
		}
		if l.Filter != "" {
			msg = "  no match for /" + l.Filter
		}
		return st.Dim.Render(msg)
	}
	l.clamp()
	var b strings.Builder
	end := l.Offset + l.MaxShown
	if end > len(fs) {
		end = len(fs)
	}
	for i := l.Offset; i < end; i++ {
		it := fs[i]
		row := it.Rendered
		if row == "" {
			row = "  " + it.Label
		}
		row = Clamp(row, width)
		if it.Detail != "" {
			d := st.Dim.Render(it.Detail)
			room := width - VisibleWidth(row) - VisibleWidth(d)
			if room > 1 {
				row += strings.Repeat(" ", room) + d
			}
		}
		if i == l.Cursor {
			row = Fit(row, width)
			cseq := StyleSeq(st.RowSelBlur)
			if l.Focused {
				cseq = StyleSeq(st.RowSel)
			}
			row = ReStyle(row, cseq)
		}
		b.WriteString(row)
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
