package device

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// InstalledApp is the version of a package currently on the device.
type InstalledApp struct {
	Package     string
	VersionCode int64
	VersionName string
	CertSHA256  string // hex SHA-256 of active signing certificate when available
	Queryable   bool   // true when dumpsys package could actually be executed
}

var (
	reVCode = regexp.MustCompile(`versionCode=(\d+)`)
	reVName = regexp.MustCompile(`versionName=([^\s]+)`)
	rePkg   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)
)

// ValidPackage reports whether pkg is a syntactically valid Android Java
// package identifier safe to pass to dumpsys/pm.
func ValidPackage(pkg string) bool {
	return len(pkg) <= 255 && rePkg.MatchString(pkg)
}

// QueryPackage inspects dumpsys for an installed Android package.
// On Android/Termux it tries Shizuku rish first, then local dumpsys/pm;
// when adb is available it tries `adb shell dumpsys package <pkg>`.
func QueryPackage(pkg string) (InstalledApp, bool) {
	if !ValidPackage(pkg) {
		return InstalledApp{}, false
	}
	onAndroid := detectOSFn()
	type probeCmd struct {
		bin  string
		args []string
	}
	var cmds []probeCmd
	if onAndroid {
		cmds = append(cmds,
			probeCmd{"rish", []string{"-c", "dumpsys package " + pkg}},
			probeCmd{"dumpsys", []string{"package", pkg}},
			probeCmd{"pm", []string{"dump", pkg}},
		)
	}
	cmds = append(cmds, probeCmd{"adb", []string{"shell", "dumpsys", "package", pkg}})

	queryable := false
	for _, c := range cmds {
		if !hasPath(c.bin) {
			continue
		}
		out, err := runShortCmd(4*time.Second, c.bin, c.args...)
		if err != nil || out == "" {
			continue
		}
		if strings.Contains(out, "SecurityException") || strings.Contains(out, "Permission Denial") {
			continue
		}
		queryable = true
		app, found := parseDumpsysPackage(pkg, out)
		if found {
			app.Queryable = true
			return app, true
		}
	}
	return InstalledApp{Package: pkg, Queryable: queryable}, false
}

func parseDumpsysPackage(pkg, out string) (InstalledApp, bool) {
	if strings.Contains(out, "Unable to find package") {
		return InstalledApp{Package: pkg}, false
	}
	m := reVCode.FindStringSubmatch(out)
	if len(m) < 2 {
		return InstalledApp{Package: pkg}, false
	}
	code, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return InstalledApp{Package: pkg}, false
	}
	var name string
	if n := reVName.FindStringSubmatch(out); len(n) >= 2 {
		name = n[1]
	}
	return InstalledApp{
		Package:     pkg,
		VersionCode: code,
		VersionName: name,
	}, true
}

var _ = exec.CommandContext
var _ = context.Background
