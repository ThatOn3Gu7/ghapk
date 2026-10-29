package ghapi

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CopyAPK publishes a uniquely named copy without overwriting existing files.
// A private temporary file prevents partial downloads from appearing as APKs.
func CopyAPK(ctx context.Context, source, dir string) (string, error) {
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, dir[2:])
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	f, err := os.CreateTemp(dir, ".ghapk-copy-*.part")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	buf := make([]byte, 64*1024)
	for {
		if err = ctx.Err(); err != nil {
			break
		}
		var n int
		n, err = input.Read(buf)
		if n > 0 {
			if _, e := f.Write(buf[:n]); e != nil {
				err = e
				break
			}
		}
		if err == io.EOF {
			err = nil
			break
		}
		if err != nil {
			break
		}
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	var b strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		b.WriteString("app")
	}
	tag := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(tmp), ".ghapk-copy-"), ".part")
	dst := filepath.Join(dir, fmt.Sprintf("%s-%s.apk", b.String(), tag))
	// Reserve the random destination exclusively, then atomically replace our
	// own placeholder. This works on Android shared storage without hardlinks.
	reserved, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	if err = reserved.Close(); err != nil {
		os.Remove(dst)
		return "", err
	}
	if err = ctx.Err(); err != nil {
		os.Remove(dst)
		return "", err
	}
	if err = os.Rename(tmp, dst); err != nil {
		os.Remove(dst)
		return "", err
	}
	return dst, nil
}
