//go:build android || linux || darwin

package device

import (
	"os"

	"golang.org/x/sys/unix"
)

// diskBytes returns total/free bytes on the volume holding $HOME.
func diskBytes() (total, free int64) {
	dir, err := os.UserHomeDir()
	if err != nil {
		dir = os.TempDir()
	}
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, 0
	}
	bsize := int64(st.Bsize)
	return int64(st.Blocks) * bsize, int64(st.Bavail) * bsize
}
