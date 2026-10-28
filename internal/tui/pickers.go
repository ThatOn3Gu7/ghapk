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

// matchAllTokens reports whether every whitespace-separated token in query
// occurs case-insensitively in hay (matching Claude's picker filter behavior).
func matchAllTokens(hay, query string) bool {
	tokens := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	if len(tokens) == 0 {
		return true
	}
	low := strings.ToLower(hay)
	for _, tok := range tokens {
		if !strings.Contains(low, tok) {
			return false
		}
	}
	return true
}

// ───────────────────────────── Repo Picker (R) ─────────────────────────────

// RepoPicker is the modal for choosing or searching a GitHub repository.
type RepoPicker struct {
	input     textinput.Model
	list      components.List
	lastQ     string
	hits      []ghapi.SearchHit
	userRepos []ghapi.Repo
	loading   bool
}

type repoSearchDebounced struct {
	seq   int
	query string
}

func (m *Model) openRepoPicker() tea.Cmd {
	ti := textinput.New()
	ti.Placeholder = "type to filter or owner/repo"
	if m.settings.Repo != "" {
		ti.Placeholder = m.settings.Repo + " (type to filter or owner/repo)"
	}
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 44
	rp := &RepoPicker{
		input:     ti,
		userRepos: m.cachedUserRepos,
		loading:   len(m.cachedUserRepos) == 0,
	}
	rp.list.Empty = "  nothing matches — enter owner/repository"
	rp.rebuild(m)
	m.repoPicker = rp
	m.modal = ModalPickRepo
	if len(m.cachedUserRepos) == 0 {
		return fetchUserReposCmd()
	}
	return nil
}

func (rp *RepoPicker) rebuild(m *Model) {
	var items []components.Item
	seen := map[string]bool{}
	q := strings.TrimSpace(rp.input.Value())

	addRepo := func(slug, detail string) {
		key := strings.ToLower(slug)
		if slug == "" || seen[key] {
			return
		}
		if !matchAllTokens(slug+" "+detail, q) {
			return
		}
		seen[key] = true
		label := "  " + slug
		if strings.EqualFold(slug, m.settings.Repo) {
			label = m.st.G.OK + " " + slug
		}
		items = append(items, components.Item{
			ID:     slug,
			Label:  label,
			Detail: detail,
		})
	}

	if m.gitDetected != "" {
		addRepo(m.gitDetected, "git remote")
	}
	for _, r := range config.LoadRecents() {
		det := "recent"
		if r.Favourite {
			det = "★ recent"
		}
		addRepo(r.Repo, det)
	}
	for _, r := range rp.userRepos {
		det := "yours"
		if r.Private {
			det = "private"
		}
		addRepo(r.FullName, det)
	}
	for _, h := range rp.hits {
		addRepo(h.FullName, fmt.Sprintf("★ %d", h.Stars))
	}

	// Offer a custom "Open “owner/repo”" row when the typed slug isn't already listed.
	if validRepoSlug(q) && !seen[strings.ToLower(q)] {
		items = append(items, components.Item{
			ID:     q,
			Label:  "  Open “" + q + "”",
			Detail: "repository",
		})
	}

	keepID := any(nil)
	if sel, ok := rp.list.Selected(); ok {
		keepID = sel.ID
	}
	rp.list.SetItems(items)
	if keepID != nil {
		rp.list.SelectByID(keepID)
	} else if q == "" && m.settings.Repo != "" {
		rp.list.SelectByID(m.settings.Repo)
	}
	rp.list.MaxShown = 10
	rp.list.Focused = true
}

func (rp *RepoPicker) setSearch(m *Model, query string, hits []ghapi.SearchHit) {
	if query != rp.lastQ {
		return
	}
	rp.hits = hits
	rp.rebuild(m)
}

func (rp *RepoPicker) setUserRepos(m *Model, repos []ghapi.Repo) {
	rp.loading = false
	rp.userRepos = repos
	rp.rebuild(m)
}

func (rp *RepoPicker) update(m *Model, msg tea.Msg) tea.Cmd {
	if d, ok := msg.(repoSearchDebounced); ok {
		if d.seq == m.searchSeq && d.query == strings.TrimSpace(rp.input.Value()) {
			return searchReposCmd(d.query)
		}
		return nil
	}
	var cmd tea.Cmd
	rp.input, cmd = rp.input.Update(msg)
	return cmd
}

