package ui

// List tracks a cursor and a scroll offset over N rows. It owns no data: the
// screen draws rows itself and asks the list which ones are visible.
type List struct {
	N, Cur, Off int
}

// SetN changes the row count and keeps the cursor in range.
func (l *List) SetN(n int) {
	l.N = n
	if l.Cur >= n {
		l.Cur = n - 1
	}
	if l.Cur < 0 {
		l.Cur = 0
	}
	if l.Off > l.Cur {
		l.Off = l.Cur
	}
}

// Move shifts the cursor by d rows.
func (l *List) Move(d, h int) { l.Set(l.Cur+d, h) }

// Set puts the cursor on row i (clamped) and scrolls it into view of h rows.
func (l *List) Set(i, h int) {
	if i >= l.N {
		i = l.N - 1
	}
	if i < 0 {
		i = 0
	}
	l.Cur = i
	l.Fit(h)
}

// Fit scrolls just enough to show the cursor in a window of h rows.
func (l *List) Fit(h int) {
	if h < 1 {
		h = 1
	}
	if l.Cur < l.Off {
		l.Off = l.Cur
	}
	if l.Cur >= l.Off+h {
		l.Off = l.Cur - h + 1
	}
	if max := l.N - h; l.Off > max {
		l.Off = max
	}
	if l.Off < 0 {
		l.Off = 0
	}
}

// Scroll moves the window without moving the cursor (mouse wheel).
func (l *List) Scroll(d, h int) {
	l.Off += d
	if max := l.N - h; l.Off > max {
		l.Off = max
	}
	if l.Off < 0 {
		l.Off = 0
	}
}

// Visible returns the first and one-past-last row to draw in a window of h.
func (l *List) Visible(h int) (from, to int) {
	l.Fit(h)
	to = l.Off + h
	if to > l.N {
		to = l.N
	}
	return l.Off, to
}
