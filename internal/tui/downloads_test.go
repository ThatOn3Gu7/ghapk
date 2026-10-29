package tui

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ThatOn3Gu7/ghapk/internal/apk"
	"github.com/ThatOn3Gu7/ghapk/internal/config"
	"github.com/ThatOn3Gu7/ghapk/internal/device"
	"github.com/ThatOn3Gu7/ghapk/internal/ghapi"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
)

func jobTestEnv(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHAPK_CACHE_DIR", filepath.Join(root, "cache"))
	t.Setenv("GHAPK_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("GHAPK_HISTORY", filepath.Join(root, "history.json"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return root, bin
}
func jobZip(t *testing.T, root string) (string, string) {
	t.Helper()
	path := filepath.Join(root, "fixture.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("app-universal-debug.apk")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("PK\x03\x04test-apk"))
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, fmt.Sprintf("sha256:%x", sha256.Sum256(b))
}
func fakeCommand(t *testing.T, bin, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
}
func collectJob(id int, ctx context.Context, s jobSpec) jobFinishedMsg {
	ch := make(chan tea.Msg, 64)
	go runJob(id, ctx, s, ch)
	var result jobFinishedMsg
	for msg := range ch {
		if v, ok := msg.(jobFinishedMsg); ok {
			result = v
		}
	}
	return result
}
func collectJobWithAnswers(id int, ctx context.Context, s jobSpec, answer int) jobFinishedMsg {
	ch := make(chan tea.Msg, 64)
	go runJob(id, ctx, s, ch)
	var result jobFinishedMsg
	for msg := range ch {
		switch v := msg.(type) {
		case jobQuestionMsg:
			v.reply <- answer
		case jobFinishedMsg:
			result = v
		}
	}
	return result
}
func TestConcurrentJobsSnapshotRepoAndSaveUniqueFiles(t *testing.T) {
	root, bin := jobTestEnv(t)
	zipPath, digest := jobZip(t, root)
	t.Setenv("JOB_ZIP", zipPath)
	t.Setenv("JOB_CALLS", filepath.Join(root, "calls"))
	fakeCommand(t, bin, "gh", `echo "$*" >> "$JOB_CALLS"; cat "$JOB_ZIP"`)
	fakeCommand(t, bin, "adb", `echo invoked >> "$JOB_CALLS"`)
	settings := config.Defaults()
	a := jobSpec{repo: "one/repo", settings: settings, choice: planChoice{art: ghapi.Artifact{ID: 3, Name: "android", Digest: digest}}}
	b := a
	b.repo = "two/repo"
	var results [2]jobFinishedMsg
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); results[0] = collectJob(1, context.Background(), a) }()
	go func() { defer wg.Done(); results[1] = collectJob(2, context.Background(), b) }()
	wg.Wait()
	for _, r := range results {
		if r.err != nil || r.status != "done" || r.meta.DigestStatus != apk.DigestVerified {
			t.Fatalf("job: %+v", r)
		}
		if _, err := os.Stat(r.path); err != nil {
			t.Fatal(err)
		}
	}
	if results[0].path == results[1].path {
		t.Fatal("jobs overwrote each other")
	}
	calls, _ := os.ReadFile(filepath.Join(root, "calls"))
	if !strings.Contains(string(calls), "repos/one/repo/") || !strings.Contains(string(calls), "repos/two/repo/") || strings.Contains(string(calls), "invoked") {
		t.Fatalf("wrong sources or installer invoked: %s", calls)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "cache", "work"))
	if len(entries) != 0 {
		t.Fatalf("scratch left behind: %v", entries)
	}
	m := New(Options{Settings: settings})
	if len(m.savedFiles) != 2 {
		t.Fatalf("saved files: %+v", m.savedFiles)
	}
	origins := map[string]bool{}
	for _, f := range m.savedFiles {
		origins[f.repo] = true
	}
	if !origins["one/repo"] || !origins["two/repo"] {
		t.Fatalf("origins: %v", origins)
	}
}
func TestJobCacheDigestAndReleaseZip(t *testing.T) {
	root, bin := jobTestEnv(t)
	p, digest := jobZip(t, root)
	t.Setenv("JOB_ZIP", p)
	calls := filepath.Join(root, "calls")
	t.Setenv("JOB_CALLS", calls)
	fakeCommand(t, bin, "gh", `echo "$*" >> "$JOB_CALLS"; cat "$JOB_ZIP"`)
	settings := config.Defaults()
	settings.KeepCache = true
	s := jobSpec{repo: "one/repo", settings: settings, choice: planChoice{release: true, asset: ghapi.ReleaseAsset{ID: 4, Name: "release.zip", Digest: digest}}}
	for i := 0; i < 2; i++ {
		r := collectJob(i+1, context.Background(), s)
		if r.err != nil || r.meta.DigestStatus != apk.DigestVerified {
			t.Fatalf("release %d: %+v", i, r)
		}
	}
	data, _ := os.ReadFile(calls)
	if n := strings.Count(string(data), "releases/assets"); n != 1 {
		t.Fatalf("expected cached second download, calls=%q", data)
	}
	s.choice.asset.Digest = "sha256:" + strings.Repeat("0", 64)
	r := collectJob(3, context.Background(), s)
	if r.err == nil || !strings.Contains(r.err.Error(), "mismatch") || r.path != "" {
		t.Fatalf("digest mismatch not rejected: %+v", r)
	}
}
func TestJobCancellationCleansScratchAndNeverInstalls(t *testing.T) {
	root, bin := jobTestEnv(t)
	marker := filepath.Join(root, "ready")
	t.Setenv("READY", marker)
	t.Setenv("INSTALL_MARKER", filepath.Join(root, "installed"))
	fakeCommand(t, bin, "gh", `printf 'partial'; echo yes > "$READY"; sleep 20`)
	fakeCommand(t, bin, "adb", `echo yes > "$INSTALL_MARKER"`)
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan tea.Msg, 64)
	go runJob(1, ctx, jobSpec{repo: "one/repo", settings: config.Defaults(), install: true, choice: planChoice{art: ghapi.Artifact{ID: 2, Name: "app"}}}, ch)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("fake download did not start")
	}
	cancel()
	timeout := time.After(4 * time.Second)
	var result jobFinishedMsg
