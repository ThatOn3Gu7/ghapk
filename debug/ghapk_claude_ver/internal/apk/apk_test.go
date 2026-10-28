package apk

import (
	"archive/zip"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The fixtures come from scripts/fakegh/gen.py, which builds a real binary
// manifest and a real v2 signing block.
func TestAgainstGeneratedAPK(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("no openssl")
	}
	dir := t.TempDir()
	gen, _ := filepath.Abs("../../scripts/fakegh/gen.py")
	if out, err := exec.Command("python3", gen, dir).CombinedOutput(); err != nil {
		t.Fatalf("gen: %v\n%s", err, out)
	}
	zr, err := zip.OpenReader(filepath.Join(dir, "zips", "5001.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	apkPath := filepath.Join(dir, "x.apk")
	rc, _ := zr.File[0].Open()
	out, _ := os.Create(apkPath)
	io.Copy(out, rc)
	out.Close()
	rc.Close()

	in, err := ReadInfo(apkPath)
	if err != nil {
		t.Fatal(err)
	}
	if in.Package != "quiz.thaton3app.nazo" || in.VersionCode != 142 || in.VersionName != "1.4.2" || in.MinSDK != 24 {
		t.Fatalf("manifest: %+v", in)
	}
	c, err := ReadCert(apkPath)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := os.ReadFile(filepath.Join(dir, "cert.der"))
	want := Fingerprint(hex.EncodeToString(der))
	if c.SHA256 != want || c.Scheme != "v2" || c.Subject == "" {
		t.Fatalf("cert: %+v want %s", c, want)
	}
	if h, err := FileSHA256(apkPath); err != nil || len(h) != 64 {
		t.Fatal("sha")
	}
}

func TestNotAnAPK(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.zip")
	f, _ := os.Create(p)
	w := zip.NewWriter(f)
	w.Create("hello.txt")
	w.Close()
	f.Close()
	if _, err := ReadInfo(p); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ReadCert(p); err == nil {
		t.Fatal("expected no signature")
	}
}
