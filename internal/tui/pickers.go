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
	input       textinput.Model
	list        components.List
	lastQ       string
	hits        []ghapi.SearchHit
	userRepos   []ghapi.Repo
	loading     bool
	errMsg      string
	touchActive bool
	touchMoved  bool
	touchLastY  int
	touchValue  string
}

// Shared text and cell widths keep the visible hints and their touch targets aligned.
const (
	repoChooseHint    = "↵ choose"
	repoHintSeparator = " · "
	repoCancelHint    = "Esc cancel"
)

type repoSearchDebounced struct {
	seq   int
	query string
}

func (m *Model) openRepoPicker() tea.Cmd {
	ti := textinput.New()
	ti.Placeholder = "Filter or enter owner/repo"
	ti.Focus()
	ti.CharLimit = 100
	rp := &RepoPicker{input: ti, userRepos: m.cachedUserRepos}
	rp.list.Empty = "  No repositories found"
	m.repoPicker = rp
	m.modal = ModalPickRepo
	m.searchSeq++ // invalidate searches from an earlier picker
	rp.resize(m)
	rp.rebuild(m)
	if !m.userReposLoaded && len(m.cachedUserRepos) == 0 {
		if m.userReposLoading {
			rp.loading = true
			return m.spin.Tick
		}
		return m.fetchUserRepos()
	}
	return nil
}

func (m *Model) fetchUserRepos() tea.Cmd {
	if m.userReposLoading {
		return nil
	}
	m.userReposLoading = true
	m.userReposSeq++
	if m.repoPicker != nil {
		m.repoPicker.loading = true
		m.repoPicker.errMsg = ""
	}
	return tea.Batch(fetchUserReposCmd(m.userReposSeq), m.spin.Tick)
}

// Keep the modal and its hit targets inside even the smallest supported screen.
func (rp *RepoPicker) resize(m *Model) {
	rp.input.Width = max(1, m.modalWindowWidth()-4-components.VisibleWidth(rp.input.Prompt))
	rp.list.MaxShown = min(10, max(1, m.height-7))
	rp.list.CachedClamp()
	rp.touchActive = false
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
	rp.list.Focused = true
	rp.list.CachedClamp()
}

func (rp *RepoPicker) setSearch(m *Model, query string, hits []ghapi.SearchHit) {
	if query != rp.lastQ {
		return
	}
	rp.hits = hits
	rp.rebuild(m)
}

func (rp *RepoPicker) setUserRepos(m *Model, repos []ghapi.Repo, err error) {
	rp.loading = false
	rp.errMsg = shortErr(err)
	rp.userRepos = repos
	rp.rebuild(m)
}

func (rp *RepoPicker) update(m *Model, msg tea.Msg) tea.Cmd {
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
	case "ctrl+r":
		if rp.errMsg != "" {
			return *m, m.fetchUserRepos()
		}
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
			err := m.setRepo(repo, true)
			m.toastf("repo: %s", repo)
			if err != nil {
				m.toastf("could not save selection: %s", shortErr(err))
			}
			return *m, tea.Batch(m.refreshAll()...)
		}
		config.TouchRecent(repo)
		if err := config.RememberSelection(repo, true, nil, nil); err != nil {
			m.toastf("could not save selection: %s", shortErr(err))
		}
		return *m, nil
	}

	var cmd tea.Cmd
	prev := rp.input.Value()
	rp.input, cmd = rp.input.Update(msg)
	if rp.input.Value() != prev {
		q := strings.TrimSpace(rp.input.Value())
		if q != rp.lastQ {
			rp.lastQ = q
			rp.hits = nil
			m.searchSeq++
			seq := m.searchSeq
			rp.rebuild(m)
			if strings.Contains(q, "/") {
				return *m, tea.Batch(cmd, searchReposCmd(q, seq))
			}
			if len(q) >= 3 {
				return *m, tea.Batch(cmd, tea.Tick(350*time.Millisecond, func(t time.Time) tea.Msg { return repoSearchDebounced{seq: seq, query: q} }))
			}
		} else {
			rp.rebuild(m)
		}
	}
	return *m, cmd
}

