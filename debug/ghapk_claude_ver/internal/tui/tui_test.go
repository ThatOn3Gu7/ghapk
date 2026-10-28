package tui

import (
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ghapk/internal/engine"
	"ghapk/internal/term"
)

func world(t *testing.T) *engine.Engine {
	t.Helper()
	for _, tool := range []string{"python3", "openssl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool)
		}
	}
	root, _ := filepath.Abs("../../scripts/fakegh")
	fix := filepath.Join(t.TempDir(), "fake")
	if out, err := exec.Command("python3", filepath.Join(root, "gen.py"), fix).CombinedOutput(); err != nil {
		t.Fatalf("gen: %v\n%s", err, out)
	}
	t.Setenv("PATH", root+":"+filepath.Join(root, "bin")+":"+os.Getenv("PATH"))
	t.Setenv("FAKEGH_DIR", fix)
	t.Setenv("FAKEGH_SPEED", "0")
	t.Setenv("FAKEPHONE_DIR", t.TempDir())
	t.Setenv("FAKE_INSTALL_DELAY", "1")
	t.Setenv("TERMUX_VERSION", "1")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	t.Setenv("GHAPK_CACHE_DIR", t.TempDir())
	t.Setenv("GHAPK_HISTORY", filepath.Join(t.TempDir(), "h.json"))
	t.Setenv("GHAPK_RISH", "off")
	t.Setenv("GHAPK_REPO", "acme/nazo")
	e := engine.New(map[string]string{"install-wait": "15"})
	e.GH.Retries = 0
	return e
}

func press(a *App, keys ...string) {
	for _, k := range keys {
		switch k {
		case "enter":
			a.Event(keyEv(term.KeyEnter))
		case "esc":
			a.Event(keyEv(term.KeyEsc))
		case "down":
			a.Event(keyEv(term.KeyDown))
		case "right":
			a.Event(keyEv(term.KeyRight))
		default:
			for _, r := range k {
				a.Event(runeEv(r))
			}
		}
		a.Frame()
	}
}

func shown(a *App) string { return a.Frame().String() }

func ready(t *testing.T, w, h int) (*App, *engine.Engine) {
	e := world(t)
	t.Cleanup(e.Close)
	a := New(e, w, h, Options{})
	a.Start()
	a.Pump(400 * time.Millisecond)
	if !strings.Contains(shown(a), "#411") {
		t.Fatalf("builds did not load:\n%s", shown(a))
	}
	return a, e
}

func TestBuildsList(t *testing.T) {
	a, _ := ready(t, 100, 30)
	s := shown(a)
	for _, want := range []string{"acme/nazo", "1 Builds", "#412", "fix: quiz timer drift", "↵ install"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
}

func TestPlanAndInstall(t *testing.T) {
	a, e := ready(t, 100, 30)
	press(a, "j") // #411, the newest finished build
	a.Pump(300 * time.Millisecond)
	press(a, "i")
	a.Pump(500 * time.Millisecond)
	if s := shown(a); !strings.Contains(s, "Install this build?") {
		t.Fatalf("no plan dialog:\n%s", s)
	}
	press(a, "enter")
	if a.tab != TabDownloads || len(a.jobs) != 1 {
		t.Fatalf("job not started: tab %d jobs %d", a.tab, len(a.jobs))
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		a.Pump(150 * time.Millisecond)
		if a.modal != nil { // "already installed" question: reinstall
			if q, ok := a.modal.(*questionModal); ok && strings.Contains(q.title, "Already") {
				press(a, "2") // reinstall
			} else {
				press(a, "1") // "Install it?" -> Install
			}
		}
		if !a.jobs[0].job.Running() {
			break
		}
	}
	v := a.jobs[0].job.Snapshot()
	if v.State != engine.JobDone {
		t.Fatalf("job ended %v: %v\n%s", v.State, v.Err, shown(a))
	}
	if v.Res.InstalledNow != 142 {
		t.Errorf("installed code %d; steps: %+v", v.Res.InstalledNow, v.Steps)
	}
	a.Pump(100 * time.Millisecond)
	s := shown(a)
	if !strings.Contains(s, "quiz.thaton3app.nazo") || !strings.Contains(s, "Saved APKs") {
		t.Errorf("results not shown:\n%s", s)
	}
	_ = e
}

func TestMouse(t *testing.T) {
	a, _ := ready(t, 100, 30)
	a.Frame()
	// tap the "3 Device" tab
	h := findHit(a, "tab", 2)
	a.Event(term.Event{Kind: term.EvMouse, X: h.R.X + 1, Y: h.R.Y, Button: term.BtnLeft, Action: term.MousePress})
	if a.tab != TabDevice {
		t.Fatalf("tab = %d", a.tab)
	}
	a.Event(runeEv('1'))
	a.Frame()
	// tap the 4th run row selects it
	h = findHit(a, "runs.row", 3)
	a.Event(term.Event{Kind: term.EvMouse, X: h.R.X + 3, Y: h.R.Y, Button: term.BtnLeft, Action: term.MousePress})
	if a.builds.list.Cur != 3 {
		t.Fatalf("cursor = %d", a.builds.list.Cur)
	}
	// wheel scrolls without moving the cursor
	a.Frame()
	a.Event(term.Event{Kind: term.EvMouse, X: h.R.X + 3, Y: h.R.Y, Button: term.BtnWheelDown, Action: term.MouseWheel})
	if a.builds.list.Cur != 3 {
		t.Fatalf("wheel moved cursor")
	}
}

func findHit(a *App, id string, n int) (h struct{ R struct{ X, Y, W, H int } }) {
	for x := 0; x < a.W; x++ {
		for y := 0; y < a.H; y++ {
			if hit, ok := a.hits.At(x, y); ok && hit.ID == id && hit.N == n {
				h.R.X, h.R.Y = hit.R.X, hit.R.Y
				return
			}
		}
	}
	return
}

func TestQuit(t *testing.T) {
	a, _ := ready(t, 80, 24)
	press(a, "q")
	if !a.Quit() {
		t.Fatal("q did not quit")
	}
}

func TestSizesAndKeyMashing(t *testing.T) {
	a, _ := ready(t, 100, 30)
	r := rand.New(rand.NewSource(3))
	keys := []string{"j", "k", "g", "G", "enter", "esc", "tab", "1", "2", "3", "4", "5", "?", "/", "x", "f", "b", "w", "r", "d", "s", " ", "right", "left", "i", "e", "o"}
	sizes := [][2]int{{100, 30}, {46, 30}, {32, 9}, {60, 12}, {140, 50}, {20, 5}, {80, 24}}
	for n := 0; n < 1500; n++ {
		if n%50 == 0 {
			s := sizes[r.Intn(len(sizes))]
			a.Event(term.Event{Kind: term.EvResize, W: s[0], H: s[1]})
		}
		k := keys[r.Intn(len(keys))]
		if k == "tab" {
			a.Event(keyEv(term.KeyTab))
		} else if k == "left" {
			a.Event(keyEv(term.KeyLeft))
		} else {
			press(a, k)
		}
		if k == "o" || k == "q" {
			continue
		}
		a.Frame()
		if n%25 == 0 {
			a.Pump(20 * time.Millisecond)
		}
		a.quit = false
		if a.modal != nil && r.Intn(3) == 0 {
			a.Event(keyEv(term.KeyEsc))
		}
	}
}
