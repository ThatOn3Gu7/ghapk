package ui

// Hit is a clickable region.
type Hit struct {
	R    Rect
	ID   string
	N    int // row index or other payload
	Wide bool
}

// Hits collects the clickable regions of the current frame. Later regions sit
// on top of earlier ones, matching draw order.
type Hits struct{ list []Hit }

// Reset forgets all regions (call at the start of each frame, and before
// drawing a modal so nothing behind it can be clicked).
func (h *Hits) Reset() { h.list = h.list[:0] }

// Add registers a region.
func (h *Hits) Add(r Rect, id string, n int) {
	if r.W > 0 && r.H > 0 {
		h.list = append(h.list, Hit{R: r, ID: id, N: n})
	}
}

// At returns the topmost region under x,y.
func (h *Hits) At(x, y int) (Hit, bool) {
	for i := len(h.list) - 1; i >= 0; i-- {
		if h.list[i].R.Contains(x, y) {
			return h.list[i], true
		}
	}
	return Hit{}, false
}
