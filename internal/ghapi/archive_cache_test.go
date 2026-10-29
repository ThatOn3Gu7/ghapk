package ghapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ThatOn3Gu7/ghapk/internal/score"
)

func TestNestedZipExtractionAndSecurityGuards(t *testing.T) {
	arm64Phone := score.Device{ABIs: []string{"arm64-v8a"}, SDK: 34}
	prefs := score.Prefs{Arch: "auto", Variant: "auto"}

	t.Run("extracts highest-ranked APK from nested zip while enforcing arch policy", func(t *testing.T) {
		innerBytes := buildTestZipBytes(t, []zipEntrySpec{
			{name: "outputs/apk/debug/app-x86_64-debug.apk", body: []byte("x86-apk")},
			{name: "outputs/apk/debug/app-arm64-v8a-debug.apk", body: []byte("arm64-debug-apk")},
			{name: "outputs/apk/release/app-arm64-v8a-release.apk", body: []byte("arm64-release-apk")},
		})
		outerZip := buildTestZip(t, []zipEntrySpec{
			{name: "README.txt", body: []byte("artifact bundle")},
			{name: "nested-bundle.zip", body: innerBytes},
		})
		outDir := t.TempDir()
		got, err := ExtractBestAPK(outerZip, outDir, "", prefs, arm64Phone)
		if err != nil {
			t.Fatalf("ExtractBestAPK on nested zip failed: %v", err)
		}
		if got.Name != "app-arm64-v8a-debug.apk" {
			t.Fatalf("picked %q, want app-arm64-v8a-debug.apk", got.Name)
		}
		data, err := os.ReadFile(got.Path)
		if err != nil || string(data) != "arm64-debug-apk" {
			t.Fatalf("extracted bytes = %q (%v), want arm64-debug-apk", data, err)
		}
		assertNoTempPartFiles(t, outDir)
	})

	t.Run("rejects path traversal inside nested zip", func(t *testing.T) {
		innerEvil := buildTestZipBytes(t, []zipEntrySpec{
			{name: "../../escape.apk", body: []byte("evil")},
		})
		outerZip := buildTestZip(t, []zipEntrySpec{
			{name: "inner.zip", body: innerEvil},
		})
		outDir := t.TempDir()
		if _, err := ExtractBestAPK(outerZip, outDir, "", prefs, arm64Phone); err == nil {
			t.Fatal("expected traversal inside nested zip to be rejected")
		}
		assertNoTempPartFiles(t, outDir)
	})

	t.Run("rejects symlink entry inside nested zip", func(t *testing.T) {
		innerSym := buildTestZipBytes(t, []zipEntrySpec{
			{name: "link.apk", body: []byte("/etc/passwd"), mode: os.ModeSymlink | 0o777, hasMode: true},
		})
		outerZip := buildTestZip(t, []zipEntrySpec{
			{name: "inner.zip", body: innerSym},
		})
		outDir := t.TempDir()
		if _, err := ExtractBestAPK(outerZip, outDir, "", prefs, arm64Phone); err == nil {
			t.Fatal("expected symlink inside nested zip to be rejected")
		}
	})

	t.Run("enforces nested zip byte cap", func(t *testing.T) {
		innerBig := buildTestZipBytes(t, []zipEntrySpec{
			{name: "app-arm64-v8a-debug.apk", body: []byte(strings.Repeat("A", 4096)), hasMethod: true, method: 0},
		})
		outerZip := buildTestZip(t, []zipEntrySpec{
			{name: "inner.zip", body: innerBig},
		})
		outDir := t.TempDir()
		opts := defaultExtractOpts()
		opts.maxNestedBytes = 512
		if _, err := extractBestAPKWithOpts(outerZip, outDir, "", prefs, arm64Phone, opts); err == nil {
			t.Fatal("expected nested zip exceeding maxNestedBytes to fail")
		}
	})
}

func TestCacheReuseAndPruneSafety(t *testing.T) {
	zipPath := buildTestZip(t, []zipEntrySpec{
		{name: "app-universal-debug.apk", body: []byte("apk-bytes")},
	})
	raw, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	hexSum := hex.EncodeToString(sum[:])

	// Matching digest -> reusable
	if !CanReuseArtifactZip(zipPath, Artifact{ID: 1, Digest: "sha256:" + hexSum}) {
		t.Fatal("expected valid zip with matching digest to be reusable")
	}
	// Mismatched digest -> NOT reusable
	if CanReuseArtifactZip(zipPath, Artifact{ID: 1, Digest: "sha256:" + strings.Repeat("0", 64)}) {
		t.Fatal("tampered/mismatched digest must never be reused from cache")
	}
	// Symlink -> NOT reusable
	linkPath := filepath.Join(t.TempDir(), "sym.zip")
	if err := os.Symlink(zipPath, linkPath); err == nil {
		if CanReuseArtifactZip(linkPath, Artifact{ID: 1}) {
			t.Fatal("symlink cache entry must never be reused")
		}
	}

	// PruneAPKCache keeps newest N and protects specified path
	cacheDir := t.TempDir()
	now := time.Now()
	var paths []string
	for i := 0; i < 4; i++ {
		p := filepath.Join(cacheDir, "app-"+string(rune('a'+i))+".apk")
		if err := os.WriteFile(p, []byte("apk"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(time.Duration(i) * time.Minute)
		_ = os.Chtimes(p, mt, mt)
		paths = append(paths, p)
	}
	// Protect oldest (paths[0]) and keep 2 others (paths[3], paths[2]); paths[1] should be pruned.
	PruneAPKCache(cacheDir, 2, paths[0])
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("protected file was pruned: %v", err)
	}
	if _, err := os.Stat(paths[3]); err != nil {
		t.Fatalf("newest file was pruned: %v", err)
	}
	if _, err := os.Stat(paths[2]); err != nil {
		t.Fatalf("second-newest file was pruned: %v", err)
	}
	if _, err := os.Stat(paths[1]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected older file %s to be pruned, stat err=%v", paths[1], err)
	}
}

func TestDownloadStallTimeoutAndReleaseRanking(t *testing.T) {
	rels := []Release{
		{
			TagName:     "v1.2.0",
			PublishedAt: time.Now(),
			Assets: []ReleaseAsset{
				{ID: 10, Name: "checksums.txt", Size: 128},
				{ID: 11, Name: "app-x86_64-release.apk", Size: 10 << 20},
				{ID: 12, Name: "app-arm64-v8a-debug.apk", Size: 12 << 20},
			},
		},
	}
	ranked := RankReleaseAssets(
		rels, "", "", "*x86*",
		score.Prefs{Arch: "auto", Variant: "auto"},
		score.Device{ABIs: []string{"arm64-v8a"}, SDK: 34},
	)
	if len(ranked) != 1 || ranked[0].Asset.ID != 12 {
		t.Fatalf("unexpected release ranking: %+v", ranked)
	}

	if runtime.GOOS == "windows" {
		return
	}
	script := filepath.Join(t.TempDir(), "slow-gh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := defaultStreamOpts()
	opts.ghBin = script
	opts.stallTimeout = 80 * time.Millisecond
	dest := filepath.Join(t.TempDir(), "stalled.zip")
	err := downloadArtifactWithOpts(opts, "acme/rocket", Artifact{ID: 55, SizeInBytes: 100}, dest, nil)
	if !errors.Is(err, ErrDownloadStalled) {
		t.Fatalf("expected ErrDownloadStalled, got %v", err)
	}
}
