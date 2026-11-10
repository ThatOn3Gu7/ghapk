package tui

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
	tea "github.com/charmbracelet/bubbletea"
)

// whyState is the "why did this build fail?" dialog: the failing job and step
// plus the part of the log around the first error, fetched on demand.
type whyState struct {
	seq     int
	run     ghapi.Run
	loading bool
	cancel  context.CancelFunc
	err     error
	verdict failureReport
	offset  int
}

// failureReport is everything the dialog shows, already reduced to text.
type failureReport struct {
	JobName   string
	JobURL    string
	StepNo    int
	StepName  string
	Passed    int
	Failed    int
	Excerpt   []string // log lines around the first error
	NoLog     string   // why there is no excerpt, when there is none
	Cancelled bool     // the run was cancelled rather than failed
}

type whyDoneMsg struct {
	seq    int
	report failureReport
	err    error
}

func (m *Model) openWhy(run ghapi.Run) tea.Cmd {
	if m.why != nil && m.why.cancel != nil {
		m.why.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.whySeq++
	m.why = &whyState{seq: m.whySeq, run: run, loading: true, cancel: cancel}
	m.modal = ModalWhy
	repo, seq := m.settings.Repo, m.whySeq
	return func() tea.Msg {
		jobs, err := ghapi.ListJobs(ctx, repo, run.ID)
		if err != nil {
			return whyDoneMsg{seq: seq, err: err}
		}
		var jobLog func(int64) (string, error)
		jobLog = func(id int64) (string, error) { return ghapi.JobLog(ctx, repo, id) }
		return whyDoneMsg{seq: seq, report: buildFailureReport(jobs, jobLog)}
	}
}

var (
	logStamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T[\d:.]+Z\s?`)
	logANSI  = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
)

// buildFailureReport picks the first failed job, names its failed step and
// cuts the log down to the lines around the first error. Pure so it can be
// tested without a network.
func buildFailureReport(jobs []ghapi.Job, jobLog func(int64) (string, error)) failureReport {
	var rep failureReport
	var bad *ghapi.Job
	for i := range jobs {
		if jobs[i].Failed() {
			bad = &jobs[i]
			break
		}
	}
	if bad == nil {
		for i := range jobs {
			if jobs[i].Conclusion == "cancelled" {
				rep.Cancelled, rep.JobName = true, jobs[i].Name
				return rep
			}
		}
		rep.NoLog = "No failed job was reported for this run."
		return rep
	}
	rep.JobName, rep.JobURL = bad.Name, bad.HTMLURL
	for _, s := range bad.Steps {
		switch s.Conclusion {
		case "success":
			rep.Passed++
		case "failure", "timed_out":
			rep.Failed++
		}
	}
	if st, ok := bad.FailedStep(); ok {
		rep.StepNo, rep.StepName = st.Number, st.Name
	}
	text, err := jobLog(bad.ID)
	if err != nil {
		title, _ := ghapi.Describe(err)
		rep.NoLog = "The log could not be loaded: " + title + ". Logs expire after a while."
		return rep
	}
	rep.Excerpt = excerptAroundError(text, 14)
	if len(rep.Excerpt) == 0 {
		rep.NoLog = "The log is empty."
	}
	return rep
}

// excerptAroundError returns up to max cleaned lines ending at the first
// error annotation (with that error and the next line or two), or the tail of
// the log when nothing is marked as an error.
func excerptAroundError(log string, max int) []string {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(log, "\r", ""), "\n") {
		l = logANSI.ReplaceAllString(logStamp.ReplaceAllString(l, ""), "")
		l = strings.TrimRight(l, " \t")
		if strings.HasPrefix(l, "##[group]") || strings.HasPrefix(l, "##[endgroup]") {
			continue
		}
		lines = append(lines, l)
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	first := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "##[error]") {
			first = i
			break
		}
	}
	if first < 0 {
		start := len(lines) - max
		if start < 0 {
			start = 0
		}
		return cleanExcerpt(lines[start:])
	}
	// Keep the run of consecutive error annotations, not what follows them.
	end := first + 1
	for end < len(lines) && strings.HasPrefix(lines[end], "##[error]") {
		end++
	}
	start := end - max
	if start < 0 {
		start = 0
	}
	return cleanExcerpt(lines[start:end])
}

func cleanExcerpt(in []string) []string {
	out := make([]string, 0, len(in))
	for _, l := range in {
		l = strings.TrimPrefix(l, "##[error]")
		out = append(out, safeJobText(l))
	}
	return out
}

func (w *whyState) scroll(n, total, rows int) {
	w.offset += n
	if limit := total - rows; w.offset > limit {
		w.offset = limit
	}
	if w.offset < 0 {
		w.offset = 0
	}
}

// lines renders the dialog body at the given width.
func (w *whyState) lines(m *Model, width int) []string {
	st := m.st
	title := fmt.Sprintf("Run #%d", w.run.RunNumber)
	if w.run.RunNumber == 0 {
		title = fmt.Sprintf("Run %d", w.run.ID)
	}
	if t := safeJobText(w.run.DisplayTitle); t != "" {
		title += " · " + t
	}
	out := []string{st.Bold.Render(components.Clamp(title, width))}
	meta := []string{}
	if w.run.HeadBranch != "" {
		meta = append(meta, safeJobText(w.run.HeadBranch))
	}
	if a := agoText(w.run.CreatedAt); a != "" {
		meta = append(meta, a)
	}
	if len(meta) > 0 {
		out = append(out, st.Dim.Render(strings.Join(meta, " · ")))
	}
	out = append(out, "")
	switch {
	case w.loading:
		return append(out, m.spin.View()+" "+st.Text.Render("reading the run…"))
	case w.err != nil:
		t, hint := ghapi.Describe(w.err)
		out = append(out, st.Err.Render(st.G.Fail+" "+components.Clamp(t, width-2)))
		if hint != "" {
			out = append(out, st.Muted.Render(components.Clamp(hint, width)))
		}
		return out
	}
	r := w.verdict
	if r.Cancelled {
		return append(out, st.Warn.Render(st.G.Cancelled+" This run was cancelled"+suffixIf(r.JobName, " (job "+r.JobName+")")+"."),
			st.Muted.Render("Nothing failed; start it again from GitHub if you need the build."))
	}
	if r.JobName != "" {
		out = append(out, st.Err.Render(st.G.Fail+" Job   ")+st.Text.Render(components.Clamp(safeJobText(r.JobName), width-9)))
	}
	if r.StepName != "" {
		out = append(out, st.Err.Render(st.G.Fail+" Step  ")+st.Text.Render(components.Clamp(fmt.Sprintf("%d · %s", r.StepNo, safeJobText(r.StepName)), width-9)))
		out = append(out, "         "+st.Dim.Render(fmt.Sprintf("%d earlier step(s) passed", r.Passed)))
	}
	out = append(out, "")
	if len(r.Excerpt) > 0 {
		out = append(out, st.PanelTitleFoc.Render("Log around the first error"))
		for _, l := range r.Excerpt {
			sty := st.Muted
			low := strings.ToLower(l)
			if strings.Contains(low, "error") || strings.Contains(low, "failed") || strings.Contains(low, "exception") {
				sty = st.Err
			}
			out = append(out, sty.Render(components.Clamp("  "+l, width)))
		}
	} else if r.NoLog != "" {
		out = append(out, st.Muted.Render(components.Clamp(r.NoLog, width)))
	}
	return out
}

func suffixIf(s, suffix string) string {
	if s == "" {
		return ""
	}
	return suffix
}

func (w *whyState) view(m *Model) string {
	width := max(20, m.modalWindowWidth()-6)
	all := w.lines(m, width)
	rows := max(6, min(len(all), m.height-8))
	w.scroll(0, len(all), rows)
	shown := all[w.offset:min(len(all), w.offset+rows)]
	foot := "o open run · "
	if len(all) > rows {
		foot += "j/k scroll · "
	}
	foot += "esc close"
	return strings.Join(shown, "\n") + "\n\n" + m.st.Dim.Render(foot)
}

func (m Model) handleWhyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	w := m.why
	if w == nil {
		m.modal = ModalNone
		return m, nil
	}
	switch msg.String() {
	case "esc", "q", "e":
		if w.cancel != nil {
			w.cancel()
		}
		m.modal, m.why = ModalNone, nil
	case "j", "down":
		w.offset++
	case "k", "up":
		w.offset--
	case "pgdown", "ctrl+d":
		w.offset += 8
	case "pgup", "ctrl+u":
		w.offset -= 8
	case "g":
		w.offset = 0
	case "o":
		url := w.run.HTMLURL
		if w.verdict.JobURL != "" {
			url = w.verdict.JobURL
		}
		return m, openURL(url)
	}
	if w.offset < 0 {
		w.offset = 0
	}
	return m, nil
}

// explainSelectedRun opens the failure dialog for the run under the cursor.
func (m *Model) explainSelectedRun() tea.Cmd {
	run, ok := m.selectedRun()
	if !ok || m.settings.Repo == "" {
		return nil
	}
	switch {
	case run.Active():
		m.toastf("that build is still running")
		return nil
	case run.Conclusion == "success":
		m.toastf("that build did not fail")
		return nil
	}
	return m.openWhy(run)
}
