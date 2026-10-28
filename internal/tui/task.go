package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/apk"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
	"github.com/ghapk/ghapk/internal/tui/theme"
)

// TaskState tracks a download/install operation shown in the task modal.
type TaskState struct {
	Kind     string // "download" / "install"
	Title    string
	Artifact string

	bar      progress.Model
	received int64
	total    int64

	phase      string // "downloading", "extracting", "installing", "launched", "done", "failed"
	logs       []string
	err        error
	result     ghapi.ExtractedAPK
	installRes device.InstallResult
	buttons    []string
	btnIdx     int
	started    time.Time

	running bool // still streaming
}

func newTask(title, artifact string, st progressColor) *TaskState {
	bar := progress.New(progress.WithGradient(string(st.full), string(st.alt)),
		progress.WithoutPercentage())
	bar.Width = 40
	return &TaskState{
		Kind:     "download",
		Title:    title,
		Artifact: artifact,
		bar:      bar,
		phase:    "downloading",
		started:  time.Now(),
		running:  true,
	}
}

type progressColor struct {
	full, alt lipgloss.Color
}

func (t *TaskState) setProgress(got, total int64) {
	t.received = got
	if total > 0 {
		t.total = total
	}
}

func (t *TaskState) addLog(line string) {
	t.logs = append(t.logs, line)
	if len(t.logs) > 6 {
		t.logs = t.logs[len(t.logs)-6:]
	}
}

func (t *TaskState) fail(err error) {
	t.phase = "failed"
	t.err = err
	t.running = false
	t.buttons = []string{"Close"}
}

func (t *TaskState) downloadDone(msg DownloadDoneMsg) {
	t.result = msg.APK
	t.phase = "done"
	t.running = false
	t.buttons = []string{"Close"}
	if device_CanInstall {
		t.buttons = []string{"Install", "Close"}
	}
	t.btnIdx = 0
}

var device_CanInstall bool // set each render from model

func (t *TaskState) installDone(res device.InstallResult, err error) {
	t.running = false
	t.installRes = res
	switch {
	case err != nil || res.Outcome == device.OutcomeFailed:
		t.phase = "failed"
		if err != nil {
			t.err = err
		} else {
			t.err = fmt.Errorf("installation failed")
		}
	case res.Outcome == device.OutcomeLaunched:
		t.phase = "launched"
	default:
		t.phase = "done"
		t.addLog("installation confirmed via " + res.Via)
	}
	t.buttons = []string{"Close"}
}

func (t *TaskState) isDismissable() bool { return !t.running }
func (t *TaskState) moveButton(d int) {
	t.btnIdx += d
	if t.btnIdx < 0 {
		t.btnIdx = 0
	}
	if t.btnIdx >= len(t.buttons) {
		t.btnIdx = len(t.buttons) - 1
	}
}

func (t *TaskState) update(msg tea.Msg) tea.Cmd { return nil }

// activateFocused runs the action of the focused result button.
func (t *TaskState) activateFocused(m *Model) tea.Cmd {
	if !t.isDismissable() || t.btnIdx < 0 || t.btnIdx >= len(t.buttons) {
		return nil
	}
	switch t.buttons[t.btnIdx] {
	case "Install":
		m.pendingInstall = false
		return m.startInstall(t.result.Path)
	case "Close":
		m.modal = ModalNone
		m.task = nil
		return nil
	}
	return nil
}

func (t *TaskState) percent() float64 {
	if t.total <= 0 {
		return 0
	}
	p := float64(t.received) / float64(t.total)
	if p > 1 {
		return 1
	}
	return p
}