loop:
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				break loop
			}
			if v, ok := msg.(jobFinishedMsg); ok {
				result = v
			}
		case <-timeout:
			t.Fatal("cancel did not stop transfer")
		}
	}
	if result.status != "cancelled" || result.path != "" {
		t.Fatalf("cancel result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "installed")); !os.IsNotExist(err) {
		t.Fatal("installer was invoked")
	}
	entries, _ := os.ReadDir(filepath.Join(root, "cache", "work"))
	if len(entries) != 0 {
		t.Fatalf("scratch leaked: %v", entries)
	}
}
func TestPlanChoicesAndTouchOnPhone(t *testing.T) {
	for _, size := range [][2]int{{32, 18}, {40, 24}, {80, 26}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := buildModel(t, size[0], size[1])
			m.dev = device.Info{CanInstall: true}
			mm, cmd := m.Update(key("i"))
			m = mm.(Model)
			if cmd == nil || m.plan == nil || !m.plan.loading {
				t.Fatal("missing checking plan")
			}
			mm, _ = m.Update(planReadyMsg{seq: m.plan.seq, dev: device.Info{OS: "android", CanInstall: true, ABIs: []string{"arm64-v8a"}}})
			m = mm.(Model)
			view := components.StripANSI(m.View())
			if !strings.Contains(view, "Download only") || !strings.Contains(view, "Cancel") {
				t.Fatalf("buttons missing: %s", view)
			}
			layout := m.plan.layout(&m)
			if len(layout.choices) < 2 {
				t.Fatal("multiple choices missing")
			}
			box := m.modalBox()
			x := (m.width-lipgloss.Width(box))/2 + 2
			y := (m.height-lipgloss.Height(box))/2 + 1
			choiceHit := layout.choices[1]
			mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x + choiceHit.x, Y: y + choiceHit.y})
			m = mm.(Model)
			name := m.plan.choices[m.plan.selected].name()
			layout = m.plan.layout(&m)
			buttonHit := layout.buttons[1]
			mm, cmd = m.Update(tea.MouseMsg{Type: tea.MouseLeft, X: x + buttonHit.x, Y: y + buttonHit.y})
			m = mm.(Model)
			if cmd == nil || len(m.jobs) != 1 || m.jobs[0].spec.install || m.jobs[0].spec.choice.name() != name {
				t.Fatal("touch download choice failed")
			}
			lineWidths(t, size[0], size[1], strings.Split(m.View(), "\n"))
		})
	}
}
func TestJobSafetyPromptQueueAndCancel(t *testing.T) {
	m := buildModel(t, 40, 24)
	m.startJob(jobSpec{})
	m.startJob(jobSpec{})
	q1 := jobQuestionMsg{id: 1, reply: make(chan int, 1), title: "warning", body: []string{"unsafe"}, options: []string{"Cancel", "Install anyway"}, danger: -1}
	q2 := jobQuestionMsg{id: 2, reply: make(chan int, 1), title: q1.title, body: q1.body, options: q1.options, danger: -1}
	mm, _ := m.Update(q1)
	m = mm.(Model)
	mm, _ = m.Update(q2)
	m = mm.(Model)
	if m.modal != ModalConfirm || m.jobQuestionID != 1 {
		t.Fatal("first prompt not shown")
	}
	mm, _ = m.Update(keyOf("esc"))
	m = mm.(Model)
	if <-q1.reply != 0 {
		t.Fatal("escape accepted warning")
	}
	if m.modal != ModalConfirm || m.jobQuestionID != 2 {
		t.Fatal("second warning not queued")
	}
	mm, _ = m.Update(keyOf("esc"))
	m = mm.(Model)
	if <-q2.reply != 0 {
		t.Fatal("second warning accepted")
	}
	for _, j := range m.jobs {
		j.cancel()
	}
}

