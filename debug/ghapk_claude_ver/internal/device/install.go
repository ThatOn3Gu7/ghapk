package device

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

func fingerprint(hexDER string) string {
	der, err := hex.DecodeString(hexDER)
	if err != nil || len(der) == 0 {
		return ""
	}
	s := sha256.Sum256(der)
	return hex.EncodeToString(s[:])
}

func freeBytes(path string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return -1
	}
	return int64(st.Bavail) * int64(st.Bsize)
}

const apkMime = "application/vnd.android.package-archive"

// ErrNoInstaller means nothing on this machine could open the APK.
var ErrNoInstaller = errors.New("no installer available")

// Open hands an APK to whatever installer the machine has, in the order the
// shell script used: termux-open, am start, adb, xdg-open, open. It returns the
// name of the method that took it. Opening the installer is not installing:
// the person still taps through Android's prompt, so callers verify afterwards.
func (d *Device) Open(ctx context.Context, apk string, note func(string)) (string, error) {
	say := func(s string) {
		if note != nil {
			note(s)
		}
	}
	type try struct {
		name string
		run  func() error
	}
	var tries []try
	if d.Android {
		if p, err := exec.LookPath("termux-open"); err == nil {
			tries = append(tries, try{"termux-open", func() error {
				return runQuiet(ctx, 20*time.Second, p, "--content-type", apkMime, apk)
			}})
		}
		if p := androidBin("am"); p != "" {
			tries = append(tries, try{"am start", func() error {
				return runQuiet(ctx, 20*time.Second, p, "start", "-a", "android.intent.action.VIEW", "-d", "file://"+apk, "-t", apkMime)
			}})
		}
	}
	if p, err := exec.LookPath("adb"); err == nil && (d.Android || adbHasDevice(ctx, p)) {
		tries = append(tries, try{"adb install", func() error {
			return runQuiet(ctx, 2*time.Minute, p, "install", "-r", apk)
		}})
	}
	if d.Android {
		if p, err := exec.LookPath("xdg-open"); err == nil {
			tries = append(tries, try{"xdg-open", func() error { return runQuiet(ctx, 20*time.Second, p, apk) }})
		}
	}
	if len(tries) == 0 {
		return "", ErrNoInstaller
	}
	for _, t := range tries {
		say("trying " + t.name)
		if err := t.run(); err == nil {
			return t.name, nil
		} else if ctx.Err() != nil {
			return "", ctx.Err()
		} else {
			say(t.name + " failed: " + err.Error())
		}
	}
	return "", ErrNoInstaller
}

func adbHasDevice(ctx context.Context, adb string) bool {
	out, err := runTool(ctx, 5*time.Second, adb, "devices")
	if err != nil {
		return false
	}
	for _, ln := range strings.Split(out, "\n")[1:] {
		if strings.HasSuffix(strings.TrimSpace(ln), "device") {
			return true
		}
	}
	return false
}

func runQuiet(ctx context.Context, timeout time.Duration, bin string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = scrubbed()
	cmd.Stdin = nil
	return cmd.Run()
}

// Verify polls until the device reports versionCode want for pkg, the wait runs
// out, or ctx is cancelled. tick is called once a second with the seconds
// waited and the code currently installed. Only a positive mismatch is a
// failure: every "could not tell" is reported as inconclusive, because a check
// that cries wolf makes a working install look broken.
type VerifyResult int

const (
	VerifyLanded VerifyResult = iota
	VerifyNotLanded
	VerifyUnknown
)

// Verify implements the post-install check.
func (d *Device) Verify(ctx context.Context, pkg string, want int64, wait time.Duration, tick func(waited time.Duration, got int64)) (VerifyResult, int64, string) {
	if !d.Live(ctx) {
		return VerifyUnknown, 0, "no way to read the installed apps here"
	}
	if pkg == "" {
		return VerifyUnknown, 0, "no package name to look up"
	}
	if want <= 0 {
		return VerifyUnknown, 0, "the APK's versionCode could not be read"
	}
	got := d.VersionCode(ctx, pkg)
	start := time.Now()
	for got != want && time.Since(start) < wait {
		select {
		case <-ctx.Done():
			return VerifyUnknown, got, "stopped waiting"
		case <-time.After(time.Second):
		}
		got = d.VersionCode(ctx, pkg)
		if tick != nil {
			tick(time.Since(start), got)
		}
	}
	switch {
	case got == want:
		return VerifyLanded, got, ""
	case got == 0:
		return VerifyUnknown, got, "the device reported no versionCode for " + pkg
	}
	return VerifyNotLanded, got, fmt.Sprintf("the device is still on versionCode %d", got)
}

// Diagnosis explains why an install did not land.
type Diagnosis struct {
	Code   string // INSTALL_FAILED_* if Android said so
	Source string // where that came from
	Fix    string
	Detail string
	Exit   int
}

var reFail = regexp.MustCompile(`INSTALL_FAILED_[A-Z_]*`)

