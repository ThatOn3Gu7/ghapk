package tui

import (
	"os"
	"os/exec"
	"path/filepath"
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
	Err     error
}

type InstallLogMsg struct{ Line string }
type InstallDoneMsg struct{ Err error }

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

// downloadApkCmd downloads the artifact zip and extracts the best APK.
// Progress flows through progCh; completion arrives as DownloadDoneMsg.
func downloadApkCmd(repo string, art ghapi.Artifact, zipPath, outDir, filePattern string, dev device.Info, prefs archPrefs, progCh chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		prog := func(got, total int64) {
			select {
			case progCh <- DownloadProgressMsg{Received: got, Total: total}:
			default:
			}
		}
		err := ghapi.DownloadArtifact(repo, art, zipPath, prog)
		if err != nil {
			progCh <- DownloadDoneMsg{Err: err}
			return nil
		}
		sd := scoreDeviceFrom(dev)
		apk, err := ghapi.ExtractBestAPK(zipPath, outDir, filePattern, scorePrefsFrom(prefs), sd)
		progCh <- DownloadDoneMsg{ZipPath: zipPath, APK: apk, Err: err}
		return nil
	}
}

func downloadReleaseApkCmd(repo string, asset ghapi.ReleaseAsset, dest, filePattern string, progCh chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		target, err := ghapi.ResolveReleaseAssetDest(dest, asset.Name)
		if err != nil {
			progCh <- DownloadDoneMsg{Err: err}
			return nil
		}
		prog := func(got, total int64) {
			select {
			case progCh <- DownloadProgressMsg{Received: got, Total: total}:
			default:
			}
		}
		if err := ghapi.DownloadReleaseAsset(repo, asset, target, prog); err != nil {
			progCh <- DownloadDoneMsg{Err: err}
			return nil
		}
		size := asset.Size
		if fi, statErr := os.Lstat(target); statErr == nil && fi.Mode().IsRegular() {
			size = fi.Size()
		}
		progCh <- DownloadDoneMsg{
			ZipPath: "",
			APK:     ghapi.ExtractedAPK{Path: target, Name: filepath.Base(target), Size: size, Candidate: true},
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
		err := device.Install(path, allowDowngrade, logf)
		logCh <- InstallDoneMsg{Err: err}
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
