package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
)

func buildModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := seedArtifacts(seedRuns(newTestModel()))
	mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = mm.(Model)
	m.focus = PanelRuns
	m.updateListBounds()
	return m
}

func TestBuildsLayoutAndSmallPhone(t *testing.T) {
	for _, size := range [][2]int{{32, 18}, {40, 20}, {63, 26}, {64, 18}, {70, 26}, {96, 30}, {120, 40}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := buildModel(t, size[0], size[1])
			l := m.buildsLayout()
			if size[0] < 64 {
				if l.runs.w != size[0] || l.arts.w != 0 {
					t.Fatalf("narrow layout: %+v", l)
				}
			} else if l.runs.h != size[1]-3 || l.arts.y != 2 || l.details.y != l.arts.y+l.arts.h || l.runs.w+l.arts.w != size[0] {
				t.Fatalf("wide layout: %+v", l)
			}
			check := func() {
				lines := strings.Split(m.View(), "\n")
				if len(lines) != size[1] {
					t.Fatalf("height=%d, want %d", len(lines), size[1])
				}
				lineWidths(t, size[0], size[1], lines)
				for p, r := range m.compactTabRects() {
					if r.x+r.w > size[0] {
						t.Fatalf("tab %v is off screen: %+v", p, r)
					}
				}
			}
			check()
			if size[0] < 64 {
				mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
				m = mm.(Model)
				if !m.buildDetail || m.focus != PanelArtifacts {
					t.Fatal("right did not open details")
				}
				check()
				mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
				m = mm.(Model)
				if m.buildDetail || m.focus != PanelRuns {
					t.Fatal("left did not return to builds")
				}
			}
		})
	}
}

func TestBuildSelectionFollowsCursorAndLateReplies(t *testing.T) {
	m := buildModel(t, 120, 40)
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mm.(Model)
	if cmd == nil || m.selectedRunID != 100 || len(m.arts.Items) != 0 {
		t.Fatalf("selection=%d items=%v cmd=%v", m.selectedRunID, m.arts.Items, cmd)
	}
	// A reply for the previous build must never appear in the new selection.
	mm, _ = m.Update(ArtifactsMsg{Repo: m.settings.Repo, RunID: 101, List: []ghapi.Artifact{{ID: 700, Name: "old"}}})
	m = mm.(Model)
	if len(m.arts.Items) != 0 {
		t.Fatal("stale build artifact shown")
	}
	mm, _ = m.Update(ArtifactsMsg{Repo: "other/repo", RunID: 100, List: []ghapi.Artifact{{ID: 701, Name: "other repo"}}})
	m = mm.(Model)
	if len(m.arts.Items) != 0 {
		t.Fatal("stale repository artifact shown")
	}
	mm, _ = m.Update(ArtifactsMsg{Repo: m.settings.Repo, RunID: 100, List: []ghapi.Artifact{{ID: 702, Name: "new-build-debug", SizeInBytes: 1024}}})
	m = mm.(Model)
	a, ok := m.selectedArtifact()
	if !ok || a.ID != 702 {
		t.Fatalf("selected artifact: %+v %v", a, ok)
	}
	if !strings.Contains(components.StripANSI(m.View()), "new-build-debug") {
		t.Fatal("new build artifact missing")
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = mm.(Model)
	if m.selectedRunID != 101 {
		t.Fatal("did not return to first build")
	}
	a, ok = m.selectedArtifact()
	if !ok || a.ID != 700 {
		t.Fatalf("cached selection: %+v %v", a, ok)
	}
}

func TestBuildArtifactSelectionAndActions(t *testing.T) {
	m := buildModel(t, 120, 40)
	m.dev = device.Info{CanInstall: true}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = mm.(Model)
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mm.(Model)
	a, ok := m.cursorArtifact()
	if !ok || a.ID != m.selectedArtID {
		t.Fatalf("cursor and selected differ: %+v", a)
	}
	mm, cmd := m.Update(key("d"))
	m = mm.(Model)
	if cmd == nil || len(m.jobs) != 1 || m.jobs[0].spec.choice.name() != a.Name || m.jobs[0].spec.install {
		t.Fatalf("download: %+v", m.task)
	}
	m = buildModel(t, 120, 40)
	m.dev = device.Info{CanInstall: true}
	m.arts.Filter = "universal"
	m.arts.Cursor = 0
	m.syncArtifactSelection()
	a, ok = m.selectedArtifact()
	if !ok || !strings.Contains(a.Name, "universal") {
		t.Fatalf("filtered selection: %+v", a)
	}
	mm, cmd = m.Update(key("i"))
	m = mm.(Model)
	if cmd == nil || m.modal != ModalPlan || m.plan == nil || m.plan.choices[m.plan.selected].name() != a.Name {
		t.Fatalf("install: %+v", m.task)
	}
}

func TestBuildTouchAndActionButtons(t *testing.T) {
	m := buildModel(t, 100, 30)
	m.dev = device.Info{CanInstall: true}
	r := m.buildsLayout()
	// Select the second artifact with a tap, then press the download button.
	mm, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: r.arts.x + 3, Y: r.arts.y + 2})
	m = mm.(Model)
	a, ok := m.cursorArtifact()
	if !ok || a.ID != m.selectedArtID {
		t.Fatalf("tap did not select artifact: %+v", a)
	}
	_, hits := m.buildActionChips(r.details.w - 4)
	mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: r.details.x + 2 + hits[1].start, Y: r.details.y + r.details.h - 2})
	m = mm.(Model)
	if cmd == nil || len(m.jobs) != 1 || m.jobs[0].spec.choice.name() != a.Name || m.jobs[0].spec.install {
		t.Fatalf("download tap: %+v", m.task)
	}
	// On narrow screens tapping a build drills in, and tapping the header goes back.
	m = buildModel(t, 40, 24)
	r = m.buildsLayout()
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 3, Y: r.runs.y + 2})
	m = mm.(Model)
	if !m.buildDetail || m.selectedRunID != 100 {
		t.Fatal("narrow build tap did not drill in")
	}
	r = m.buildsLayout()
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: 3, Y: r.arts.y})
	m = mm.(Model)
	if m.buildDetail || m.focus != PanelRuns {
		t.Fatal("narrow header tap did not go back")
	}
}

