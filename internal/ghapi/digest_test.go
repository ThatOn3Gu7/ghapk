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

	"github.com/ghapk/ghapk/internal/apk"
)

func TestDownloadTrustedDigestVerification(t *testing.T) {
	if DigestStatusFor("") != apk.DigestUnverified {
		t.Fatal("empty digest must report DigestUnverified")
	}
	if DigestStatusFor("null") != apk.DigestUnverified {
		t.Fatal("'null' digest must report DigestUnverified")
	}
	validHex := strings.Repeat("a", 64)
	if DigestStatusFor("sha256:"+validHex) != apk.DigestVerified {
		t.Fatal("valid sha256 digest must report DigestVerified")
	}

	if runtime.GOOS == "windows" {
		return
	}

	payload := "AUTHENTIC_ARTIFACT_PAYLOAD"
	sum := sha256.Sum256([]byte(payload))
	wantHex := hex.EncodeToString(sum[:])

	script := filepath.Join(t.TempDir(), "fake-gh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' '"+payload+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("matching published artifact digest succeeds", func(t *testing.T) {
		outDir := t.TempDir()
		dest := filepath.Join(outDir, "art.zip")
		opts := defaultStreamOpts()
		opts.ghBin = script
		err := downloadArtifactWithOpts(opts, "acme/rocket", Artifact{
			ID:          42,
			Name:        "app-debug",
			SizeInBytes: int64(len(payload)),
			Digest:      "sha256:" + wantHex,
		}, dest, nil)
		if err != nil {
			t.Fatalf("unexpected error on matching digest: %v", err)
		}
		got, err := os.ReadFile(dest)
		if err != nil || string(got) != payload {
			t.Fatalf("downloaded content = %q (%v), want %q", got, err, payload)
		}
	})

	t.Run("mismatched published artifact digest aborts atomically", func(t *testing.T) {
		outDir := t.TempDir()
		dest := filepath.Join(outDir, "art.zip")
		opts := defaultStreamOpts()
		opts.ghBin = script
		err := downloadArtifactWithOpts(opts, "acme/rocket", Artifact{
			ID:          42,
			Name:        "app-debug",
			SizeInBytes: int64(len(payload)),
			Digest:      "sha256:" + strings.Repeat("f", 64),
		}, dest, nil)
		if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
			t.Fatalf("expected SHA-256 mismatch error, got %v", err)
		}
		if _, statErr := os.Stat(dest); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("corrupted/mismatched file must never be published to dest: %v", statErr)
		}
		assertNoTempPartFiles(t, outDir)
	})

	t.Run("mismatched published release asset digest aborts atomically", func(t *testing.T) {
		outDir := t.TempDir()
		opts := defaultStreamOpts()
		opts.ghBin = script
		err := downloadReleaseAssetWithOpts(opts, "acme/rocket", ReleaseAsset{
			ID:     7,
			Name:   "app-release.apk",
			Size:   int64(len(payload)),
			Digest: "sha256:" + strings.Repeat("1", 64),
		}, outDir, nil)
		if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
			t.Fatalf("expected SHA-256 mismatch error, got %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(outDir, "app-release.apk")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("mismatched release asset must never be published: %v", statErr)
		}
		assertNoTempPartFiles(t, outDir)
	})
}
