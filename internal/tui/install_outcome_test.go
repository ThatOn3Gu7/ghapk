package tui

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/apk"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
)

func TestTaskModalHonestInstallOutcomes(t *testing.T) {
	// 1. Launched (unconfirmed) must NOT render as confirmed/installed.
	m := newTestModel()
	m.task = newTask("Installing", "app-debug.apk", progressColor{m.st.P.Accent, m.st.P.AccentAlt})
	m.task.Kind = "install"
	m.task.phase = "installing"
	m.modal = ModalTask

	mm, _ := m.Update(InstallDoneMsg{
		Result: device.InstallResult{
			Outcome: device.OutcomeLaunched,
			Via:     "termux-open",
			Detail:  "handed off to Android package installer",
		},
	})
	mo := mm.(Model)
	v := mo.View()
	if !strings.Contains(v, "installer launched (unconfirmed)") {
		t.Fatalf("expected unconfirmed launched status in modal view, got:\n%s", v)
	}
	if strings.Contains(v, "✓ installed") {
		t.Fatalf("launched intent must never render as confirmed installed:\n%s", v)
	}
	if !strings.Contains(mo.toast, "confirm on device") {
		t.Fatalf("expected honest toast for launched installer, got %q", mo.toast)
	}

	// 2. Positively confirmed install via adb renders as confirmed installed.
	m2 := newTestModel()
	m2.task = newTask("Installing", "app-debug.apk", progressColor{m2.st.P.Accent, m2.st.P.AccentAlt})
	m2.task.Kind = "install"
	m2.task.phase = "installing"
	m2.modal = ModalTask

	mm2, _ := m2.Update(InstallDoneMsg{
		Result: device.InstallResult{
			Outcome: device.OutcomeInstalled,
			Via:     "adb",
			Detail:  "installed and confirmed via adb",
		},
	})
	mo2 := mm2.(Model)
	v2 := mo2.View()
	if !strings.Contains(v2, "installed") {
		t.Fatalf("expected confirmed installed status in modal view, got:\n%s", v2)
	}
	if !strings.Contains(mo2.toast, "installed via adb") {
		t.Fatalf("expected confirmed toast, got %q", mo2.toast)
	}

	// 3. Failure surfaces diagnosis and error clearly.
	m3 := newTestModel()
	m3.task = newTask("Installing", "app-debug.apk", progressColor{m3.st.P.Accent, m3.st.P.AccentAlt})
	m3.task.Kind = "install"
	m3.task.phase = "installing"
	m3.modal = ModalTask

	mm3, _ := m3.Update(InstallDoneMsg{
		Result: device.InstallResult{
			Outcome:     device.OutcomeFailed,
			Via:         "adb",
			FailureCode: "INSTALL_FAILED_UPDATE_INCOMPATIBLE",
			Diagnosis:   "signed with a different key",
		},
		Err: errors.New("adb install failed (INSTALL_FAILED_UPDATE_INCOMPATIBLE)"),
	})
	mo3 := mm3.(Model)
	v3 := mo3.View()
	if !strings.Contains(v3, "failed") || !strings.Contains(v3, "different key") {
		t.Fatalf("expected failure and diagnosis in modal view, got:\n%s", v3)
	}
}

