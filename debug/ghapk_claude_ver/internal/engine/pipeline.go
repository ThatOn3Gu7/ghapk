package engine

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"ghapk/internal/apk"
	"ghapk/internal/config"
	"ghapk/internal/device"
	"ghapk/internal/exit"
	"ghapk/internal/ghub"
)

var reUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func safeName(s string) string {
	s = reUnsafe.ReplaceAllString(s, "_")
	if s == "" {
		return "unnamed"
	}
	return s
}

// FmtBytes renders a size like 12.3 MB.
func FmtBytes(n int64) string {
	const k = 1000.0
	f := float64(n)
	switch {
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case f < k*k:
		return fmt.Sprintf("%.1f kB", f/k)
	case f < k*k*k:
		return fmt.Sprintf("%.1f MB", f/(k*k))
	}
	return fmt.Sprintf("%.2f GB", f/(k*k*k))
}

// tracker turns byte counts into a smoothed rate and an ETA.
type tracker struct {
	j        *Job
	total    int64
	method   string
	start    time.Time
	lastAt   time.Time
	lastDone int64
	rate     float64
}

func (t *tracker) cb(done int64) {
	now := time.Now()
	if now.Sub(t.lastAt) < 150*time.Millisecond && done < t.total {
		return
	}
	if dt := now.Sub(t.lastAt).Seconds(); dt > 0 && !t.lastAt.IsZero() {
		inst := float64(done-t.lastDone) / dt
		if t.rate == 0 {
			t.rate = inst
		} else {
			t.rate = 0.7*t.rate + 0.3*inst
		}
	}
	t.lastAt, t.lastDone = now, done
	total := t.total
	if done > total {
		total = done
	}
	p := Progress{Active: true, Done: done, Total: total, Rate: t.rate, Method: t.method}
	if t.rate > 1 && total > done {
		p.ETA = time.Duration(float64(total-done) / t.rate * float64(time.Second))
	}
	t.j.setProg(p)
}

func (j *Job) run(ctx context.Context) {
	e := j.e
	cfg := e.Config()
	j.work = e.WorkDir(j.ID)
	if err := os.MkdirAll(j.work, 0o700); err != nil {
		j.fail(StDownload, exit.NoSpace, fmt.Errorf("cannot create scratch space: %w", err))
		return
	}
	defer os.RemoveAll(j.work)
	j.logf("start %s", j.Src.Label())

	// ---- download -----------------------------------------------------------
	var files []string
	var skipUnpack bool
	switch j.Src.Kind {
	case "release":
		dest := filepath.Join(j.work, safeName(j.Src.Name))
		if !j.download(ctx, dest) {
			return
		}
		j.finish(StVerify, StepSkip, "releases publish no digest")
		j.finish(StUnpack, StepSkip, "a release asset is the file itself")
		files = []string{dest}
		skipUnpack = true
	default:
		zp := filepath.Join(j.work, safeName(j.Src.Name)+".zip")
		viaDir, ok := j.downloadArtifact(ctx, zp)
		if !ok {
			return
		}
		if viaDir != "" { // gh run download already unpacked it
			j.finish(StVerify, StepSkip, "gh unpacked it: nothing left to check")
			j.finish(StUnpack, StepOK, "unpacked by gh")
			files = listFiles(viaDir)
			skipUnpack = true
		} else {
			if !j.verify(ctx, zp, cfg) {
				return
			}
		}
		if !skipUnpack {
			var ok bool
			if files, ok = j.unpack(ctx, zp); !ok {
				return
			}
		}
	}
	if ctx.Err() != nil {
		return
	}

	// ---- choose the APK -------------------------------------------------------
	apkPath, ok := j.chooseAPK(ctx, files, cfg)
	if !ok {
		return
	}

	// ---- validate -------------------------------------------------------------
	skipInstall, ok := j.validate(ctx, apkPath, cfg)
	if !ok {
		return
	}

	// ---- save -----------------------------------------------------------------
	final, ok := j.save(ctx, apkPath, cfg)
	if !ok {
		return
	}

	// ---- install --------------------------------------------------------------
	j.install(ctx, final, cfg, skipInstall)
	j.notifyDone(cfg)
}

// ---- download ----------------------------------------------------------------

func stallTimeout() time.Duration {
	if n, _ := strconv.Atoi(os.Getenv("STALL_TIMEOUT")); n > 0 {
		return time.Duration(n) * time.Second
	}
	return 180 * time.Second
}