// view renders the modal body.
func (t *TaskState) view(m *Model) string {
	st := m.st
	var b strings.Builder
	b.WriteString(st.Bold.Render(t.Title) + "\n")
	if t.Artifact != "" {
		b.WriteString(st.Muted.Render(t.Artifact) + "\n")
	}
	b.WriteString("\n")
	switch t.phase {
	case "downloading":
		b.WriteString(m.spin.View() + " " + st.Text.Render("downloading from GitHub Actions…") + "\n")
		if t.total > 0 {
			t.bar.Width = 42
			b.WriteString(t.bar.ViewAs(t.percent()) + "\n")
			b.WriteString(st.Dim.Render(fmt.Sprintf("%s / %s", ghapi.HumanBytes(t.received), ghapi.HumanBytes(t.total))))
		} else {
			b.WriteString(st.Dim.Render(ghapi.HumanBytes(t.received) + " received"))
		}
	case "extracting":
		b.WriteString(m.spin.View() + " " + st.Text.Render("picking the best APK out of the zip…") + "\n")
	case "installing":
		b.WriteString(m.spin.View() + " " + st.Text.Render("running Android installer…") + "\n")
	case "launched":
		via := t.installRes.Via
		if via == "" {
			via = "Android installer"
		}
		b.WriteString(st.Warn.Render("↗ installer launched (unconfirmed)") + " " +
			st.Dim.Render("via "+via) + "\n")
		b.WriteString(st.Muted.Render("Accept the prompt on your Android device — installation is not confirmed yet.") + "\n")
	case "done":
		label := "done"
		if t.Kind == "install" && t.installRes.Confirmed() {
			label = "installed"
		}
		b.WriteString(st.OK.Render(st.G.OK+" "+label) + " " +
			st.Dim.Render("in "+time.Since(t.started).Round(time.Second).String()) + "\n")
		if t.result.Path != "" {
			b.WriteString(st.Accent.Render(filepath.Base(t.result.Path)) +
				st.Muted.Render("  "+ghapi.HumanBytes(t.result.Size)) + "\n")
			b.WriteString(st.Dim.Render(t.result.Path) + "\n")
			renderAPKMetaLines(&b, st, t.result)
		}
	case "failed":
		b.WriteString(st.Err.Render(st.G.Fail+" failed") + "\n")
		if t.err != nil {
			b.WriteString(st.Warn.Render(t.err.Error()) + "\n")
		}
		if t.installRes.Diagnosis != "" && (t.err == nil || !strings.Contains(t.err.Error(), t.installRes.Diagnosis)) {
			b.WriteString(st.Muted.Render(t.installRes.Diagnosis) + "\n")
		}
	}
	if len(t.logs) > 0 {
		b.WriteString("\n")
		for _, l := range t.logs {
			b.WriteString(st.Dim.Render("  "+l) + "\n")
		}
	}
	if t.isDismissable() && len(t.buttons) > 0 {
		b.WriteString("\n")
		var btns []string
		for i, bt := range t.buttons {
			if i == t.btnIdx {
				btns = append(btns, st.ButtonFocus.Render(bt))
			} else {
				btns = append(btns, st.ButtonBlur.Render(bt))
			}
		}
		b.WriteString(strings.Join(btns, "  "))
	}
	return b.String()
}

// ─────────────────── start actions on the model ───────────────────

// startDownload launches download of the selected workflow artifact or release asset.
func (m *Model) startDownload(withInstall bool) tea.Cmd {
	cacheDir := config.CacheDir()
	outDir := filepath.Join(cacheDir, "apk")

	if m.usingReleaseAssets() {
		ra, ok := m.selectedReleaseAsset()
		if !ok {
			m.toastf("no release asset selected")
			return nil
		}
		m.pendingInstall = withInstall
		m.progCh = make(chan tea.Msg, 64)
		device_CanInstall = m.dev.CanInstall
		m.task = newTask("Downloading release asset", ra.Asset.Name, progressColor{m.st.P.Accent, m.st.P.AccentAlt})
		m.busy = true
		m.busyMsg = "downloading"
		m.modal = ModalTask
		return tea.Batch(
			downloadReleaseApkCmd(m.settings.Repo, ra.Asset, outDir, m.settings.FilePattern, m.settings.ExcludePattern,
				m.settings.KeepCache, m.settings.KeepCount, m.settings.VerifyDigest, m.dev, archPrefs{m.settings.Arch, m.settings.Variant}, m.progCh),
			waitProgress(m.progCh),
		)
	}

	art, ok := m.selectedArtifact()
	if !ok {
		m.toastf("no artifact selected")
		return nil
	}
	run, _ := m.selectedRun()
	zipPath := filepath.Join(outDir, fmt.Sprintf("%d-%s.zip", art.ID, sanitize(art.Name)))
	m.pendingInstall = withInstall
	m.progCh = make(chan tea.Msg, 64)
	device_CanInstall = m.dev.CanInstall
	m.task = newTask("Downloading artifact", art.Name, progressColor{m.st.P.Accent, m.st.P.AccentAlt})
	_ = run
	m.busy = true
	m.busyMsg = "downloading"
	m.modal = ModalTask
	return tea.Batch(
		downloadApkCmd(m.settings.Repo, art, zipPath, outDir, m.settings.FilePattern, m.settings.ExcludePattern,
			m.settings.KeepCache, m.settings.KeepCount, m.settings.VerifyDigest, m.dev, archPrefs{m.settings.Arch, m.settings.Variant}, m.progCh),
		waitProgress(m.progCh),
	)
}