// Diagnose works out why the device is not on the new build. It reads Android's
// own error code when it can (via Shizuku's logcat), and otherwise only states
// causes it can prove from numbers it already holds.
func (d *Device) Diagnose(ctx context.Context, apkVCode, gotVCode int64, apkSize int64, minSDK int, cacheDir string, pkg string) Diagnosis {
	if d.HasRish() {
		if out, err := d.rishRun(ctx, "logcat -d -t 3000 2>/dev/null | grep -o 'INSTALL_FAILED_[A-Z_]*' | tail -1"); err == nil {
			if m := reFail.FindString(out); m != "" {
				return explain(m, "the device log", pkg, apkVCode, gotVCode, apkSize, d.SDK)
			}
		}
	}
	if apkVCode > 0 && gotVCode > apkVCode {
		return explain("INSTALL_FAILED_VERSION_DOWNGRADE", "", pkg, apkVCode, gotVCode, apkSize, d.SDK)
	}
	if minSDK > 0 && d.SDK > 0 && minSDK > d.SDK {
		return explain("INSTALL_FAILED_OLDER_SDK", "", pkg, apkVCode, gotVCode, apkSize, d.SDK)
	}
	if free := FreeBytes(cacheDir); free >= 0 && apkSize > 0 && free < apkSize*3 {
		return explain("INSTALL_FAILED_INSUFFICIENT_STORAGE", "", pkg, apkVCode, gotVCode, apkSize, d.SDK)
	}
	return explain("", "", pkg, apkVCode, gotVCode, apkSize, d.SDK)
}

func explain(code, src, pkg string, apkV, gotV, size int64, sdk int) Diagnosis {
	d := Diagnosis{Code: code, Source: src}
	if pkg == "" {
		pkg = "the app"
	}
	switch code {
	case "INSTALL_FAILED_UPDATE_INCOMPATIBLE":
		d.Fix = "the installed copy is signed with a different key"
		d.Detail = "no override exists: uninstall " + pkg + ", then install again"
	case "INSTALL_FAILED_VERSION_DOWNGRADE":
		d.Fix = fmt.Sprintf("this build is %d, the device has %d", apkV, gotV)
		d.Detail = "set downgrade to allow, or pick a newer build"
	case "INSTALL_FAILED_INSUFFICIENT_STORAGE":
		d.Fix = "the device ran out of space part-way through"
		d.Detail = fmt.Sprintf("free up storage: this APK is %.1f MB and the install needs about 3x", float64(size)/1e6)
	case "INSTALL_FAILED_OLDER_SDK":
		d.Fix = fmt.Sprintf("this build needs a newer Android than API %d", sdk)
		d.Detail = "pick an older build of the app"
	case "INSTALL_FAILED_MISSING_SPLIT":
		d.Fix, d.Detail = "this is a split bundle, not a single APK", "install base.apk together with its config APKs"
	case "INSTALL_FAILED_INVALID_APK":
		d.Fix, d.Detail = "Android does not accept the file as an APK", "the download may be corrupt: clear the cache and retry"
	case "INSTALL_FAILED_VERIFICATION_FAILURE":
		d.Fix, d.Detail = "Play Protect rejected the APK", "check the Play Protect notice on the device"
	case "INSTALL_FAILED_ABORTED", "INSTALL_FAILED_USER_RESTRICTED":
		d.Fix, d.Detail = "the install was cancelled or blocked on the device", "accept the prompt in your installer, then try again"
	case "INSTALL_FAILED_ALREADY_EXISTS":
		d.Fix, d.Detail = pkg+" is installed and would not be replaced", "uninstall it first, then try again"
	case "":
		d.Fix = "the installer opened but this build never landed"
		d.Detail = "usual causes: a different signing key, or a declined prompt"
	default:
		d.Fix = "Android reported " + strings.TrimPrefix(code, "INSTALL_FAILED_")
		d.Detail = "ghapk does not know that code yet"
	}
	return d
}

// ---- Termux integration: notifications, wake lock --------------------------

// Notify raises a notification (and optionally a toast and speech) through
// Termux:API, each only if the corresponding tool exists.
type Notifier struct {
	On    string // always success failure never
	Speak bool
	Toast bool
}

// Send delivers one outcome. kind is ok, err, warn or info.
func (n Notifier) Send(title, content, kind, url string) {
	if _, err := exec.LookPath("termux-notification"); err != nil && !n.Speak && !n.Toast {
		return
	}
	allow := false
	switch n.On {
	case "never":
	case "success":
		allow = kind == "ok"
	case "failure":
		allow = kind == "err" || kind == "warn"
	default:
		allow = true
	}
	if !allow {
		return
	}
	if n.Speak {
		if p, err := exec.LookPath("termux-tts-speak"); err == nil {
			_ = exec.Command(p, strings.ReplaceAll(title+". "+content, " · ", ", ")).Start()
		}
	}
	if n.Toast {
		if p, err := exec.LookPath("termux-toast"); err == nil {
			_ = exec.Command(p, "-g", "bottom", title+": "+content).Start()
		}
	}
	p, err := exec.LookPath("termux-notification")
	if err != nil {
		return
	}
	help, _ := exec.Command(p, "--help").CombinedOutput()
	supports := func(flag string) bool { return strings.Contains(string(help), flag) }
	args := []string{"--title", title, "--content", content}
	if supports("--id") {
		args = append(args, "--id", "ghapk-"+kind)
	}
	if supports("--group") {
		args = append(args, "--group", "ghapk")
	}
	if supports("--priority") {
		pri := "default"
		if kind == "err" {
			pri = "high"
		}
		args = append(args, "--priority", pri)
	}
	if url != "" && supports("--action") && !strings.ContainsAny(url, `'"`) {
		args = append(args, "--action", `termux-open-url '`+url+`'`)
	}
	cmd := exec.Command(p, args...)
	cmd.Stdin = nil // else termux-notification waits 3 s for stdin
	_ = cmd.Run()
}

// WakeLock holds or releases Termux's wake lock, so a long wait does not stall
// when Android puts the CPU to sleep. It reports whether the lock was taken.
func WakeLock(on bool) bool {
	name := "termux-wake-unlock"
	if on {
		name = "termux-wake-lock"
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return false
	}
	return exec.Command(p).Run() == nil
}

// Tool reports whether a command exists, for the doctor.
func Tool(name string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	if p := androidBin(name); p != "" {
		return p, true
	}
	return "", false
}

var _ = os.Getenv