func jobTestAPK(t *testing.T, path string, code int32) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("AndroidManifest.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(jobTestAXML("com.example.app", "1.0", code, 21, 34)); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
}
func TestSavedAPKInstallUsesSafetyChecks(t *testing.T) {
	root, bin := jobTestEnv(t)
	path := filepath.Join(root, "cache", "apk", "saved.apk")
	jobTestAPK(t, path, 10)
	t.Setenv("INSTALL_MARKER", filepath.Join(root, "installed"))
	fakeCommand(t, bin, "adb", `case "$*" in *get-state*) echo device;; *ro.product.cpu.abilist*) echo arm64-v8a;; *ro.build.version.sdk*) echo 34;; *dumpsys*) echo 'versionCode=50 versionName=5.0';; *install*) echo yes > "$INSTALL_MARKER"; echo Success;; esac`)
	s := config.Defaults()
	s.Repo = "one/repo"
	s.PackageName = "com.example.app"
	s.Downgrade = "never"
	result := collectJob(1, context.Background(), jobSpec{repo: s.Repo, settings: s, install: true, savedPath: path})
	if result.err == nil || !strings.Contains(result.err.Error(), "downgrade") {
		t.Fatalf("downgrade not blocked: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "installed")); !os.IsNotExist(err) {
		t.Fatal("blocked install reached adb")
	}
	// Automatic policy asks before a downgrade; cancellation must not launch it.
	s.Downgrade = "auto"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan tea.Msg, 64)
	go runJob(2, ctx, jobSpec{repo: s.Repo, settings: s, install: true, savedPath: path}, ch)
	asked := false
	timer := time.After(5 * time.Second)
loop:
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				break loop
			}
			if _, ok := msg.(jobQuestionMsg); ok {
				asked = true
				cancel()
			}
		case <-timer:
			t.Fatal("safety prompt hung")
		}
	}
	if !asked {
		t.Fatal("missing saved-APK safety prompt")
	}
	if _, err := os.Stat(filepath.Join(root, "installed")); !os.IsNotExist(err) {
		t.Fatal("cancelled warning reached adb")
	}
}
func TestLaunchedInstallerNotMistakenForConfirmation(t *testing.T) {
	root, bin := jobTestEnv(t)
	path := filepath.Join(root, "cache", "apk", "saved.apk")
	jobTestAPK(t, path, 10)
	t.Setenv("TERMUX_VERSION", "1")
	fakeCommand(t, bin, "termux-open", `exit 0`)
	fakeCommand(t, bin, "dumpsys", `echo 'versionCode=10 versionName=1.0'`)
	s := config.Defaults()
	s.PackageName = "com.example.app"
	result := collectJobWithAnswers(1, context.Background(), jobSpec{repo: "one/repo", settings: s, install: true, savedPath: path}, 1)
	if result.err != nil || result.status != "launched" {
		t.Fatalf("same preinstalled version is not proof of installation: %+v", result)
	}
}
func TestSavedDeleteAndTouchScroll(t *testing.T) {
	root, _ := jobTestEnv(t)
	for _, n := range []string{"first.apk", "second.apk"} {
		jobTestAPK(t, filepath.Join(root, "cache", "apk", n), 10)
	}
	m := New(Options{Settings: config.Defaults()})
	m.width = 40
	m.height = 24
	m.screen = ScreenDashboard
	m.focus = PanelDownloads
	m.refreshSaved()
	m.downloadFocus = 1
	target := m.savedFiles[0].path
	mm, _ := m.Update(key("x"))
	m = mm.(Model)
	if m.modal != ModalConfirm || !m.confirm.Danger {
		t.Fatal("delete must require a destructive confirmation")
	}
	mm, _ = m.Update(keyOf("esc"))
	m = mm.(Model)
	if _, err := os.Stat(target); err != nil {
		t.Fatal("cancel deleted APK")
	}
	mm, _ = m.Update(key("x"))
	m = mm.(Model)
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = mm.(Model)
	mm, _ = m.Update(keyOf("enter"))
	m = mm.(Model)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("confirmed delete failed")
	}
	// A swipe over a saved row must scroll, never launch an installer.
	r := m.downloadRects().saved
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, Action: tea.MouseActionPress, X: 3, Y: r.y + 1})
	m = mm.(Model)
	mm, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: 3, Y: r.y + 3})
	m = mm.(Model)
	mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseRelease, Action: tea.MouseActionRelease, X: 3, Y: r.y + 3})
	m = mm.(Model)
	if len(m.jobs) != 0 {
		t.Fatal("swipe started installation")
	}
}