func TestStartInstallPromptsOnDowngradeAutoAndBlocksReflexY(t *testing.T) {
	t.Setenv("GHAPK_HISTORY", filepath.Join(t.TempDir(), "history.json"))
	apkPath := filepath.Join(t.TempDir(), "app-older.apk")
	if err := os.WriteFile(apkPath, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestModel()
	m.settings.Downgrade = "auto"
	m.settings.PackageName = "com.example.rocket"
	m.instPkg = "com.example.rocket"
	m.instFound = true
	m.instApp = device.InstalledApp{
		Package:     "com.example.rocket",
		VersionCode: 500,
		VersionName: "5.0.0",
		CertSHA256:  strings.Repeat("a", 64),
		Queryable:   true,
	}
	m.downloadedAPK = ghapi.ExtractedAPK{
		Path: apkPath,
		Name: "app-older.apk",
		Size: 5,
		Meta: apk.Inspection{
			HaveInfo: true,
			Info: apk.Info{
				Package:     "com.example.rocket",
				VersionCode: 400,
				VersionName: "4.0.0",
			},
			Cert: apk.Cert{SHA256: strings.Repeat("b", 64), Scheme: "v3"},
		},
	}

	cmd := m.startInstall(apkPath)
	if cmd != nil {
		t.Fatal("downgrade=auto + signature mismatch must open ModalConfirm before running installer")
	}
	if m.modal != ModalConfirm || m.confirm.Cursor != 1 {
		t.Fatalf("expected ModalConfirm with safe default Cursor=1 (Cancel), got modal=%v cursor=%d", m.modal, m.confirm.Cursor)
	}
	v := m.View()
	if !strings.Contains(v, "Downgrade warning") || !strings.Contains(v, "Signing key mismatch") {
		t.Fatalf("expected downgrade and signing key warnings in modal:\n%s", v)
	}

	// Pressing 'y' must NOT bypass safety override confirmation!
	mm, cmd2 := m.Update(key("y"))
	mo := mm.(Model)
	if mo.modal != ModalConfirm || cmd2 != nil {
		t.Fatalf("reflex 'y' must not bypass safety confirmation: modal=%v cmd=%v", mo.modal, cmd2)
	}

	// Pressing Enter on default cursor (1 = Cancel) cancels install cleanly.
	mm3, cmd3 := mo.Update(keyOf("enter"))
	mo3 := mm3.(Model)
	if mo3.modal != ModalNone || cmd3 != nil {
		t.Fatalf("Enter on default Cancel must abort install cleanly: modal=%v cmd=%v", mo3.modal, cmd3)
	}
}

// Install opens a preflight plan, even when NoOpen is set.
func TestInstallOpensPreflight(t *testing.T) {
	for _, k := range []tea.KeyMsg{key("i"), keyOf("enter")} {
		m := seedArtifacts(seedRuns(newTestModel()))
		m.settings.NoOpen = true
		mm, _ := m.Update(key("3"))
		mm, _ = mm.Update(keyOf("enter"))
		m = mm.(Model)
		mm, cmd := m.Update(k)
		m = mm.(Model)
		if cmd == nil || m.modal != ModalPlan || m.plan == nil || !m.plan.loading {
			t.Fatalf("%s did not open checking plan", k.String())
		}
		mm, _ = m.Update(planReadyMsg{seq: m.plan.seq, dev: device.Info{CanInstall: true}})
		m = mm.(Model)
		mm, cmd = m.Update(keyOf("enter"))
		m = mm.(Model)
		if cmd == nil || m.focus != PanelDownloads || len(m.jobs) != 1 || !m.jobs[0].spec.install {
			t.Fatal("plan did not start install job")
		}
	}
}

// TestPostDownloadMenuInstallAndCloseActions verifies both buttons ("Install"
// and "Close") on the post-download TaskState menu via keyboard (Enter, Space,
// 'i', Tab) and mouse click, including async InstallPreparedMsg handoff.
func TestPostDownloadMenuInstallAndCloseActions(t *testing.T) {
	t.Setenv("GHAPK_HISTORY", filepath.Join(t.TempDir(), "history.json"))
	apkPath := filepath.Join(t.TempDir(), "app-debug.apk")
	if err := os.WriteFile(apkPath, []byte("apk"), 0o644); err != nil {
		t.Fatal(err)
	}

	makePostDownloadModel := func() Model {
		m := seedArtifacts(seedRuns(newTestModel()))
		m.dev = device.Info{OS: "android", CanInstall: true, InstallVia: "termux-open"}
		m.settings.NoOpen = true // regression guard: NoOpen must not make Install button a no-op
		m.task = newTask("Downloading artifact", "app-debug.apk", progressColor{m.st.P.Accent, m.st.P.AccentAlt})
		m.modal = ModalTask
		mm, _ := m.Update(DownloadDoneMsg{
			APK: ghapi.ExtractedAPK{
				Path: apkPath,
				Name: "app-debug.apk",
				Size: 3,
				Meta: apk.Inspection{
					HaveInfo: true,
					Info:     apk.Info{Package: "com.acme.rocket", VersionCode: 12, VersionName: "1.2"},
				},
			},
		})
		mo := mm.(Model)
		if mo.task == nil || len(mo.task.buttons) != 2 || mo.task.buttons[0] != "Install" || mo.task.buttons[1] != "Close" {
			t.Fatalf("expected [Install Close] buttons, got %+v", mo.task)
		}
		return mo
	}

	// 1. Pressing Enter on "Install" starts installation flow -> InstallPreparedMsg -> executeInstall.
	for _, trigger := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyEnter},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}},
	} {
		m := makePostDownloadModel()
		mm, prepCmd := m.Update(trigger)
		m = mm.(Model)
		if prepCmd == nil || m.task == nil || m.task.Kind != "install" || m.task.phase != "installing" {
			t.Fatalf("trigger %T (%v) on Install did nothing: cmdNil=%v task=%+v", trigger, trigger, prepCmd == nil, m.task)
		}

		// Complete async prepare step -> must launch installer cmd batch.
		mm, execCmd := m.Update(InstallPreparedMsg{
			Path:     apkPath,
			Meta:     m.downloadedAPK.Meta,
			QueryPkg: "com.acme.rocket",
		})
		m = mm.(Model)
		if execCmd == nil || m.task == nil || m.task.phase != "installing" {
			t.Fatalf("InstallPreparedMsg did not start installer execution: cmdNil=%v task=%+v", execCmd == nil, m.task)
		}
	}

	// 2. Selecting "Close" via Right+Enter or Tab+Enter dismisses without installing.
	for _, nav := range []tea.KeyMsg{
		{Type: tea.KeyRight},
		{Type: tea.KeyTab},
	} {
		m := makePostDownloadModel()
		mm, _ := m.Update(nav)
		m = mm.(Model)
		if m.task.btnIdx != 1 {
			t.Fatalf("nav %q did not focus Close (btnIdx=%d)", nav.String(), m.task.btnIdx)
		}
		mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = mm.(Model)
		if cmd != nil || m.modal != ModalNone || m.task != nil {
			t.Fatalf("Close did not cleanly dismiss modal without installing: cmd=%v modal=%v task=%v", cmd, m.modal, m.task)
		}
	}

	// 3. Mouse clicks on "Install" and "Close" buttons hit exact coordinates.
	findBtnCoords := func(m Model, targetIdx int) (int, int) {
		lines := strings.Split(m.modalLayer(), "\n")
		for y := len(lines) - 1; y >= 1; y-- {
			plain := components.StripANSI(lines[y])
			if strings.Contains(plain, "╰─") || strings.Contains(plain, "+-") {
				btnLine := []rune(components.StripANSI(lines[y-1]))
				for i, r := range btnLine {
					if r == '│' || r == '|' {
						x := components.VisibleWidth(string(btnLine[:i])) + 3
						for b := 0; b < targetIdx; b++ {
							x += components.VisibleWidth(m.task.buttons[b]) + 6
						}
						return x + 2, y - 1
					}
				}
			}
		}
		t.Fatal("modal button row not found in View()")
		return 0, 0
	}

	mClickInstall := makePostDownloadModel()
	ix, iy := findBtnCoords(mClickInstall, 0)
	mm, clickCmd := mClickInstall.Update(tea.MouseMsg{Type: tea.MouseLeft, X: ix, Y: iy})
	mClickInstall = mm.(Model)
	if clickCmd == nil || mClickInstall.task == nil || mClickInstall.task.Kind != "install" {
		t.Fatalf("mouse click on Install failed at (%d,%d): cmdNil=%v task=%+v", ix, iy, clickCmd == nil, mClickInstall.task)
	}

	mClickClose := makePostDownloadModel()
	cx, cy := findBtnCoords(mClickClose, 1)
	mm, closeCmd := mClickClose.Update(tea.MouseMsg{Type: tea.MouseLeft, X: cx, Y: cy})
	mClickClose = mm.(Model)
	if closeCmd != nil || mClickClose.modal != ModalNone || mClickClose.task != nil {
		t.Fatalf("mouse click on Close failed at (%d,%d): cmd=%v modal=%v task=%v", cx, cy, closeCmd, mClickClose.modal, mClickClose.task)
	}
}

type syncBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// Drive an actual Tea program with stubbed gh and adb, from Install straight
// through download to adb, without a download confirmation.
func TestLiveTeaProgramDirectInstallEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHAPK_CONFIG_DIR", filepath.Join(tmp, "cfg"))
	t.Setenv("GHAPK_CACHE_DIR", filepath.Join(tmp, "cache"))
	t.Setenv("GHAPK_HISTORY", filepath.Join(tmp, "cfg", "history.json"))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Create a valid artifact zip containing app-arm64-v8a-debug.apk.
	artZip := filepath.Join(tmp, "artifact.zip")
	zf, err := os.Create(artZip)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	w, err := zw.Create("app-universal-debug.apk")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("PK\x03\x04fake-apk-payload"))
	_ = zw.Close()
	_ = zf.Close()

	adbMarker := filepath.Join(tmp, "adb-invoked.txt")
	adbScript := "#!/bin/sh\n" +
		"if [ \"$1\" = \"get-state\" ]; then echo device; exit 0; fi\n" +
		"if [ \"$1\" = \"shell\" ]; then echo 'Unable to find package'; exit 0; fi\n" +
		"if [ \"$1\" = \"install\" ]; then echo \"$@\" > '" + adbMarker + "'; echo 'Performing Streamed Install'; echo 'Success'; exit 0; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "adb"), []byte(adbScript), 0o755); err != nil {
		t.Fatal(err)
	}

	ghScript := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  *--version*) echo 'gh version 2.62.0'; exit 0 ;;\n" +
		"  *'api user'*) echo '{\"login\":\"tester\"}'; exit 0 ;;\n" +
		"  *'/zip'*) cat '" + artZip + "'; exit 0 ;;\n" +
		"  *'/artifacts'*) echo '{\"artifacts\":[{\"id\":9001,\"name\":\"app-debug\",\"size_in_bytes\":128,\"expired\":false}]}'; exit 0 ;;\n" +
		"  *'/runs'*) echo '{\"workflow_runs\":[{\"id\":101,\"run_number\":1,\"name\":\"CI\",\"head_branch\":\"main\",\"status\":\"completed\",\"conclusion\":\"success\",\"display_title\":\"build\"}]}'; exit 0 ;;\n" +
		"  *'/workflows'*) echo '{\"workflows\":[{\"id\":1,\"name\":\"CI\",\"path\":\".github/workflows/ci.yml\",\"state\":\"active\"}]}'; exit 0 ;;\n" +
		"  *'/releases'*) echo '[]'; exit 0 ;;\n" +
		"  *'repos/acme/rocket'*) echo '{\"full_name\":\"acme/rocket\",\"default_branch\":\"main\"}'; exit 0 ;;\n" +
		"esac\n" +
		"echo '{}'\n"
	if err := os.WriteFile(filepath.Join(binDir, "gh"), []byte(ghScript), 0o755); err != nil {
		t.Fatal(err)
	}

	inR, inW := io.Pipe()
	defer inR.Close()
	var out syncBuf

	s := config.Defaults()
	s.Repo = "acme/rocket"
	s.NoOpen = true // ensure even with no-open set, interactive Install works
	m := New(Options{Settings: s})
	p := tea.NewProgram(m, tea.WithInput(inR), tea.WithOutput(&out), tea.WithoutSignalHandler())

	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()

	p.Send(tea.WindowSizeMsg{Width: 120, Height: 36})

	waitForOut := func(sub string) {
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(components.StripANSI(out.String()), sub) {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %q in live TUI output:\n%s", sub, components.StripANSI(out.String()))
	}

	// Select Builds, wait for the preflight, then install.
	_, _ = inW.Write([]byte("3"))
	waitForOut("app-debug")
	waitForOut("d:download")
	_, _ = inW.Write([]byte("i"))
	waitForOut("unknown (package not known)")
	_, _ = inW.Write([]byte("\r"))
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(adbMarker); err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if strings.Contains(components.StripANSI(out.String()), "Confirm download") {
		t.Fatal("unexpected download confirmation")
	}

	gotADB, err := os.ReadFile(adbMarker)
	if err != nil || !strings.Contains(string(gotADB), "install -r") {
		t.Fatalf("expected live Install action to run `adb install -r`, got marker=%q err=%v", string(gotADB), err)
	}

	// Wait for the background job to finish before quitting.
	waitForOut("app-debug  installed")
	_, _ = inW.Write([]byte("q"))
	_ = inW.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("tea.Program exited with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tea.Program did not exit after q")
	}
}
