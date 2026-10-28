package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ghapk/internal/config"
	"ghapk/internal/device"
	"ghapk/internal/ghub"
	"ghapk/internal/pick"
)

// Reload re-reads the config layers after a file changed.
func (e *Engine) Reload() {
	e.mu.Lock()
	in := e.inRepo
	e.mu.Unlock()
	e.ReloadConfig(in)
}

// Save writes one setting to the global file or the current repo's file and
// reloads. An empty value removes the key. It returns the path written.
func (e *Engine) Save(key, val string, global bool) (string, error) {
	path := config.GlobalFile()
	if !global {
		if r := e.Repo(); r != "" {
			path = config.RepoFile(r)
		}
	}
	if err := config.SetKey(path, key, val); err != nil {
		return path, err
	}
	e.Reload()
	return path, nil
}

// PlanRun builds a plan for one particular run. artifactID picks an artifact
// of that run; 0 means the best match.
func (e *Engine) PlanRun(ctx context.Context, runID, artifactID int64) (*Plan, error) {
	run, err := e.GH.GetRun(ctx, e.Repo(), runID)
	if err != nil {
		return nil, err
	}
	cands, err := e.RunArtifacts(ctx, runID)
	if err != nil {
		return nil, err
	}
	var chosen *pick.Candidate
	for i := range cands {
		c := &cands[i]
		if artifactID != 0 && c.Art.ID == artifactID {
			chosen = c
			break
		}
		if artifactID == 0 && c.Match && chosen == nil {
			chosen = c
		}
	}
	if chosen == nil {
		var names []string
		for _, c := range cands {
			names = append(names, c.Art.Name)
		}
		return nil, &NoArtifactError{Msg: fmt.Sprintf("run #%d has no matching artifact", run.RunNumber), Seen: names}
	}
	p := &Plan{Run: run, Candidates: cands, Source: FromCandidate(run, *chosen)}
	e.annotate(ctx, p)
	return p, nil
}

