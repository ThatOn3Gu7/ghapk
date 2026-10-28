package engine

import (
	"context"
	"fmt"
	"time"

	"ghapk/internal/config"
	"ghapk/internal/device"
	"ghapk/internal/exit"
)

// install hands the APK to the installer and then checks that it landed.
func (j *Job) install(ctx context.Context, apkPath string, cfg *config.Config, skipInstall bool) {
	e := j.e
	if skipInstall {
		j.finish(StInstall, StepSkip, "already on the device")
		j.finish(StConfirm, StepSkip, "")
		return
	}
	if j.Opts.NoInstall {
		j.finish(StInstall, StepSkip, "download only")
		j.finish(StConfirm, StepSkip, "")
		return
	}
	if !e.Dev.CanInstall() {
		j.finish(StInstall, StepSkip, e.Dev.Host+" cannot install Android apps: copy the APK to the phone")
		j.finish(StConfirm, StepSkip, "")
		return
	}
	if cfg.Bool("confirm-install") && !j.Opts.Yes {
		a := j.ask(ctx, Question{Kind: "handoff", Title: "Install it?",
			Body:    "Hand " + shortBase(apkPath) + " to the Android installer.",
			Options: []string{"Install", "Keep the APK only"}, Default: 0})
		if a != 0 {
			if ctx.Err() == nil {
				j.finish(StInstall, StepSkip, "kept the APK only")
				j.finish(StConfirm, StepSkip, "")
			}
			return
		}
	}
	j.begin(StInstall, "opening the installer")
	t0 := time.Now()
	j.recordHistory()
	method, err := e.Dev.Open(ctx, apkPath, func(s string) { j.logf("%s", s) })
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		j.finish(StInstall, StepWarn, "no installer available: the APK is ready at "+shorten(apkPath))
		j.finish(StConfirm, StepSkip, "")
		j.mu.Lock()
		j.res.NotLanded = true
		j.mu.Unlock()
		return
	}
	j.mu.Lock()
	j.res.HandedOff, j.res.Method = true, method
	j.mu.Unlock()
	j.finish(StInstall, StepOK, fmt.Sprintf("opened via %s in %s: accept the prompt in Android", method, time.Since(t0).Round(100*time.Millisecond)))
	j.confirm(ctx, apkPath, cfg, method)
}

func shortBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// confirm polls the device until it is on the new versionCode.
func (j *Job) confirm(ctx context.Context, apkPath string, cfg *config.Config, method string) {
	e := j.e
	if !cfg.Bool("verify-install") {
		j.finish(StConfirm, StepSkip, "turned off in settings")
		return
	}
	j.mu.Lock()
	info := j.res.Info
	j.mu.Unlock()
	pkg := info.Package
	if pkg == "" {
		pkg = e.Package()
	}
	wait := time.Duration(cfg.Int("install-wait")) * time.Second
	if method == "adb install" { // synchronous: a short grace is enough
		wait = 10 * time.Second
	}
	j.begin(StConfirm, fmt.Sprintf("waiting for %s to reach versionCode %d", pkg, info.VersionCode))
	res, got, why := e.Dev.Verify(ctx, pkg, info.VersionCode, wait, func(w time.Duration, now int64) {
		j.detail(StConfirm, fmt.Sprintf("waiting for versionCode %d (device has %d) · %ds", info.VersionCode, now, int(w.Seconds())))
	})
	if ctx.Err() != nil {
		return
	}
	j.mu.Lock()
	j.res.InstalledNow = got
	j.mu.Unlock()
	switch res {
	case device.VerifyLanded:
		j.mu.Lock()
		j.res.Verified = true
		j.mu.Unlock()
		j.finish(StConfirm, StepOK, fmt.Sprintf("%s is on versionCode %d", pkg, got))
		e.RefreshInstalled(ctx)
	case device.VerifyNotLanded:
		d := e.Dev.Diagnose(ctx, info.VersionCode, got, j.res.Size, info.MinSDK, e.CacheDir, pkg)
		j.mu.Lock()
		j.res.NotLanded, j.res.Diag = true, &d
		if d.Exit == 0 {
			d.Exit = exit.Install
		}
		j.mu.Unlock()
		msg := why + ": " + d.Fix
		j.finish(StConfirm, StepWarn, msg)
		j.logf("not landed: %s (%s) %s", d.Fix, d.Code, d.Detail)
	default:
		j.finish(StConfirm, StepSkip, "not verified: "+why)
	}
}

// recordHistory remembers this install for the repo.
func (j *Job) recordHistory() {
	e := j.e
	repo := e.Repo()
	if repo == "" {
		return
	}
	prev, _ := config.History(repo)
	j.mu.Lock()
	info, cert := j.res.Info, j.res.Cert
	j.mu.Unlock()
	ent := config.Entry{
		Installs: prev.Installs + 1,
		Run:      j.Src.Run.ID,
		SHA:      j.Src.Run.HeadSHA,
		Artifact: j.Src.Name,
		Digest:   j.Src.Digest,
		Pkg:      info.Package,
		When:     time.Now().Unix(),
		Sig:      cert.SHA256,
	}
	if info.VersionCode > 0 {
		ent.VCode = fmt.Sprint(info.VersionCode)
	}
	if ent.Pkg == "" {
		ent.Pkg = prev.Pkg
	}
	if ent.Sig == "" {
		ent.Sig = prev.Sig // the key belongs to the repo, not the run
	}
	if err := config.SaveHistory(repo, ent); err != nil {
		e.Log.Logf("could not save history: %v", err)
	}
}

// notifyDone raises a notification for the outcome.
func (j *Job) notifyDone(cfg *config.Config) {
	v := j.Snapshot()
	kind, title, body := "ok", "ghapk: ready", shortBase(v.Res.APK)
	switch {
	case v.Res.Verified:
		title, body = "ghapk: installed", fmt.Sprintf("%s v%s", v.Res.Info.Package, v.Res.Info.VersionName)
	case v.Res.NotLanded:
		kind, title = "warn", "ghapk: not installed"
		if v.Res.Diag != nil {
			body = v.Res.Diag.Fix
		}
	case v.Res.HandedOff:
		title, body = "ghapk: accept the install", shortBase(v.Res.APK)
	}
	n := device.Notifier{On: cfg.Str("notify-on"), Speak: cfg.Bool("speak"), Toast: cfg.Bool("toast")}
	url := ""
	if j.Src.Run.ID != 0 {
		url = j.Src.Run.HTMLURL
	}
	go n.Send(title, body, kind, url)
}
