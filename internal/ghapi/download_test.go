package ghapi

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ThatOn3Gu7/ghapk/internal/score"
)

type zipEntrySpec struct {
	name          string
	body          []byte
	mode          os.FileMode
	hasMode       bool
	creatorVer    uint16
	externalAttrs uint32
	rawAttrs      bool
	method        uint16
	hasMethod     bool
}

func buildTestZip(t *testing.T, specs []zipEntrySpec) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "test.zip")
	if err := os.WriteFile(zipPath, buildTestZipBytes(t, specs), 0o644); err != nil {
		t.Fatalf("writing test zip: %v", err)
	}
	return zipPath
}

func buildTestZipBytes(t *testing.T, specs []zipEntrySpec) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, sp := range specs {
		hdr := &zip.FileHeader{
			Name:   sp.name,
			Method: zip.Deflate,
		}
		if sp.hasMethod {
			hdr.Method = sp.method
		}
		if sp.hasMode {
			hdr.SetMode(sp.mode)
		}
		if sp.rawAttrs {
			hdr.CreatorVersion = sp.creatorVer
			hdr.ExternalAttrs = sp.externalAttrs
		}
		if strings.HasSuffix(sp.name, "/") && !sp.rawAttrs && !sp.hasMode {
			hdr.Method = zip.Store
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("CreateHeader(%q): %v", sp.name, err)
		}
		if len(sp.body) > 0 {
			if _, err := w.Write(sp.body); err != nil {
				t.Fatalf("Write(%q): %v", sp.name, err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zw.Close: %v", err)
	}
	return buf.Bytes()
}

func assertNoTempPartFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ghapk-") || strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("leftover temporary file %q in %q", e.Name(), dir)
		}
	}
}

type faultyTempFile struct {
	f         *os.File
	failAfter int
	written   int
	writeErr  error
	closeErr  error
}

func (ft *faultyTempFile) Write(p []byte) (int, error) {
	if ft.writeErr != nil && ft.failAfter >= 0 && ft.written+len(p) > ft.failAfter {
		allow := ft.failAfter - ft.written
		if allow > 0 {
			n, _ := ft.f.Write(p[:allow])
			ft.written += n
			return n, ft.writeErr
		}
		return 0, ft.writeErr
	}
	n, err := ft.f.Write(p)
	ft.written += n
	return n, err
}

func (ft *faultyTempFile) Name() string { return ft.f.Name() }

func (ft *faultyTempFile) Close() error {
	realErr := ft.f.Close()
	if ft.closeErr != nil {
		return ft.closeErr
	}
	return realErr
}

func TestExtractBestAPK_ValidArchiveAndRanking(t *testing.T) {
	arm64Payload := []byte("PK\x03\x04fake-arm64-debug-apk-payload")
	uniPayload := []byte("PK\x03\x04fake-universal-release-apk")

	zipPath := buildTestZip(t, []zipEntrySpec{
		{name: "./", mode: os.ModeDir | 0o755, hasMode: true},
		{name: "app/build/outputs/apk/", mode: os.ModeDir | 0o755, hasMode: true},
		{name: "logs/2026-10-28T12:00:00Z/build.log", body: []byte("ok\n")},
		{name: "app/build/outputs/apk/output-metadata.json", body: []byte(`{"version":3}`)},
		{name: "app/build/outputs/apk/release/com.example.app-universal-release.apk", body: uniPayload},
		{
			name:    "./app/build/outputs/apk/debug/com.example.app-arm64-v8a-debug.apk",
			body:    arm64Payload,
			mode:    os.ModeSetuid | 0o755,
			hasMode: true,
		},
	})

	outDir := filepath.Join(t.TempDir(), "nested", "out")
	prefs := score.Prefs{Arch: "auto", Variant: "auto"}
	dev := score.Device{ABIs: []string{"arm64-v8a", "armeabi-v7a"}, SDK: 34}

	got, err := ExtractBestAPK(zipPath, outDir, "", prefs, dev)
	if err != nil {
		t.Fatalf("ExtractBestAPK failed: %v", err)
	}
	if got.Name != "com.example.app-arm64-v8a-debug.apk" {
		t.Fatalf("got.Name = %q, want com.example.app-arm64-v8a-debug.apk", got.Name)
	}
	wantPath := filepath.Join(outDir, "com.example.app-arm64-v8a-debug.apk")
	if got.Path != wantPath {
		t.Fatalf("got.Path = %q, want %q", got.Path, wantPath)
	}
	if got.Size != int64(len(arm64Payload)) {
		t.Fatalf("got.Size = %d, want %d", got.Size, len(arm64Payload))
	}
	if !got.Candidate {
		t.Errorf("got.Candidate = false, want true")
	}

	fi, err := os.Lstat(got.Path)
	if err != nil {
		t.Fatalf("Lstat(%q): %v", got.Path, err)
	}
	if runtime.GOOS != "windows" {
		if fi.Mode().Perm()&0o111 != 0 || fi.Mode()&os.ModeSetuid != 0 {
			t.Errorf("extracted APK inherited unsafe mode bits: %s", fi.Mode())
		}
	}

	data, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", got.Path, err)
	}
	if !bytes.Equal(data, arm64Payload) {
		t.Fatalf("extracted payload mismatch: got %q, want %q", data, arm64Payload)
	}
	assertNoTempPartFiles(t, outDir)
}

