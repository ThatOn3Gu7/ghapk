package device

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ghapk/ghapk/internal/apk"
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
	reVCode    = regexp.MustCompile(`versionCode=(\d+)`)
	reVName    = regexp.MustCompile(`versionName=([^\s]+)`)
	reCodePath = regexp.MustCompile(`codePath=([^\s]+)`)
	reCertHex  = regexp.MustCompile(`(?i)(?:sha-?256[:=\s]+|cert(?:ificate)?[:=\s]+)([0-9a-f]{64}|(?:[0-9a-f]{2}:){31}[0-9a-f]{2})`)
	reSigDER   = regexp.MustCompile(`(?i)signatures:\s*\[([0-9a-f]+)\]`)
	rePkg      = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)
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
	return QueryPackageContext(context.Background(), pkg)
}

// QueryPackageContext prefers Shizuku and permits interactive jobs to cancel.
func QueryPackageContext(ctx context.Context, pkg string) (InstalledApp, bool) {
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
	if onAndroid {
		cmds = append(cmds, probeCmd{"rish", []string{"-c", "pm list packages --show-versioncode " + pkg}}, probeCmd{"pm", []string{"list", "packages", "--show-versioncode", pkg}})
	}
	cmds = append(cmds, probeCmd{"adb", []string{"shell", "pm", "list", "packages", "--show-versioncode", pkg}})

	queryable := false
	for _, c := range cmds {
		if ctx.Err() != nil {
			return InstalledApp{}, false
		}
		if !hasPath(c.bin) {
			continue
		}
		out, err := runShortCmdContext(ctx, 1500*time.Millisecond, c.bin, c.args...)
		if err != nil || out == "" {
			continue
		}
		if strings.Contains(out, "SecurityException") || strings.Contains(out, "Permission Denial") {
			continue
		}
		// A non-empty error or partial dump is not evidence that an app is
		// absent. Only an explicit missing-package response is definitive.
		if strings.Contains(out, "Unable to find package") || strings.Contains(out, "Unknown package") {
			queryable = true
		}
		app, found := parseDumpsysPackage(pkg, out)
		if strings.Contains(strings.Join(c.args, " "), "list packages") {
			app, found = parsePackageList(pkg, out)
		}
		if found {
			app.Queryable = true
			if app.CertSHA256 == "" && onAndroid {
				app.CertSHA256 = readInstalledAPKCert(out)
			}
			return app, true
		}
	}
	return InstalledApp{Package: pkg, Queryable: queryable}, false
}

// A filtered pm list can include similarly named packages. Only an exact
// package line is evidence, and an empty list is not proof of absence.
func parsePackageList(pkg, out string) (InstalledApp, bool) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "package:"+pkg {
			continue
		}
		app := InstalledApp{Package: pkg}
		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "versionCode:") {
				app.VersionCode, _ = strconv.ParseInt(strings.TrimPrefix(field, "versionCode:"), 10, 64)
			}
		}
		return app, true
	}
	return InstalledApp{Package: pkg}, false
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
	var certSHA string
	if cm := reCertHex.FindStringSubmatch(out); len(cm) >= 2 {
		certSHA = strings.ToLower(strings.ReplaceAll(cm[1], ":", ""))
	}
	if certSHA == "" {
		if m := reSigDER.FindStringSubmatch(out); len(m) == 2 && len(m[1]) >= 64 && len(m[1]) <= 32768 {
			if der, err := hex.DecodeString(m[1]); err == nil {
				if _, err = x509.ParseCertificate(der); err == nil {
					sum := sha256.Sum256(der)
					certSHA = hex.EncodeToString(sum[:])
				}
			}
		}
	}
	return InstalledApp{
		Package:     pkg,
		VersionCode: code,
		VersionName: name,
		CertSHA256:  certSHA,
	}, true
}

func readInstalledAPKCert(dumpsysOut string) string {
	m := reCodePath.FindStringSubmatch(dumpsysOut)
	if len(m) < 2 {
		return ""
	}
	cp := strings.TrimSpace(m[1])
	candidates := []string{cp}
	if !strings.HasSuffix(strings.ToLower(cp), ".apk") {
		candidates = []string{filepath.Join(cp, "base.apk"), cp}
	}
	for _, p := range candidates {
		if c, err := apk.ReadCert(p); err == nil && c.SHA256 != "" {
			return c.SHA256
		}
	}
	return ""
}

var _ = exec.CommandContext
var _ = context.Background
