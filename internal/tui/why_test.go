package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
)

const sampleLog = `2026-11-01T10:00:00.1234567Z ##[group]Run ./gradlew assembleDebug
2026-11-01T10:00:01.0000000Z > Task :app:compileDebugKotlin
2026-11-01T10:00:02.0000000Z e: Unresolved reference: foo
2026-11-01T10:00:02.5000000Z ##[endgroup]
2026-11-01T10:00:03.0000000Z ##[error]Execution failed for task ':app:compileDebugKotlin'.
2026-11-01T10:00:04.0000000Z ##[error]Process completed with exit code 1.
2026-11-01T10:00:05.0000000Z Post job cleanup.
`

func failedJobs() []ghapi.Job {
	return []ghapi.Job{
		{ID: 1, Name: "lint", Conclusion: "success"},
		{ID: 2, Name: "build", Conclusion: "failure", HTMLURL: "https://example/job/2", Steps: []ghapi.Step{
			{Number: 1, Name: "Checkout", Conclusion: "success"},
			{Number: 2, Name: "Set up JDK", Conclusion: "success"},
			{Number: 3, Name: "Assemble debug", Conclusion: "failure"},
			{Number: 4, Name: "Upload", Conclusion: "skipped"},
		}},
	}
}

func TestFailureReportNamesJobStepAndTrimsLog(t *testing.T) {
	rep := buildFailureReport(failedJobs(), func(id int64) (string, error) {
		if id != 2 {
			t.Fatalf("asked for the wrong job log: %d", id)
		}
		return sampleLog, nil
	})
	if rep.JobName != "build" || rep.StepNo != 3 || rep.StepName != "Assemble debug" || rep.Passed != 2 {
		t.Fatalf("report: %+v", rep)
	}
	joined := strings.Join(rep.Excerpt, "\n")
	for _, want := range []string{"Unresolved reference: foo", "Execution failed for task"} {
		if !strings.Contains(joined, want) {
			t.Errorf("excerpt missing %q:\n%s", want, joined)
		}
	}
	for _, bad := range []string{"2026-11-01", "##[", "Post job cleanup"} {
		if strings.Contains(joined, bad) {
			t.Errorf("excerpt should not contain %q:\n%s", bad, joined)
		}
	}
}

func TestFailureReportFallbacks(t *testing.T) {
	// no error markers: the tail of the log
	rep := buildFailureReport(failedJobs(), func(int64) (string, error) { return "a\nb\nc\n", nil })
	if len(rep.Excerpt) != 3 || rep.Excerpt[2] != "c" {
		t.Fatalf("tail: %v", rep.Excerpt)
	}
	// log gone: say so, keep the job and step
	rep = buildFailureReport(failedJobs(), func(int64) (string, error) {
		return "", &ghapi.Error{Class: ghapi.ClassGone, Endpoint: "x", Msg: "gone"}
	})
	if rep.NoLog == "" || rep.StepName == "" || len(rep.Excerpt) != 0 {
		t.Fatalf("expired log: %+v", rep)
	}
	// cancelled and unknown
	if r := buildFailureReport([]ghapi.Job{{Name: "b", Conclusion: "cancelled"}}, nil); !r.Cancelled {
		t.Fatal("cancelled run not recognised")
	}
	if r := buildFailureReport(nil, nil); r.NoLog == "" {
		t.Fatal("no jobs should explain itself")
	}
}

func TestEKeyOpensExplanationOnlyForFailedBuilds(t *testing.T) {
	m := seedRuns(newTestModel())
	mm, _ := m.Update(key("3"))
	m = mm.(Model)
	// run 101 succeeded, so "e" explains nothing
	mm, cmd := m.Update(key("e"))
	if mm.(Model).modal != ModalNone || cmd != nil {
		t.Fatal("e opened a dialog for a successful build")
	}
	// make the cursor run a failed one
	mm, _ = m.Update(RunsMsg{List: []ghapi.Run{{ID: 7, RunNumber: 7, DisplayTitle: "break it", HeadBranch: "main", Status: "completed", Conclusion: "failure"}}})
	m = mm.(Model)
	mm, cmd = m.Update(key("e"))
	m = mm.(Model)
	if m.modal != ModalWhy || cmd == nil || !m.why.loading {
		t.Fatalf("e did not start the explanation: modal=%v", m.modal)
	}
	mm, _ = m.Update(whyDoneMsg{seq: m.why.seq, report: buildFailureReport(failedJobs(), func(int64) (string, error) { return sampleLog, nil })})
	v := components.StripANSI(mm.(Model).View())
	for _, want := range []string{"Why did it fail?", "break it", "Assemble debug", "Unresolved reference"} {
		if !strings.Contains(v, want) {
			t.Errorf("dialog missing %q:\n%s", want, v)
		}
	}
	mm, _ = mm.Update(key("q"))
	if mm.(Model).modal != ModalNone {
		t.Fatal("q did not close the dialog")
	}
}

func TestExplanationShowsClassifiedErrors(t *testing.T) {
	m := seedRuns(newTestModel())
	m.why = &whyState{seq: 1, run: ghapi.Run{ID: 1}}
	m.modal = ModalWhy
	mm, _ := m.Update(whyDoneMsg{seq: 1, err: &ghapi.Error{Class: ghapi.ClassForbidden, Status: 403, Endpoint: "repos/o/r/actions/runs/1/jobs", Msg: "x"}})
	if v := components.StripANSI(mm.(Model).View()); !strings.Contains(v, "refused access") {
		t.Fatalf("error not explained:\n%s", v)
	}
	_ = errors.New
}