// download fetches a release asset.
func (j *Job) download(ctx context.Context, dest string) bool {
	j.begin(StDownload, "gh api")
	tr := &tracker{j: j, total: j.Src.Size, method: "gh api", start: time.Now()}
	err := j.e.GH.DownloadAsset(ctx, j.e.Repo(), j.Src.Asset.ID, dest, stallTimeout(), tr.cb)
	return j.downloadDone(ctx, err, tr, dest)
}

func (j *Job) downloadDone(ctx context.Context, err error, tr *tracker, dest string) bool {
	if err == nil {
		st, serr := os.Stat(dest)
		if serr != nil {
			err = serr
		} else {
			el := time.Since(tr.start)
			avg := float64(st.Size()) / maxF(el.Seconds(), 0.001)
			j.setProg(Progress{Done: st.Size(), Total: st.Size(), Method: tr.method})
			j.finish(StDownload, StepOK, fmt.Sprintf("%s in %s · %s/s · %s", FmtBytes(st.Size()), el.Round(100*time.Millisecond), FmtBytes(int64(avg)), tr.method))
			return true
		}
	}
	if ctx.Err() != nil {
		return false
	}
	code := exit.Download
	var ge *ghub.Error
	if errors.As(err, &ge) {
		code = ge.Code()
	}
	if errors.Is(err, ghub.ErrStalled) {
		err = fmt.Errorf("download stalled: no bytes for %s (set STALL_TIMEOUT to wait longer)", stallTimeout())
	}
	j.fail(StDownload, code, err)
	return false
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// downloadArtifact tries gh api, then curl, then gh run download. It returns
// the directory when the last of those was needed (it unpacks by itself).
func (j *Job) downloadArtifact(ctx context.Context, zp string) (viaDir string, ok bool) {
	gh, repo, art := j.e.GH, j.e.Repo(), j.Src.Art
	j.begin(StDownload, "gh api")
	stall := stallTimeout()

	quick := func() error {
		zr, err := zip.OpenReader(zp)
		if err != nil {
			return fmt.Errorf("not a valid zip: %w", err)
		}
		return zr.Close()
	}
	tr := &tracker{j: j, total: j.Src.Size, method: "gh api", start: time.Now()}
	err := gh.DownloadArtifact(ctx, repo, art.ID, zp, stall, tr.cb)
	if err == nil {
		if err = quick(); err == nil {
			return "", j.downloadDone(ctx, nil, tr, zp)
		}
	}
	if ctx.Err() != nil {
		return "", false
	}
	if errors.Is(err, ghub.ErrStalled) {
		j.fail(StDownload, exit.Download, fmt.Errorf("download stalled: no bytes for %s (set STALL_TIMEOUT to wait longer)", stall))
		return "", false
	}
	if ghub.IsClass(err, ghub.ClassGone) || ghub.IsClass(err, ghub.ClassMissing) {
		j.expired(ctx)
		return "", false
	}
	j.logf("gh api download failed (%v); trying curl", err)
	_ = os.Remove(zp)

	j.detail(StDownload, "curl")
	tr = &tracker{j: j, total: j.Src.Size, method: "curl", start: time.Now()}
	if cerr := gh.DownloadViaCurl(ctx, repo, art.ID, zp, stall, tr.cb); cerr == nil {
		if cerr = quick(); cerr == nil {
			return "", j.downloadDone(ctx, nil, tr, zp)
		}
	} else if ctx.Err() != nil {
		return "", false
	} else {
		j.logf("curl failed: %v; trying gh run download", cerr)
	}
	_ = os.Remove(zp)

	j.detail(StDownload, "gh run download")
	dir := filepath.Join(j.work, fmt.Sprintf("dl-%d", art.ID))
	t0 := time.Now()
	if derr := gh.DownloadViaRunDownload(ctx, repo, j.Src.Run.ID, art.Name, dir); derr != nil {
		if ctx.Err() != nil {
			return "", false
		}
		if j.isExpired(ctx) {
			j.expired(ctx)
			return "", false
		}
		code := exit.Download
		var ge *ghub.Error
		if errors.As(derr, &ge) {
			code = ge.Code()
		}
		j.fail(StDownload, code, fmt.Errorf("could not download %s: %v", art.Name, derr))
		return "", false
	}
	j.finish(StDownload, StepOK, fmt.Sprintf("%s in %s · gh run download", FmtBytes(j.Src.Size), time.Since(t0).Round(100*time.Millisecond)))
	return dir, true
}

func (j *Job) isExpired(ctx context.Context) bool {
	a, err := j.e.GH.GetArtifact(ctx, j.e.Repo(), j.Src.Art.ID)
	if err != nil {
		return ghub.IsClass(err, ghub.ClassGone) || ghub.IsClass(err, ghub.ClassMissing)
	}
	return a.Expired
}

func (j *Job) expired(ctx context.Context) {
	j.fail(StDownload, exit.Gone, fmt.Errorf("%s has expired: GitHub no longer has it (artifacts live 90 days by default)", j.Src.Name))
}

// verify compares the zip with the digest GitHub published for it.
func (j *Job) verify(ctx context.Context, zp string, cfg *config.Config) bool {
	d := j.Src.Digest
	switch {
	case !cfg.Bool("verify-digest"):
		j.finish(StVerify, StepSkip, "turned off in settings")
		return true
	case d == "" || d == "null":
		j.finish(StVerify, StepSkip, "GitHub published no digest for this one")
		return true
	}
	j.begin(StVerify, "")
	got, err := apk.FileSHA256(zp)
	if err != nil {
		j.finish(StVerify, StepSkip, "could not read the download: "+err.Error())
		return true
	}
	want := strings.TrimPrefix(d, "sha256:")
	if strings.EqualFold(got, want) {
		j.mu.Lock()
		j.res.Digest = "match"
		j.mu.Unlock()
		j.finish(StVerify, StepOK, "sha256 matches what GitHub published")
		return true
	}
	_ = os.Remove(zp)
	j.mu.Lock()
	j.res.Digest = "mismatch"
	j.mu.Unlock()
	j.fail(StVerify, exit.Checksum, fmt.Errorf("the download does not match the checksum GitHub published (expected %.12s…, got %.12s…): try again; if it repeats the artifact is bad", want, got))
	return false
}

// unpack extracts the zip and lists the files inside that match the file globs.
func (j *Job) unpack(ctx context.Context, zp string) ([]string, bool) {
	j.begin(StUnpack, "")
	dir := filepath.Join(j.work, "x")
	n, err := extractZip(zp, dir)
	if err != nil {
		j.fail(StUnpack, exit.Archive, fmt.Errorf("could not unpack the artifact: %w", err))
		return nil, false
	}
	unpackNested(dir, func(s string) { j.logf("%s", s) })
	all := listFiles(dir)
	j.finish(StUnpack, StepOK, fmt.Sprintf("%d file(s), %s", len(all), FmtBytes(n)))
	return all, true
}

// chooseAPK picks the file to install out of everything the artifact held.
func (j *Job) chooseAPK(ctx context.Context, files []string, cfg *config.Config) (string, bool) {
	inc, exc := cfg.Patterns("file"), cfg.Patterns("exclude")
	var apks []string
	for _, f := range files {
		b := filepath.Base(f)
		if matchAny(b, inc) && !matchAny(b, exc) {
			apks = append(apks, f)
		}
	}
	if len(apks) == 0 {
		var names []string
		for _, f := range files {
			names = append(names, filepath.Base(f))
		}
		sort.Strings(names)
		if len(names) > 8 {
			names = append(names[:8], "…")
		}
		j.fail(StUnpack, exit.NoAPK, fmt.Errorf("no file matching %s inside the artifact (it held: %s)", cfg.Str("file"), strings.Join(names, ", ")))
		return "", false
	}
	pol := j.e.Policy()
	best, bestScore, unique := 0, -1<<30, true
	for i, f := range apks {
		sc := pol.APKRank(filepath.Base(f))
		if sc > bestScore {
			best, bestScore, unique = i, sc, true
		} else if sc == bestScore {
			unique = false
		}
	}
	if len(apks) == 1 || unique || j.Opts.Yes {
		return apks[best], true
	}
	opts := make([]string, len(apks))
	for i, f := range apks {
		opts[i] = filepath.Base(f)
	}
	a := j.ask(ctx, Question{Kind: "pickapk", Title: "Several APKs in this artifact",
		Body: "They rank equally, so ghapk will not guess. Which one?", Options: opts, Default: best})
	if a < 0 || a >= len(apks) {
		if ctx.Err() == nil {
			j.fail(StValidate, exit.NoAPK, errors.New("nothing picked: refusing to guess between several APKs"))
		}
		return "", false
	}
	return apks[a], true
}

// ---- validate ------------------------------------------------------------------

func (j *Job) validate(ctx context.Context, path string, cfg *config.Config) (skipInstall, ok bool) {
	e := j.e
	j.begin(StValidate, "")
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		j.fail(StValidate, exit.Archive, errors.New("the APK is empty or missing"))
		return false, false
	}
	size := st.Size()
	sha, _ := apk.FileSHA256(path)
	info, ierr := apk.ReadInfo(path)
	cert, _ := apk.ReadCert(path)
	j.mu.Lock()
	j.res.Size, j.res.SHA, j.res.Info, j.res.HaveInfo, j.res.Cert = size, sha, info, ierr == nil, cert
	j.mu.Unlock()

	var notes []string
	if ierr != nil {
		j.finish(StValidate, StepWarn, "could not read the manifest ("+ierr.Error()+"); installing unchecked")
		return false, true
	}
	label := fmt.Sprintf("%s · v%s (%d)", info.Package, orDash(info.VersionName), info.VersionCode)

	// Which app should this be? The setting, else history, else the APK itself.
	expect := e.Package()
	query := expect
	if query == "" {
		query = info.Package
	}
	inst := e.Dev.Query(ctx, query)
	e.mu.Lock()
	e.installed = inst
	e.mu.Unlock()

	if expect != "" && info.Package != expect && cfg.Bool("pkg-check") {
		a := j.ask(ctx, Question{Kind: "pkg", Danger: true, Title: "Different app",
			Body:    fmt.Sprintf("This APK is %s, but you are tracking %s. Installing it adds an unrelated app.", info.Package, expect),
			Options: []string{"Cancel", "Install anyway"}, Default: 0})
		if a != 1 {
			j.cancelled(ctx, StValidate, exit.NoAPK, "refused: the package name differs from the installed app")
			return false, false
		}
		notes = append(notes, "package differs")
	}

	// Downgrade: Android enforces versionCode, so that is the authority.
	if inst.Found && info.VersionCode > 0 && info.VersionCode < inst.VersionCode {
		msg := fmt.Sprintf("versionCode %d is older than the installed %d (%s)", info.VersionCode, inst.VersionCode, orDash(inst.VersionName))
		switch cfg.Str("downgrade") {
		case "allow":
			notes = append(notes, "downgrade allowed")
		case "skip":
			j.fail(StValidate, exit.Downgrade, fmt.Errorf("%s: set downgrade to allow to force it", msg))
			return false, false
		default:
			a := j.ask(ctx, Question{Kind: "downgrade", Title: "Older than what is installed", Body: msg + ". Android will refuse a downgrade unless the app is a debug build.", Options: []string{"Cancel", "Install anyway"}, Default: 0})
			if a != 1 {
				j.cancelled(ctx, StValidate, exit.Downgrade, "skipped: would downgrade "+msg)
				return false, false
			}
			notes = append(notes, "downgrade confirmed")
		}
	}

	if info.MinSDK > 0 && e.Dev.SDK > 0 && info.MinSDK > e.Dev.SDK {
		a := j.ask(ctx, Question{Kind: "minsdk", Danger: true, Title: "Needs a newer Android",
			Body:    fmt.Sprintf("This build needs Android API %d or newer; this device is on %d. It will not install.", info.MinSDK, e.Dev.SDK),
			Options: []string{"Cancel", "Try anyway"}, Default: 0})
		if a != 1 {
			j.cancelled(ctx, StValidate, exit.Install, "refused: the build needs a newer Android")
			return false, false
		}
	}

	// Signing key: Android refuses an update across keys, and the only fix is
	// uninstalling first, so say so before handing it over.
	if cfg.Bool("sig-check") && cert.SHA256 != "" {
		have, src := inst.CertSHA, "the app on this device"
		if have == "" {
			if h, ok := config.History(e.Repo()); ok && h.Sig != "" && (h.Pkg == "" || h.Pkg == info.Package) {
				have, src = h.Sig, "the last build you installed"
			}
		}
		switch {
		case have == "":
			notes = append(notes, "signing key not compared")
		case strings.EqualFold(have, cert.SHA256):
			notes = append(notes, "same signing key")
		default:
			opts := []string{"Cancel", "Install anyway (Android will refuse)"}
			canUninstall := src == "the app on this device" && e.Dev.HasRish() && device.ValidPackage(info.Package)
			if canUninstall {
				opts = append(opts, "Uninstall first, then install (erases app data)")
			}
			a := 0
			if cfg.Bool("uninstall-first") && canUninstall {
				a = 2
			} else {
				a = j.ask(ctx, Question{Kind: "sig", Danger: true, Title: "Signed with a different key",
					Body:    fmt.Sprintf("This APK's key is %.12s…, but %s has %.12s…. Android will not update an app across two signing keys.", cert.SHA256, src, have),
					Options: opts, Default: 0})
			}
			switch {
			case a == 2 && canUninstall:
				j.detail(StValidate, "uninstalling "+info.Package)
				if err := e.Dev.Uninstall(ctx, info.Package); err != nil {
					j.fail(StValidate, exit.SigMismatch, fmt.Errorf("could not uninstall %s: %v", info.Package, err))
					return false, false
				}
				notes = append(notes, "old app uninstalled")
				inst = device.Installed{Pkg: info.Package, Known: true}
			case a == 1:
				notes = append(notes, "different key, installing anyway")
			default:
				j.cancelled(ctx, StValidate, exit.SigMismatch, "refused: signed with a different key than "+src)
				return false, false
			}
		}
	}

	// A build you already have.
	if cfg.Bool("identical-check") && inst.Found && info.VersionCode > 0 && info.VersionCode == inst.VersionCode {
		a := j.ask(ctx, Question{Kind: "identical", Title: "Already installed",
			Body:    fmt.Sprintf("The device is already on versionCode %d. Install it again anyway?", inst.VersionCode),
			Options: []string{"Keep the APK, skip install", "Reinstall"}, Default: 0})
		if a == 0 {
			skipInstall = true
			notes = append(notes, "already installed")
		} else if a < 0 {
			j.cancelled(ctx, StValidate, exit.OK, "stopped")
			return false, false
		}
	}

	detail := label
	if info.MinSDK > 0 {
		detail += fmt.Sprintf(" · min API %d", info.MinSDK)
	}
	if len(notes) > 0 {
		detail += " · " + strings.Join(notes, ", ")
	}
	j.finish(StValidate, StepOK, detail)
	return skipInstall, true
}

