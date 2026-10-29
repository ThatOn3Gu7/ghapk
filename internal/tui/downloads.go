package tui

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/apk"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
)

type jobSpec struct {
	repo      string
	settings  config.Settings
	dev       device.Info
	run       ghapi.Run
	choice    planChoice
	install   bool
	savedPath string
}
type downloadJob struct {
	id            int
	spec          jobSpec
	ctx           context.Context
	cancel        context.CancelFunc
	events        chan tea.Msg
	status, phase string
	logs          []string
	got, total    int64
	started       time.Time
	progressAt    time.Time
	rate          float64
	path          string
	meta          apk.Inspection
	question      *jobQuestionMsg
	handedOff     bool
}
type jobEventMsg struct {
	id         int
	phase, log string
	got, total int64
	path       string
	meta       apk.Inspection
	status     string
	handoff    bool
}
type jobQuestionMsg struct {
	id      int
	title   string
	body    []string
	options []string
	danger  int // index of destructive option; -1 when none
	reply   chan int
}
type jobFinishedMsg struct {
	id               int
	err              error
	status, path     string
	meta             apk.Inspection
	handoff          bool
	uninstallStarted bool
}
type savedAPK struct {
	path string
	size int64
	repo string
}

func (m *Model) jobByID(id int) *downloadJob {
	for _, j := range m.jobs {
		if j.id == id {
			return j
		}
	}
	return nil
}
func waitJob(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}
func (m *Model) startJob(spec jobSpec) tea.Cmd {
	m.nextJobID++
	ctx, cancel := context.WithCancel(context.Background())
	j := &downloadJob{id: m.nextJobID, spec: spec, ctx: ctx, cancel: cancel, events: make(chan tea.Msg, 64), status: "running", phase: "Queued", started: time.Now()}
	m.jobs = append(m.jobs, j)
	m.focus = PanelDownloads
	m.downloadFocus = 0
	m.downloadDetail = m.width < 80
	m.downloadScroll = 0
	m.rebuildDownloadLists()
	m.jobList.SelectByID(j.id)
	return tea.Batch(func() tea.Msg { runJob(j.id, ctx, spec, j.events); return nil }, waitJob(j.events))
}
func emitJob(ctx context.Context, ch chan<- tea.Msg, msg tea.Msg) bool {
	select {
	case ch <- msg:
		return true
	case <-ctx.Done():
		return false
	}
}
func runJob(id int, ctx context.Context, s jobSpec, ch chan tea.Msg) {
	var result jobFinishedMsg
	result.id = id
	result.status = "done"
	defer func() {
		if ctx.Err() != nil {
			result.err = ctx.Err()
			result.status = "cancelled"
		}
		ch <- result
		close(ch)
	}()
	emit := func(phase, log string) { emitJob(ctx, ch, jobEventMsg{id: id, phase: phase, log: log}) }
	fail := func(err error) { result.err = err; result.status = "failed" }
	ask := func(title, body string, options []string, danger int) (int, bool) {
		reply := make(chan int, 1)
		if !emitJob(ctx, ch, jobQuestionMsg{id: id, title: title, body: []string{body}, options: options, danger: danger, reply: reply}) {
			return 0, false
		}
		select {
		case choice := <-reply:
			if choice < 0 || choice >= len(options) {
				return 0, true
			}
			return choice, true
		case <-ctx.Done():
			return 0, false
		}
	}
	path := s.savedPath
	var meta apk.Inspection
	if path == "" {
		emit("Download", "Starting "+s.choice.name())
		root := config.CacheDir()
		if err := os.MkdirAll(filepath.Join(root, "work"), 0700); err != nil {
			fail(err)
			return
		}
		work, err := os.MkdirTemp(filepath.Join(root, "work"), "job-")
		if err != nil {
			fail(err)
			return
		}
		defer os.RemoveAll(work)
		var download string
		digest := ""
		iszip := true
		prog := func(got, total int64) {
			select {
			case ch <- jobEventMsg{id: id, got: got, total: total}:
			default:
			}
		}
		if s.choice.release {
			download, err = ghapi.ResolveReleaseAssetDest(work, s.choice.asset.Name)
			digest = s.choice.asset.Digest
			iszip = strings.HasSuffix(strings.ToLower(s.choice.asset.Name), ".zip")
		} else {
			download = filepath.Join(work, "artifact.zip")
			digest = s.choice.art.Digest
		}
		if err != nil {
			fail(err)
			return
		}
		// Reuse a cache only after validating a private copy, so a concurrent
		// job replacing the shared cache cannot change what we extract.
		cacheKey := sha256.Sum256([]byte(fmt.Sprintf("%s/%t/%d/%s/%d", s.repo, s.choice.release, func() int64 {
			if s.choice.release {
				return s.choice.asset.ID
			}
			return s.choice.art.ID
		}(), digest, func() int64 {
			if s.choice.release {
				return s.choice.asset.Size
			}
			return s.choice.art.SizeInBytes
		}())))
		cached := filepath.Join(root, "archives", fmt.Sprintf("%x", cacheKey[:]), filepath.Base(download))
		checkArt := s.choice.art
		checkAsset := s.choice.asset
		if !s.settings.VerifyDigest {
			checkArt.Digest = ""
			checkAsset.Digest = ""
		}
		cacheOK := false
		if s.settings.KeepCache {
			valid := false
			if s.choice.release {
				valid = ghapi.CanReuseReleaseAssetContext(ctx, cached, checkAsset)
			} else {
				valid = ghapi.CanReuseArtifactZipContext(ctx, cached, checkArt)
			}
			if valid && ctx.Err() == nil {
				err = copyJobFile(ctx, cached, download)
				if err == nil {
					if s.choice.release {
						cacheOK = ghapi.CanReuseReleaseAssetContext(ctx, download, checkAsset)
					} else {
						cacheOK = ghapi.CanReuseArtifactZipContext(ctx, download, checkArt)
					}
				}
				if cacheOK {
					if fi, e := os.Stat(download); e == nil {
						prog(fi.Size(), fi.Size())
					}
					emit("Download", "Reused validated cache")
				}
			}
		}
		if ctx.Err() != nil {
			return
		}
		if !cacheOK {
			if s.choice.release {
				err = ghapi.DownloadReleaseAssetContext(ctx, s.repo, s.choice.asset, download, s.settings.VerifyDigest, prog)
			} else {
				err = ghapi.DownloadArtifactContext(ctx, s.repo, s.choice.art, download, s.settings.VerifyDigest, prog)
			}
			if err == nil && s.settings.KeepCache && ctx.Err() == nil {
				if e := cacheJobFile(ctx, download, cached); e != nil {
					emit("", "Could not cache archive: "+e.Error())
				}
			}
		}

		if err != nil {
			fail(err)
			return
		}
		if ctx.Err() != nil {
			return
		}
		if !s.settings.VerifyDigest {
			digest = ""
		}
		ds := ghapi.DigestStatusFor(digest)
		emit("Checksum", "GitHub digest: "+string(ds))
		if iszip {
			emit("Unpack", "Safely extracting best matching APK")
			var out ghapi.ExtractedAPK
			out, err = ghapi.ExtractBestAPKContextChoice(ctx, download, filepath.Join(work, "extracted"), s.settings.FilePattern, s.settings.ExcludePattern, prefsFromSettings(s.settings), scoreDeviceFrom(s.dev), func(candidates []ghapi.APKCandidate) (int, error) {
				for i := 0; i < len(candidates); {
					c := candidates[i]
					body := fmt.Sprintf("Several APKs rank equally. Candidate %d of %d: %s (%s). Select this APK or look at the next one.", i+1, len(candidates), c.Name, ghapi.HumanBytes(int64(c.Size)))
					choice, ok := ask("Several APKs", body, []string{"Cancel", "Next APK", "Use this APK"}, -1)
					if !ok {
						return -1, ctx.Err()
					}
					switch choice {
					case 0:
						return -1, errors.New("no APK selected")
					case 1:
						i = (i + 1) % len(candidates)
					case 2:
						return i, nil
					}
				}
				return -1, errors.New("no APK selected")
			})
			if err != nil {
				fail(err)
				return
			}
			path = out.Path
			meta = out.Meta
		} else {
			if !strings.HasSuffix(strings.ToLower(s.choice.asset.Name), ".apk") {
				fail(errors.New("release asset is not an APK or ZIP"))
				return
			}
			path = download
			meta, err = apk.InspectContext(ctx, path)
			if err != nil {
				fail(err)
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		meta.DigestStatus = ds
		emit("Validate APK", "APK inspected")
		if !meta.HaveInfo {
			emit("", "Manifest unavailable: "+meta.InfoErr)
		}
		if meta.Cert.SHA256 == "" {
			emit("", "Signing certificate unavailable")
		}
		emit("Save", "Saving APK")
		path, err = publishAPK(ctx, path, filepath.Join(root, "apk"))
		if err != nil {
			fail(err)
			return
		}
		if s.repo != "" {
			data, _ := json.Marshal(struct {
				Repo string `json:"repo"`
			}{s.repo})
			f, e := os.OpenFile(path+".origin", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e == nil {
				_, e = f.Write(data)
				if closeErr := f.Close(); e == nil {
					e = closeErr
				}
			}
			if e != nil {
				emit("", "Could not save source information: "+e.Error())
			}
		}
		result.path = path
		result.meta = meta
		emitJob(ctx, ch, jobEventMsg{id: id, path: path, meta: meta, log: "Saved: " + path})
	} else {
		s.dev = device.ProbeContext(ctx)
		if ctx.Err() != nil {
			return
		}
		emit("Validate APK", "Inspecting saved APK")
		var err error
		fi, e := os.Lstat(path)
		if e != nil || !fi.Mode().IsRegular() {
			fail(errors.New("saved APK is not a regular file"))
			return
		}
		meta, err = apk.InspectContext(ctx, path)
		if err != nil {
			fail(err)
			return
		}
		meta.DigestStatus = apk.DigestUnverified
		if !meta.HaveInfo {
			emit("", "Manifest unavailable: "+meta.InfoErr)
		}
		if meta.Cert.SHA256 == "" {
			emit("", "Signing certificate unavailable")
		}
		result.path = path
		result.meta = meta
	}
	if ctx.Err() != nil || !s.install {
		return
	}
	emit("Validate APK", "Checking package on device")
	hist, _ := config.History(s.repo)
	expected := s.settings.PackageName
	if expected == "" {
		expected = hist.Pkg
	}
	pkg := expected
	if meta.HaveInfo && meta.Info.Package != "" {
		pkg = meta.Info.Package
	}
	var installed device.InstalledApp
	var found bool
	if pkg != "" {
		installed, found = device.QueryPackageContext(ctx, pkg)
	}
	if ctx.Err() != nil {
		return
	}
	safety := apk.EvaluateSafety(meta, apk.SafetyInput{ExpectedPackage: expected, InstalledFound: found, InstalledQueryable: installed.Queryable, InstalledVCode: installed.VersionCode, InstalledVName: installed.VersionName, InstalledCert: installed.CertSHA256, HistoryCert: hist.Sig, HistoryPackage: hist.Pkg, DeviceSDK: s.dev.SDK, DowngradeMode: s.settings.Downgrade, CheckPackage: s.settings.PkgCheck, CheckSignature: s.settings.SigCheck})
	for _, n := range safety.Notes {
		emit("", n)
	}
	if safety.BlockedReason != "" {
		fail(errors.New(safety.BlockedReason))
		return
	}
	allow := safety.AllowDowngrade

	uninstallFirst := false
	// Ask separately, as the reference does, so the user can read every risk.
	// The APK is already saved before any of these choices.
	for _, w := range safety.Warnings {
		if w.Blocked {
			continue
		}
		options := []string{"Cancel", "Install anyway"}
		danger := -1
		if w.Kind == apk.WarnSameVersion {
			options = []string{"Keep APK, skip install", "Reinstall"}
			if uninstallFirst {
				continue
			}
		} else if w.Kind == apk.WarnSignatureMismatch && found && installed.CertSHA256 != "" &&
			meta.Cert.SHA256 != "" && !strings.EqualFold(installed.CertSHA256, meta.Cert.SHA256) && s.dev.OS == "android" && s.dev.HasRish {
			options = append(options, "Uninstall first")
			danger = 2
		}
		choice, ok := ask(w.Title, w.Message, options, danger)
		if !ok {
			return
		}
		if choice == 0 {
			emit("Skipped", "APK kept; installation skipped")
			result.status = "done"
			return
		}
		if choice == 2 && danger == 2 {
			uninstallFirst = true
		}
		if w.Kind == apk.WarnDowngrade {
			allow = true
		}
	}
	if uninstallFirst {
		body := fmt.Sprintf("Uninstall %s from this device? This permanently erases the app and its data. Installation may still fail. This cannot be undone.", pkg)
		choice, ok := ask("Erase app data?", body, []string{"Cancel", "Uninstall and install"}, 1)
		if !ok || choice == 0 {
			emit("Skipped", "APK kept; app not uninstalled")
			return
		}
		emit("Uninstall", "Checking the installed app again before uninstalling "+pkg)
		result.uninstallStarted = true
		emit("", "Uninstall requested; app data may be erased even if cancelled")
		if err := device.UninstallMismatchedRishContext(ctx, pkg, installed.CertSHA256, meta.Cert.SHA256); err != nil {
			fail(fmt.Errorf("uninstall not confirmed (app or data may have changed): %w", err))
			return
		}
		emit("Uninstall", "App uninstalled; its data was erased")
		found = false
	}

	if ctx.Err() != nil {
		return
	}
	emit("Installer", "Handing APK to Android installer; cancellation cannot undo handoff")
	result.handoff = true
	res, err := device.InstallAPKContext(ctx, path, allow, func(line string) { emit("", line) })
	if err != nil {
		fail(err)
		return
	}
	if res.Outcome == device.OutcomeLaunched {
		result.status = "launched"
		emit("Confirm on device", "Installer launched; installation not yet confirmed")
		if s.settings.VerifyInstall && meta.HaveInfo && installed.Queryable && !(found && (meta.Info.VersionCode <= 0 || installed.VersionCode <= 0 || installed.VersionCode == meta.Info.VersionCode)) {
			v := device.VerifyInstall(ctx, meta.Info.Package, meta.Info.VersionCode, 30*time.Second)
			emit("", v.Detail)
			if v.Outcome == device.VerifyConfirmed {
				result.status = "installed"
			}
		} else if s.settings.VerifyInstall {
			emit("", "Installation unconfirmed: a pre-existing or unknown version cannot prove a reinstall")
		}
	} else if res.Outcome == device.OutcomeInstalled {
		result.status = "installed"
	} else {
		fail(errors.New("installation failed"))
		return
	}
	if ctx.Err() != nil {
		return
	}
	if result.status == "installed" && (meta.HaveInfo || meta.Cert.SHA256 != "") {
		entry := config.HistoryEntry{SHA: meta.LocalSHA256, Sig: meta.Cert.SHA256, Run: s.run.ID, Artifact: s.choice.name()}
		if meta.HaveInfo {
			entry.Pkg = meta.Info.Package
			entry.VCode = meta.Info.VersionCode
			entry.VName = meta.Info.VersionName
		}
		_ = config.RecordInstall(s.repo, entry)
	}
}

type jobReader struct {
	ctx context.Context
	r   io.Reader
}

func (r jobReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}
func copyJobFile(ctx context.Context, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if fi, e := in.Stat(); e != nil || !fi.Mode().IsRegular() || fi.Size() > 4<<30 {
		return errors.New("invalid cached download")
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, jobReader{ctx, in})
	if e := out.Close(); err == nil {
		err = e
	}
	if err != nil {
		_ = os.Remove(dst)
	}
	return err
}
func cacheJobFile(ctx context.Context, src, dst string) error {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".cache-*.part")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Close(); err != nil {
		return err
	}
	if err = copyJobFile(ctx, src, tmp); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
func publishAPK(ctx context.Context, src, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.CreateTemp(dir, ".ghapk-save-*.part")
	if err != nil {
		return "", err
	}
	tmp := out.Name()
	defer os.Remove(tmp)
	_, err = io.Copy(out, jobReader{ctx, in})
	if e := out.Close(); err == nil {
		err = e
	}
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	tag := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(tmp), ".ghapk-save-"), ".part")
	base := sanitize(strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)))
	if base == "" {
		base = "app"
	}
	target := filepath.Join(dir, base+"-"+tag+".apk")
	if err = os.Rename(tmp, target); err != nil {
		return "", err
	}
	return target, nil
}

