//go:build !linux && !android && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly && !windows

package activity

import "os"

func lockFile(path string) (*os.File, error) { return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600) }
func unlockFile(f *os.File)                  { _ = f.Close() }
