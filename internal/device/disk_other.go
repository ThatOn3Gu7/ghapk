//go:build !android && !linux && !darwin

package device

// diskBytes is unavailable without statfs; zeros mean "don't show".
func diskBytes() (total, free int64) { return 0, 0 }
