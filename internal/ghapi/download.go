package ghapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ghapk/ghapk/internal/apk"
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

	// defaultMaxNestedDepth bounds nested .zip archives inspected in memory.
	defaultMaxNestedDepth = 2

	// defaultMaxNestedZipBytes caps a single nested .zip entry and total nested
	// bytes buffered in memory during nested archive inspection.
	defaultMaxNestedZipBytes = int64(256) << 20 // 256 MiB
)

// ErrDownloadStalled is returned when a download stream receives no bytes for
// longer than the configured stall timeout.
var ErrDownloadStalled = errors.New("download stalled")

func defaultStallTimeout() time.Duration {
	if s := strings.TrimSpace(os.Getenv("STALL_TIMEOUT")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 180 * time.Second
}

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
	ctx            context.Context
	maxBytes       int64
	stallTimeout   time.Duration
	ghBin          string
	fs             atomicFS
	expectedDigest string
	skipDigest     bool
}

func defaultStreamOpts() streamOpts {
	return streamOpts{
		maxBytes:     defaultMaxDownloadBytes,
		stallTimeout: defaultStallTimeout(),
		ghBin:        "gh",
		fs:           defaultAtomicFS(),
	}
}

type extractOpts struct {
	ctx            context.Context
	maxEntries     int
	maxAPKBytes    int64
	maxNestedDepth int
	maxNestedBytes int64
	excludePattern string
	choose         func([]APKCandidate) (int, error)
	fs             atomicFS
}

func defaultExtractOpts() extractOpts {
	return extractOpts{
		maxEntries:     defaultMaxZipEntries,
		maxAPKBytes:    defaultMaxExtractedBytes,
		maxNestedDepth: defaultMaxNestedDepth,
		maxNestedBytes: defaultMaxNestedZipBytes,
		fs:             defaultAtomicFS(),
	}
}

// DownloadArtifactCachedWithDigest behaves like DownloadArtifactCached while
// allowing trusted local settings to disable digest verification when verifyDigest is false.
func DownloadArtifactCachedWithDigest(repo string, art Artifact, destZip string, allowCache, verifyDigest bool, prog Progress) (cached bool, err error) {
	checkArt := art
	if !verifyDigest {
		checkArt.Digest = ""
	}
	if allowCache && CanReuseArtifactZip(destZip, checkArt) {
		if fi, statErr := os.Lstat(destZip); statErr == nil && prog != nil {
			prog(fi.Size(), fi.Size())
		}
		return true, nil
	}
	opts := defaultStreamOpts()
	opts.skipDigest = !verifyDigest
	return false, downloadArtifactWithOpts(opts, repo, art, destZip, prog)
}

// DownloadArtifactCached reuses destZip when allowCache is true and destZip
// passes path, regular-file, ZIP-integrity, and published SHA-256 digest checks;
// otherwise it downloads atomically.
func DownloadArtifactCached(repo string, art Artifact, destZip string, allowCache bool, prog Progress) (cached bool, err error) {
	return DownloadArtifactCachedWithDigest(repo, art, destZip, allowCache, true, prog)
}

// CanReuseArtifactZip reports whether destZip is an existing, safe regular ZIP
// file whose SHA-256 matches art.Digest (when GitHub published a digest).
func CanReuseArtifactZip(destZip string, art Artifact) bool {
	return CanReuseArtifactZipContext(context.Background(), destZip, art)
}
func CanReuseArtifactZipContext(ctx context.Context, destZip string, art Artifact) bool {
	if ctx.Err() != nil {
		return false
	}
	if _, _, err := validateDestFilePath(destZip); err != nil {
		return false
	}
	fi, err := os.Lstat(destZip)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() <= 0 {
		return false
	}
	if _, present, derr := apk.NormalizeExpectedSHA256(art.Digest); derr != nil {
		return false
	} else if present {
		st, _, verr := apk.VerifyExpectedSHA256Context(ctx, destZip, art.Digest)
		if verr != nil || st != apk.DigestVerified {
			return false
		}
	}
	zr, err := openZipReader(destZip)
	if err != nil {
		return false
	}
	count := len(zr.File)
	_ = zr.Close()
	return count > 0 && count <= defaultMaxZipEntries
}

