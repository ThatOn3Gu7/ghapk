package ghapi

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ghapk/ghapk/internal/score"
)

const (
	// defaultMaxZipEntries caps archive directory traversal count to bound CPU
	// and memory on crafted archives with huge central directories.
	defaultMaxZipEntries = 10_000

	// defaultMaxExtractedBytes caps uncompressed bytes written for a single APK
	// while streaming (not trusting the ZIP header's UncompressedSize64).
	defaultMaxExtractedBytes = int64(2) << 30 // 2 GiB

	// defaultMaxDownloadBytes caps streamed artifact/release asset downloads.
	defaultMaxDownloadBytes = int64(4) << 30 // 4 GiB
)

// Progress is called periodically during DownloadArtifact / DownloadReleaseAsset.
// total may be 0 if unknown.
type Progress func(received, total int64)

type atomicFile interface {
	io.Writer
	Name() string
	Close() error
}

type atomicFS struct {
	createTemp func(dir, pattern string) (atomicFile, error)
	rename     func(oldpath, newpath string) error
	remove     func(name string) error
}

func defaultAtomicFS() atomicFS {
	return atomicFS{
		createTemp: func(dir, pattern string) (atomicFile, error) {
			return os.CreateTemp(dir, pattern)
		},
		rename: os.Rename,
		remove: os.Remove,
	}
}

type streamOpts struct {
	maxBytes int64
	ghBin    string
	fs       atomicFS
}

func defaultStreamOpts() streamOpts {
	return streamOpts{
		maxBytes: defaultMaxDownloadBytes,
		ghBin:    "gh",
		fs:       defaultAtomicFS(),
	}
}

type extractOpts struct {
	maxEntries  int
	maxAPKBytes int64
	fs          atomicFS
}

func defaultExtractOpts() extractOpts {
	return extractOpts{
		maxEntries:  defaultMaxZipEntries,
		maxAPKBytes: defaultMaxExtractedBytes,
		fs:          defaultAtomicFS(),
	}
}

// DownloadArtifact streams the zip for an actions artifact to destZip atomically.
func DownloadArtifact(repo string, art Artifact, destZip string, prog Progress) error {
	return downloadArtifactWithOpts(defaultStreamOpts(), repo, art, destZip, prog)
}

func downloadArtifactWithOpts(opts streamOpts, repo string, art Artifact, destZip string, prog Progress) error {
	endpoint := fmt.Sprintf("repos/%s/actions/artifacts/%d/zip", repo, art.ID)
	return streamWithOpts(opts, endpoint, destZip, art.SizeInBytes, prog, "api", endpoint)
}

// DownloadReleaseAsset streams one asset of a release to dest atomically after
// validating both the release asset metadata name and the destination path.
func DownloadReleaseAsset(repo string, asset ReleaseAsset, dest string, prog Progress) error {
	return downloadReleaseAssetWithOpts(defaultStreamOpts(), repo, asset, dest, prog)
}

func downloadReleaseAssetWithOpts(opts streamOpts, repo string, asset ReleaseAsset, dest string, prog Progress) error {
	resolvedDest := dest
	if asset.Name != "" {
		var err error
		resolvedDest, err = ResolveReleaseAssetDest(dest, asset.Name)
		if err != nil {
			return err
		}
	}
	endpoint := fmt.Sprintf("repos/%s/releases/assets/%d", repo, asset.ID)
	return streamWithOpts(opts, endpoint, resolvedDest, asset.Size, prog,
		"api", "-H", "Accept: application/octet-stream", endpoint)
}