// WatchRun polls a run until it finishes, calling tick after each poll. It
// gives up after timeout.
func (e *Engine) WatchRun(ctx context.Context, runID int64, interval, timeout time.Duration, tick func(ghub.Run)) (ghub.Run, error) {
	if interval < 3*time.Second {
		interval = 3 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		run, err := e.GH.GetRun(ctx, e.Repo(), runID)
		if err != nil {
			return run, err
		}
		if tick != nil {
			tick(run)
		}
		if !run.Active() {
			return run, nil
		}
		if time.Now().After(deadline) {
			return run, fmt.Errorf("gave up waiting after %s: the build is still running", timeout.Round(time.Second))
		}
		select {
		case <-ctx.Done():
			return run, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// Check is one line of the device/doctor view.
type Check struct {
	Group string
	Name  string
	Value string
	State int // CheckOK, CheckWarn, CheckFail, CheckInfo
	Hint  string
}

// Check states.
const (
	CheckOK = iota
	CheckWarn
	CheckFail
	CheckInfo
)

// Doctor inspects everything ghapk depends on and says what is missing.
func (e *Engine) Doctor(ctx context.Context) []Check {
	var out []Check
	add := func(group, name, val string, st int, hint string) {
		out = append(out, Check{group, name, val, st, hint})
	}

	// GitHub
	if p, ok := e.GH.Available(); ok {
		add("GitHub", "gh CLI", p, CheckOK, "")
		if ok, txt := e.GH.AuthOK(ctx); ok {
			who := e.User()
			if who == "" {
				who, _ = e.GH.User(ctx)
			}
			add("GitHub", "signed in", "@"+who, CheckOK, "")
		} else {
			add("GitHub", "signed in", firstLine(txt, "not logged in"), CheckFail, "run: gh auth login")
		}
		if rem, lim, reset, err := e.GH.RateLimit(ctx); err == nil {
			st := CheckOK
			if rem < lim/10 {
				st = CheckWarn
			}
			add("GitHub", "rate limit", fmt.Sprintf("%d / %d left, resets in %s", rem, lim, time.Until(reset).Round(time.Minute)), st, "")
		}
	} else {
		add("GitHub", "gh CLI", "not installed", CheckFail, "pkg install gh")
	}

	// Phone
	d := e.Dev
	host := d.Host
	if d.Model != "" {
		host += " · " + d.Model
	}
	if d.SDK > 0 {
		host += fmt.Sprintf(" · Android %s (API %d)", d.Release, d.SDK)
	}
	add("Device", "system", host, CheckInfo, "")
	if len(d.ABIs) > 0 {
		add("Device", "architectures", strings.Join(d.ABIs, ", "), CheckInfo, "")
	}
	if d.CanInstall() {
		door := d.Probe(ctx)
		switch door {
		case device.DoorNone:
			add("Device", "package manager", "not reachable", CheckWarn, "install Shizuku + rish, or run from adb, to compare installed versions")
		default:
			add("Device", "package manager", "reachable ("+string(door)+")", CheckOK, "")
		}
		if d.HasRish() {
			add("Device", "Shizuku (rish)", "found", CheckOK, "")
		} else {
			add("Device", "Shizuku (rish)", "not found", CheckInfo, "optional: lets ghapk read install errors and uninstall")
		}
		var have []string
		for _, t := range []string{"termux-open", "am", "adb", "xdg-open"} {
			if _, ok := device.Tool(t); ok {
				have = append(have, t)
			}
		}
		if len(have) == 0 {
			add("Device", "installer", "none found", CheckFail, "pkg install termux-tools")
		} else {
			add("Device", "installer", strings.Join(have, ", "), CheckOK, "")
		}
		if _, ok := device.Tool("termux-notification"); ok {
			add("Device", "notifications", "termux-api available", CheckOK, "")
		} else {
			add("Device", "notifications", "termux-api missing", CheckInfo, "optional: pkg install termux-api")
		}
	} else {
		add("Device", "install", "this machine cannot install APKs; downloads still work", CheckInfo, "")
	}
	free := device.FreeBytes(e.CacheDir)
	st := CheckOK
	if free > 0 && free < 300<<20 {
		st = CheckWarn
	}
	if free > 0 {
		add("Device", "free space", byteStr(free), st, "")
	}

	// The app
	pkg := e.Package()
	if pkg == "" {
		add("App", "package", "not known yet", CheckInfo, "it is learned from the first APK you download, or set it in Settings")
	} else {
		in := e.RefreshInstalled(ctx)
		add("App", "package", pkg, CheckInfo, "")
		switch {
		case in.Found:
			add("App", "installed", fmt.Sprintf("%s (code %d) %s", dash(in.VersionName), in.VersionCode, in.ABI), CheckOK, "")
			if in.CertSHA != "" {
				add("App", "signing key", short(in.CertSHA), CheckInfo, "")
			}
		case in.Known:
			add("App", "installed", "not installed", CheckInfo, "")
		default:
			add("App", "installed", "unknown (package manager not reachable)", CheckWarn, "")
		}
		if h, ok := config.History(e.Repo()); ok && h.Installs > 0 {
			add("App", "last install", fmt.Sprintf("run %d · %s · %d total", h.Run, time.Unix(h.When, 0).Format("Jan 02 15:04"), h.Installs), CheckInfo, "")
		}
	}

	// Files
	add("Files", "config", config.ConfigDir(), CheckInfo, "")
	add("Files", "cache", fmt.Sprintf("%s (%s)", e.CacheDir, byteStr(e.CacheSize())), CheckInfo, "")
	if p := e.Log.Path(); p != "" {
		add("Files", "log", p, CheckInfo, "")
	}
	return out
}

func firstLine(s, def string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func dash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}

func short(s string) string {
	if len(s) > 23 {
		return s[:23] + "…"
	}
	return s
}

func byteStr(n int64) string { return FmtBytes(n) }

// KeepAPKDir is where finished APKs are saved: used by the UI to show the path.
func (e *Engine) KeepAPKDir() string { return filepath.Join(e.CacheDir, "apk") }

// RemoveCached deletes one kept APK.
func (e *Engine) RemoveCached(path string) error {
	if filepath.Dir(path) != e.APKDir() {
		return fmt.Errorf("not in the APK cache")
	}
	return os.Remove(path)
}