func TestExtractBestAPK_EnforcesSelectionPolicy(t *testing.T) {
	arm64Phone := score.Device{ABIs: []string{"arm64-v8a", "armeabi-v7a"}, SDK: 34}

	t.Run("rejects incompatible ABI when only x86_64 APK present", func(t *testing.T) {
		zipPath := buildTestZip(t, []zipEntrySpec{
			{name: "app-x86_64-debug.apk", body: []byte("x86-apk")},
		})
		outDir := t.TempDir()
		_, err := ExtractBestAPK(zipPath, outDir, "", score.Prefs{Arch: "auto", Variant: "auto"}, arm64Phone)
		if err == nil {
			t.Fatal("expected policy error for x86_64 APK on arm64 phone, got nil")
		}
		if !strings.Contains(err.Error(), "policy") {
			t.Errorf("expected policy message in error, got: %v", err)
		}
		entries, _ := os.ReadDir(outDir)
		if len(entries) != 0 {
			t.Errorf("expected empty outDir after policy rejection, found %d entries", len(entries))
		}
	})

	t.Run("rejects disallowed variant when release requested", func(t *testing.T) {
		zipPath := buildTestZip(t, []zipEntrySpec{
			{name: "app-arm64-v8a-debug.apk", body: []byte("debug-apk")},
		})
		outDir := t.TempDir()
		_, err := ExtractBestAPK(zipPath, outDir, "", score.Prefs{Arch: "auto", Variant: "release"}, arm64Phone)
		if err == nil {
			t.Fatal("expected policy error for debug APK when Variant=release, got nil")
		}
	})

	t.Run("selects policy-allowed APK among disallowed candidates", func(t *testing.T) {
		wantBody := []byte("arm64-release-apk")
		zipPath := buildTestZip(t, []zipEntrySpec{
			{name: "app-x86_64-release.apk", body: []byte("x86-release-apk")},
			{name: "app-arm64-v8a-debug.apk", body: []byte("arm64-debug-apk")},
			{name: "app-arm64-v8a-release.apk", body: wantBody},
		})
		outDir := t.TempDir()
		got, err := ExtractBestAPK(zipPath, outDir, "", score.Prefs{Arch: "auto", Variant: "release"}, arm64Phone)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != "app-arm64-v8a-release.apk" {
			t.Fatalf("got.Name = %q, want app-arm64-v8a-release.apk", got.Name)
		}
	})

	t.Run("enforces policy and exact glob semantics on filePattern matches", func(t *testing.T) {
		zipPath := buildTestZip(t, []zipEntrySpec{
			{name: "bundle-x86_64-debug.custom", body: []byte("x86-custom")},
			{name: "notes.custom.txt", body: []byte("not-a-custom-bundle")},
		})
		outDir := t.TempDir()
		_, err := ExtractBestAPK(zipPath, outDir, "*.custom", score.Prefs{Arch: "auto", Variant: "auto"}, arm64Phone)
		if err == nil {
			t.Fatal("expected policy rejection on incompatible filePattern candidate (and non-matching .txt ignored)")
		}
		if !strings.Contains(err.Error(), "policy") {
			t.Fatalf("expected policy error rather than extracting notes.custom.txt, got: %v", err)
		}
	})
}

