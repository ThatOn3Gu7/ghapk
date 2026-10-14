// Package flow is the classic linear pipeline — the non-TUI counterpart of
// the original script: resolve repo → scan runs → pick the best artifact →
// download → hand off to the installer. It exists for scripts, CI and
// quick one-liners; the TUI is the primary interface.
package flow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/score"
)

// GetOptions controls a classic run.
type GetOptions struct {
	Settings config.Settings
	Yes      bool   // skip confirmations
	Install  bool   // hand off to the installer after download
	PrintDir string // override output dir
	DryRun   bool   // resolve and print the plan only
}

// Run executes the condensed pipeline, returning exit codes that match the
// spirit of the script's EX_* constants.
func Run(o GetOptions) int {
	s := o.Settings
	out := func(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) }

	// [1/6] preflight
	if !ghapi.Available() {
		out("ghapk: the GitHub CLI (gh) is not installed — see https://cli.github.com")
		return 3
	}
	user, err := ghapi.AuthStatus()
	if err != nil {
		out("ghapk: gh is not authenticated — run: gh auth login")
		return 4
	}
	_ = user

	// device probe (informational)
	dev := device.Probe()
	sd := score.Device{ABIs: dev.ABIs, SDK: dev.SDK}
	prefs := score.Prefs{Arch: s.Arch, Variant: s.Variant}

	// [2/6] repo
	repo := s.Repo
	if repo == "" {
		for _, r := range config.LoadRecents() {
			repo = r.Repo
			break
		}
	}
	if repo == "" {
		out("ghapk: no repository — pass --repo owner/name or run the TUI once")
		return 2
	}
	out("ghapk: repo %s", repo)

	// [3/6] runs
	runs, err := ghapi.ListRuns(repo, s.Workflow, s.Branch, s.RunLimit)
	if err != nil {
		out("ghapk: listing runs: %v", err)
		return 5
	}
	var good []ghapi.Run
	for _, r := range runs {
		if r.Finished() && r.Conclusion == "success" {
			good = append(good, r)
		}
	}
	if len(good) == 0 {
		out("ghapk: no successful runs found for %s%s", repo, branchSuffix(s.Branch))
		return 6
	}
	out("ghapk: %d successful run(s), newest #%d", len(good), good[0].RunNumber)

	// [4/6] locate artifact
	var (
		art    ghapi.Artifact
		run    ghapi.Run
		empty  int
		unseen int
	)
	for _, r := range good {
		arts, err := ghapi.ListArtifacts(repo, r.ID)
		if err != nil {
			unseen++
			continue
		}
		var ranked []ghapi.Artifact
		for _, a := range arts {
			if a.Expired {
				continue
			}
			if s.ArtifactPattern != "" && !containsFold(a.Name, s.ArtifactPattern) {
				continue
			}
			a.Score = score.ArtifactRank(a.Name, prefs, sd)
			if !score.AllowedByPolicy(a.Name, prefs, sd) {
				continue
			}
			ranked = append(ranked, a)
		}
		if len(ranked) == 0 {
			empty++
			continue
		}
		best := ranked[0]
		for _, a := range ranked[1:] {
			if a.Score > best.Score {
				best = a
			}
		}
		art, run = best, r
		break
	}
	if art.ID == 0 {
		out("ghapk: no matching artifact in %d run(s) (%d empty, %d unreadable)",
			len(good), empty, unseen)
		return 7
	}
	out("ghapk: artifact %s (%s) from run #%d", art.Name,
		ghapi.HumanBytes(art.SizeInBytes), run.RunNumber)

	if o.DryRun {
		out("ghapk: dry run — would download to %s", config.CacheDir()+"/apk")
		return 0
	}

	// [5/6] download + unpack
	dir := o.PrintDir
	if dir == "" {
		dir = filepath.Join(config.CacheDir(), "apk")
	}
	zipPath := filepath.Join(dir, fmt.Sprintf("%d.zip", art.ID))
	lastReport := int64(-1)
	prog := func(got, total int64) {
		mb := got / (1 << 20)
		if mb != lastReport {
			lastReport = mb
			fmt.Fprintf(os.Stderr, "\rghapk: downloading… %s", ghapi.HumanBytes(got))
		}
	}
	if err := ghapi.DownloadArtifact(repo, art, zipPath, prog); err != nil {
		out("\nghapk: download failed: %v", err)
		return 8
	}
	out("\rghapk: downloaded %s   ", ghapi.HumanBytes(art.SizeInBytes))

	apk, err := ghapi.ExtractBestAPK(zipPath, dir, s.FilePattern, prefs, sd)
	if err != nil {
		out("ghapk: unpacking: %v", err)
		return 9
	}
	out("ghapk: apk %s (%s)", apk.Path, ghapi.HumanBytes(apk.Size))

	// [6/6] hand off
	if o.Install {
		allowDowngrade := s.Downgrade != "never"
		if err := device.Install(apk.Path, allowDowngrade, func(line string) {
			out("ghapk: installer: %s", line)
		}); err != nil {
			out("ghapk: installer failed: %v", err)
			return 10
		}
	} else {
		out("ghapk: pass --install to hand off to the package installer")
	}
	return 0
}

func branchSuffix(b string) string {
	if b == "" {
		return ""
	}
	return " (branch " + b + ")"
}

func containsFold(name, pattern string) bool {
	return strings.Contains(
		strings.ToLower(name),
		strings.ToLower(strings.Trim(pattern, "*")))
}