func TestJobRejectsZipTraversal(t *testing.T) {
	root, bin := jobTestEnv(t)
	path := filepath.Join(root, "evil.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("../escape.apk")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("payload"))
	_ = z.Close()
	_ = f.Close()
	t.Setenv("JOB_ZIP", path)
	fakeCommand(t, bin, "gh", `cat "$JOB_ZIP"`)
	r := collectJob(1, context.Background(), jobSpec{repo: "one/repo", settings: config.Defaults(), choice: planChoice{art: ghapi.Artifact{ID: 1, Name: "bad"}}})
	if r.err == nil || r.path != "" {
		t.Fatalf("unsafe archive accepted: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, "cache", "escape.apk")); !os.IsNotExist(err) {
		t.Fatal("archive escaped scratch directory")
	}
}

func TestQuitCancelsJobsAndWaitsForCleanup(t *testing.T) {
	m := buildModel(t, 40, 24)
	m.startJob(jobSpec{})
	mm, cmd := m.Update(key("q"))
	m = mm.(Model)
	if cmd != nil || m.confirmKind != confirmQuitJobs {
		t.Fatal("quit must ask first")
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = mm.(Model)
	mm, cmd = m.Update(keyOf("enter"))
	m = mm.(Model)
	if cmd != nil || !m.quitAfterJobs || m.quitting || m.jobs[0].ctx.Err() == nil {
		t.Fatal("quit must cancel and wait")
	}
	mm, cmd = m.Update(jobFinishedMsg{id: 1, status: "cancelled"})
	m = mm.(Model)
	if cmd == nil || !m.quitting {
		t.Fatal("quit did not complete after cleanup")
	}
}
func TestJobTextRemovesTerminalControls(t *testing.T) {
	s := safeJobText("a\x1b[2Jb\u202ec.apk")
	if strings.ContainsAny(s, "\x1b\u202e") {
		t.Fatalf("unsafe terminal text: %q", s)
	}
}