func (m *Model) refreshSaved() {
	dir := filepath.Join(config.CacheDir(), "apk")
	entries, _ := os.ReadDir(dir)
	files := make([]savedAPK, 0)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".apk") {
			continue
		}
		info, err := e.Info()
		if err == nil && info.Mode().IsRegular() {
			path := filepath.Join(dir, e.Name())
			var origin struct {
				Repo string `json:"repo"`
			}
			originPath := path + ".origin"
			if fi, err := os.Lstat(originPath); err == nil && fi.Mode().IsRegular() && fi.Size() <= 4096 {
				if b, e := os.ReadFile(originPath); e == nil {
					_ = json.Unmarshal(b, &origin)
				}
			}
			if !validRepoSlug(origin.Repo) {
				origin.Repo = ""
			}
			files = append(files, savedAPK{path: path, size: info.Size(), repo: origin.Repo})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	m.savedFiles = files
	m.rebuildDownloadLists()
}
func (m *Model) rebuildDownloadLists() {
	items := make([]components.Item, 0, len(m.jobs))
	for _, j := range m.jobs {
		name := j.spec.choice.name()
		if j.spec.savedPath != "" {
			name = filepath.Base(j.spec.savedPath)
		}
		items = append(items, components.Item{ID: j.id, Label: fmt.Sprintf("#%d %s", j.id, safeJobText(name)), Detail: j.status})
	}
	m.jobList.Empty = "  no jobs yet"
	m.jobList.SetItems(items)
	items = nil
	for _, f := range m.savedFiles {
		items = append(items, components.Item{ID: f.path, Label: safeJobText(filepath.Base(f.path)), Detail: humanSize(f.size)})
	}
	m.savedList.Empty = "  no saved APKs"
	m.savedList.SetItems(items)
	m.downloadListBounds()
}
func (m *Model) downloadListBounds() {
	r := m.downloadRects()
	m.jobList.MaxShown = max(1, r.jobs.h-2)
	m.savedList.MaxShown = max(1, r.saved.h-2)
	m.jobList.MoveBy(0)
	m.savedList.MoveBy(0)
}

