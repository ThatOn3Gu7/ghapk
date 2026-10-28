package engine

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxTotalUnpacked = 4 << 30 // refuse a zip that inflates past this (zip bomb)
	maxFileUnpacked  = 2 << 30
)

// safeJoin joins an archive entry name under root and refuses anything that
// would land outside it (absolute paths, "..", drive letters).
func safeJoin(root, name string) (string, error) {
	n := filepath.FromSlash(name)
	if filepath.IsAbs(n) || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return "", fmt.Errorf("unsafe path %q", name)
	}
	for _, part := range strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return "", fmt.Errorf("unsafe path %q", name)
		}
	}
	p := filepath.Join(root, n)
	if rel, err := filepath.Rel(root, p); err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("unsafe path %q", name)
	}
	return p, nil
}

// extractZip unpacks src into dir, skipping symlinks and refusing entries that
// escape dir. It returns the number of bytes written.
func extractZip(src, dir string) (int64, error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return 0, err
	}
	defer zr.Close()
	var total int64
	for _, f := range zr.File {
		if f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		dst, err := safeJoin(dir, f.Name)
		if err != nil {
			return total, err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return total, err
			}
			continue
		}
		if f.UncompressedSize64 > maxFileUnpacked {
			return total, errors.New("entry too large: " + f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return total, err
		}
		rc, err := f.Open()
		if err != nil {
			return total, err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			rc.Close()
			return total, err
		}
		n, err := io.Copy(out, io.LimitReader(rc, maxFileUnpacked+1))
		out.Close()
		rc.Close()
		total += n
		if err != nil {
			return total, err
		}
		if total > maxTotalUnpacked {
			return total, errors.New("archive inflates past the size limit")
		}
	}
	return total, nil
}

// unpackNested opens zips found inside dir (up to 3 levels), the way build
// systems sometimes zip a zip. Each nested zip is removed once unpacked.
func unpackNested(dir string, note func(string)) {
	for pass := 0; pass < 3; pass++ {
		var zips []string
		_ = filepath.Walk(dir, func(p string, st os.FileInfo, err error) error {
			if err == nil && !st.IsDir() && strings.EqualFold(filepath.Ext(p), ".zip") {
				zips = append(zips, p)
			}
			return nil
		})
		if len(zips) == 0 {
			return
		}
		for _, z := range zips {
			dst := strings.TrimSuffix(z, filepath.Ext(z)) + "-extracted"
			if _, err := extractZip(z, dst); err != nil && note != nil {
				note("skipping nested " + filepath.Base(z) + ": " + err.Error())
			}
			_ = os.Remove(z)
		}
	}
}

// listFiles returns every regular file under dir.
func listFiles(dir string) []string {
	var out []string
	_ = filepath.Walk(dir, func(p string, st os.FileInfo, err error) error {
		if err == nil && st.Mode().IsRegular() {
			out = append(out, p)
		}
		return nil
	})
	return out
}
