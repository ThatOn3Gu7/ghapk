package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ThatOn3Gu7/ghapk/internal/device"
	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
)

func openPlan(t *testing.T) Model {
	t.Helper()
	m := seedArtifacts(seedRuns(newTestModel()))
	mm, _ := m.Update(key("3"))
	mm, _ = mm.Update(keyOf("enter"))
	mm, _ = mm.Update(key("i"))
	m = mm.(Model)
	if m.modal != ModalPlan || m.plan == nil {
		t.Fatal("plan did not open")
	}
	return m
}

func commits(n int) []ghapi.Commit {
	var out []ghapi.Commit
	for i := 0; i < n; i++ {
		var c ghapi.Commit
		c.SHA = strings.Repeat(string(rune('a'+i%6)), 40)
		c.Commit.Message = "change number " + string(rune('A'+i)) + "\n\nbody"
		out = append(out, c)
	}
	return out
}

func TestPlanShowsCommitsSinceLastInstall(t *testing.T) {
	m := openPlan(t)
	mm, _ := m.Update(planReadyMsg{seq: m.plan.seq, dev: device.Info{CanInstall: true},
		changes: commits(3), ahead: 3, since: time.Now().Add(-72 * time.Hour)})
	v := mm.(Model).planBox()
	for _, want := range []string{"Since your last install", "3 commits", "3d ago", "change number C", "Install"} {
		if !strings.Contains(v, want) {
			t.Errorf("plan missing %q:\n%s", want, v)
		}
	}
	// newest first
	if strings.Index(v, "change number C") > strings.Index(v, "change number A") {
		t.Error("commits should list newest first")
	}
}

func TestPlanTrimsCommitsOnShortScreensKeepingButtons(t *testing.T) {
	m := openPlan(t)
	m.height = 24
	mm, _ := m.Update(planReadyMsg{seq: m.plan.seq, dev: device.Info{CanInstall: true},
		changes: commits(40), ahead: 40})
	v := mm.(Model).planBox()
	if !strings.Contains(v, "40 commits") || !strings.Contains(v, "more") {
		t.Fatalf("expected a count and an overflow line:\n%s", v)
	}
	if !strings.Contains(v, "Cancel") {
		t.Fatalf("buttons fell off a short screen:\n%s", v)
	}
	if got := strings.Count(v, "\n") + 1; got > 24-2 {
		t.Fatalf("plan is %d lines on a 24-line screen", got)
	}
}