// startInstall validates APK identity, signing continuity, and version safety
// before streaming the installer handoff.
func (m *Model) startInstall(path string) tea.Cmd {
	if path == "" {
		m.toastf("nothing downloaded yet")
		return nil
	}
	if m.settings.NoOpen {
		m.toastf("open installer is off in settings — APK saved")
		return nil
	}
	meta := m.downloadedAPK.Meta
	if m.downloadedAPK.Path != path || (!meta.HaveInfo && meta.InfoErr == "") {
		if ins, err := apk.Inspect(path); err == nil {
			meta = ins
		}
	}

	hist, hasHist := config.History(m.settings.Repo)
	expectPkg := m.settings.PackageName
	if expectPkg == "" && hasHist {
		expectPkg = hist.Pkg
	}
	queryPkg := expectPkg
	if queryPkg == "" && meta.HaveInfo {
		queryPkg = meta.Info.Package
	}

	instApp := m.instApp
	instFound := m.instFound
	if queryPkg != "" && (m.instPkg != queryPkg || !m.instFound) {
		if app, found := device.QueryPackage(queryPkg); found || app.Queryable {
			instApp = app
			instFound = found
			if m.instPkg == "" || m.instPkg == queryPkg {
				m.instPkg = queryPkg
				m.instApp = app
				m.instFound = found
			}
		}
	}

	safety := apk.EvaluateSafety(meta, apk.SafetyInput{
		ExpectedPackage: expectPkg,
		InstalledFound:  instFound,
		InstalledVCode:  instApp.VersionCode,
		InstalledVName:  instApp.VersionName,
		InstalledCert:   instApp.CertSHA256,
		HistoryCert:     hist.Sig,
		DeviceSDK:       m.dev.SDK,
		DowngradeMode:   m.settings.Downgrade,
		CheckPackage:    m.settings.PkgCheck,
		CheckSignature:  m.settings.SigCheck,
	})

	if safety.BlockedReason != "" {
		t := newTask("Install blocked", filepath.Base(path), progressColor{m.st.P.Accent, m.st.P.AccentAlt})
		t.Kind = "install"
		t.fail(fmt.Errorf("%s", safety.BlockedReason))
		m.task = t
		m.modal = ModalTask
		m.toastf("install blocked: downgrade policy")
		return nil
	}

	if safety.NeedsConfirm {
		var body []string
		for _, w := range safety.Warnings {
			body = append(body, m.st.Warn.Render("! "+w.Title))
			body = append(body, w.Message)
		}
		m.safetyInstallPath = path
		m.safetyAllowDowngrade = safety.IsDowngrade || m.settings.Downgrade == "allow"
		m.confirmKind = confirmSafetyOverride
		m.confirm = components.Confirm{
			Title:   "Safety check before install",
			Body:    body,
			Options: []string{"Install anyway", "Cancel"},
			Cursor:  1, // safe default: Cancel
		}
		m.modal = ModalConfirm
		return nil
	}

	return m.executeInstall(path, safety.AllowDowngrade, safety.Notes)
}

func (m *Model) executeInstall(path string, allowDowngrade bool, notes []string) tea.Cmd {
	m.logCh = make(chan tea.Msg, 64)
	t := newTask("Installing", filepath.Base(path), progressColor{m.st.P.Accent, m.st.P.AccentAlt})
	t.Kind = "install"
	t.phase = "installing"
	if m.downloadedAPK.Path == path {
		t.result = m.downloadedAPK
	}
	for _, n := range notes {
		t.addLog(n)
	}
	device_CanInstall = m.dev.CanInstall
	m.task = t
	m.modal = ModalTask
	return tea.Batch(
		installApkCmd(path, allowDowngrade, m.logCh),
		waitInstallLog(m.logCh),
	)
}

func renderAPKMetaLines(b *strings.Builder, st theme.Styles, res ghapi.ExtractedAPK) {
	if res.Meta.HaveInfo {
		inf := res.Meta.Info
		ver := inf.VersionName
		if ver == "" {
			ver = "?"
		}
		b.WriteString(st.Muted.Render(fmt.Sprintf("pkg %s · v%s (%d)", inf.Package, ver, inf.VersionCode)) + "\n")
	}
	if res.Meta.Cert.SHA256 != "" {
		b.WriteString(st.Dim.Render(fmt.Sprintf("key %.12s… (%s)", res.Meta.Cert.SHA256, res.Meta.Cert.Scheme)) + "\n")
	}
	switch res.DigestStatus {
	case apk.DigestVerified:
		b.WriteString(st.OK.Render(st.G.OK+" sha256 verified against GitHub digest") + "\n")
	case apk.DigestUnverified:
		b.WriteString(st.Dim.Render("digest unverified (GitHub published none)") + "\n")
	}
}

func sanitize(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, s)
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}