func streamWithOpts(opts streamOpts, endpoint, dest string, expected int64, prog Progress, ghArgs ...string) error {
	if opts.maxBytes <= 0 {
		opts.maxBytes = defaultMaxDownloadBytes
	}
	if opts.ghBin == "" {
		opts.ghBin = "gh"
	}
	if opts.fs.createTemp == nil {
		opts.fs = defaultAtomicFS()
	}

	dir, leaf, err := validateDestFilePath(dest)
	if err != nil {
		return err
	}
	if expected < 0 {
		return fmt.Errorf("invalid negative expected download size (%d)", expected)
	}
	if expected > opts.maxBytes {
		return fmt.Errorf("download size (%d bytes) exceeds maximum allowed (%d bytes)", expected, opts.maxBytes)
	}

	if opts.ghBin == "gh" {
		if !Available() {
			return ErrNoGH
		}
	} else if _, err := exec.LookPath(opts.ghBin); err != nil {
		return ErrNoGH
	}

	return writeFileAtomicInDir(opts.fs, dir, leaf, func(w io.Writer) error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, opts.ghBin, ghArgs...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		stderr := &limitedBuffer{max: 4096}
		cmd.Stderr = stderr
		if err := cmd.Start(); err != nil {
			return err
		}
		reaped := false
		killAndWait := func() {
			if reaped {
				return
			}
			reaped = true
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
		}
		defer killAndWait()

		buf := make([]byte, 128*1024)
		var got int64
		tick := time.Now()
		for {
			n, rerr := stdout.Read(buf)
			if n > 0 {
				if got+int64(n) > opts.maxBytes {
					killAndWait()
					return fmt.Errorf("gh %s: download exceeds maximum allowed size (%d bytes)", endpoint, opts.maxBytes)
				}
				nw, werr := w.Write(buf[:n])
				if werr != nil {
					killAndWait()
					return fmt.Errorf("writing download stream: %w", werr)
				}
				if nw != n {
					killAndWait()
					return io.ErrShortWrite
				}
				got += int64(n)
				if prog != nil && time.Since(tick) > 120*time.Millisecond {
					prog(got, expected)
					tick = time.Now()
				}
			}
			if rerr != nil {
				if errors.Is(rerr, io.EOF) {
					break
				}
				killAndWait()
				return fmt.Errorf("gh %s: reading stream: %w", endpoint, rerr)
			}
		}

		reaped = true
		if err := cmd.Wait(); err != nil {
			msg := firstLine(stderr.String())
			if msg != "" {
				return fmt.Errorf("gh %s: %s: %w", endpoint, msg, err)
			}
			return fmt.Errorf("gh %s: %w", endpoint, err)
		}
		if got == 0 {
			return fmt.Errorf("gh %s: empty response (artifact may have expired)", endpoint)
		}
		if prog != nil {
			prog(got, expected)
		}
		return nil
	})
}

type limitedBuffer struct {
	buf bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	origLen := len(p)
	if rem := l.max - l.buf.Len(); rem > 0 {
		if len(p) > rem {
			p = p[:rem]
		}
		_, _ = l.buf.Write(p)
	}
	return origLen, nil
}

func (l *limitedBuffer) String() string { return l.buf.String() }

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
	return inspectZipWithMax(zipPath, defaultMaxZipEntries)
}

func inspectZipWithMax(zipPath string, maxEntries int) ([]string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if maxEntries > 0 && len(r.File) > maxEntries {
		return nil, fmt.Errorf("zip archive has %d entries (exceeds limit of %d)", len(r.File), maxEntries)
	}
	names := make([]string, 0, len(r.File))
	for _, f := range r.File {
		sz := int64(math.MaxInt64)
		if f.UncompressedSize64 <= math.MaxInt64 {
			sz = int64(f.UncompressedSize64)
		}
		names = append(names, fmt.Sprintf("%s  (%s)", sanitizeDisplayPath(f.Name), humanBytes(sz)))
	}
	return names, nil
}

// ExtractBestAPK mirrors step_download + the in-zip ranking: validate archive
// entries, enforce architecture/variant policy, rank allowed .apk files (plus
// files matching filePattern), and atomically publish the winner in outDir.
func ExtractBestAPK(zipPath, outDir, filePattern string, p score.Prefs, dev score.Device) (ExtractedAPK, error) {
	return extractBestAPKWithOpts(zipPath, outDir, filePattern, p, dev, defaultExtractOpts())
}

