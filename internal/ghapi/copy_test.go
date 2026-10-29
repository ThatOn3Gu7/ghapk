package ghapi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyAPKCreatesUniqueCompleteCopiesAndHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "my app.apk")
	if err := os.WriteFile(src, []byte("APK content"), 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "output")
	first, err := CopyAPK(context.Background(), src, out)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CopyAPK(context.Background(), src, out)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(filepath.Base(first), "my_app-") {
		t.Fatalf("unsafe names: %s %s", first, second)
	}
	for _, p := range []string{first, second} {
		b, e := os.ReadFile(p)
		if e != nil || string(b) != "APK content" {
			t.Fatalf("copy %s: %q %v", p, b, e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CopyAPK(ctx, src, out); err == nil {
		t.Fatal("cancelled copy succeeded")
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 2 {
		t.Fatalf("partial output left behind: %v", entries)
	}
	if _, err = CopyAPK(context.Background(), src, src); err == nil {
		t.Fatal("output path that is a file accepted")
	}
}
