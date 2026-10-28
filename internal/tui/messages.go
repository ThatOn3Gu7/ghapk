package tui

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/ghapi"
)

// ───────────────────────────── messages ─────────────────────────────

type AuthCheckedMsg struct {
	Version string
	User    string
	Err     error
}

type RepoInfoMsg struct {
	Repo ghapi.Repo
	Err  error
}

type WorkflowsMsg struct {
	List []ghapi.Workflow
	Err  error
}

type RunsMsg struct {
	List []ghapi.Run
	Err  error
}

// WorkflowFileMsg carries a workflow YAML preview.
type WorkflowFileMsg struct {
	Path string
	YAML string
	Err  error
}

type ArtifactsMsg struct {
	RunID int64
	List  []ghapi.Artifact
	Err   error
}

type BranchesMsg struct {
	List []ghapi.Branch
	Err  error
}

type ReleasesMsg struct {
	List []ghapi.Release
	Err  error
}

type DeviceMsg struct {
	Info device.Info
}

type PackageQueryMsg struct {
	Pkg   string
	App   device.InstalledApp
	Found bool
	Err   error
}

type SearchResultsMsg struct {
	Query string
	Hits  []ghapi.SearchHit
	Err   error
}

type DownloadProgressMsg struct {
	Received int64
	Total    int64
}

type DownloadDoneMsg struct {
	ZipPath string
	APK     ghapi.ExtractedAPK
	Cached  bool
	Err     error
}

type InstallLogMsg struct{ Line string }
type InstallDoneMsg struct {
	Result device.InstallResult
	Err    error
}
type InstallVerifyMsg struct {
	Pkg    string
	Result device.VerifyResult
}

type ZipInspectedMsg struct {
	Lines []string
	Err   error
}

type RefreshTickMsg struct{ At time.Time }
type ShimmerTickMsg struct{ At time.Time }
type ClearToastMsg struct{}

type FatalErrMsg struct {
	Context string
	Err     error
}

// ───────────────────────────── commands ─────────────────────────────

func checkAuthCmd() tea.Cmd {
	return func() tea.Msg {
		if !ghapi.Available() {
			return AuthCheckedMsg{Err: ghapi.ErrNoGH}
		}
		v := ghapi.Version()
		u, err := ghapi.AuthStatus()
		return AuthCheckedMsg{Version: v, User: u, Err: err}
	}
}

func fetchRepoCmd(repo string) tea.Cmd {
	return func() tea.Msg {
		r, err := ghapi.GetRepo(repo)
		return RepoInfoMsg{Repo: r, Err: err}
	}
}

func fetchWorkflowsCmd(repo string) tea.Cmd {
	return func() tea.Msg {
		ws, err := ghapi.ListWorkflows(repo)
		return WorkflowsMsg{List: ws, Err: err}
	}
}

func fetchRunsCmd(repo, workflow, branch string, limit int) tea.Cmd {
	return func() tea.Msg {
		rs, err := ghapi.ListRuns(repo, workflow, branch, limit)
		return RunsMsg{List: rs, Err: err}
	}
}

// fetchWorkflowFileCmd downloads one workflow's YAML source.
func fetchWorkflowFileCmd(repo, path string) tea.Cmd {
	return func() tea.Msg {
		out, err := ghapi.FetchWorkflowFile(repo, path)
		const maxYAML = 24 << 10
		if len(out) > maxYAML {
			out = out[:maxYAML]
		}
		return WorkflowFileMsg{Path: path, YAML: out, Err: err}
	}
}

func fetchArtifactsCmd(repo string, runID int64) tea.Cmd {
	return func() tea.Msg {
		as, err := ghapi.ListArtifacts(repo, runID)
		return ArtifactsMsg{RunID: runID, List: as, Err: err}
	}
}

func fetchBranchesCmd(repo string) tea.Cmd {
	return func() tea.Msg {
		bs, err := ghapi.ListBranches(repo)
		return BranchesMsg{List: bs, Err: err}
	}
}

func fetchReleasesCmd(repo string) tea.Cmd {
	return func() tea.Msg {
		rs, err := ghapi.ListReleases(repo, 5)
		return ReleasesMsg{List: rs, Err: err}
	}
}

func probeDeviceCmd() tea.Cmd {
	return func() tea.Msg {
		return DeviceMsg{Info: device.Probe()}
	}
}

func queryPackageCmd(pkg string) tea.Cmd {
	return func() tea.Msg {
		if pkg == "" {
			return PackageQueryMsg{}
		}
		app, found := device.QueryPackage(pkg)
		return PackageQueryMsg{Pkg: pkg, App: app, Found: found}
	}
}

func searchReposCmd(query string) tea.Cmd {
	return func() tea.Msg {
		hits, err := ghapi.SearchRepos(query, 8)
		return SearchResultsMsg{Query: query, Hits: hits, Err: err}
	}
}