func extractBestAPKWithOpts(zipPath, outDir, filePattern string, p score.Prefs, dev score.Device, opts extractOpts) (ExtractedAPK, error) {
	if opts.maxEntries <= 0 {
		opts.maxEntries = defaultMaxZipEntries
	}
	if opts.maxAPKBytes <= 0 {
		opts.maxAPKBytes = defaultMaxExtractedBytes
	}
	if opts.fs.createTemp == nil {
		opts.fs = defaultAtomicFS()
	}
	if strings.TrimSpace(outDir) == "" {
		return ExtractedAPK{}, errors.New("empty output directory")
	}
	if hasControlChars(outDir) {
		return ExtractedAPK{}, errors.New("output directory contains control characters")
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return ExtractedAPK{}, fmt.Errorf("not a zip: %w", err)
	}
	defer r.Close()

	if len(r.File) > opts.maxEntries {
		return ExtractedAPK{}, fmt.Errorf("zip archive has %d entries (exceeds limit of %d)", len(r.File), opts.maxEntries)
	}

	type cand struct {
		f     *zip.File
		leaf  string
		score int
	}
	var (
		cands          []cand
		rejectedPolicy int
	)
	for _, f := range r.File {
		isDir, err := validateZipEntryMode(f)
		if err != nil {
			return ExtractedAPK{}, err
		}
		leaf, err := validateZipEntryPath(f.Name, isDir)
		if err != nil {
			return ExtractedAPK{}, err
		}
		if isDir {
			continue
		}

		lower := strings.ToLower(leaf)
		isAPK := strings.HasSuffix(lower, ".apk")
		matchesPattern := !isAPK && filePattern != "" && matchGlobFold(filePattern, leaf)
		if !isAPK && !matchesPattern {
			continue
		}
		if !score.AllowedByPolicy(leaf, p, dev) {
			rejectedPolicy++
			continue
		}
		sc := score.APKRank(leaf, p, dev)
		if !isAPK {
			sc -= 20 // slight penalty for non-.apk files matched by pattern
		}
		cands = append(cands, cand{f: f, leaf: leaf, score: sc})
	}

	if len(cands) == 0 {
		if rejectedPolicy > 0 {
			return ExtractedAPK{}, fmt.Errorf("no APK inside the zip matches policy (arch=%q variant=%q; rejected %d candidate(s))",
				p.Arch, p.Variant, rejectedPolicy)
		}
		return ExtractedAPK{}, fmt.Errorf("no APK inside the zip (found %d files)", len(r.File))
	}

	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	best := cands[0]

	dest, err := SafeDestinationPath(outDir, best.leaf)
	if err != nil {
		return ExtractedAPK{}, err
	}
	written, err := extractOneLimited(opts.fs, best.f, outDir, best.leaf, opts.maxAPKBytes)
	if err != nil {
		return ExtractedAPK{}, err
	}
	return ExtractedAPK{
		Path:      dest,
		Name:      best.leaf,
		Size:      written,
		Score:     best.score,
		Candidate: true,
	}, nil
}