// DownloadReleaseAssetCached reuses the resolved destination file when
// allowCache is true and the file passes regular-file, size, and published
// SHA-256 digest checks; otherwise it streams atomically.
func DownloadReleaseAssetCached(repo string, asset ReleaseAsset, dest string, allowCache bool, prog Progress) (resolved string, cached bool, err error) {
	resolved, err = ResolveReleaseAssetDest(dest, asset.Name)
	if err != nil {
		return "", false, err
	}
	if allowCache && CanReuseReleaseAsset(resolved, asset) {
		if fi, statErr := os.Lstat(resolved); statErr == nil && prog != nil {
			prog(fi.Size(), fi.Size())
		}
		return resolved, true, nil
	}
	if err := DownloadReleaseAsset(repo, asset, resolved, prog); err != nil {
		return "", false, err
	}
	return resolved, false, nil
}

// CanReuseReleaseAsset reports whether targetPath is an existing regular file
// matching asset.Size and asset.Digest (when published).
func CanReuseReleaseAsset(targetPath string, asset ReleaseAsset) bool {
	return CanReuseReleaseAssetContext(context.Background(), targetPath, asset)
}
func CanReuseReleaseAssetContext(ctx context.Context, targetPath string, asset ReleaseAsset) bool {
	if ctx.Err() != nil {
		return false
	}
	if _, _, err := validateDestFilePath(targetPath); err != nil {
		return false
	}
	fi, err := os.Lstat(targetPath)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() <= 0 {
		return false
	}
	if asset.Size > 0 && fi.Size() != asset.Size {
		return false
	}
	if _, present, derr := apk.NormalizeExpectedSHA256(asset.Digest); derr != nil {
		return false
	} else if present {
		st, _, verr := apk.VerifyExpectedSHA256Context(ctx, targetPath, asset.Digest)
		if verr != nil || st != apk.DigestVerified {
			return false
		}
	}
	return true
}

// DownloadArtifact streams the zip for an actions artifact to destZip atomically.
func DownloadArtifact(repo string, art Artifact, destZip string, prog Progress) error {
	return downloadArtifactWithOpts(defaultStreamOpts(), repo, art, destZip, prog)
}

func downloadArtifactWithOpts(opts streamOpts, repo string, art Artifact, destZip string, prog Progress) error {
	if !opts.skipDigest && opts.expectedDigest == "" {
		opts.expectedDigest = art.Digest
	}
	endpoint := fmt.Sprintf("repos/%s/actions/artifacts/%d/zip", repo, art.ID)
	return streamWithOpts(opts, endpoint, destZip, art.SizeInBytes, prog, "api", endpoint)
}

// DownloadReleaseAsset streams one asset of a release to dest atomically after
// validating both the release asset metadata name and the destination path.
func DownloadReleaseAsset(repo string, asset ReleaseAsset, dest string, prog Progress) error {
	return downloadReleaseAssetWithOpts(defaultStreamOpts(), repo, asset, dest, prog)
}

func downloadReleaseAssetWithOpts(opts streamOpts, repo string, asset ReleaseAsset, dest string, prog Progress) error {
	resolvedDest, err := ResolveReleaseAssetDest(dest, asset.Name)
	if err != nil {
		return err
	}
	if !opts.skipDigest && opts.expectedDigest == "" {
		opts.expectedDigest = asset.Digest
	}
	endpoint := fmt.Sprintf("repos/%s/releases/assets/%d", repo, asset.ID)
	return streamWithOpts(opts, endpoint, resolvedDest, asset.Size, prog,
		"api", "-H", "Accept: application/octet-stream", endpoint)
}