func (rp *RepoPicker) handleKey(msg tea.KeyMsg, m *Model) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.modal = ModalNone
		m.repoPicker = nil
		return *m, nil
	case "down", "ctrl+n", "tab":
		rp.list.MoveBy(1)
		return *m, nil
	case "up", "ctrl+p", "shift+tab":
		rp.list.MoveBy(-1)
		return *m, nil
	case "pgdown", "ctrl+d":
		rp.list.MoveBy(6)
		return *m, nil
	case "pgup", "ctrl+u":
		rp.list.MoveBy(-6)
		return *m, nil
	case "enter":
		var repo string
		q := strings.TrimSpace(rp.input.Value())
		if sel, ok := rp.list.Selected(); ok {
			repo, _ = sel.ID.(string)
		}
		if repo == "" && validRepoSlug(q) {
			repo = q
		}
		if repo == "" && q == "" {
			repo = m.settings.Repo
		}
		if !validRepoSlug(repo) {
			m.toastf("type owner/repository or pick a repository")
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

	var cmd tea.Cmd
	prev := rp.input.Value()
	rp.input, cmd = rp.input.Update(msg)
	if rp.input.Value() != prev {
		rp.rebuild(m)
		q := strings.TrimSpace(rp.input.Value())
		if q != rp.lastQ && len(q) >= 3 && !strings.Contains(q, "/") {
			rp.lastQ = q
			m.searchSeq++
			seq := m.searchSeq
			return *m, tea.Batch(cmd, tea.Tick(350*time.Millisecond, func(t time.Time) tea.Msg {
				return repoSearchDebounced{seq: seq, query: q}
			}))
		}
		if strings.Contains(q, "/") && q != rp.lastQ {
			rp.lastQ = q
			return *m, tea.Batch(cmd, searchReposCmd(q))
		}
	}
	return *m, cmd
}

func (rp *RepoPicker) view(m *Model) string {
	st := m.st
	var b strings.Builder
	sub := "Filter recents & your GitHub repos, or type owner/repo:"
	if rp.loading {
		sub += "  " + m.spin.View()
	}
	b.WriteString(st.Muted.Render(sub) + "\n\n")
	b.WriteString(rp.input.View() + "\n\n")
	b.WriteString(st.Dim.Render("↑/↓ select · type to filter/search · enter choose · esc close") + "\n")
	w := 58
	if m.width-16 < w {
		w = m.width - 16
	}
	if w < 30 {
		w = 30
	}
	b.WriteString(rp.list.Render(st, w))
	return b.String()
}

// ───────────────────── Filter Picker (Branch B / Workflow W) ─────────────────

type pickerOption struct {
	Value  string
	Label  string
	Detail string
}

// FilterPicker is a reusable live-filtered modal picker for branches and workflows.
type FilterPicker struct {
	title       string
	hint        string
	customVerb  string // e.g. "Use branch" or "Use workflow"
	current     string
	loading     bool
	errMsg      string
	options     []pickerOption
	input       textinput.Model
	list        components.List
}

func newFilterPicker(title, hint, placeholder, customVerb, current string, loading bool) *FilterPicker {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 42
	fp := &FilterPicker{
		title:      title,
		hint:       hint,
		customVerb: customVerb,
		current:    current,
		loading:    loading,
		input:      ti,
	}
	fp.list.Empty = "  nothing matches"
	fp.list.Focused = true
	return fp
}

func (fp *FilterPicker) setOptions(m *Model, opts []pickerOption, err error) {
	fp.loading = false
	if err != nil {
		fp.errMsg = shortErr(err)
	} else {
		fp.errMsg = ""
	}
	fp.options = opts
	fp.rebuild(m, true)
}

func (fp *FilterPicker) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	fp.input, cmd = fp.input.Update(msg)
	return cmd
}

func (fp *FilterPicker) rebuild(m *Model, selectCurrent bool) {
	q := strings.TrimSpace(fp.input.Value())
	var items []components.Item
	exactMatch := false
	for _, opt := range fp.options {
		if !matchAllTokens(opt.Label+" "+opt.Detail+" "+opt.Value, q) {
			continue
		}
		if q != "" && (strings.EqualFold(opt.Value, q) || strings.EqualFold(opt.Label, q)) {
			exactMatch = true
		}
		prefix := "  "
		if opt.Value == fp.current {
			prefix = m.st.G.OK + " "
		}
		items = append(items, components.Item{
			ID:     opt.Value,
			Label:  prefix + opt.Label,
			Detail: opt.Detail,
		})
	}
	if fp.customVerb != "" && q != "" && !exactMatch && !strings.ContainsAny(q, " \t") {
		items = append(items, components.Item{
			ID:     q,
			Label:  "  " + fp.customVerb + " “" + q + "”",
			Detail: "custom",
		})
	}

	keepID := any(nil)
	if !selectCurrent {
		if sel, ok := fp.list.Selected(); ok {
			keepID = sel.ID
		}
	}
	fp.list.SetItems(items)
	if selectCurrent {
		fp.list.SelectByID(fp.current)
	} else if keepID != nil {
		fp.list.SelectByID(keepID)
	}
	fp.list.MaxShown = 10
	fp.list.Focused = true
}