func (j *Job) cancelled(ctx context.Context, step string, code int, msg string) {
	if ctx.Err() != nil {
		return
	}
	j.fail(step, code, errors.New(msg))
}

func orDash(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

// ---- save ------------------------------------------------------------------------

func (j *Job) save(ctx context.Context, src string, cfg *config.Config) (string, bool) {
	e := j.e
	j.begin(StSave, "")
	if err := os.MkdirAll(e.APKDir(), 0o755); err != nil {
		j.fail(StSave, exit.NoSpace, err)
		return "", false
	}
	final := filepath.Join(e.APKDir(), filepath.Base(src))
	if err := moveFile(src, final); err != nil {
		j.fail(StSave, exit.NoSpace, fmt.Errorf("could not save the APK: %w", err))
		return "", false
	}
	_ = os.Chtimes(final, time.Now(), time.Now())
	detail := shorten(final)
	if out := cfg.Str("out"); out != "" {
		out = config.ExpandHome(out)
		if err := os.MkdirAll(out, 0o755); err == nil {
			cp := filepath.Join(out, filepath.Base(final))
			if err := copyFile(final, cp); err == nil {
				j.mu.Lock()
				j.res.Copy = cp
				j.mu.Unlock()
				detail += " · copy in " + shorten(out)
			} else {
				detail += " · could not copy to " + out
			}
		}
	}
	j.mu.Lock()
	j.res.APK = final
	j.mu.Unlock()
	j.pruneCache(cfg.Int("keep"), final)
	j.finish(StSave, StepOK, detail)
	return final, true
}

// pruneCache keeps the newest n APKs (plus the one just saved).
func (j *Job) pruneCache(n int, current string) {
	if n < 0 {
		return
	}
	var others []CachedAPK
	for _, c := range j.e.CachedAPKs() {
		if c.Path != current {
			others = append(others, c)
		}
	}
	for i, c := range others { // already newest first
		if i >= n {
			_ = os.Remove(c.Path)
		}
	}
}

func shorten(p string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, h+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := out.ReadFrom(in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