// downloadApkCmd downloads (or reuses cached) the artifact zip and extracts the best APK.
// Progress flows through progCh; completion arrives as DownloadDoneMsg.
func downloadApkCmd(repo string, art ghapi.Artifact, zipPath, outDir, filePattern, excludePattern string, keepCache bool, keepCount int, verifyDigest bool, dev device.Info, prefs archPrefs, progCh chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		prog := func(got, total int64) {
			select {
			case progCh <- DownloadProgressMsg{Received: got, Total: total}:
			default:
			}
		}
		cached, err := ghapi.DownloadArtifactCachedWithDigest(repo, art, zipPath, keepCache, verifyDigest, prog)
		if err != nil {
			progCh <- DownloadDoneMsg{Err: err}
			return nil
		}
		sd := scoreDeviceFrom(dev)
		apkOut, err := ghapi.ExtractBestAPKFiltered(zipPath, outDir, filePattern, excludePattern, scorePrefsFrom(prefs), sd)
		if !keepCache {
			_ = os.Remove(zipPath)
			if err == nil {
				ghapi.PruneAPKCache(outDir, keepCount, apkOut.Path)
			}
		}
		if err == nil {
			checked := art.Digest
			if !verifyDigest {
				checked = ""
			}
			ds := ghapi.DigestStatusFor(checked)
			apkOut.DigestStatus = ds
			apkOut.Meta.DigestStatus = ds
		}
		progCh <- DownloadDoneMsg{ZipPath: zipPath, APK: apkOut, Cached: cached, Err: err}
		return nil
	}
}

func downloadReleaseApkCmd(repo string, asset ghapi.ReleaseAsset, outDir, filePattern, excludePattern string, keepCache bool, keepCount int, verifyDigest bool, dev device.Info, prefs archPrefs, progCh chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		prog := func(got, total int64) {
			select {
			case progCh <- DownloadProgressMsg{Received: got, Total: total}:
			default:
			}
		}
		sd := scoreDeviceFrom(dev)
		apkOut, err := ghapi.FetchReleaseAPKWithDigest(repo, asset, outDir, filePattern, excludePattern, keepCache, verifyDigest, scorePrefsFrom(prefs), sd, prog)
		if err != nil {
			progCh <- DownloadDoneMsg{Err: err}
			return nil
		}
		if !keepCache {
			ghapi.PruneAPKCache(outDir, keepCount, apkOut.Path)
		}
		progCh <- DownloadDoneMsg{
			ZipPath: "",
			APK:     apkOut,
		}
		return nil
	}
}

// waitProgress yields one message from progCh per invocation; the app
// re-issues it until the channel closes.
func waitProgress(progCh chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		m, ok := <-progCh
		if !ok {
			return DownloadDoneMsg{Err: nil}
		}
		return m
	}
}

func installApkCmd(path string, allowDowngrade bool, logCh chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		logf := func(line string) {
			select {
			case logCh <- InstallLogMsg{Line: line}:
			default:
			}
		}
		res, err := device.InstallAPK(path, allowDowngrade, logf)
		logCh <- InstallDoneMsg{Result: res, Err: err}
		return nil
	}
}

func waitInstallLog(logCh chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		m, ok := <-logCh
		if !ok {
			return InstallDoneMsg{}
		}
		return m
	}
}

func verifyInstallCmd(pkg string, wantVCode int64, wait time.Duration) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), wait+2*time.Second)
		defer cancel()
		res := device.VerifyInstall(ctx, pkg, wantVCode, wait)
		return InstallVerifyMsg{Pkg: pkg, Result: res}
	}
}

func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		for _, c := range [][]string{
			{"termux-open-url", url},
			{"xdg-open", url},
			{"am", "start", "-a", "android.intent.action.VIEW", "-d", url},
		} {
			if _, err := exec.LookPath(c[0]); err == nil {
				_ = exec.Command(c[0], c[1:]...).Start()
				return nil
			}
		}
		return nil
	}
}

func refreshTickCmd(secs int) tea.Cmd {
	if secs <= 0 {
		return nil
	}
	return tea.Tick(time.Duration(secs)*time.Second, func(t time.Time) tea.Msg {
		return RefreshTickMsg{At: t}
	})
}

func shimmerTickCmd() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(t time.Time) tea.Msg {
		return ShimmerTickMsg{At: t}
	})
}

func detectGitRepoCmd() tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("git", "remote", "get-url", "origin").Output()
		if err != nil {
			return nil
		}
		return GitRemoteMsg{Repo: parseGitRemote(string(out))}
	}
}

// GitRemoteMsg carries an owner/repo detected from the working tree.
type GitRemoteMsg struct{ Repo string }

func parseGitRemote(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, ".git")
	url = strings.TrimSuffix(url, "/")
	if i := strings.Index(url, "github.com"); i >= 0 {
		rest := url[i+len("github.com"):]
		rest = strings.TrimPrefix(rest, ":")
		rest = strings.TrimPrefix(rest, "/")
		if parts := strings.Split(rest, "/"); len(parts) == 2 &&
			parts[0] != "" && parts[1] != "" && !strings.Contains(parts[0], " ") {
			return rest
		}
	}
	return ""
}
