package tui

import (
	"context"
	"strings"

	"ghapk/internal/engine"
	"ghapk/internal/term"
	"ghapk/internal/ui"
)

type engineQuestion = engine.Question

type pickItem struct {
	Label string
	Sub   string
	Value string
	Mute  bool // drawn dim (e.g. "all branches")
}

// pickerModal is a filterable list: repositories, branches, workflows, enum
// choices. Typing filters; Enter or a tap chooses.
type pickerModal struct {
	title    string
	items    []pickItem
	filtered []int
	in       *ui.Input
	list     ui.List
	loading  bool
	errMsg   string
	custom   string // when set, free text is allowed; the label for it ("Use")
	current  string // value to mark as current
	onPick   func(pickItem)
	viewH    int
	id       int
}

func (m *pickerModal) refilter() {
	q := strings.Fields(strings.ToLower(m.in.String()))
	m.filtered = m.filtered[:0]
	for i, it := range m.items {
		hay := strings.ToLower(it.Label + " " + it.Sub)
		ok := true
		for _, t := range q {
			if !strings.Contains(hay, t) {
				ok = false
				break
			}
		}
		if ok {
			m.filtered = append(m.filtered, i)
		}
	}
	n := len(m.filtered)
	if m.customRow() {
		n++
	}
	m.list.SetN(n)
}

// customRow reports whether a "use what I typed" row is offered.
func (m *pickerModal) customRow() bool {
	t := strings.TrimSpace(m.in.String())
	if m.custom == "" || t == "" {
		return false
	}
	for _, it := range m.items {
		if strings.EqualFold(it.Value, t) {
			return false
		}
	}
	return true
}

func (m *pickerModal) setItems(items []pickItem, err error) {
	m.loading = false
	m.items = items
	if err != nil {
		m.errMsg = err.Error()
	}
	m.refilter()
	for i, idx := range m.filtered {
		if m.current != "" && m.items[idx].Value == m.current {
			m.list.Set(i, 8)
			break
		}
	}
}

func (a *App) pickerLoading() bool {
	pm, ok := a.modal.(*pickerModal)
	return ok && pm.loading
}

func (m *pickerModal) draw(a *App) {
	p, th := &a.P, a.th
	w := a.dialogWidth(70)
	rows := m.list.N
	if rows < 3 {
		rows = 3
	}
	h := rows + 6
	if h > a.H-2 {
		h = a.H - 2
	}
	r := a.centered(w, h)
	note := ""
	if !m.loading {
		note = ui.Plural(len(m.items), "item")
	}
	in := p.Box(r, m.title, note, true)
	a.hits.Add(ui.Rect{X: 0, Y: 0, W: a.W, H: a.H}, "modal.bg", 0)
	a.hits.Add(r, "modal.box", 0)
	p.Text(in.X+1, in.Y, "›", th.Acc(), 1)
	ph := "type to filter"
	if m.custom != "" {
		ph = "type to filter, or enter a name"
	}
	m.in.Draw(p, in.X+3, in.Y, in.W-4, true, ph)
	p.HLine(in.X, in.Y+1, in.W, th.Frame(false))
	m.viewH = in.H - 3
	if m.viewH < 1 {
		m.viewH = 1
	}
	top := in.Y + 2
	switch {
	case m.loading:
		p.Text(in.X+1, top, p.Spinner()+" loading…", th.Dim(), in.W-2)
	case m.errMsg != "" && len(m.items) == 0:
		for i, ln := range ui.Wrap(m.errMsg, in.W-2) {
			if i >= m.viewH {
				break
			}
			p.Text(in.X+1, top+i, ln, th.Bad(), in.W-2)
		}
	default:
		from, to := m.list.Visible(m.viewH)
		for i := from; i < to; i++ {
			y := top + i - from
			sel := i == m.list.Cur
			st, dim := th.Base(), th.Dim()
			if sel {
				st = th.Sel(true)
				dim = st
				p.Row(in.X, y, in.W, st)
			}
			if i >= len(m.filtered) { // the custom row
				p.Text(in.X+1, y, term.Truncate(m.custom+" “"+strings.TrimSpace(m.in.String())+"”", in.W-2, "…"), th.Acc().Bg(st.BG).With(st.Attr), in.W-2)
			} else {
				it := m.items[m.filtered[i]]
				mark := "  "
				if m.current != "" && it.Value == m.current {
					mark = a.gl.Check + " "
				}
				ls := st
				if it.Mute && !sel {
					ls = dim
				}
				label, sub := ui.FitLeftRight(it.Label, it.Sub, in.W-4, a.gl.Ellipsis)
				p.Text(in.X+1, y, mark+label, ls, in.W-2)
				if sub != "" {
					p.Right(in.X+1, y, in.W-2, []ui.Span{ui.Sp(sub, dim)})
				}
			}
			a.hits.Add(ui.Rect{X: in.X, Y: y, W: in.W, H: 1}, "pick.row", i)
		}
		if m.list.N == 0 && !m.loading {
			p.Text(in.X+1, top, "nothing matches", th.Dim(), in.W-2)
		}
		p.Scrollbar(in.X+in.W-1, top, m.viewH, m.list.N, m.list.Off, m.viewH)
	}
	p.Text(in.X+1, in.Y+in.H-1, "↵ choose · Esc cancel", th.Dim(), in.W-2)
}

