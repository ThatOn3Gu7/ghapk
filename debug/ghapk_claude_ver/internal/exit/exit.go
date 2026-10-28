// Package exit defines ghapk's process exit codes. They are the same numbers
// the original shell script used, so scripts that checked them keep working.
package exit

const (
	OK          = 0
	Usage       = 2
	Dep         = 3 // a required tool is missing
	Auth        = 4 // gh is not logged in
	API         = 5 // GitHub API error
	NoArtifact  = 6 // no matching artifact anywhere
	Download    = 7
	Archive     = 8
	NoAPK       = 9  // artifact held no APK
	Install     = 10 // the installer could not be launched / did not land
	BuildFail   = 11 // the watched build failed (--strict)
	Downgrade   = 12 // refused: older than installed
	Doctor      = 14
	RateLimit   = 15
	Network     = 16
	Gone        = 17 // artifact expired
	Scope       = 18 // token lacks access
	Checksum    = 19
	SigMismatch = 20 // installed copy is signed with a different key
	NoSpace     = 21
	Interrupted = 130
)

// Describe is the one-line meaning of a code, for --help and error panels.
func Describe(c int) string {
	switch c {
	case OK:
		return "ok"
	case Usage:
		return "usage"
	case Dep:
		return "missing tool"
	case Auth:
		return "auth"
	case API:
		return "api"
	case NoArtifact:
		return "no artifact"
	case Download:
		return "download"
	case Archive:
		return "archive"
	case NoAPK:
		return "no apk"
	case Install:
		return "install"
	case BuildFail:
		return "watched build failed"
	case Downgrade:
		return "downgrade refused"
	case Doctor:
		return "doctor found a blocker"
	case RateLimit:
		return "rate limited"
	case Network:
		return "network"
	case Gone:
		return "artifact gone (expired)"
	case Scope:
		return "token lacks access"
	case Checksum:
		return "checksum mismatch"
	case SigMismatch:
		return "installed copy is signed with a different key"
	case NoSpace:
		return "device out of space"
	case Interrupted:
		return "interrupted"
	}
	return "error"
}