// DigestStatusFor returns DigestVerified when a valid expected digest string is
// present (having passed DownloadArtifact / DownloadReleaseAsset verification),
// or DigestUnverified when GitHub published no digest.
func DigestStatusFor(publishedDigest string) apk.DigestStatus {
	_, present, err := apk.NormalizeExpectedSHA256(publishedDigest)
	if err == nil && present {
		return apk.DigestVerified
	}
	return apk.DigestUnverified
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

	var wantDigest string
	var checkDigest bool
	if !opts.skipDigest {
		var derr error
		wantDigest, checkDigest, derr = apk.NormalizeExpectedSHA256(opts.expectedDigest)
		if derr != nil {
			return fmt.Errorf("gh %s: %w", endpoint, derr)
		}
	}

	if opts.ghBin == "gh" {
		if !Available() {
			return ErrNoGH
		}
	} else if _, err := exec.LookPath(opts.ghBin); err != nil {
		return ErrNoGH
	}

	return writeFileAtomicInDir(opts.fs, dir, leaf, func(w io.Writer) error {
		parent := opts.ctx
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, opts.ghBin, ghArgs...)
		cmd.WaitDelay = time.Second
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

		hasher := sha256.New()
		var lastByteNano atomic.Int64
		lastByteNano.Store(time.Now().UnixNano())
		var didStall atomic.Bool
		stallDone := make(chan struct{})
		defer close(stallDone)
		// An inherited pipe held by a child must not keep a cancelled read stuck.
		go func() {
			select {
			case <-ctx.Done():
				_ = stdout.Close()
			case <-stallDone:
			}
		}()
		if opts.stallTimeout > 0 {
			pollDur := 500 * time.Millisecond
			if opts.stallTimeout/4 > 0 && opts.stallTimeout/4 < pollDur {
				pollDur = opts.stallTimeout / 4
			}
			if pollDur < 10*time.Millisecond {
				pollDur = 10 * time.Millisecond
			}
			go func() {
				tk := time.NewTicker(pollDur)
				defer tk.Stop()
				for {
					select {
					case <-stallDone:
						return
					case <-ctx.Done():
						return
					case <-tk.C:
						if time.Since(time.Unix(0, lastByteNano.Load())) > opts.stallTimeout {
							didStall.Store(true)
							cancel()
							return
						}
					}
				}
			}()
		}

		buf := make([]byte, 128*1024)
		var got int64
		tick := time.Now()
		for {
			n, rerr := stdout.Read(buf)
			if n > 0 {
				lastByteNano.Store(time.Now().UnixNano())
				if int64(n) > opts.maxBytes-got {
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
				_, _ = hasher.Write(buf[:n])
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
				if didStall.Load() {
					return fmt.Errorf("gh %s: %w (no bytes for %s)", endpoint, ErrDownloadStalled, opts.stallTimeout)
				}
				return fmt.Errorf("gh %s: reading stream: %w", endpoint, rerr)
			}
		}

		reaped = true
		if err := cmd.Wait(); err != nil {
			if didStall.Load() {
				return fmt.Errorf("gh %s: %w (no bytes for %s)", endpoint, ErrDownloadStalled, opts.stallTimeout)
			}
			msg := firstLine(stderr.String())
			if msg != "" {
				return fmt.Errorf("gh %s: %s: %w", endpoint, msg, err)
			}
			return fmt.Errorf("gh %s: %w", endpoint, err)
		}
		if got == 0 {
			return fmt.Errorf("gh %s: empty response (artifact may have expired)", endpoint)
		}
		if checkDigest {
			gotDigest := hex.EncodeToString(hasher.Sum(nil))
			if !strings.EqualFold(gotDigest, wantDigest) {
				return fmt.Errorf(
					"gh %s: SHA-256 mismatch against published digest (expected %.12s…, got %.12s…)",
					endpoint, wantDigest, gotDigest,
				)
			}
		}
		if opts.ctx != nil && opts.ctx.Err() != nil {
			return opts.ctx.Err()
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

// ExtractedAPK is what we pulled out of an artifact zip (or downloaded release asset).
type ExtractedAPK struct {
	Path         string
	Name         string
	Size         int64
	Score        int
	Candidate    bool           // false for files inside the zip that lost the ranking
	Meta         apk.Inspection // parsed AndroidManifest.xml + signing cert + local SHA-256
	DigestStatus apk.DigestStatus
}

func openZipReader(zipPath string) (*zip.ReadCloser, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		if r != nil {
			_ = r.Close()
		}
		return nil, err
	}
	return r, nil
}

// InspectZip lists the contents without extracting (for the UI detail pane).
func InspectZip(zipPath string) ([]string, error) {
	return inspectZipWithMax(zipPath, defaultMaxZipEntries)
}

func inspectZipWithMax(zipPath string, maxEntries int) ([]string, error) {
	r, err := openZipReader(zipPath)
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

func openZipBytesReader(data []byte) (*zip.Reader, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, err
	}
	return r, nil
}

// ExtractBestAPK mirrors step_download + the in-zip ranking: validate archive
// entries, enforce architecture/variant policy, rank allowed .apk files (plus
// files matching filePattern), and atomically publish the winner in outDir.
func ExtractBestAPK(zipPath, outDir, filePattern string, p score.Prefs, dev score.Device) (ExtractedAPK, error) {
	return extractBestAPKWithOpts(zipPath, outDir, filePattern, p, dev, defaultExtractOpts())
}

// ExtractBestAPKFiltered behaves like ExtractBestAPK while also applying an
// optional comma-separated excludePattern glob list.
func ExtractBestAPKFiltered(zipPath, outDir, filePattern, excludePattern string, p score.Prefs, dev score.Device) (ExtractedAPK, error) {
	opts := defaultExtractOpts()
	opts.excludePattern = excludePattern
	return extractBestAPKWithOpts(zipPath, outDir, filePattern, p, dev, opts)
}

// APKCandidate is a policy-allowed archive member presented when the best
// rank is tied. Names are display-sanitized; extraction still uses the
// validated original entry.
type APKCandidate struct {
	Name  string
	Size  uint64
	Score int
}

type zipCandidate struct {
	f     *zip.File
	leaf  string
	score int
}

func extractBestAPKWithOpts(zipPath, outDir, filePattern string, p score.Prefs, dev score.Device, opts extractOpts) (ExtractedAPK, error) {
	if opts.maxEntries <= 0 {
		opts.maxEntries = defaultMaxZipEntries
	}
	if opts.maxAPKBytes <= 0 {
		opts.maxAPKBytes = defaultMaxExtractedBytes
	}
	if opts.maxNestedDepth < 0 {
		opts.maxNestedDepth = 0
	}
	if opts.maxNestedBytes <= 0 {
		opts.maxNestedBytes = defaultMaxNestedZipBytes
	}
	if opts.fs.createTemp == nil {
		opts.fs = defaultAtomicFS()
	}
	if err := validateOutDir(outDir); err != nil {
		return ExtractedAPK{}, err
	}

	r, err := openZipReader(zipPath)
	if err != nil {
		return ExtractedAPK{}, fmt.Errorf("not a zip: %w", err)
	}
	defer r.Close()

	entriesLeft := opts.maxEntries
	nestedBytesLeft := opts.maxNestedBytes
	var (
		cands          []zipCandidate
		rejectedPolicy int
	)

	var scanEntries func(files []*zip.File, depth int) error
	scanEntries = func(files []*zip.File, depth int) error {
		if len(files) > entriesLeft {
			return fmt.Errorf("zip archive has more than %d entries (exceeds limit of %d)", opts.maxEntries, opts.maxEntries)
		}
		entriesLeft -= len(files)

		for _, f := range files {
			if opts.ctx != nil && opts.ctx.Err() != nil {
				return opts.ctx.Err()
			}
			isDir, err := validateZipEntryMode(f)
			if err != nil {
				return err
			}
			leaf, err := validateZipEntryPath(f.Name, isDir)
			if err != nil {
				return err
			}
			if isDir {
				continue
			}

			lower := strings.ToLower(leaf)
			if opts.excludePattern != "" && matchAnyGlobFold(opts.excludePattern, leaf) {
				continue
			}

			// Nested .zip archive inspection through the same hardened pipeline.
			if strings.HasSuffix(lower, ".zip") && depth < opts.maxNestedDepth {
				capBytes := opts.maxNestedBytes
				if nestedBytesLeft < capBytes {
					capBytes = nestedBytesLeft
				}
				if capBytes <= 0 || f.UncompressedSize64 > uint64(capBytes) {
					return fmt.Errorf("nested zip %q exceeds size limit (%d bytes)", sanitizeDisplayPath(f.Name), opts.maxNestedBytes)
				}
				rc, oerr := f.Open()
				if oerr != nil {
					return fmt.Errorf("opening nested zip %q: %w", sanitizeDisplayPath(f.Name), oerr)
				}
				var nbuf bytes.Buffer
				var reader io.Reader = rc
				if opts.ctx != nil {
					reader = cancelReader{opts.ctx, rc}
				}
				n, cerr := copyBounded(&nbuf, reader, capBytes)
				_ = rc.Close()
				if cerr != nil {
					return fmt.Errorf("reading nested zip %q: %w", sanitizeDisplayPath(f.Name), cerr)
				}
				if uint64(n) != f.UncompressedSize64 {
					return fmt.Errorf("nested zip %q size mismatch (read %d bytes, header declared %d)",
						sanitizeDisplayPath(f.Name), n, f.UncompressedSize64)
				}
				nestedBytesLeft -= n
				nr, zerr := openZipBytesReader(nbuf.Bytes())
				if zerr != nil {
					return fmt.Errorf("corrupt nested zip %q: %w", sanitizeDisplayPath(f.Name), zerr)
				}
				if err := scanEntries(nr.File, depth+1); err != nil {
					return err
				}
				continue
			}

			isAPK := strings.HasSuffix(lower, ".apk")
			matchesPattern := !isAPK && filePattern != "" && matchAnyGlobFold(filePattern, leaf)
			if isAPK && filePattern != "" && !strings.EqualFold(strings.TrimSpace(filePattern), "*.apk") && !matchAnyGlobFold(filePattern, leaf) {
				continue
			}
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
			cands = append(cands, zipCandidate{f: f, leaf: leaf, score: sc})
		}
		return nil
	}

	if err := scanEntries(r.File, 0); err != nil {
		return ExtractedAPK{}, err
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
	if opts.choose != nil && len(cands) > 1 && cands[0].score == cands[1].score {
		choices := make([]APKCandidate, len(cands))
		for i, c := range cands {
			choices[i] = APKCandidate{Name: sanitizeDisplayPath(c.f.Name), Size: c.f.UncompressedSize64, Score: c.score}
		}
		idx, err := opts.choose(choices)
		if err != nil {
			return ExtractedAPK{}, err
		}
		if idx < 0 || idx >= len(cands) {
			return ExtractedAPK{}, errors.New("no APK selected")
		}
		best = cands[idx]
	}

	dest, err := SafeDestinationPath(outDir, best.leaf)
	if err != nil {
		return ExtractedAPK{}, err
	}
	written, err := extractOneLimitedContext(opts.ctx, opts.fs, best.f, outDir, best.leaf, opts.maxAPKBytes)
	if err != nil {
		return ExtractedAPK{}, err
	}
	var meta apk.Inspection
	if opts.ctx != nil {
		meta, err = apk.InspectContext(opts.ctx, dest)
		if err != nil && opts.ctx.Err() != nil {
			return ExtractedAPK{}, opts.ctx.Err()
		}
	} else {
		meta, _ = apk.Inspect(dest)
	}
	return ExtractedAPK{
		Path:         dest,
		Name:         best.leaf,
		Size:         written,
		Score:        best.score,
		Candidate:    true,
		Meta:         meta,
		DigestStatus: apk.DigestUnverified,
	}, nil
}

func copyBounded(w io.Writer, r io.Reader, maxBytes int64) (int64, error) {
	if maxBytes <= 0 {
		return 0, errors.New("invalid maximum byte limit")
	}
	limit := maxBytes
	if maxBytes < math.MaxInt64 {
		limit = maxBytes + 1
	}
	n, err := io.Copy(w, io.LimitReader(r, limit))
	if n > maxBytes {
		return n, fmt.Errorf("exceeds maximum extracted size (%d bytes)", maxBytes)
	}
	return n, err
}

func extractOneLimited(fs atomicFS, f *zip.File, outDir, leaf string, maxBytes int64) (int64, error) {
	return extractOneLimitedContext(nil, fs, f, outDir, leaf, maxBytes)
}

type cancelReader struct {
	ctx context.Context
	r   io.Reader
}

func (r cancelReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func extractOneLimitedContext(ctx context.Context, fs atomicFS, f *zip.File, outDir, leaf string, maxBytes int64) (int64, error) {
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
		var reader io.Reader = rc
		if ctx != nil {
			reader = cancelReader{ctx, rc}
		}
		n, copyErr := copyBounded(w, reader, maxBytes)
		if copyErr == nil && ctx != nil {
			copyErr = ctx.Err()
		}
		closeErr := rc.Close()
		written = n
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

func validateOutDir(outDir string) error {
	if strings.TrimSpace(outDir) == "" {
		return errors.New("empty output directory")
	}
	if hasControlChars(outDir) {
		return errors.New("output directory contains control characters")
	}
	if hasWindowsDrivePrefix(outDir) {
		if runtime.GOOS != "windows" {
			return fmt.Errorf("output directory %q contains a Windows drive prefix", outDir)
		}
		if len(outDir) < 3 || (outDir[2] != '\\' && outDir[2] != '/') {
			return fmt.Errorf("output directory %q is a drive-relative path", outDir)
		}
	}
	if runtime.GOOS != "windows" && strings.Contains(outDir, `\`) {
		return fmt.Errorf("output directory %q contains backslash path separator", outDir)
	}
	return nil
}

// SafeDestinationPath validates leaf as a single safe file name and verifies
// that placing leaf inside outDir remains a direct child of outDir (both
// lexically and after resolving directory symlinks).
func SafeDestinationPath(outDir, leaf string) (string, error) {
	if err := validateOutDir(outDir); err != nil {
		return "", err
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

func isUnsafeControlOrBidiRune(r rune) bool {
	if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
		return true
	}
	// Reject Unicode bidirectional override/isolate controls used in filename spoofing.
	return (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}

func hasControlChars(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	for _, r := range s {
		if isUnsafeControlOrBidiRune(r) {
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
		if isUnsafeControlOrBidiRune(r) {
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

// matchGlobFold is path.Match against a lowercased leaf name and pattern,
// falling back to substring match only when pattern has invalid glob syntax.
func matchGlobFold(pattern, name string) bool {
	p := strings.ToLower(strings.TrimSpace(pattern))
	if p == "" {
		return false
	}
	n := strings.ToLower(name)
	ok, err := path.Match(p, n)
	if err != nil {
		sub := strings.Trim(p, "*")
		return sub != "" && strings.Contains(n, sub)
	}
	return ok
}

// matchAnyGlobFold matches name against a comma-separated list of globs.
func matchAnyGlobFold(patterns, name string) bool {
	for _, part := range strings.Split(patterns, ",") {
		part = strings.TrimSpace(part)
		if part != "" && matchGlobFold(part, name) {
			return true
		}
	}
	return false
}

// RankedReleaseAsset pairs a ReleaseAsset with its parent Release metadata and
// computed score/policy flags.
type RankedReleaseAsset struct {
	ReleaseTag  string
	ReleaseName string
	PublishedAt time.Time
	Prerelease  bool
	HTMLURL     string
	Asset       ReleaseAsset
}

// RankReleaseAssets scores .apk and .zip assets across recent GitHub Releases
// using the exact same architecture/variant policy as workflow artifacts.
func RankReleaseAssets(rels []Release, artifactPattern, filePattern, excludePattern string, p score.Prefs, dev score.Device) []RankedReleaseAsset {
	var out []RankedReleaseAsset
	for _, rel := range rels {
		for _, a := range rel.Assets {
			if validateSafeLeafName(a.Name) != nil {
				continue
			}
			low := strings.ToLower(a.Name)
			isAPK := strings.HasSuffix(low, ".apk")
			isZip := strings.HasSuffix(low, ".zip")
			matchesFile := filePattern != "" && matchAnyGlobFold(filePattern, a.Name)
			if !isAPK && !isZip && !matchesFile {
				continue
			}
			if artifactPattern != "" && !matchAnyGlobFold(artifactPattern, a.Name) {
				continue
			}
			if excludePattern != "" && matchAnyGlobFold(excludePattern, a.Name) {
				continue
			}
			a.Score = score.APKRank(a.Name, p, dev)
			if isZip {
				a.Score -= 5 // direct .apk beats a .zip of equal rank
			}
			a.Rejected = !score.AllowedByPolicy(a.Name, p, dev)
			out = append(out, RankedReleaseAsset{
				ReleaseTag:  rel.TagName,
				ReleaseName: rel.Name,
				PublishedAt: rel.PublishedAt,
				Prerelease:  rel.Prerelease,
				HTMLURL:     rel.HTMLURL,
				Asset:       a,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Asset.Rejected != out[j].Asset.Rejected {
			return !out[i].Asset.Rejected
		}
		if out[i].PublishedAt.Equal(out[j].PublishedAt) {
			return out[i].Asset.Score > out[j].Asset.Score
		}
		if out[i].Asset.Score != out[j].Asset.Score {
			return out[i].Asset.Score > out[j].Asset.Score
		}
		return out[i].PublishedAt.After(out[j].PublishedAt)
	})
	return out
}

// FetchReleaseAPK downloads (or reuses cached) a GitHub Release asset, verifies
// any published SHA-256 digest, and if the asset is a .zip archive extracts the
// best matching APK through the single hardened extraction pipeline.
func FetchReleaseAPK(repo string, asset ReleaseAsset, outDir, filePattern, excludePattern string, allowCache bool, p score.Prefs, dev score.Device, prog Progress) (ExtractedAPK, error) {
	return FetchReleaseAPKWithDigest(repo, asset, outDir, filePattern, excludePattern, allowCache, true, p, dev, prog)
}

// FetchReleaseAPKWithDigest behaves like FetchReleaseAPK while honoring verifyDigest.
func FetchReleaseAPKWithDigest(repo string, asset ReleaseAsset, outDir, filePattern, excludePattern string, allowCache, verifyDigest bool, p score.Prefs, dev score.Device, prog Progress) (ExtractedAPK, error) {
	checkAsset := asset
	if !verifyDigest {
		checkAsset.Digest = ""
	}
	resolved, err := ResolveReleaseAssetDest(outDir, asset.Name)
	if err != nil {
		return ExtractedAPK{}, err
	}
	if allowCache && CanReuseReleaseAsset(resolved, checkAsset) {
		if fi, statErr := os.Lstat(resolved); statErr == nil && prog != nil {
			prog(fi.Size(), fi.Size())
		}
	} else {
		opts := defaultStreamOpts()
		opts.skipDigest = !verifyDigest
		if err := downloadReleaseAssetWithOpts(opts, repo, asset, resolved, prog); err != nil {
			return ExtractedAPK{}, err
		}
	}
	ds := DigestStatusFor(checkAsset.Digest)
	if strings.HasSuffix(strings.ToLower(asset.Name), ".zip") {
		apkOut, xerr := ExtractBestAPKFiltered(resolved, outDir, filePattern, excludePattern, p, dev)
		if !allowCache {
			_ = os.Remove(resolved)
		}
		if xerr != nil {
			return ExtractedAPK{}, xerr
		}
		apkOut.DigestStatus = ds
		apkOut.Meta.DigestStatus = ds
		return apkOut, nil
	}

	fi, statErr := os.Lstat(resolved)
	if statErr != nil || !fi.Mode().IsRegular() {
		return ExtractedAPK{}, fmt.Errorf("downloaded release asset %q is not a regular file", resolved)
	}
	meta, _ := apk.Inspect(resolved)
	meta.DigestStatus = ds
	return ExtractedAPK{
		Path:         resolved,
		Name:         filepath.Base(resolved),
		Size:         fi.Size(),
		Score:        asset.Score,
		Candidate:    true,
		Meta:         meta,
		DigestStatus: ds,
	}, nil
}

// PruneAPKCache keeps at most keepCount older regular .apk/.zip files in apkDir
// (newest mtime first), never touching symlinks, directories, or any path listed
// in protect. A negative keepCount disables pruning.
func PruneAPKCache(apkDir string, keepCount int, protect ...string) {
	if keepCount < 0 {
		return
	}
	entries, err := os.ReadDir(apkDir)
	if err != nil {
		return
	}
	protected := make(map[string]bool, len(protect))
	for _, p := range protect {
		if p != "" {
			if abs, aerr := filepath.Abs(p); aerr == nil {
				protected[abs] = true
			}
			protected[filepath.Clean(p)] = true
		}
	}

	type cachedEntry struct {
		path  string
		mtime time.Time
	}
	var candidates []cachedEntry
	for _, de := range entries {
		full := filepath.Join(apkDir, de.Name())
		abs, _ := filepath.Abs(full)
		if protected[full] || (abs != "" && protected[abs]) {
			continue
		}
		low := strings.ToLower(de.Name())
		if !strings.HasSuffix(low, ".apk") && !strings.HasSuffix(low, ".zip") {
			continue
		}
		fi, lerr := os.Lstat(full)
		if lerr != nil || !fi.Mode().IsRegular() {
			continue
		}
		candidates = append(candidates, cachedEntry{path: full, mtime: fi.ModTime()})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].mtime.After(candidates[j].mtime)
	})
	for i := keepCount; i < len(candidates); i++ {
		_ = os.Remove(candidates[i].path)
	}
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

// DownloadArtifactContext and ExtractBestAPKContext are cancellable variants
// used by interactive jobs. Existing CLI APIs retain their original behavior.
func DownloadArtifactContext(ctx context.Context, repo string, art Artifact, dest string, verify bool, prog Progress) error {
	opts := defaultStreamOpts()
	opts.ctx = ctx
	opts.skipDigest = !verify
	return downloadArtifactWithOpts(opts, repo, art, dest, prog)
}
func DownloadReleaseAssetContext(ctx context.Context, repo string, asset ReleaseAsset, dest string, verify bool, prog Progress) error {
	opts := defaultStreamOpts()
	opts.ctx = ctx
	opts.skipDigest = !verify
	return downloadReleaseAssetWithOpts(opts, repo, asset, dest, prog)
}
func ExtractBestAPKContext(ctx context.Context, zipPath, outDir, filePattern, excludePattern string, p score.Prefs, dev score.Device) (ExtractedAPK, error) {
	opts := defaultExtractOpts()
	opts.ctx = ctx
	opts.excludePattern = excludePattern
	return extractBestAPKWithOpts(zipPath, outDir, filePattern, p, dev, opts)
}

// ExtractBestAPKContextChoice preserves all extraction safeguards while letting
// an interactive caller resolve a tie without guessing.
func ExtractBestAPKContextChoice(ctx context.Context, zipPath, outDir, filePattern, excludePattern string, p score.Prefs, dev score.Device, choose func([]APKCandidate) (int, error)) (ExtractedAPK, error) {
	opts := defaultExtractOpts()
	opts.ctx, opts.excludePattern, opts.choose = ctx, excludePattern, choose
	return extractBestAPKWithOpts(zipPath, outDir, filePattern, p, dev, opts)
}