func (rp *RepoPicker) view(m *Model) string {
	width := max(1, m.modalWindowWidth()-4)
	rows := rp.list.MaxShown
	if rows < 1 {
		rows = 1
	}
	var lines []string
	lines = append(lines, components.Clamp(rp.input.View(), width))
	lines = append(lines, m.st.Dim.Render(strings.Repeat("─", width)))
	l := rp.list
	listWidth := width
	scroll := len(l.Filtered()) > rows && width > 2
	if scroll {
		listWidth -= 2
	}
	rendered := strings.Split(l.Render(m.st, listWidth), "\n")
	thumbStart, thumbEnd := 0, 0
	if scroll {
		n := len(l.Filtered())
		thumbSize := max(1, rows*rows/n)
		thumbStart = (rows - thumbSize) * l.Offset / (n - rows)
		thumbEnd = thumbStart + thumbSize
	}
	for i := 0; i < rows; i++ {
		line := ""
		if i < len(rendered) {
			line = rendered[i]
		}
		line = components.Fit(line, listWidth)
		if scroll {
			mark := "│"
			if i >= thumbStart && i < thumbEnd {
				mark = "┃"
			}
			line += " " + m.st.Dim.Render(mark)
		}
		lines = append(lines, line)
	}
	status := ""
	if rp.loading {
		status = m.spin.View() + " Loading repositories…"
	} else if rp.errMsg != "" {
		status = "Load failed · tap retry: " + rp.errMsg
	}
	lines = append(lines, m.st.Dim.Render(components.Clamp(status, width)))
	lines = append(lines, m.st.Dim.Render(repoChooseHint+repoHintSeparator+repoCancelHint))
	return strings.Join(lines, "\n")
}

// ───────────────────── Filter Picker (Branch B / Workflow W) ─────────────────

type pickerOption struct {
	Value  string
	Label  string
	Detail string
}

// FilterPicker is a reusable live-filtered modal picker for branches and workflows.
type FilterPicker struct {
	title      string
	hint       string
	customVerb string // e.g. "Use branch" or "Use workflow"
	current    string
	loading    bool
	errMsg     string
	options    []pickerOption
	input      textinput.Model
	list       components.List
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
			cmd := onChoose(val)
			return *m, cmd
		}
		q := strings.TrimSpace(fp.input.Value())
		if q != "" && fp.customVerb != "" {
			m.modal = ModalNone
			cmd := onChoose(q)
			return *m, cmd
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
	fp.setOptions(m, m.buildBranchOptions(m.apiBranches), nil)
	fp.loading = true
	m.branchPicker = fp
	m.branchLoading = true
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
	for _, name := range m.localBranches {
		if name != "" && !seen[name] {
			seen[name] = true
			opts = append(opts, pickerOption{Value: name, Label: name, Detail: "local"})
		}
	}
	// Keep branches seen on runs even after a different filter clears the list.
	for _, name := range m.recentBranches {
		if name != "" && !seen[name] {
			seen[name] = true
			det := "recent run"
			if name == def && def != "" {
				det = "default"
			}
			opts = append(opts, pickerOption{Value: name, Label: name, Detail: det})
		}
	}
	if m.branchSel != "" && !seen[m.branchSel] {
		opts = append(opts, pickerOption{Value: m.branchSel, Label: m.branchSel, Detail: "current"})
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
	if m.settings.Workflow != "" && !seen[m.settings.Workflow] {
		opts = append(opts, pickerOption{Value: m.settings.Workflow, Label: m.settings.Workflow, Detail: "current"})
	}
	return opts
}

// Rebuild without moving a user's cursor when asynchronous results arrive.
func (m *Model) rebuildBranchList() {
	var keep any = m.branchSel
	if item, ok := m.branchList.Selected(); ok {
		keep = item.ID
	}
	options := m.buildBranchOptions(m.apiBranches)
	items := make([]components.Item, 0, len(options))
	for _, opt := range options {
		items = append(items, components.Item{ID: opt.Value, Label: opt.Label, Detail: opt.Detail})
	}
	m.branchList.SetItems(items)
	m.branchList.SelectByID(keep)
	m.updateListBounds()
}

func (m *Model) rememberRunBranches(runs []ghapi.Run) {
	seen := make(map[string]bool, len(m.recentBranches))
	for _, name := range m.recentBranches {
		seen[name] = true
	}
	for _, run := range runs {
		if run.HeadBranch != "" && !seen[run.HeadBranch] {
			m.recentBranches = append(m.recentBranches, run.HeadBranch)
			seen[run.HeadBranch] = true
		}
	}
}
