package ghapi

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ghapk/ghapk/internal/score"
)

// Progress is called during downloads. total is 0 when unknown
// (GitHub does not send Content-Length through gh api reliably).
type Progress func(received, total int64)

// DownloadArtifact streams the artifact zip through `gh api` to destZip,
// reporting progress. Mirrors download_via_ghapi_with_curl from the script.
func DownloadArtifact(repo string, art Artifact, destZip string, prog Progress) error {
	endpoint := fmt.Sprintf("repos/%s/actions/artifacts/%d/zip", repo, art.ID)
	return streamGH(endpoint, destZip, art.SizeInBytes, prog)
}

// DownloadReleaseAsset downloads one asset of a release.
func DownloadReleaseAsset(repo string, asset ReleaseAsset, dest string, prog Progress) error {
	endpoint := fmt.Sprintf("repos/%s/releases/assets/%d", repo, asset.ID)
	return streamGHAccept(endpoint, dest, asset.Size, prog)
}

func streamGH(endpoint, dest string, expected int64, prog Progress) error {
	return stream(endpoint, dest, expected, prog, "api", endpoint)
}

func streamGHAccept(endpoint, dest string, expected int64, prog Progress) error {
	return stream(endpoint, dest, expected, prog, "api", "-H", "Accept: application/octet-stream", endpoint)
}

func stream(endpoint, dest string, expected int64, prog Progress, ghArgs ...string) error {
	if !Available() {
		return ErrNoGH
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", ghArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = nil
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := cmd.Start(); err != nil {
		return err
	}
	buf := make([]byte, 128*1024)
	var got int64
	tick := time.Now()
	for {
		n, rerr := stdout.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				_ = cmd.Process.Kill()
				return err
			}
			got += int64(n)
			if prog != nil && time.Since(tick) > 120*time.Millisecond {
				prog(got, expected)
				tick = time.Now()
			}
		}
		if rerr != nil {
			break
		}
	}
	if prog != nil {
		prog(got, expected)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("gh %s: %w", endpoint, err)
	}
	if got == 0 {
		return fmt.Errorf("gh %s: empty response (artifact may have expired)", endpoint)
	}
	return nil
}

// ExtractedAPK is what we pulled out of an artifact zip.
type ExtractedAPK struct {
	Path      string
	Name      string
	Size      int64
	Score     int
	Candidate bool // false for files inside the zip that lost the ranking
}

// InspectZip lists the contents without extracting (for the UI detail pane).
func InspectZip(zipPath string) ([]string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var names []string
	for _, f := range r.File {
		names = append(names, fmt.Sprintf("%s  (%s)", f.Name, humanBytes(int64(f.UncompressedSize64))))
	}
	return names, nil
}

// ExtractBestAPK mirrors step_download + the in-zip ranking: pull every .apk
// (plus files matching filePattern), keep the best-ranked one in outDir, and
// delete the losers unless keepAll is set.
func ExtractBestAPK(zipPath, outDir, filePattern string, p score.Prefs, dev score.Device) (ExtractedAPK, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return ExtractedAPK{}, fmt.Errorf("not a zip: %w", err)
	}
	defer r.Close()
	type cand struct {
		f     *zip.File
		score int
	}
	var cands []cand
	for _, f := range r.File {
		base := path.Base(f.Name)
		lower := strings.ToLower(base)
		if f.FileInfo().IsDir() {
			continue
		}
		if strings.HasSuffix(lower, ".apk") {
			cands = append(cands, cand{f, score.APKRank(base, p, dev)})
			continue
		}
		if filePattern != "" && matchGlobFold(filePattern, base) {
			cands = append(cands, cand{f, score.APKRank(base, p, dev) - 20})
		}
	}
	if len(cands) == 0 {
		return ExtractedAPK{}, fmt.Errorf("no APK inside the zip (found %d files)", len(r.File))
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return ExtractedAPK{}, err
	}
	best := cands[0]
	dest := filepath.Join(outDir, path.Base(best.f.Name))
	if err := extractOne(best.f, dest); err != nil {
		return ExtractedAPK{}, err
	}
	return ExtractedAPK{
		Path:  dest,
		Name:  path.Base(best.f.Name),
		Size:  int64(best.f.UncompressedSize64),
		Score: best.score,
	}, nil
}

func extractOne(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// matchGlobFold is filepath.Match against a lowercased name and pattern,
// tolerating the shell-style globs the script accepted.
func matchGlobFold(pattern, name string) bool {
	ok, err := path.Match(strings.ToLower(pattern), strings.ToLower(name))
	if err != nil {
		return strings.Contains(strings.ToLower(name), strings.ToLower(strings.Trim(pattern, "*")))
	}
	return ok
}

// HumanBytes is exported for the UI.
func humanBytes(b int64) string { return HumanBytes(b) }

// HumanBytes formats sizes like the script's fmt_size.
func HumanBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