func (m *pickerModal) choose(i int) bool {
	if i < 0 || i >= m.list.N {
		return false
	}
	var it pickItem
	if i >= len(m.filtered) {
		t := strings.TrimSpace(m.in.String())
		it = pickItem{Label: t, Value: t}
	} else {
		it = m.items[m.filtered[i]]
	}
	if m.onPick != nil {
		m.onPick(it)
	}
	return true
}

func (m *pickerModal) key(a *App, ev term.Event) bool {
	h := m.viewH
	if h < 1 {
		h = 5
	}
	switch {
	case ev.Key == term.KeyEsc:
		return true
	case ev.Key == term.KeyEnter:
		return m.choose(m.list.Cur)
	case ev.Key == term.KeyDown, ev.IsCtrl('n'):
		m.list.Move(1, h)
	case ev.Key == term.KeyUp, ev.IsCtrl('p'):
		m.list.Move(-1, h)
	case ev.Key == term.KeyPgDn:
		m.list.Move(h, h)
	case ev.Key == term.KeyPgUp:
		m.list.Move(-h, h)
	default:
		if m.in.Handle(ev) {
			m.refilter()
			m.list.Set(0, h)
		}
	}
	return false
}

func (m *pickerModal) click(a *App, h ui.Hit, ok bool, ev term.Event) bool {
	if ok && h.ID == "pick.row" {
		return m.choose(h.N)
	}
	return !ok || h.ID == "modal.bg"
}

func (m *pickerModal) wheel(a *App, dir int) { m.list.Scroll(dir, m.viewH) }

// openPicker shows a ready list.
func (a *App) openPicker(title string, items []pickItem, current, custom string, onPick func(pickItem)) *pickerModal {
	m := &pickerModal{title: title, in: ui.NewInput(""), custom: custom, current: current, onPick: onPick}
	m.setItems(items, nil)
	a.modal = m
	return m
}

var pickSeq int

// openPickerAsync shows a picker straight away in its loading state and fills
// it in when load returns.
func (a *App) openPickerAsync(title, current, custom string, load func(ctx context.Context) ([]pickItem, error), onPick func(pickItem)) {
	pickSeq++
	m := &pickerModal{title: title, in: ui.NewInput(""), custom: custom, current: current, onPick: onPick, loading: true, id: pickSeq}
	a.modal = m
	a.async(func(ctx context.Context) func(*App) {
		items, err := load(ctx)
		return func(a *App) {
			if cur, ok := a.modal.(*pickerModal); ok && cur.id == m.id {
				cur.setItems(items, err)
			}
		}
	})
}