func TestBuildArtifactErrorAndNoStaleInstall(t *testing.T) {
	m := buildModel(t, 100, 30)
	m.dev = device.Info{CanInstall: true}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mm.(Model)
	mm, _ = m.Update(ArtifactsMsg{RunID: 100, Err: errors.New("network problem")})
	m = mm.(Model)
	if !strings.Contains(components.StripANSI(m.View()), "Couldn't load artifacts") {
		t.Fatal("error not visible")
	}
	m.haveDownload = true
	m.downloadedAPK.Path = "/old.apk"
	mm, cmd := m.Update(key("i"))
	m = mm.(Model)
	if cmd != nil || m.modal != ModalNone {
		t.Fatal("missing artifact must not install an old download")
	}
}

func TestBuildConfirmFitsPhone(t *testing.T) {
	for _, w := range []int{32, 40, 48, 63, 64, 80} {
		m := buildModel(t, w, 24)
		m.dev = device.Info{CanInstall: true}
		m.modal = ModalConfirm
		m.confirmKind = confirmSafetyOverride
		m.confirm = components.Confirm{Title: "Safety check before install", Body: []string{"Different signing key"}, Options: []string{"Install anyway", "Cancel"}, Cursor: 1}
		lines := strings.Split(m.View(), "\n")
		if len(lines) > 24 {
			t.Errorf("%d columns: confirm overflows height: %d", w, len(lines))
		}
		lineWidths(t, w, 24, lines)
	}
}

func TestPhoneConfirmButtonsHitCorrectly(t *testing.T) {
	for _, width := range []int{32, 40, 64, 100} {
		for _, option := range []string{"Download", "Download & Install", "Cancel"} {
			m := buildModel(t, width, 24)
			m.dev = device.Info{CanInstall: true}
			m.modal = ModalConfirm
			m.confirmKind = confirmSafetyOverride
			m.confirm = components.Confirm{Title: "Safety check", Options: []string{"Download", "Download & Install", "Cancel"}}
			lines := strings.Split(m.View(), "\n")
			var x, y = -1, -1
			for i := len(lines) - 1; i >= 0; i-- {
				line := components.StripANSI(lines[i])
				p := strings.Index(line, option)
				if p >= 0 {
					candidate := components.VisibleWidth(line[:p]) + len(option)/2
					if hit := m.confirmButtonHit(candidate, i, m.confirm.Options); hit >= 0 && m.confirm.Options[hit] == option {
						x, y = candidate, i
						break
					}
				}
			}
			if x < 0 {
				t.Fatalf("%d: %q missing: %s", width, option, components.StripANSI(m.View()))
			}
			if got := m.confirmButtonHit(x, y, m.confirm.Options); got < 0 || m.confirm.Options[got] != option {
				t.Fatalf("%d: tap %q returned %d", width, option, got)
			}
		}
	}
}

func TestBuildReleaseAssetsAndFilteredRefresh(t *testing.T) {
	m := buildModel(t, 100, 30)
	m.settings.Source = "releases"
	m.releases = []ghapi.Release{{TagName: "v1", HTMLURL: "https://github.com/acme/rocket/releases/tag/v1", Assets: []ghapi.ReleaseAsset{{ID: 1, Name: "arm64-debug.apk", Size: 10}, {ID: 2, Name: "universal-debug.apk", Size: 20}}}}
	m.fetchArtifactsForSelection()
	if !m.usingReleaseAssets() || len(m.arts.Items) != 2 {
		t.Fatal("release assets not shown")
	}
	m.arts.Filter = "universal"
	m.arts.Cursor = 0
	m.syncArtifactSelection()
	m.syncReleaseArtifactItems()
	a, ok := m.selectedReleaseAsset()
	if !ok || a.Asset.ID != 2 {
		t.Fatalf("wrong filtered release after refresh: %+v", a)
	}
	mm, cmd := m.Update(key("d"))
	m = mm.(Model)
	if cmd == nil || len(m.jobs) != 1 || m.jobs[0].spec.choice.name() != "universal-debug.apk" || m.jobs[0].spec.install {
		t.Fatalf("wrong release: %+v", m.task)
	}
}

func TestPhoneCancelTouchDoesNotDownload(t *testing.T) {
	m := buildModel(t, 40, 24)
	m.dev = device.Info{CanInstall: true}
	m.modal = ModalConfirm
	m.confirmKind = confirmSafetyOverride
	m.confirm = components.Confirm{Title: "Safety check", Options: []string{"Install anyway", "Cancel"}, Cursor: 1}
	lines := strings.Split(m.View(), "\n")
	for y, line := range lines {
		plain := components.StripANSI(line)
		i := strings.Index(plain, "Cancel")
		if i < 0 {
			continue
		}
		x := components.VisibleWidth(plain[:i]) + 2
		mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x, Y: y})
		m = mm.(Model)
		if m.modal != ModalNone || cmd != nil || m.busy {
			t.Fatalf("cancel triggered an action: modal=%d cmd=%v busy=%v", m.modal, cmd != nil, m.busy)
		}
		return
	}
	t.Fatal("cancel not shown")
}