func (fp *FilterPicker) handleKey(msg tea.KeyMsg, m *Model, onChoose func(val string) tea.Cmd) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.modal = ModalNone
		return *m, nil
	case "down", "ctrl+n", "tab":
		fp.list.MoveBy(1)
		return *m, nil
	case "up", "ctrl+p", "shift+tab":
		fp.list.MoveBy(-1)
		return *m, nil
	case "pgdown", "ctrl+d":
		fp.list.MoveBy(6)
		return *m, nil
	case "pgup", "ctrl+u":
		fp.list.MoveBy(-6)
		return *m, nil
	case "enter":
		if sel, ok := fp.list.Selected(); ok {
			val, _ := sel.ID.(string)
			m.modal = ModalNone
			return *m, onChoose(val)
		}
		q := strings.TrimSpace(fp.input.Value())
		if q != "" && fp.customVerb != "" {
			m.modal = ModalNone
			return *m, onChoose(q)
		}
		return *m, nil
	}

	prev := fp.input.Value()
	var cmd tea.Cmd
	fp.input, cmd = fp.input.Update(msg)
	if fp.input.Value() != prev {
		fp.rebuild(m, false)
	}
	return *m, cmd
}

func (fp *FilterPicker) view(m *Model) string {
	st := m.st
	var b strings.Builder
	hdr := fp.hint
	if fp.loading {
		hdr += "  " + m.spin.View() + " loading…"
	}
	b.WriteString(st.Muted.Render(hdr) + "\n\n")
	b.WriteString(fp.input.View() + "\n\n")
	if fp.errMsg != "" && len(fp.options) <= 1 {
		b.WriteString(st.Warn.Render("  "+fp.errMsg) + "\n")
	}
	b.WriteString(st.Dim.Render("↑/↓ select · type to filter · enter choose · esc close") + "\n")
	w := 56
	if m.width-16 < w {
		w = m.width - 16
	}
	if w < 30 {
		w = 30
	}
	b.WriteString(fp.list.Render(st, w))
	return b.String()
}

// openBranchPicker opens the filterable branch modal seeded with any branches
// already visible on loaded runs and triggers fetchBranchesCmd in the background.
func (m *Model) openBranchPicker() tea.Cmd {
	fp := newFilterPicker(
		"Branch",
		"Filter runs by branch, or enter a branch name:",
		"type to filter branches…",
		"Use branch",
		m.branchSel,
		true,
	)
	fp.setOptions(m, m.buildBranchOptions(nil), nil)
	fp.loading = true
	m.branchPicker = fp
	m.modal = ModalPickBranch
	return fetchBranchesCmd(m.settings.Repo)
}

func (m *Model) buildBranchOptions(apiBranches []ghapi.Branch) []pickerOption {
	opts := []pickerOption{{Value: "", Label: "(all branches)", Detail: "unfiltered"}}
	seen := map[string]bool{"": true}
	def := m.repo.DefaultBranch
	for _, b := range apiBranches {
		if b.Name == "" || seen[b.Name] {
			continue
		}
		seen[b.Name] = true
		det := ""
		if def != "" && b.Name == def {
			det = "default"
		} else if b.Protected {
			det = "protected"
		}
		opts = append(opts, pickerOption{Value: b.Name, Label: b.Name, Detail: det})
	}
	// Include branches from loaded runs (e.g. PR branches beyond page 1 of /branches).
	for _, it := range m.runs.Items {
		if rr, ok := it.ID.(runRow); ok && rr.run.HeadBranch != "" && !seen[rr.run.HeadBranch] {
			seen[rr.run.HeadBranch] = true
			det := "recent run"
			if def != "" && rr.run.HeadBranch == def {
				det = "default"
			}
			opts = append(opts, pickerOption{Value: rr.run.HeadBranch, Label: rr.run.HeadBranch, Detail: det})
		}
	}
	return opts
}

// openWorkflowPicker opens the filterable workflow modal.
func (m *Model) openWorkflowPicker() tea.Cmd {
	haveWFs := len(m.wfCatalog) > 0
	fp := newFilterPicker(
		"Workflow",
		"Filter runs by GitHub Actions workflow:",
		"type to filter workflows…",
		"Use workflow",
		m.settings.Workflow,
		!haveWFs,
	)
	fp.setOptions(m, m.buildWorkflowOptions(), nil)
	fp.loading = !haveWFs
	m.workflowPicker = fp
	m.modal = ModalPickWorkflow
	if !haveWFs {
		return fetchWorkflowsCmd(m.settings.Repo)
	}
	return nil
}

func (m *Model) buildWorkflowOptions() []pickerOption {
	opts := []pickerOption{{Value: "", Label: "(all workflows)", Detail: "any"}}
	seen := map[string]bool{"": true}
	for _, w := range m.wfCatalog {
		if w.Name == "" || seen[w.Name] {
			continue
		}
		seen[w.Name] = true
		opts = append(opts, pickerOption{
			Value:  w.Name,
			Label:  w.Name,
			Detail: w.File(),
		})
	}
	return opts
}
