// Package flow is the classic linear pipeline — the non-TUI counterpart of
// the original script: resolve repo → scan runs → pick the best artifact →
// download → hand off to the installer. It exists for scripts, CI and
// quick one-liners; the TUI is the primary interface.
package flow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ghapk/ghapk/internal/apk"
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

	// [3/6] & [4/6] locate source (actions artifacts and/or releases)
	var (
		art      ghapi.Artifact
		run      ghapi.Run
		relPick  ghapi.RankedReleaseAsset
		useRel   bool
	)

	if s.Source != "releases" {
		runs, err := ghapi.ListRuns(repo, s.Workflow, s.Branch, s.RunLimit)
		if err != nil && s.Source == "actions" {
			out("ghapk: listing runs: %v", err)
			return 5
		}
		var good []ghapi.Run
		for _, r := range runs {
			if r.Finished() && r.Conclusion == "success" {
				good = append(good, r)
			}
		}
		if len(good) == 0 && s.Source == "actions" {
			out("ghapk: no successful runs found for %s%s", repo, branchSuffix(s.Branch))
			return 6
		}
		if len(good) > 0 {
			out("ghapk: %d successful run(s), newest #%d", len(good), good[0].RunNumber)
		}

		var empty, unseen int
		for _, r := range good {
			arts, aerr := ghapi.ListArtifacts(repo, r.ID)
			if aerr != nil {
				unseen++
				continue
			}
			var ranked []ghapi.Artifact
			for _, a := range arts {
				if a.Expired {
					continue
				}
				if s.ArtifactName != "" && !strings.EqualFold(a.Name, s.ArtifactName) {
					continue
				}
				if s.ArtifactName == "" {
					if s.ArtifactPattern != "" && !containsFold(a.Name, s.ArtifactPattern) {
						continue
					}
					if s.ExcludePattern != "" && containsFold(a.Name, s.ExcludePattern) {
						continue
					}
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
		if art.ID == 0 && s.Source == "actions" {
			out("ghapk: no matching artifact in %d run(s) (%d empty, %d unreadable)",
				len(good), empty, unseen)
			return 7
		}
	}

	if art.ID == 0 && s.Source != "actions" {
		rels, err := ghapi.ListReleases(repo, 10)
		if err != nil {
			out("ghapk: listing releases: %v", err)
			return 5
		}
		rankedRels := ghapi.RankReleaseAssets(rels, s.ArtifactPattern, s.FilePattern, s.ExcludePattern, prefs, sd)
		for _, cand := range rankedRels {
			if !cand.Asset.Rejected {
				relPick = cand
				useRel = true
				break
			}
		}
		if !useRel {
			out("ghapk: no matching APK found in workflow artifacts or releases")
			return 7
		}
		out("ghapk: release asset %s (%s) from %s",
			relPick.Asset.Name, ghapi.HumanBytes(relPick.Asset.Size), relPick.ReleaseTag)
	} else {
		out("ghapk: artifact %s (%s) from run #%d", art.Name,
			ghapi.HumanBytes(art.SizeInBytes), run.RunNumber)
	}

	if o.DryRun {
		out("ghapk: dry run — would download to %s", config.CacheDir()+"/apk")
		return 0
	}

	// [5/6] download + unpack
	dir := o.PrintDir
	if dir == "" {
		dir = filepath.Join(config.CacheDir(), "apk")
	}
	lastReport := int64(-1)
	prog := func(got, total int64) {
		mb := got / (1 << 20)
		if mb != lastReport {
			lastReport = mb
			fmt.Fprintf(os.Stderr, "\rghapk: downloading… %s", ghapi.HumanBytes(got))
		}
	}

	var apkOut ghapi.ExtractedAPK
	if useRel {
		var rerr error
		apkOut, rerr = ghapi.FetchReleaseAPKWithDigest(repo, relPick.Asset, dir, s.FilePattern, s.ExcludePattern, s.KeepCache, s.VerifyDigest, prefs, sd, prog)
		if rerr != nil {
			out("\nghapk: release download failed: %v", rerr)
			return 8
		}
		if !s.KeepCache && o.PrintDir == "" {
			ghapi.PruneAPKCache(dir, s.KeepCount, apkOut.Path)
		}
		if apkOut.DigestStatus == apk.DigestVerified {
			out("\rghapk: downloaded release asset (sha256 verified against GitHub digest)   ")
		} else {
			out("\rghapk: downloaded release asset (digest unverified — GitHub published none)   ")
		}
	} else {
		zipPath := filepath.Join(dir, fmt.Sprintf("%d.zip", art.ID))
		cached, derr := ghapi.DownloadArtifactCachedWithDigest(repo, art, zipPath, s.KeepCache, s.VerifyDigest, prog)
		if derr != nil {
			out("\nghapk: download failed: %v", derr)
			return 8
		}
		checked := art.Digest
		if !s.VerifyDigest {
			checked = ""
		}
		ds := ghapi.DigestStatusFor(checked)
		switch {
		case cached:
			out("\rghapk: reused verified cached archive %s   ", filepath.Base(zipPath))
		case ds == apk.DigestVerified:
			out("\rghapk: downloaded %s (sha256 verified against GitHub digest)   ", ghapi.HumanBytes(art.SizeInBytes))
		default:
			out("\rghapk: downloaded %s (digest unverified — GitHub published none)   ", ghapi.HumanBytes(art.SizeInBytes))
		}

		var xerr error
		apkOut, xerr = ghapi.ExtractBestAPKFiltered(zipPath, dir, s.FilePattern, s.ExcludePattern, prefs, sd)
		if !s.KeepCache {
			_ = os.Remove(zipPath)
			if xerr == nil && o.PrintDir == "" {
				ghapi.PruneAPKCache(dir, s.KeepCount, apkOut.Path)
			}
		}
		if xerr != nil {
			out("ghapk: unpacking: %v", xerr)
			return 9
		}
		apkOut.DigestStatus = ds
		apkOut.Meta.DigestStatus = ds
	}
	out("ghapk: apk %s (%s)", apkOut.Path, ghapi.HumanBytes(apkOut.Size))
	if apkOut.Meta.HaveInfo {
		out("ghapk: manifest %s · v%s (%d)",
			apkOut.Meta.Info.Package, apkOut.Meta.Info.VersionName, apkOut.Meta.Info.VersionCode)
	}
	if apkOut.Meta.Cert.SHA256 != "" {
		out("ghapk: signer %.12s… (%s)", apkOut.Meta.Cert.SHA256, apkOut.Meta.Cert.Scheme)
	}

	// [6/6] hand off / install
	if o.Install {
		if s.NoOpen {
			out("ghapk: --no-open is set — skipping installer (APK saved at %s)", apkOut.Path)
			return 0
		}
		hist, _ := config.History(repo)
		expectPkg := s.PackageName
		if expectPkg == "" {
			expectPkg = hist.Pkg
		}
		queryPkg := expectPkg
		if queryPkg == "" && apkOut.Meta.HaveInfo {
			queryPkg = apkOut.Meta.Info.Package
		}
		var instApp device.InstalledApp
		var instFound bool
		if queryPkg != "" {
			instApp, instFound = device.QueryPackage(queryPkg)
		}

		safety := apk.EvaluateSafety(apkOut.Meta, apk.SafetyInput{
			ExpectedPackage: expectPkg,
			InstalledFound:  instFound,
			InstalledVCode:  instApp.VersionCode,
			InstalledVName:  instApp.VersionName,
			InstalledCert:   instApp.CertSHA256,
			HistoryCert:     hist.Sig,
			DeviceSDK:       dev.SDK,
			DowngradeMode:   s.Downgrade,
			CheckPackage:    s.PkgCheck,
			CheckSignature:  s.SigCheck,
		})
		for _, n := range safety.Notes {
			out("ghapk: check: %s", n)
		}
		if safety.BlockedReason != "" {
			out("ghapk: refused: %s", safety.BlockedReason)
			return 11
		}
		for _, w := range safety.Warnings {
			out("ghapk: warning (%s): %s", w.Kind, w.Message)
			if w.Kind == apk.WarnDowngrade && strings.ToLower(s.Downgrade) != "allow" {
				out("ghapk: refusing automatic downgrade under downgrade=%s — pass --downgrade=allow to confirm downgrade", s.Downgrade)
				return 11
			}
			if !o.Yes {
				out("ghapk: refusing unconfirmed %s in non-interactive mode (inspect or pass --yes for non-downgrade warnings)", w.Kind)
				return 11
			}
		}

		res, err := device.InstallAPK(apkOut.Path, safety.AllowDowngrade, func(line string) {
			out("ghapk: installer: %s", line)
		})
		if err != nil || res.Outcome == device.OutcomeFailed {
			out("ghapk: install failed: %v", err)
			return 10
		}
		if res.Outcome == device.OutcomeLaunched {
			out("ghapk: installer launched via %s — accept the prompt on your Android device", res.Via)
			if s.VerifyInstall && queryPkg != "" && apkOut.Meta.HaveInfo {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				vr := device.VerifyInstall(ctx, queryPkg, apkOut.Meta.Info.VersionCode, 25*time.Second)
				cancel()
				switch vr.Outcome {
				case device.VerifyConfirmed:
					out("ghapk: verified installed on device (%s)", vr.Detail)
					recordFlowHistory(repo, art, run, apkOut.Meta)
				case device.VerifyUnchanged:
					out("ghapk: post-install check: %s (installation not confirmed)", vr.Detail)
				default:
					out("ghapk: installation remains unconfirmed (%s)", vr.Detail)
				}
			}
		} else {
			out("ghapk: installed via %s (confirmed)", res.Via)
			recordFlowHistory(repo, art, run, apkOut.Meta)
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

func recordFlowHistory(repo string, art ghapi.Artifact, run ghapi.Run, meta apk.Inspection) {
	entry := config.HistoryEntry{
		Run:      run.ID,
		Artifact: art.Name,
		Digest:   art.Digest,
		SHA:      meta.LocalSHA256,
		Sig:      meta.Cert.SHA256,
	}
	if meta.HaveInfo {
		entry.Pkg = meta.Info.Package
		entry.VCode = meta.Info.VersionCode
		entry.VName = meta.Info.VersionName
	}
	_ = config.RecordInstall(repo, entry)
}
