package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
)

// RepoPicker is the modal for choosing/searching a repository.
type RepoPicker struct {
	input textinput.Model
	list  components.List // recents + detected + search hits
	lastQ string
	hits  []ghapi.SearchHit
	mode  int // 0 = typing, 1 = navigating list
}

type repoSearchDebounced struct {
	seq   int
	query string
}

func (m *Model) openRepoPicker() {
	ti := textinput.New()
	ti.Placeholder = "owner/repository"
	if m.settings.Repo != "" {
		ti.Placeholder = m.settings.Repo
	}
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40
	rp := &RepoPicker{input: ti}
	rp.rebuild(m, nil)
	m.repoPicker = rp
	m.modal = ModalPickRepo
}

func (rp *RepoPicker) rebuild(m *Model, hits []ghapi.SearchHit) {
	var items []components.Item
	q := strings.TrimSpace(rp.input.Value())
	if m.gitDetected != "" {
		items = append(items, components.Item{
			ID:    m.gitDetected,
			Label: m.gitDetected + "  (from git remote)",
		})
	}
	for _, r := range config.LoadRecents() {
		if r.Repo == m.gitDetected {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.Repo), strings.ToLower(q)) {
			continue
		}
		items = append(items, components.Item{ID: r.Repo, Label: r.Repo})
	}
	for _, h := range hits {
		dup := false
		for _, it := range items {
			if it.ID == h.FullName {
				dup = true
				break
			}
		}
		if !dup {
			items = append(items, components.Item{
				ID:     h.FullName,
				Label:  h.FullName,
				Detail: fmt.Sprintf("★ %d", h.Stars),
			})
		}
	}
	keepID := any(nil)
	if sel, ok := rp.list.Selected(); ok {
		keepID = sel.ID
	}
	rp.list.SetItems(items)
	if keepID != nil {
		rp.list.SelectByID(keepID)
	}
	rp.list.MaxShown = 9
	rp.list.Focused = rp.mode == 1
}

func (rp *RepoPicker) setSearch(query string, hits []ghapi.SearchHit) {
	if query != rp.lastQ {
		return
	}
	rp.hits = hits
}

// update is a no-op: rebuilds happen eagerly on keystrokes.
func (rp *RepoPicker) update(msg tea.Msg) tea.Cmd { return nil }

func (rp *RepoPicker) handleKey(msg tea.KeyMsg, m *Model) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.modal = ModalNone
		m.repoPicker = nil
		// if we still have no repo and no data, quitting the picker is fine
		return *m, nil
	case "tab", "down":
		rp.mode = 1
		rp.list.Focused = true
		rp.list.MoveBy(1)
		return *m, nil
	case "up":
		if rp.mode == 1 {
			rp.list.MoveBy(-1)
			if rp.list.SelectedIndex() <= 0 {
				// stay; up past top returns to typing? keep simple
			}
		} else {
			rp.mode = 1
			rp.list.Focused = true
		}
		return *m, nil
	case "enter":
		var repo string
		if rp.mode == 1 {
			if sel, ok := rp.list.Selected(); ok {
				repo, _ = sel.ID.(string)
			}
		} else {
			repo = strings.TrimSpace(rp.input.Value())
			if repo == "" {
				repo = m.settings.Repo // accept the placeholder repo
			}
		}
		if repo == "" || !strings.Contains(repo, "/") {
			m.toastf("type owner/repository or pick a suggestion")
			return *m, nil
		}
		m.modal = ModalNone
		m.repoPicker = nil
		if repo != m.settings.Repo {
			m.setRepo(repo, true)
			m.toastf("repo: %s", repo)
			return *m, tea.Batch(m.refreshAll()...)
		}
		return *m, nil
	}

	if rp.mode == 1 {
		switch key {
		case "j":
			rp.list.MoveBy(1)
			return *m, nil
		case "k":
			rp.list.MoveBy(-1)
			return *m, nil
		case "shift+tab":
			rp.mode = 0
			rp.list.Focused = false
			return *m, nil
		}
		// any typing returns focus to the input
		rp.mode = 0
		rp.list.Focused = false
	}

	var cmd tea.Cmd
	prev := rp.input.Value()
	rp.input, cmd = rp.input.Update(msg)
	if rp.input.Value() != prev {
		rp.rebuild(m, rp.hits)
		q := strings.TrimSpace(rp.input.Value())
		if q != rp.lastQ && len(q) >= 3 && !strings.Contains(q, "/") {
			rp.lastQ = q
			m.searchSeq++
			seq := m.searchSeq
			return *m, tea.Batch(cmd, tea.Tick(450*time.Millisecond, func(t time.Time) tea.Msg {
				return repoSearchDebounced{seq: seq, query: q}
			}))
		}
		if strings.Contains(q, "/") {
			rp.lastQ = q
			return *m, tea.Batch(cmd, searchReposCmd(q))
		}
	}
	return *m, cmd
}

// search debounce message handled in app.go — add case here via method
// (repoSearchDebounced is processed in Update below)

// view renders the repo picker modal.
func (rp *RepoPicker) view(m *Model) string {
	st := m.st
	var b strings.Builder
	b.WriteString(st.Muted.Render("Type a GitHub repository, or pick a recent one:") + "\n\n")
	b.WriteString(rp.input.View() + "\n\n")
	if rp.list.Focused {
		b.WriteString(st.Dim.Render("↑/↓ navigate · enter to choose · esc to close") + "\n")
	} else {
		b.WriteString(st.Dim.Render("tab to browse the list · enter to use what you typed · esc to close") + "\n")
	}
	w := 56
	if m.width-20 < w {
		w = m.width - 20
	}
	if w < 30 {
		w = 30
	}
	b.WriteString(rp.list.Render(st, w))
	return b.String()
}