type downloadLayout struct{ jobs, saved, detail rect }

func (m Model) downloadRects() downloadLayout {
	h := max(6, m.height-3)
	if m.width < 80 && m.downloadDetail {
		return downloadLayout{detail: rect{0, 2, m.width, h}}
	}
	w := m.width
	if m.width >= 80 {
		w = 38
	}
	jh := h * 3 / 5
	jh = max(3, min(h-3, jh))
	l := downloadLayout{jobs: rect{0, 2, w, jh}, saved: rect{0, 2 + jh, w, h - jh}}
	if m.width >= 80 {
		l.detail = rect{w, 2, m.width - w, h}
	}
	return l
}
func (m Model) selectedJob() *downloadJob {
	it, ok := m.jobList.Selected()
	if !ok {
		return nil
	}
	id, _ := it.ID.(int)
	return m.jobByID(id)
}
func (m *Model) handleJobEvent(e jobEventMsg) tea.Cmd {
	j := m.jobByID(e.id)
	if j == nil {
		return nil
	}
	if e.phase != "" {
		j.phase = e.phase
	}
	if e.log != "" {
		j.logs = append(j.logs, e.log)
		if len(j.logs) > 100 {
			j.logs = j.logs[len(j.logs)-100:]
		}
	}
	if e.got > 0 {
		now := time.Now()
		delta := e.got - j.got
		if !j.progressAt.IsZero() && delta > 0 {
			if seconds := now.Sub(j.progressAt).Seconds(); seconds > 0 {
				sample := float64(delta) / seconds
				if j.rate == 0 {
					j.rate = sample
				} else {
					j.rate = 0.25*sample + 0.75*j.rate
				}
			}
		}
		j.progressAt = now
		j.got = e.got
		j.total = e.total
	}
	if e.path != "" {
		j.path = e.path
		j.meta = e.meta
		m.refreshSaved()
	}
	if e.handoff {
		j.handedOff = true
	}
	return waitJob(j.events)
}
func (m *Model) handleJobQuestion(q jobQuestionMsg) tea.Cmd {
	j := m.jobByID(q.id)
	if j == nil {
		return nil
	}
	if j.ctx.Err() != nil {
		return waitJob(j.events)
	}
	j.question = &q
	j.status = "asking"
	m.rebuildDownloadLists()
	return waitJob(j.events)
}
func (m *Model) showNextJobQuestion() {
	if m.modal != ModalNone {
		return
	}
	for _, j := range m.jobs {
		if j.question == nil {
			continue
		}
		if j.ctx.Err() != nil {
			j.question = nil
			continue
		}
		q := j.question
		var body []string
		for _, line := range q.body {
			body = append(body, safeJobText(line))
		}
		m.confirmKind = confirmJobSafety
		m.jobQuestionID = j.id
		m.jobQuestionScroll = 0
		m.confirm = components.Confirm{Title: safeJobText(q.title), Body: body, Options: q.options, Cursor: 0, DangerIndex: q.danger}

		m.modal = ModalConfirm
		return
	}
}
func (m *Model) answerJobQuestion(choice int) {
	j := m.jobByID(m.jobQuestionID)
	m.jobQuestionID = 0
	if j != nil && j.question != nil {
		j.question.reply <- choice
		j.question = nil
		if j.ctx.Err() != nil {
			j.status = "cancelling"
		} else {
			j.status = "running"
		}
		m.rebuildDownloadLists()
	}
}
func (m *Model) handleJobFinished(e jobFinishedMsg) {
	j := m.jobByID(e.id)
	if j == nil {
		return
	}
	j.status = e.status
	j.path = e.path
	j.meta = e.meta
	j.handedOff = e.handoff
	j.question = nil
	j.cancel()
	if e.path != "" {
		m.downloadedAPK = ghapi.ExtractedAPK{Path: e.path, Name: filepath.Base(e.path), Size: e.meta.Size, Meta: e.meta, DigestStatus: e.meta.DigestStatus}
		m.haveDownload = true
	}
	if e.err != nil {
		j.logs = append(j.logs, e.err.Error())
	}
	if e.status == "cancelled" && e.uninstallStarted {
		j.logs = append(j.logs, "Uninstall may have completed and erased app data; check the device.")
	}
	if e.status == "cancelled" && e.handoff {
		j.logs = append(j.logs, "Installer may still be open; cancellation cannot undo a handoff.")
	}
	if m.jobQuestionID == j.id && m.confirmKind == confirmJobSafety {
		m.resetConfirm()
	}
	// Never prune while another job might be using a published file.
	active := false
	protected := []string{}
	for _, other := range m.jobs {
		if other.status == "running" || other.status == "asking" || other.status == "cancelling" {
			active = true
		}
		if other.path != "" {
			protected = append(protected, other.path)
		}
		if other.spec.savedPath != "" {
			protected = append(protected, other.spec.savedPath)
		}
	}
	if !active && !j.spec.settings.KeepCache {
		ghapi.PruneAPKCache(filepath.Join(config.CacheDir(), "apk"), j.spec.settings.KeepCount, protected...)
	}
	m.refreshSaved()
}
func (m *Model) downloadAction(key string) tea.Cmd {
	switch key {
	case "s":
		m.downloadFocus = 1
		m.downloadDetail = false
		m.refreshSaved()
	case "esc":
		if m.downloadDetail {
			m.downloadDetail = false
			m.downloadScroll = 0
		} else {
			m.downloadFocus = 0
		}
	case "enter":
		if m.downloadFocus == 1 {
			return m.installSaved()
		}
		m.downloadDetail = true
		m.downloadScroll = 0
	case "i":
		if m.downloadFocus == 1 {
			return m.installSaved()
		}
	case "c":
		if j := m.selectedJob(); j != nil && (j.status == "running" || j.status == "asking") {
			j.cancel()
			j.status = "cancelling"
			if m.jobQuestionID == j.id && m.confirmKind == confirmJobSafety {
				m.resetConfirm()
			}
			m.rebuildDownloadLists()
		}
	case "x":
		if m.downloadFocus == 1 {
			if item, ok := m.savedList.Selected(); ok {
				m.confirmTarget = item.ID.(string)
				m.confirmKind = confirmDeleteSaved
				m.confirm = components.Confirm{Title: "Delete saved APK?", Danger: true, Body: []string{safeJobText(filepath.Base(m.confirmTarget))}, Options: []string{"Delete", "Cancel"}, Cursor: 1}
				m.modal = ModalConfirm
			}
		} else if j := m.selectedJob(); j != nil && j.status != "running" && j.status != "asking" && j.status != "cancelling" {
			for i, x := range m.jobs {
				if x.id == j.id {
					m.jobs = append(m.jobs[:i], m.jobs[i+1:]...)
					break
				}
			}
			m.rebuildDownloadLists()
		}
	case "X":
		jobs := m.jobs[:0]
		for _, j := range m.jobs {
			if j.status == "running" || j.status == "asking" || j.status == "cancelling" {
				jobs = append(jobs, j)
			}
		}
		m.jobs = jobs
		m.rebuildDownloadLists()
	}
	m.downloadListBounds()
	return nil
}
func (m *Model) installSaved() tea.Cmd {
	item, ok := m.savedList.Selected()
	if !ok {
		return nil
	}
	path, _ := item.ID.(string)
	if path == "" {
		return nil
	}
	repo := m.settings.Repo
	settings := m.settings
	for _, f := range m.savedFiles {
		if f.path == path && f.repo != "" {
			repo = f.repo
			if repo != m.settings.Repo {
				settings = config.LoadForRepo(repo)
			}
			break
		}
	}
	return m.startJob(jobSpec{repo: repo, settings: settings, dev: m.dev, install: true, savedPath: path})
}