func TestPlanComparesVersionsAndWarnsAboutBuildingRun(t *testing.T) {
	m := openPlan(t)
	m.plan.choices[m.plan.selected].art.Name = "nazo-v1.4.2-debug.apk"
	mm, _ := m.Update(planReadyMsg{seq: m.plan.seq, dev: device.Info{CanInstall: true},
		app: device.InstalledApp{VersionName: "1.4.1"}, found: true})
	v := mm.(Model).planBox()
	for _, want := range []string{"1.4.2", "installed 1.4.1", "newer than installed"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	mm, _ = m.Update(planReadyMsg{seq: m.plan.seq, dev: device.Info{CanInstall: true},
		app: device.InstalledApp{VersionName: "1.5.0"}, found: true})
	if v := mm.(Model).planBox(); !strings.Contains(v, "older than what is installed") {
		t.Errorf("downgrade not flagged:\n%s", v)
	}
}

func TestAgoText(t *testing.T) {
	for in, want := range map[time.Duration]string{30 * time.Second: "just now", 5 * time.Minute: "5m ago", 3 * time.Hour: "3h ago", 48 * time.Hour: "2d ago"} {
		if got := agoText(time.Now().Add(-in)); got != want {
			t.Errorf("%v: %q want %q", in, got, want)
		}
	}
	if got := agoText(time.Now().Add(-90 * 24 * time.Hour)); strings.Contains(got, "ago") || got == "" {
		t.Errorf("old dates should be plain: %q", got)
	}
}

func TestInstallNewestLoadsArtifactsThenOpensPreview(t *testing.T) {
	m := seedRuns(newTestModel())
	mm, cmd := m.Update(key("I"))
	m = mm.(Model)
	if cmd == nil || m.pendingPlanRun != 101 || m.modal != ModalNone {
		t.Fatalf("I should fetch the newest successful run's artifacts first: cmd=%v pending=%d", cmd != nil, m.pendingPlanRun)
	}
	mm, _ = m.Update(ArtifactsMsg{RunID: 101, List: []ghapi.Artifact{{ID: 9001, Name: "app-arm64-v8a-debug.apk", SizeInBytes: 14 << 20}}})
	m = mm.(Model)
	if m.modal != ModalPlan || m.plan == nil || m.plan.run.ID != 101 {
		t.Fatalf("preview did not open for run 101: modal=%v", m.modal)
	}
	if m.pendingPlanRun != 0 {
		t.Fatal("pending marker should clear")
	}
}

func TestInstallNewestWithCachedArtifactsOpensImmediately(t *testing.T) {
	m := seedArtifacts(seedRuns(newTestModel()))
	mm, _ := m.Update(key("I"))
	if got := mm.(Model); got.modal != ModalPlan {
		t.Fatalf("modal=%v", got.modal)
	}
}

func TestInstallNewestWithoutSuccessfulBuildsExplains(t *testing.T) {
	m := newTestModel()
	mm, _ := m.Update(RunsMsg{List: []ghapi.Run{{ID: 5, Status: "completed", Conclusion: "failure"}}})
	mm, cmd := mm.Update(key("I"))
	if cmd != nil || mm.(Model).modal != ModalNone {
		t.Fatal("must not start anything without a successful build")
	}
}

func mixedRuns(m Model) Model {
	mm, _ := m.Update(RunsMsg{List: []ghapi.Run{
		{ID: 1, RunNumber: 3, DisplayTitle: "ok build", Status: "completed", Conclusion: "success"},
		{ID: 2, RunNumber: 2, DisplayTitle: "broken build", Status: "completed", Conclusion: "failure"},
		{ID: 3, RunNumber: 1, DisplayTitle: "busy build", Status: "in_progress"},
	}})
	return mm.(Model)
}

func TestBuildStatusFilterCyclesAndClears(t *testing.T) {
	m := mixedRuns(newTestModel())
	mm, _ := m.Update(key("3"))
	m = mm.(Model)
	want := []struct {
		filter string
		shown  int
	}{{"success", 1}, {"failed", 1}, {"running", 1}, {"", 3}}
	for _, w := range want {
		mm, _ = m.Update(key("f"))
		m = mm.(Model)
		if m.statusFilter != w.filter || len(m.runs.Items) != w.shown {
			t.Fatalf("after f: filter=%q shown=%d, want %q/%d", m.statusFilter, len(m.runs.Items), w.filter, w.shown)
		}
	}
	mm, _ = m.Update(key("f"))
	m = mm.(Model)
	if v := components.StripANSI(m.View()); !strings.Contains(v, "success") || strings.Contains(v, "broken build") {
		t.Fatalf("filtered view wrong:\n%s", v)
	}
	mm, _ = m.Update(key("x"))
	m = mm.(Model)
	if m.statusFilter != "" || len(m.runs.Items) != 3 {
		t.Fatalf("x did not clear: %q %d", m.statusFilter, len(m.runs.Items))
	}
}

func TestStatusFilterSurvivesRefreshAndNewestIgnoresIt(t *testing.T) {
	m := mixedRuns(newTestModel())
	mm, _ := m.Update(key("3"))
	m = mm.(Model)
	for i := 0; i < 2; i++ { // success -> failed
		mm, _ = m.Update(key("f"))
		m = mm.(Model)
	}
	m = mixedRuns(m) // a refresh arrives
	if m.statusFilter != "failed" || len(m.runs.Items) != 1 {
		t.Fatalf("refresh dropped the filter: %q %d", m.statusFilter, len(m.runs.Items))
	}
	mm, cmd := m.Update(key("I"))
	m = mm.(Model)
	if cmd == nil || m.pendingPlanRun != 1 || m.statusFilter != "" {
		t.Fatalf("install newest should reveal the newest good build: pending=%d filter=%q", m.pendingPlanRun, m.statusFilter)
	}
}

func TestHeaderShowsInstalledVersion(t *testing.T) {
	m := newTestModel()
	m.instFound, m.instApp = true, device.InstalledApp{VersionName: "1.4.1-quiz", Package: "q.n"}
	if v := components.StripANSI(m.renderHeader()); !strings.Contains(v, "v1.4.1-quiz") || !strings.Contains(v, "@tester") {
		t.Fatalf("header: %q", v)
	}
	m.instFound = false
	if v := components.StripANSI(m.renderHeader()); strings.Contains(v, "1.4.1") {
		t.Fatalf("unknown install must show nothing: %q", v)
	}
}

func TestInstallWhenDoneWaitsThenOpensPreview(t *testing.T) {
	m := seedRuns(newTestModel()) // run 100 is in progress
	mm, _ := m.Update(key("3"))
	m = mm.(Model)
	mm, _ = m.Update(key("j")) // onto the running build
	m = mm.(Model)
	mm, _ = m.Update(key("n"))
	m = mm.(Model)
	if m.armedRun != 100 {
		t.Fatalf("not armed: %d", m.armedRun)
	}
	// still running: nothing happens
	mm, _ = m.Update(RunsMsg{List: []ghapi.Run{{ID: 100, RunNumber: 40, Status: "in_progress"}}})
	m = mm.(Model)
	if m.armedRun != 100 || m.modal != ModalNone {
		t.Fatal("must keep waiting while running")
	}
	// finished successfully: artifacts are fetched, then the preview opens
	mm, cmd := m.Update(RunsMsg{List: []ghapi.Run{{ID: 100, RunNumber: 40, Status: "completed", Conclusion: "success"}}})
	m = mm.(Model)
	if cmd == nil || m.pendingPlanRun != 100 || m.armedRun != 0 {
		t.Fatalf("did not move on: cmd=%v pending=%d armed=%d", cmd != nil, m.pendingPlanRun, m.armedRun)
	}
	mm, _ = m.Update(ArtifactsMsg{RunID: 100, List: []ghapi.Artifact{{ID: 1, Name: "app-debug.apk", SizeInBytes: 1 << 20}}})
	if got := mm.(Model); got.modal != ModalPlan {
		t.Fatalf("preview not opened: %v", got.modal)
	}
}

func TestInstallWhenDoneGivesUpOnFailureAndNonRunning(t *testing.T) {
	m := seedRuns(newTestModel())
	mm, _ := m.Update(key("3"))
	m = mm.(Model) // cursor on the finished build
	mm, _ = m.Update(key("n"))
	if mm.(Model).armedRun != 0 {
		t.Fatal("armed a finished build")
	}
	mm, _ = m.Update(key("j"))
	mm, _ = mm.Update(key("n"))
	m = mm.(Model)
	mm, _ = m.Update(RunsMsg{List: []ghapi.Run{{ID: 100, RunNumber: 40, Status: "completed", Conclusion: "failure"}}})
	m = mm.(Model)
	if m.armedRun != 0 || m.modal != ModalNone || !m.toastErr {
		t.Fatalf("a failed build should end the wait with an error toast: armed=%d toastErr=%v", m.armedRun, m.toastErr)
	}
}

func TestStatusFilterDoesNotStopWatching(t *testing.T) {
	m := mixedRuns(newTestModel())
	mm, _ := m.Update(key("3"))
	m = mm.(Model)
	mm, _ = m.Update(key("f")) // success only: the running build is hidden
	m = mm.(Model)
	if !m.watchActive() {
		t.Fatal("hiding a running build must not stop refreshing it")
	}
}

func TestBuildsListMarksRunsThatShipArtifacts(t *testing.T) {
	m := seedRuns(newTestModel())
	mm, _ := m.Update(key("3"))
	m = mm.(Model)
	if v := components.StripANSI(m.View()); strings.Contains(v, "▣") {
		t.Fatal("no marks before the index has loaded")
	}
	mm, _ = m.Update(ArtifactIndexMsg{Repo: "acme/rocket", Count: map[int64]int{101: 2}})
	m = mm.(Model)
	v := components.StripANSI(m.View())
	if strings.Count(v, "▣") != 1 {
		t.Fatalf("want exactly one mark (run 101):\n%s", v)
	}
	// a failed index load keeps the old marks and never raises an error
	mm, _ = m.Update(ArtifactIndexMsg{Repo: "acme/rocket", Err: errors.New("boom")})
	m = mm.(Model)
	if m.artIndex == nil || m.toastErr {
		t.Fatal("index failure must be silent and non-destructive")
	}
	// another repo's answer is ignored
	mm, _ = m.Update(ArtifactIndexMsg{Repo: "other/repo", Count: map[int64]int{}})
	if len(mm.(Model).artIndex) != 1 {
		t.Fatal("foreign index applied")
	}
}
