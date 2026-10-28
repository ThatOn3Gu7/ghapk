package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheSizeHandlesMissingAndSymlinks(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	if err := os.Mkdir(cache, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "a"), []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(root, "outside")
	if err := os.WriteFile(external, []byte("123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(cache, "linked-file")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "cache-link")
	if err := os.Symlink(cache, link); err != nil {
		t.Fatal(err)
	}
	if size, ok := cacheSize(context.Background(), link); !ok || size != 5 {
		t.Fatalf("%d, %v", size, ok)
	}
	if size, ok := cacheSize(context.Background(), filepath.Join(root, "missing")); !ok || size != 0 {
		t.Fatalf("%d, %v", size, ok)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := cacheSize(ctx, cache); ok {
		t.Fatal("cancelled scan cannot be reported complete")
	}
}