func TestExtractBestAPK_RejectsMaliciousPaths(t *testing.T) {
	maliciousNames := []string{
		"../evil.apk",
		"../../evil.apk",
		"sub/../../evil.apk",
		"sub/../evil.apk",
		"/etc/evil.apk",
		"/evil.apk",
		`\evil.apk`,
		`\\server\share\evil.apk`,
		`..\..\evil.apk`,
		`sub\evil.apk`,
		"C:evil.apk",
		`C:\evil.apk`,
		"C:/evil.apk",
		"d:sub/evil.apk",
		"evil.apk:$DATA",
		"evil\x00.apk",
		"evil\n.apk",
		"evil\x7f.apk",
		"evil\u009b31m.apk",
		"app-\u202ekpa.exe.apk",
		".. /evil.apk",
		"evil.apk.",
		"evil.apk ",
		"NUL.apk",
		"con.apk",
		"COM1.apk",
		"lpt9.apk",
		"sub//evil.apk",
	}

	prefs := score.Prefs{Arch: "universal", Variant: "auto"}
	dev := score.Device{}

	for _, bad := range maliciousNames {
		t.Run(sanitizeDisplayPath(bad), func(t *testing.T) {
			t.Setenv("GODEBUG", "zipinsecurepath=0")
			parent := t.TempDir()
			outDir := filepath.Join(parent, "out")
			zipPath := buildTestZip(t, []zipEntrySpec{
				{name: bad, body: []byte("malicious")},
				{name: "app-universal-release.apk", body: []byte("benign")},
			})

			_, err := ExtractBestAPK(zipPath, outDir, "", prefs, dev)
			if err == nil {
				t.Fatalf("expected error for malicious zip entry %q, got nil", bad)
			}
			if strings.HasPrefix(err.Error(), "not a zip:") {
				t.Fatalf("zip.OpenReader short-circuited entry validator with %v", err)
			}

			// InspectZip must still safely list sanitized entries without leaking FDs.
			listed, ierr := InspectZip(zipPath)
			if ierr != nil {
				t.Fatalf("InspectZip failed under zipinsecurepath=0: %v", ierr)
			}
			if len(listed) != 2 {
				t.Fatalf("InspectZip returned %d entries, want 2", len(listed))
			}

			// Removing zipPath succeeds on all OSes (including Windows) only if r.Close() ran.
			if err := os.Remove(zipPath); err != nil {
				t.Fatalf("Remove(zipPath) failed (possible leaked zip file handle): %v", err)
			}

			// Verify nothing escaped into parent and nothing was published in outDir.
			parentEntries, err := os.ReadDir(parent)
			if err != nil {
				t.Fatalf("ReadDir(parent): %v", err)
			}
			for _, e := range parentEntries {
				if e.Name() != "out" {
					t.Fatalf("unexpected file %q created outside outDir for %q", e.Name(), bad)
				}
			}
			assertNoTempPartFiles(t, outDir)
		})
	}
}

func TestExtractBestAPK_RejectsSymlinksAndNonRegularEntries(t *testing.T) {
	prefs := score.Prefs{Arch: "universal", Variant: "auto"}
	dev := score.Device{}

	cases := []struct {
		label string
		entry zipEntrySpec
	}{
		{
			label: "symlink apk entry",
			entry: zipEntrySpec{
				name:    "app-universal.apk",
				body:    []byte("/etc/passwd"),
				mode:    os.ModeSymlink | 0o777,
				hasMode: true,
			},
		},
		{
			label: "unrelated symlink alongside valid apk",
			entry: zipEntrySpec{
				name:    "docs/link",
				body:    []byte("../outside"),
				mode:    os.ModeSymlink | 0o777,
				hasMode: true,
			},
		},
		{
			label: "named pipe entry",
			entry: zipEntrySpec{
				name:    "fifo.apk",
				mode:    os.ModeNamedPipe | 0o600,
				hasMode: true,
			},
		},
		{
			label: "char device entry",
			entry: zipEntrySpec{
				name:    "devnull.apk",
				mode:    os.ModeDevice | os.ModeCharDevice | 0o600,
				hasMode: true,
			},
		},
		{
			label: "socket entry",
			entry: zipEntrySpec{
				name:    "sock.apk",
				mode:    os.ModeSocket | 0o600,
				hasMode: true,
			},
		},
		{
			label: "unknown raw unix S_IFMT bits",
			entry: zipEntrySpec{
				name:          "weird.apk",
				body:          []byte("weird"),
				creatorVer:    3 << 8,
				externalAttrs: (0o160000 | 0o644) << 16,
				rawAttrs:      true,
			},
		},
		{
			label: "conflicting S_IFREG and trailing slash",
			entry: zipEntrySpec{
				name:          "app.apk/",
				creatorVer:    3 << 8,
				externalAttrs: (0o100000 | 0o644) << 16,
				rawAttrs:      true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			zipPath := buildTestZip(t, []zipEntrySpec{
				tc.entry,
				{name: "fallback-universal.apk", body: []byte("valid-apk")},
			})
			outDir := t.TempDir()
			if _, err := ExtractBestAPK(zipPath, outDir, "", prefs, dev); err == nil {
				t.Fatalf("expected %s to fail validation, got nil", tc.label)
			}
			assertNoTempPartFiles(t, outDir)
		})
	}
}