func (m Model) handleDownloadMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	r := m.downloadRects()
	if m.downloadTouchActive {
		if msg.Action == tea.MouseActionRelease || msg.Type == tea.MouseRelease {
			m.downloadTouchActive = false
			if !m.downloadTouchMoved {
				if m.downloadTouchArea == 0 && r.jobs.contains(msg.X, msg.Y) {
					idx := m.jobList.Offset + msg.Y - r.jobs.y - 1
					if idx == m.downloadTouchIndex && idx >= 0 && idx < len(m.jobList.Items) {
						if m.width < 80 && m.downloadTouchWasFocused && idx == m.jobList.Cursor {
							m.downloadDetail = true
						}
						m.jobList.Cursor = idx
						m.downloadFocus = 0
						m.downloadScroll = 0
					}
				}
				if m.downloadTouchArea == 1 && r.saved.contains(msg.X, msg.Y) {
					idx := m.savedList.Offset + msg.Y - r.saved.y - 1
					if idx == m.downloadTouchIndex && idx >= 0 && idx < len(m.savedList.Items) {
						same := m.downloadTouchWasFocused && idx == m.savedList.Cursor
						m.savedList.Cursor = idx
						m.downloadFocus = 1
						if same {
							return m, m.installSaved()
						}
					}
				}
			}
			return m, nil
		}
		if msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft {
			d := m.downloadTouchLastY - msg.Y
			if d != 0 {
				m.downloadTouchMoved = true
				switch m.downloadTouchArea {
				case 0:
					m.downloadFocus = 0
					m.jobList.MoveBy(d)
				case 1:
					m.downloadFocus = 1
					m.savedList.MoveBy(d)
				case 2:
					m.downloadScroll = max(0, m.downloadScroll+d)
				}
				m.downloadTouchLastY = msg.Y
			}
			return m, nil
		}
	}
	if msg.Type == tea.MouseWheelUp || msg.Type == tea.MouseWheelDown {
		d := 3
		if msg.Type == tea.MouseWheelUp {
			d = -3
		}
		if r.detail.contains(msg.X, msg.Y) {
			m.downloadScroll = max(0, m.downloadScroll+d)
		} else if r.saved.contains(msg.X, msg.Y) {
			m.downloadFocus = 1
			m.savedList.MoveBy(d)
		} else if r.jobs.contains(msg.X, msg.Y) {
			m.downloadFocus = 0
			m.jobList.MoveBy(d)
		}
		return m, nil
	}
	if msg.Type != tea.MouseLeft || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	area := -1
	idx := -1
	was := false
	if r.jobs.contains(msg.X, msg.Y) && msg.Y > r.jobs.y && msg.Y < r.jobs.y+r.jobs.h-1 {
		area = 0
		idx = m.jobList.Offset + msg.Y - r.jobs.y - 1
		was = m.downloadFocus == 0
	} else if r.saved.contains(msg.X, msg.Y) && msg.Y > r.saved.y && msg.Y < r.saved.y+r.saved.h-1 {
		area = 1
		idx = m.savedList.Offset + msg.Y - r.saved.y - 1
		was = m.downloadFocus == 1
	} else if r.detail.contains(msg.X, msg.Y) {
		area = 2
	}
	if area >= 0 {
		m.downloadTouchActive = true
		m.downloadTouchMoved = false
		m.downloadTouchLastY = msg.Y
		m.downloadTouchArea = area
		m.downloadTouchIndex = idx
		m.downloadTouchWasFocused = was
	}
	return m, nil
}

func (m *Model) hasActiveJobs() bool {
	for _, j := range m.jobs {
		if j.status == "running" || j.status == "asking" || j.status == "cancelling" {
			return true
		}
	}
	return false
}
func (m *Model) cancelJobsAndQuit() tea.Cmd {
	if m.plan != nil && m.plan.cancel != nil {
		m.plan.cancel()
		m.plan = nil
	}
	for _, j := range m.jobs {
		if j.status == "running" || j.status == "asking" || j.status == "cancelling" {
			j.cancel()
			j.status = "cancelling"
		}
	}
	if m.confirmKind == confirmJobSafety {
		m.resetConfirm()
	} else {
		m.modal = ModalNone
	}
	if !m.hasActiveJobs() {
		m.quitting = true
		return tea.Quit
	}
	m.quitAfterJobs = true
	m.focus = PanelDownloads
	m.downloadDetail = false
	m.toastf("cancelling jobs before exit…")
	m.rebuildDownloadLists()
	return nil
}

// Untrusted artifact names, manifests and command output must not inject
// terminal control sequences or bidirectional filename overrides.
func safeJobText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return '?'
		}
		return r
	}, s)
}