func extractOneLimited(fs atomicFS, f *zip.File, outDir, leaf string, maxBytes int64) (int64, error) {
	if maxBytes <= 0 {
		return 0, errors.New("invalid maximum APK size limit")
	}
	if f.UncompressedSize64 > uint64(maxBytes) {
		return 0, fmt.Errorf("zip entry %q uncompressed size (%d bytes) exceeds limit (%d bytes)",
			sanitizeDisplayPath(f.Name), f.UncompressedSize64, maxBytes)
	}
	var written int64
	err := writeFileAtomicInDir(fs, outDir, leaf, func(w io.Writer) error {
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("opening zip entry %q: %w", sanitizeDisplayPath(f.Name), err)
		}
		limited := io.LimitReader(rc, maxBytes+1)
		n, copyErr := io.Copy(w, limited)
		closeErr := rc.Close()
		written = n
		if n > maxBytes {
			return fmt.Errorf("zip entry %q exceeds maximum extracted size (%d bytes)",
				sanitizeDisplayPath(f.Name), maxBytes)
		}
		if copyErr != nil {
			return fmt.Errorf("extracting zip entry %q: %w", sanitizeDisplayPath(f.Name), copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("closing zip entry %q: %w", sanitizeDisplayPath(f.Name), closeErr)
		}
		if uint64(n) != f.UncompressedSize64 {
			return fmt.Errorf("zip entry %q size mismatch (wrote %d bytes, header declared %d)",
				sanitizeDisplayPath(f.Name), n, f.UncompressedSize64)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return written, nil
}

// SafeDestinationPath validates leaf as a single safe file name and verifies
// that placing leaf inside outDir remains a direct child of outDir (both
// lexically and after resolving directory symlinks).
func SafeDestinationPath(outDir, leaf string) (string, error) {
	if strings.TrimSpace(outDir) == "" {
		return "", errors.New("empty output directory")
	}
	if hasControlChars(outDir) {
		return "", errors.New("output directory contains control characters")
	}
	if err := validateSafeLeafName(leaf); err != nil {
		return "", err
	}

	cleanDir := filepath.Clean(outDir)
	absDir, err := filepath.Abs(cleanDir)
	if err != nil {
		return "", fmt.Errorf("resolving output directory: %w", err)
	}
	absDest := filepath.Join(absDir, leaf)
	if err := ensureDirectChild(absDir, absDest, leaf); err != nil {
		return "", err
	}

	realDir, err := evalSymlinksAllowMissing(absDir)
	if err != nil {
		return "", fmt.Errorf("resolving output directory symlinks: %w", err)
	}
	realDest := filepath.Join(realDir, leaf)
	if err := ensureDirectChild(realDir, realDest, leaf); err != nil {
		return "", err
	}

	if fi, err := os.Lstat(realDest); err == nil && fi.IsDir() {
		return "", fmt.Errorf("destination %q is an existing directory", filepath.Join(cleanDir, leaf))
	}
	return filepath.Join(cleanDir, leaf), nil
}

// ResolveReleaseAssetDest validates a release asset's metadata filename and
// resolves a safe destination file path. If dest is an existing directory or
// ends with a path separator, assetName is placed inside dest; otherwise dest
// itself is validated as a safe target file path.
func ResolveReleaseAssetDest(dest, assetName string) (string, error) {
	if err := validateSafeLeafName(assetName); err != nil {
		return "", fmt.Errorf("unsafe release asset name %q: %w", assetName, err)
	}
	if strings.TrimSpace(dest) == "" {
		return "", errors.New("empty destination path")
	}
	if hasControlChars(dest) {
		return "", errors.New("destination path contains control characters")
	}
	if containsDotDotSegment(dest) {
		return "", fmt.Errorf("destination path %q contains '..' traversal", dest)
	}

	if strings.HasSuffix(dest, "/") || strings.HasSuffix(dest, `\`) {
		if runtime.GOOS != "windows" && strings.Contains(dest, `\`) {
			return "", fmt.Errorf("destination path %q contains backslash separator", dest)
		}
		return SafeDestinationPath(dest, assetName)
	}
	if fi, err := os.Lstat(dest); err == nil && fi.IsDir() && filepath.Base(filepath.Clean(dest)) != assetName {
		return SafeDestinationPath(dest, assetName)
	}

	dir, leaf, err := validateDestFilePath(dest)
	if err != nil {
		return "", err
	}
	return SafeDestinationPath(dir, leaf)
}

func writeFileAtomicInDir(fs atomicFS, outDir, leaf string, writeFn func(io.Writer) error) error {
	if fs.createTemp == nil {
		fs = defaultAtomicFS()
	}
	if _, err := SafeDestinationPath(outDir, leaf); err != nil {
		return err
	}

	cleanDir := filepath.Clean(outDir)
	absDir, err := filepath.Abs(cleanDir)
	if err != nil {
		return fmt.Errorf("resolving output directory: %w", err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return err
	}
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return fmt.Errorf("resolving output directory symlinks: %w", err)
	}
	realDest := filepath.Join(realDir, leaf)
	if err := ensureDirectChild(realDir, realDest, leaf); err != nil {
		return err
	}
	if fi, err := os.Lstat(realDest); err == nil && fi.IsDir() {
		return fmt.Errorf("destination %q is an existing directory", filepath.Join(cleanDir, leaf))
	}

	tmp, err := fs.createTemp(realDir, ".ghapk-*.part")
	if err != nil {
		return fmt.Errorf("creating temporary file: %w", err)
	}
	tmpName := tmp.Name()
	closed := false
	committed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		if !committed {
			_ = fs.remove(tmpName)
		}
	}()

	if err := writeFn(tmp); err != nil {
		return err
	}
	closed = true
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temporary file: %w", err)
	}

	if fi, err := os.Lstat(realDest); err == nil {
		if fi.IsDir() {
			return fmt.Errorf("destination %q is an existing directory", filepath.Join(cleanDir, leaf))
		}
		if runtime.GOOS == "windows" && fi.Mode()&os.ModeSymlink != 0 {
			// On Windows, replacing a directory symlink with MoveFileExW can fail;
			// removing the symlink itself never modifies its target.
			if err := fs.remove(realDest); err != nil {
				return fmt.Errorf("removing existing destination symlink: %w", err)
			}
		}
	}

	if err := fs.rename(tmpName, realDest); err != nil {
		return fmt.Errorf("publishing %q: %w", filepath.Join(cleanDir, leaf), err)
	}
	committed = true
	return nil
}

func validateZipEntryMode(f *zip.File) (isDir bool, err error) {
	mode := f.Mode()
	if mode&os.ModeSymlink != 0 {
		return false, fmt.Errorf("zip entry %q is a symbolic link (rejected)", sanitizeDisplayPath(f.Name))
	}

	// CreatorVersion high byte 3 = Unix, 19 = macOS (Darwin). Inspect raw S_IFMT
	// bits so unrecognized special types cannot masquerade as regular files.
	creatorOS := f.CreatorVersion >> 8
	if creatorOS == 3 || creatorOS == 19 {
		const (
			sIFMT  = 0o170000
			sIFDIR = 0o040000
			sIFREG = 0o100000
			sIFLNK = 0o120000
		)
		unixType := (f.ExternalAttrs >> 16) & sIFMT
		switch unixType {
		case 0:
			// Unspecified S_IFMT; fall back to standard mode checks below.
		case sIFLNK:
			return false, fmt.Errorf("zip entry %q is a symbolic link (rejected)", sanitizeDisplayPath(f.Name))
		case sIFDIR:
			return true, nil
		case sIFREG:
			if strings.HasSuffix(f.Name, "/") || strings.HasSuffix(f.Name, `\`) {
				return false, fmt.Errorf("zip entry %q has conflicting regular-file and directory attributes", sanitizeDisplayPath(f.Name))
			}
		default:
			return false, fmt.Errorf("zip entry %q has non-regular unix file type %#o (rejected)", sanitizeDisplayPath(f.Name), unixType)
		}
	}

	if mode.IsDir() {
		return true, nil
	}
	if !mode.IsRegular() {
		return false, fmt.Errorf("zip entry %q has non-regular mode %s (rejected)", sanitizeDisplayPath(f.Name), mode)
	}
	return false, nil
}

func validateZipEntryPath(rawName string, isDir bool) (leaf string, err error) {
	disp := sanitizeDisplayPath(rawName)
	if rawName == "" {
		return "", errors.New("zip contains entry with empty path")
	}
	if hasControlChars(rawName) {
		return "", fmt.Errorf("zip entry %q contains control characters", disp)
	}
	if strings.HasPrefix(rawName, "/") || strings.HasPrefix(rawName, `\`) || filepath.IsAbs(rawName) {
		return "", fmt.Errorf("zip entry %q is an absolute path", disp)
	}
	if hasWindowsDrivePrefix(rawName) || filepath.VolumeName(rawName) != "" {
		return "", fmt.Errorf("zip entry %q is a drive-qualified path", disp)
	}
	if strings.Contains(rawName, `\`) {
		return "", fmt.Errorf("zip entry %q contains backslash path separator", disp)
	}
	if !isDir && strings.HasSuffix(rawName, "/") {
		return "", fmt.Errorf("non-directory zip entry %q ends with '/'", disp)
	}

	trimmed := rawName
	if isDir {
		trimmed = strings.TrimSuffix(trimmed, "/")
		if trimmed == "" || trimmed == "." {
			return "", nil
		}
	}
	for strings.HasPrefix(trimmed, "./") {
		trimmed = strings.TrimPrefix(trimmed, "./")
	}
	if trimmed == "" || trimmed == "." {
		if isDir {
			return "", nil
		}
		return "", fmt.Errorf("zip entry %q has no file name", disp)
	}

	parts := strings.Split(trimmed, "/")
	for _, seg := range parts {
		if seg == "" {
			return "", fmt.Errorf("zip entry %q contains an empty path segment", disp)
		}
		if seg == "." || seg == ".." {
			return "", fmt.Errorf("zip entry %q contains %q path traversal segment", disp, seg)
		}
		if strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") {
			return "", fmt.Errorf("zip entry %q has segment %q with trailing dot or space", disp, seg)
		}
		if isWindowsReservedName(seg) {
			return "", fmt.Errorf("zip entry %q uses reserved device name %q", disp, seg)
		}
	}

	if isDir {
		return "", nil
	}

	leaf = parts[len(parts)-1]
	if err := validateSafeLeafName(leaf); err != nil {
		return "", fmt.Errorf("zip entry %q: %w", disp, err)
	}
	return leaf, nil
}

func validateDestFilePath(dest string) (dir, leaf string, err error) {
	if strings.TrimSpace(dest) == "" {
		return "", "", errors.New("empty destination file path")
	}
	if hasControlChars(dest) {
		return "", "", errors.New("destination path contains control characters")
	}
	if strings.HasSuffix(dest, "/") || strings.HasSuffix(dest, `\`) {
		return "", "", fmt.Errorf("destination path %q points to a directory", dest)
	}
	if hasWindowsDrivePrefix(dest) {
		if runtime.GOOS != "windows" {
			return "", "", fmt.Errorf("destination path %q contains a Windows drive prefix", dest)
		}
		if len(dest) < 3 || (dest[2] != '\\' && dest[2] != '/') {
			return "", "", fmt.Errorf("destination path %q is a drive-relative path", dest)
		}
	}
	if runtime.GOOS != "windows" && strings.Contains(dest, `\`) {
		return "", "", fmt.Errorf("destination path %q contains backslash path separator", dest)
	}
	if containsDotDotSegment(dest) {
		return "", "", fmt.Errorf("destination path %q contains '..' traversal", dest)
	}

	clean := filepath.Clean(dest)
	dir = filepath.Dir(clean)
	leaf = filepath.Base(clean)
	if err := validateSafeLeafName(leaf); err != nil {
		return "", "", fmt.Errorf("destination path %q: %w", dest, err)
	}
	return dir, leaf, nil
}

func validateSafeLeafName(leaf string) error {
	if leaf == "" || leaf == "." || leaf == ".." {
		return fmt.Errorf("invalid file name %q", leaf)
	}
	if hasControlChars(leaf) {
		return fmt.Errorf("file name %q contains control characters", sanitizeDisplayPath(leaf))
	}
	if strings.ContainsAny(leaf, `/\:`) {
		return fmt.Errorf("file name %q contains path or drive separator", leaf)
	}
	if strings.HasSuffix(leaf, ".") || strings.HasSuffix(leaf, " ") {
		return fmt.Errorf("file name %q has trailing dot or space", leaf)
	}
	if isWindowsReservedName(leaf) {
		return fmt.Errorf("file name %q is a reserved device name", leaf)
	}
	return nil
}

func containsDotDotSegment(p string) bool {
	norm := strings.ReplaceAll(p, `\`, "/")
	for _, seg := range strings.Split(norm, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func hasWindowsDrivePrefix(p string) bool {
	if len(p) < 2 || p[1] != ':' {
		return false
	}
	c := p[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isWindowsReservedName(seg string) bool {
	stem := seg
	if dot := strings.IndexByte(stem, '.'); dot >= 0 {
		stem = stem[:dot]
	}
	stem = strings.TrimSpace(strings.ToUpper(stem))
	switch stem {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

func hasControlChars(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func sanitizeDisplayPath(s string) string {
	if !hasControlChars(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			b.WriteByte('?')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func ensureDirectChild(parentDir, childPath, expectedLeaf string) error {
	rel, err := filepath.Rel(parentDir, childPath)
	if err != nil {
		return fmt.Errorf("destination %q escapes output directory: %w", childPath, err)
	}
	if rel != expectedLeaf || rel == "." || rel == ".." ||
		strings.Contains(rel, "/") || strings.Contains(rel, `\`) ||
		filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return fmt.Errorf("destination %q escapes output directory %q", childPath, parentDir)
	}
	return nil
}

func evalSymlinksAllowMissing(absPath string) (string, error) {
	cur := filepath.Clean(absPath)
	var missing []string
	for {
		_, err := os.Lstat(cur)
		if err == nil {
			realCur, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", err
			}
			for i := len(missing) - 1; i >= 0; i-- {
				realCur = filepath.Join(realCur, missing[i])
			}
			return realCur, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		missing = append(missing, filepath.Base(cur))
		cur = parent
	}
}

func matchGlobFold(pattern, name string) bool {
	p := strings.ToLower(pattern)
	n := strings.ToLower(name)
	if ok, _ := filepath.Match(p, n); ok {
		return true
	}
	return strings.Contains(n, strings.Trim(p, "*"))
}

func humanBytes(b int64) string { return HumanBytes(b) }

// HumanBytes formats a byte size as KB/MB/GB.
func HumanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