func TestExtractBestAPK_ExistingDestinationFileSymlinkAndDirectory(t *testing.T) {
	newPayload := []byte("NEW_APK_BINARY_CONTENT")
	zipPath := buildTestZip(t, []zipEntrySpec{
		{name: "app-universal.apk", body: newPayload},
	})
	prefs := score.Prefs{Arch: "universal", Variant: "auto"}
	dev := score.Device{}

	t.Run("atomically replaces existing regular file", func(t *testing.T) {
		outDir := t.TempDir()
		dest := filepath.Join(outDir, "app-universal.apk")
		if err := os.WriteFile(dest, []byte("OLD"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := ExtractBestAPK(zipPath, outDir, "", prefs, dev)
		if err != nil {
			t.Fatalf("ExtractBestAPK: %v", err)
		}
		data, err := os.ReadFile(got.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, newPayload) {
			t.Fatalf("got %q, want %q", data, newPayload)
		}
		assertNoTempPartFiles(t, outDir)
	})

	t.Run("existing destination file-symlink is not traversed", func(t *testing.T) {
		root := t.TempDir()
		outDir := filepath.Join(root, "out")
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatal(err)
		}
		outsideSecret := filepath.Join(root, "secret.txt")
		if err := os.WriteFile(outsideSecret, []byte("DO_NOT_OVERWRITE"), 0o600); err != nil {
			t.Fatal(err)
		}
		destLink := filepath.Join(outDir, "app-universal.apk")
		if err := os.Symlink(outsideSecret, destLink); err != nil {
			t.Skipf("symlinks unavailable on this platform: %v", err)
		}

		got, err := ExtractBestAPK(zipPath, outDir, "", prefs, dev)
		if err != nil {
			t.Fatalf("ExtractBestAPK: %v", err)
		}

		secretAfter, err := os.ReadFile(outsideSecret)
		if err != nil {
			t.Fatal(err)
		}
		if string(secretAfter) != "DO_NOT_OVERWRITE" {
			t.Fatalf("symlink target was modified! got %q", secretAfter)
		}

		fi, err := os.Lstat(got.Path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
			t.Fatalf("expected regular file replacing symlink, got mode %s", fi.Mode())
		}
		extracted, err := os.ReadFile(got.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(extracted, newPayload) {
			t.Fatalf("extracted = %q, want %q", extracted, newPayload)
		}
		assertNoTempPartFiles(t, outDir)
	})

	t.Run("existing destination directory-symlink is not traversed", func(t *testing.T) {
		root := t.TempDir()
		outDir := filepath.Join(root, "out")
		outsideDir := filepath.Join(root, "outside_dir")
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(outsideDir, 0o755); err != nil {
			t.Fatal(err)
		}
		destLink := filepath.Join(outDir, "app-universal.apk")
		if err := os.Symlink(outsideDir, destLink); err != nil {
			t.Skipf("symlinks unavailable on this platform: %v", err)
		}

		if _, err := ExtractBestAPK(zipPath, outDir, "", prefs, dev); err != nil {
			t.Fatalf("ExtractBestAPK: %v", err)
		}
		entries, err := os.ReadDir(outsideDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("outsideDir was written into: %v", entries)
		}
	})

	t.Run("existing real directory at destination fails cleanly", func(t *testing.T) {
		outDir := t.TempDir()
		destDir := filepath.Join(outDir, "app-universal.apk")
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := ExtractBestAPK(zipPath, outDir, "", prefs, dev); err == nil {
			t.Fatal("expected error when destination is an existing directory")
		}
		assertNoTempPartFiles(t, outDir)
	})

	t.Run("symlinked outDir extracts safely inside resolved directory", func(t *testing.T) {
		root := t.TempDir()
		realOut := filepath.Join(root, "real_out")
		if err := os.MkdirAll(realOut, 0o755); err != nil {
			t.Fatal(err)
		}
		linkOut := filepath.Join(root, "link_out")
		if err := os.Symlink(realOut, linkOut); err != nil {
			t.Skipf("symlinks unavailable on this platform: %v", err)
		}

		got, err := ExtractBestAPK(zipPath, linkOut, "", prefs, dev)
		if err != nil {
			t.Fatalf("ExtractBestAPK via symlinked outDir: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(realOut, "app-universal.apk"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, newPayload) {
			t.Fatalf("payload mismatch in realOut: %q", data)
		}
		if filepath.Base(got.Path) != "app-universal.apk" {
			t.Fatalf("unexpected got.Path: %q", got.Path)
		}
		assertNoTempPartFiles(t, realOut)
	})
}

func TestExtractBestAPK_SizeAndEntryCountLimits(t *testing.T) {
	prefs := score.Prefs{Arch: "universal", Variant: "auto"}
	dev := score.Device{}

	t.Run("rejects declared header size exceeding limit", func(t *testing.T) {
		zipPath := buildTestZip(t, []zipEntrySpec{
			{name: "app-universal.apk", body: bytes.Repeat([]byte("A"), 2048)},
		})
		outDir := t.TempDir()
		opts := defaultExtractOpts()
		opts.maxAPKBytes = 1024

		if _, err := extractBestAPKWithOpts(zipPath, outDir, "", prefs, dev, opts); err == nil {
			t.Fatal("expected size-limit error, got nil")
		}
		entries, _ := os.ReadDir(outDir)
		if len(entries) != 0 {
			t.Fatalf("expected empty outDir, got %d files", len(entries))
		}
	})

	t.Run("rejects spoofed small header size while streaming", func(t *testing.T) {
		raw := buildTestZipBytes(t, []zipEntrySpec{
			{name: "app-universal.apk", body: bytes.Repeat([]byte("Z"), 4096)},
		})
		cdIdx := bytes.Index(raw, []byte("PK\x01\x02"))
		if cdIdx < 0 {
			t.Fatal("central directory signature not found")
		}
		// Spoof central directory UncompressedSize from 4096 down to 64 bytes so
		// the metadata pre-check (64 <= 1024) passes and streaming must catch it.
		binary.LittleEndian.PutUint32(raw[cdIdx+24:cdIdx+28], 64)

		zipPath := filepath.Join(t.TempDir(), "spoofed.zip")
		if err := os.WriteFile(zipPath, raw, 0o644); err != nil {
			t.Fatal(err)
		}

		outDir := t.TempDir()
		opts := defaultExtractOpts()
		opts.maxAPKBytes = 1024

		_, err := extractBestAPKWithOpts(zipPath, outDir, "", prefs, dev, opts)
		if err == nil {
			t.Fatal("expected streaming error for spoofed zip header, got nil")
		}
		if !errors.Is(err, zip.ErrFormat) && !strings.Contains(err.Error(), "exceeds maximum extracted size") {
			t.Errorf("unexpected error for spoofed zip stream: %v", err)
		}
		assertNoTempPartFiles(t, outDir)
		if _, statErr := os.Stat(filepath.Join(outDir, "app-universal.apk")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("destination file must not exist after streaming abort: %v", statErr)
		}
	})

	t.Run("copyBounded enforces streaming byte cap and preserves existing target", func(t *testing.T) {
		outDir := t.TempDir()
		dest := filepath.Join(outDir, "app-universal.apk")
		if err := os.WriteFile(dest, []byte("KEEP_ME"), 0o644); err != nil {
			t.Fatal(err)
		}

		err := writeFileAtomicInDir(defaultAtomicFS(), outDir, "app-universal.apk", func(w io.Writer) error {
			_, cerr := copyBounded(w, bytes.NewReader(bytes.Repeat([]byte("X"), 4096)), 1024)
			return cerr
		})
		if err == nil || !strings.Contains(err.Error(), "exceeds maximum extracted size") {
			t.Fatalf("expected streaming max-bytes error from copyBounded, got: %v", err)
		}
		gotOld, rerr := os.ReadFile(dest)
		if rerr != nil || string(gotOld) != "KEEP_ME" {
			t.Fatalf("existing destination modified: %q (%v)", gotOld, rerr)
		}
		assertNoTempPartFiles(t, outDir)
	})

	t.Run("rejects archive exceeding max entry count", func(t *testing.T) {
		specs := make([]zipEntrySpec, 0, 12)
		for i := 0; i < 11; i++ {
			specs = append(specs, zipEntrySpec{
				name: fmt.Sprintf("note-%d.txt", i),
				body: []byte("x"),
			})
		}
		specs = append(specs, zipEntrySpec{name: "app-universal.apk", body: []byte("apk")})
		zipPath := buildTestZip(t, specs)

		outDir := t.TempDir()
		opts := defaultExtractOpts()
		opts.maxEntries = 10

		if _, err := extractBestAPKWithOpts(zipPath, outDir, "", prefs, dev, opts); err == nil {
			t.Fatal("expected maxEntries error from extractBestAPKWithOpts")
		}
		if _, err := inspectZipWithMax(zipPath, 10); err == nil {
			t.Fatal("expected maxEntries error from inspectZipWithMax")
		}
	})
}

func TestExtractBestAPK_FailedWritesCleanUpAndPreserveExisting(t *testing.T) {
	payload := bytes.Repeat([]byte("VALID_APK_CHUNK_"), 32)
	zipPath := buildTestZip(t, []zipEntrySpec{
		{name: "app-universal.apk", body: payload},
	})
	prefs := score.Prefs{Arch: "universal", Variant: "auto"}
	dev := score.Device{}

	t.Run("mid-stream write error cleans up temp file and preserves old APK", func(t *testing.T) {
		outDir := t.TempDir()
		dest := filepath.Join(outDir, "app-universal.apk")
		if err := os.WriteFile(dest, []byte("OLD_INTACT"), 0o644); err != nil {
			t.Fatal(err)
		}

		opts := defaultExtractOpts()
		opts.fs.createTemp = func(dir, pattern string) (atomicFile, error) {
			f, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, err
			}
			return &faultyTempFile{
				f:         f,
				failAfter: 24,
				writeErr:  errors.New("simulated ENOSPC"),
			}, nil
		}

		if _, err := extractBestAPKWithOpts(zipPath, outDir, "", prefs, dev, opts); err == nil {
			t.Fatal("expected write error, got nil")
		}
		gotOld, err := os.ReadFile(dest)
		if err != nil {
			t.Fatal(err)
		}
		if string(gotOld) != "OLD_INTACT" {
			t.Fatalf("existing file corrupted: got %q", gotOld)
		}
		assertNoTempPartFiles(t, outDir)
	})

	t.Run("close error aborts publish and cleans up temp file", func(t *testing.T) {
		outDir := t.TempDir()
		dest := filepath.Join(outDir, "app-universal.apk")
		if err := os.WriteFile(dest, []byte("OLD_INTACT"), 0o644); err != nil {
			t.Fatal(err)
		}

		renameCalled := false
		opts := defaultExtractOpts()
		opts.fs.createTemp = func(dir, pattern string) (atomicFile, error) {
			f, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, err
			}
			return &faultyTempFile{
				f:        f,
				failAfter: -1,
				closeErr: errors.New("simulated EIO on close"),
			}, nil
		}
		opts.fs.rename = func(oldpath, newpath string) error {
			renameCalled = true
			return os.Rename(oldpath, newpath)
		}

		if _, err := extractBestAPKWithOpts(zipPath, outDir, "", prefs, dev, opts); err == nil {
			t.Fatal("expected close error, got nil")
		}
		if renameCalled {
			t.Fatal("rename must not be called when temp file Close fails")
		}
		gotOld, _ := os.ReadFile(dest)
		if string(gotOld) != "OLD_INTACT" {
			t.Fatalf("existing file modified: got %q", gotOld)
		}
		assertNoTempPartFiles(t, outDir)
	})

	t.Run("rename error cleans up temp file", func(t *testing.T) {
		outDir := t.TempDir()
		opts := defaultExtractOpts()
		opts.fs.rename = func(oldpath, newpath string) error {
			return errors.New("simulated EXDEV on rename")
		}

		if _, err := extractBestAPKWithOpts(zipPath, outDir, "", prefs, dev, opts); err == nil {
			t.Fatal("expected rename error, got nil")
		}
		if _, err := os.Stat(filepath.Join(outDir, "app-universal.apk")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("destination should not exist when rename fails: %v", err)
		}
		assertNoTempPartFiles(t, outDir)
	})

	t.Run("corrupt zip CRC cleans up temp file and preserves old APK", func(t *testing.T) {
		marker := []byte("STORED_PAYLOAD_FOR_CRC_CHECK_0123456789")
		raw := buildTestZipBytes(t, []zipEntrySpec{
			{
				name:      "app-universal.apk",
				body:      marker,
				method:    zip.Store,
				hasMethod: true,
			},
		})
		idx := bytes.Index(raw, marker)
		if idx < 0 {
			t.Fatal("stored payload marker not found")
		}
		raw[idx] ^= 0xff // corrupt payload so archive/zip CRC verification fails

		badZip := filepath.Join(t.TempDir(), "corrupt.zip")
		if err := os.WriteFile(badZip, raw, 0o644); err != nil {
			t.Fatal(err)
		}

		outDir := t.TempDir()
		dest := filepath.Join(outDir, "app-universal.apk")
		if err := os.WriteFile(dest, []byte("OLD_INTACT"), 0o644); err != nil {
			t.Fatal(err)
		}

		if _, err := ExtractBestAPK(badZip, outDir, "", prefs, dev); err == nil {
			t.Fatal("expected CRC error on corrupt zip, got nil")
		}
		gotOld, _ := os.ReadFile(dest)
		if string(gotOld) != "OLD_INTACT" {
			t.Fatalf("existing file modified on CRC error: got %q", gotOld)
		}
		assertNoTempPartFiles(t, outDir)
	})
}

func TestDownloadReleaseAssetAndArtifact_PathSafetyAndAtomicStream(t *testing.T) {
	t.Run("rejects unsafe release asset names and destination paths", func(t *testing.T) {
		outDir := t.TempDir()
		badAssetNames := []string{
			"",
			"../evil.apk",
			`..\evil.apk`,
			"sub/evil.apk",
			"C:evil.apk",
			"NUL.apk",
			"evil\x00.apk",
			"app-\u202ekpa.exe.apk",
		}
		for _, bad := range badAssetNames {
			if _, err := ResolveReleaseAssetDest(outDir, bad); err == nil {
				t.Errorf("ResolveReleaseAssetDest accepted unsafe assetName %q", bad)
			}
			// Even if a caller pre-joined filepath.Join(outDir, bad), DownloadReleaseAsset must reject bad asset.Name.
			preJoined := filepath.Join(outDir, "fallback.apk")
			if bad != "" {
				preJoined = filepath.Join(outDir, bad)
			}
			err := DownloadReleaseAsset("owner/repo", ReleaseAsset{ID: 1, Name: bad, Size: 10}, preJoined, nil)
			if err == nil || errors.Is(err, ErrNoGH) {
				t.Errorf("DownloadReleaseAsset(%q) returned %v, want validation error before spawning gh", bad, err)
			}
		}

		if _, err := SafeDestinationPath("C:relative", "app.apk"); err == nil {
			t.Error("SafeDestinationPath accepted drive-relative outDir 'C:relative'")
		}

		badDests := []string{
			"",
			".",
			"..",
			"../outside.zip",
			outDir + "/../outside.zip",
			outDir + "/",
			"C:relative.zip",
		}
		for _, badDest := range badDests {
			err := DownloadArtifact("owner/repo", Artifact{ID: 9, SizeInBytes: 10}, badDest, nil)
			if err == nil || errors.Is(err, ErrNoGH) {
				t.Errorf("DownloadArtifact(dest=%q) returned %v, want path validation error", badDest, err)
			}
		}
	})

	if runtime.GOOS == "windows" {
		return
	}

	writeFakeGH := func(t *testing.T, body string) string {
		t.Helper()
		script := filepath.Join(t.TempDir(), "fake-gh")
		if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return script
	}

	t.Run("successful release download into directory publishes atomically", func(t *testing.T) {
		ghScript := writeFakeGH(t, `printf 'RELEASE_APK_BYTES'`)
		opts := defaultStreamOpts()
		opts.ghBin = ghScript

		outDir := t.TempDir()
		var lastGot int64
		asset := ReleaseAsset{ID: 42, Name: "app-arm64-v8a-release.apk", Size: 17}
		err := downloadReleaseAssetWithOpts(opts, "owner/repo", asset, outDir, func(got, total int64) {
			lastGot = got
		})
		if err != nil {
			t.Fatalf("downloadReleaseAssetWithOpts: %v", err)
		}
		gotBytes, err := os.ReadFile(filepath.Join(outDir, asset.Name))
		if err != nil {
			t.Fatal(err)
		}
		if string(gotBytes) != "RELEASE_APK_BYTES" {
			t.Fatalf("got %q, want RELEASE_APK_BYTES", gotBytes)
		}
		if lastGot != 17 {
			t.Errorf("progress got = %d, want 17", lastGot)
		}
		assertNoTempPartFiles(t, outDir)
	})

	t.Run("release download replaces pre-existing destination directory-symlink without traversing", func(t *testing.T) {
		ghScript := writeFakeGH(t, `printf 'SAFE_ASSET_BYTES'`)
		opts := defaultStreamOpts()
		opts.ghBin = ghScript

		root := t.TempDir()
		outDir := filepath.Join(root, "out")
		outsideDir := filepath.Join(root, "outside_dir")
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(outsideDir, 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(outDir, "app-release.apk")
		if err := os.Symlink(outsideDir, target); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}

		asset := ReleaseAsset{ID: 43, Name: "app-release.apk", Size: 16}
		if err := downloadReleaseAssetWithOpts(opts, "owner/repo", asset, target, nil); err != nil {
			t.Fatalf("downloadReleaseAssetWithOpts: %v", err)
		}
		entries, err := os.ReadDir(outsideDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("outsideDir was traversed and modified: %v", entries)
		}
		fi, err := os.Lstat(target)
		if err != nil {
			t.Fatal(err)
		}
		if !fi.Mode().IsRegular() {
			t.Fatalf("expected regular file at %q, got mode %s", target, fi.Mode())
		}
	})

	t.Run("failed mid-stream download preserves existing file and removes temp file", func(t *testing.T) {
		ghScript := writeFakeGH(t, `printf 'PARTIAL_CORRUPT'; echo 'HTTP 502 Bad Gateway' >&2; exit 1`)
		opts := defaultStreamOpts()
		opts.ghBin = ghScript

		outDir := t.TempDir()
		destZip := filepath.Join(outDir, "99.zip")
		if err := os.WriteFile(destZip, []byte("PREVIOUS_GOOD_ZIP"), 0o644); err != nil {
			t.Fatal(err)
		}

		err := downloadArtifactWithOpts(opts, "owner/repo", Artifact{ID: 99, SizeInBytes: 100}, destZip, nil)
		if err == nil {
			t.Fatal("expected error from failing fake-gh, got nil")
		}
		if !strings.Contains(err.Error(), "HTTP 502 Bad Gateway") {
			t.Errorf("expected stderr message in error, got: %v", err)
		}
		after, err := os.ReadFile(destZip)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != "PREVIOUS_GOOD_ZIP" {
			t.Fatalf("existing file overwritten on failed download: got %q", after)
		}
		assertNoTempPartFiles(t, outDir)
	})

	t.Run("empty stream response fails cleanly and removes temp file", func(t *testing.T) {
		ghScript := writeFakeGH(t, `exit 0`)
		opts := defaultStreamOpts()
		opts.ghBin = ghScript

		outDir := t.TempDir()
		destZip := filepath.Join(outDir, "empty.zip")
		err := downloadArtifactWithOpts(opts, "owner/repo", Artifact{ID: 101, SizeInBytes: 0}, destZip, nil)
		if err == nil || !strings.Contains(err.Error(), "empty response") {
			t.Fatalf("expected empty response error, got: %v", err)
		}
		if _, statErr := os.Stat(destZip); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("empty download must not publish destZip: %v", statErr)
		}
		assertNoTempPartFiles(t, outDir)
	})

	t.Run("streaming download exceeding maxBytes aborts and cleans up", func(t *testing.T) {
		ghScript := writeFakeGH(t, `dd if=/dev/zero bs=1024 count=8 2>/dev/null`)
		opts := defaultStreamOpts()
		opts.ghBin = ghScript
		opts.maxBytes = 2048

		outDir := t.TempDir()
		destZip := filepath.Join(outDir, "oversized.zip")
		err := downloadArtifactWithOpts(opts, "owner/repo", Artifact{ID: 100, SizeInBytes: 512}, destZip, nil)
		if err == nil {
			t.Fatal("expected streaming maxBytes error, got nil")
		}
		if _, statErr := os.Stat(destZip); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("oversized download must not publish destZip: %v", statErr)
		}
		assertNoTempPartFiles(t, outDir)
	})
}
