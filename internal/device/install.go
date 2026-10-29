package device

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ThatOn3Gu7/ghapk/internal/activity"
)

// InstallOutcome distinguishes positively confirmed installation from an
// unconfirmed interactive handoff or a failure.
type InstallOutcome string

const (
	// OutcomeInstalled means the installer positively confirmed installation
	// (for example, `adb install` exited 0 and printed `Success` with no failure code).
	OutcomeInstalled InstallOutcome = "installed"

	// OutcomeLaunched means the APK was handed to Android's interactive
	// package installer intent (`termux-open` or `am start`), which still
	// requires the user to accept the on-device prompt before the app is installed.
	OutcomeLaunched InstallOutcome = "launched"

	// OutcomeFailed means installation failed or no valid Android installer exists.
	OutcomeFailed InstallOutcome = "failed"
)

// InstallResult describes the honest outcome of an APK install attempt.
type InstallResult struct {
	Outcome     InstallOutcome
	Via         string // "adb", "termux-open", "am", or ""
	FailureCode string // e.g. "INSTALL_FAILED_UPDATE_INCOMPATIBLE"
	Diagnosis   string // actionable plain-English explanation when Outcome == OutcomeFailed
	Detail      string // concise human-facing status detail
}

// Confirmed reports whether installation was positively verified.
func (r InstallResult) Confirmed() bool { return r.Outcome == OutcomeInstalled }

// Launched reports whether an interactive Android installer prompt was opened
// without positive confirmation that installation finished.
func (r InstallResult) Launched() bool { return r.Outcome == OutcomeLaunched }

var (
	reInstallCode = regexp.MustCompile(`INSTALL_(?:PARSE_)?FAILED_[A-Z0-9_]+`)
	reFailureBrk  = regexp.MustCompile(`(?m)^\s*Failure\s*(?:\[([^\]]+)\])?`)
	installExecFn = defaultInstallExec
)

// DiagnoseFailure extracts an Android PM/ADB failure code or transport error
// from raw command output and returns an actionable explanation.
func DiagnoseFailure(raw string) (code, diagnosis string) {
	if c := reInstallCode.FindString(raw); c != "" {
		code = c
	} else if m := reFailureBrk.FindStringSubmatch(raw); len(m) >= 2 && strings.TrimSpace(m[1]) != "" {
		code = strings.TrimSpace(m[1])
	}

	switch code {
	case "INSTALL_FAILED_UPDATE_INCOMPATIBLE", "INSTALL_PARSE_FAILED_INCONSISTENT_CERTIFICATES":
		return code, "the app already on the device was signed with a different key — uninstall the existing app first (which erases its data)"
	case "INSTALL_FAILED_VERSION_DOWNGRADE":
		return code, "the device already has a newer versionCode installed — enable downgrade=allow only if you intend to replace it (debug builds) or uninstall first"
	case "INSTALL_FAILED_OLDER_SDK":
		return code, "this APK requires a newer Android SDK level than this device runs"
	case "INSTALL_FAILED_NO_MATCHING_ABIS":
		return code, "this APK contains native libraries for a different CPU architecture than the target device"
	case "INSTALL_FAILED_INSUFFICIENT_STORAGE":
		return code, "not enough free storage space on the device to install this APK"
	case "INSTALL_FAILED_USER_RESTRICTED", "INSTALL_FAILED_ABORTED":
		return code, "installation was blocked or cancelled on the device (check USB install permissions / Play Protect / prompt)"
	case "INSTALL_FAILED_INVALID_APK", "INSTALL_PARSE_FAILED_NOT_APK", "INSTALL_PARSE_FAILED_BAD_MANIFEST", "INSTALL_PARSE_FAILED_NO_CERTIFICATES":
		return code, "the APK file is corrupt, unsigned, or unreadable by the Android package manager"
	case "INSTALL_FAILED_TEST_ONLY":
		return code, "this is a test-only APK (requires `adb install -t`)"
	}

	low := strings.ToLower(raw)
	switch {
	case strings.Contains(low, "no devices/emulators found") || strings.Contains(low, "device not found"):
		return "", "no Android device detected over adb — connect via USB or `adb connect` and enable USB/Wireless debugging"
	case strings.Contains(low, "unauthorized"):
		return "", "adb device is unauthorized — unlock your phone and accept the 'Allow USB debugging' prompt"
	case strings.Contains(low, "device offline"):
		return "", "adb device is offline — reconnect USB or run `adb reconnect`"
	case strings.Contains(low, "more than one device/emulator"):
		return "", "multiple adb devices are attached — set ANDROID_SERIAL to choose the target device"
	}

	if code != "" {
		return code, "Android package manager rejected the install (" + code + ")"
	}
	return "", ""
}

// InstallAPK hands an APK to a genuine Android installer and reports an honest
// outcome:
//   - OutcomeInstalled ONLY when positively confirmed (e.g. `adb install` exited 0 and printed `Success`)
//   - OutcomeLaunched when handed off to Android's interactive installer (`termux-open` or `am start`)
//   - OutcomeFailed (with non-nil error) on any error, unconfirmed/failed adb output, or missing installer
//
// Generic desktop openers such as `xdg-open` are never treated as installers.
func InstallAPK(apkPath string, allowDowngrade bool, onLine func(string)) (InstallResult, error) {
	return InstallAPKContext(context.Background(), apkPath, allowDowngrade, onLine)
}

// InstallAPKContext stops the installer command if the job is cancelled. An
// already launched Android installer remains under Android's control.
func InstallAPKContext(parent context.Context, apkPath string, allowDowngrade bool, onLine func(string)) (InstallResult, error) {
	if onLine == nil {
		onLine = func(string) {}
	}
	if strings.TrimSpace(apkPath) == "" {
		err := errors.New("empty APK path")
		return InstallResult{Outcome: OutcomeFailed, Detail: err.Error()}, err
	}
	st, statErr := os.Stat(apkPath)
	if statErr != nil {
		err := fmt.Errorf("APK file unavailable: %w", statErr)
		return InstallResult{Outcome: OutcomeFailed, Detail: err.Error()}, err
	}
	if !st.Mode().IsRegular() || st.Size() == 0 {
		err := errors.New("APK file is empty or not a regular file")
		return InstallResult{Outcome: OutcomeFailed, Detail: err.Error()}, err
	}

	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()

	onAndroid := detectOSFn()
	if onAndroid {
		if hasPath("termux-open") {
			return runTermuxOpenInstall(ctx, apkPath, onLine)
		}
		if hasPath("am") {
			return runAMInstall(ctx, apkPath, onLine)
		}
	}

	if hasPath("adb") {
		return runADBInstall(ctx, apkPath, allowDowngrade, onLine)
	}

	diag := "install Android platform-tools (`adb`) and enable USB/Wireless debugging, or copy the APK to your Android device"
	if onAndroid {
		diag = "install `termux-tools` (`pkg install termux-tools`) for `termux-open`, or open the saved APK from your Android file manager"
	}
	msg := fmt.Sprintf("no Android installer available (%s); APK saved at %s", diag, apkPath)
	onLine(msg)
	err := errors.New(msg)
	return InstallResult{
		Outcome:   OutcomeFailed,
		Diagnosis: diag,
		Detail:    msg,
	}, err
}

// Install preserves the error-only helper signature while returning non-nil
// errors whenever installation fails or no Android installer is available.
// Callers that display status should use InstallAPK directly to distinguish
// OutcomeInstalled from OutcomeLaunched.
func Install(apkPath string, allowDowngrade bool, onLine func(string)) error {
	_, err := InstallAPK(apkPath, allowDowngrade, onLine)
	return err
}

func runTermuxOpenInstall(ctx context.Context, apkPath string, onLine func(string)) (InstallResult, error) {
	onLine("opening Android package installer via termux-open…")
	args := []string{"--content-type", "application/vnd.android.package-archive", apkPath}
	out, err := installExecFn(ctx, "termux-open", args, onLine)
	if err != nil {
		detail := firstNonEmptyLine(out, err.Error())
		werr := fmt.Errorf("termux-open failed: %s", detail)
		return InstallResult{
			Outcome:   OutcomeFailed,
			Via:       "termux-open",
			Diagnosis: "check Termux storage/display permissions or open the saved APK from an Android file manager",
			Detail:    werr.Error(),
		}, werr
	}
	detail := "handed off to Android package installer via termux-open — accept the prompt on your device (installation not yet confirmed)"
	onLine(detail)
	return InstallResult{
		Outcome: OutcomeLaunched,
		Via:     "termux-open",
		Detail:  detail,
	}, nil
}

func runAMInstall(ctx context.Context, apkPath string, onLine func(string)) (InstallResult, error) {
	onLine("launching Android package installer intent via am start…")
	args := []string{
		"start", "-a", "android.intent.action.VIEW",
		"-t", "application/vnd.android.package-archive",
		"-d", "file://" + apkPath,
	}
	out, err := installExecFn(ctx, "am", args, onLine)
	low := strings.ToLower(out)
	if err != nil ||
		strings.Contains(low, "error:") ||
		strings.Contains(low, "exception") ||
		strings.Contains(low, "unable to resolve intent") {
		reason := firstNonEmptyLine(out, "")
		if reason == "" && err != nil {
			reason = err.Error()
		}
		if reason == "" {
			reason = "am start rejected VIEW intent"
		}
		werr := fmt.Errorf("am start failed: %s", reason)
		return InstallResult{
			Outcome:   OutcomeFailed,
			Via:       "am",
			Diagnosis: "Android blocked file:// APK intent (Android 7+ FileUriExposedException) — install `termux-tools` for `termux-open` or open the saved APK in a file manager",
			Detail:    werr.Error(),
		}, werr
	}
	detail := "handed off to Android package installer via am start — accept the prompt on your device (installation not yet confirmed)"
	onLine(detail)
	return InstallResult{
		Outcome: OutcomeLaunched,
		Via:     "am",
		Detail:  detail,
	}, nil
}

func runADBInstall(ctx context.Context, apkPath string, allowDowngrade bool, onLine func(string)) (InstallResult, error) {
	args := []string{"install", "-r"}
	if allowDowngrade {
		args = append(args, "-d")
	}
	args = append(args, apkPath)
	onLine("adb " + strings.Join(args[:len(args)-1], " ") + " <apk>")
	out, waitErr := installExecFn(ctx, "adb", args, onLine)
	return evaluateADBInstall(out, waitErr)
}

func evaluateADBInstall(out string, waitErr error) (InstallResult, error) {
	code, diag := DiagnoseFailure(out)
	hasFailure := code != "" || reFailureBrk.MatchString(out)
	hasSuccess := hasADBSuccessLine(out)

	if waitErr == nil && !hasFailure && hasSuccess {
		return InstallResult{
			Outcome: OutcomeInstalled,
			Via:     "adb",
			Detail:  "installed and confirmed via adb",
		}, nil
	}

	var reason string
	switch {
	case code != "":
		reason = code
	case hasFailure:
		reason = firstMatchingLine(out, "failure")
	case waitErr != nil:
		reason = firstNonEmptyLine(out, waitErr.Error())
	default:
		reason = firstNonEmptyLine(out, "adb exited without printing Success")
		if !strings.Contains(strings.ToLower(reason), "without") {
			reason = fmt.Sprintf("adb did not confirm installation (%s)", reason)
		}
	}

	var err error
	if diag != "" && !strings.Contains(reason, diag) {
		err = fmt.Errorf("adb install failed (%s): %s", reason, diag)
	} else {
		err = fmt.Errorf("adb install failed: %s", reason)
	}

	return InstallResult{
		Outcome:     OutcomeFailed,
		Via:         "adb",
		FailureCode: code,
		Diagnosis:   diag,
		Detail:      err.Error(),
	}, err
}

func hasADBSuccessLine(out string) bool {
	for _, ln := range strings.Split(out, "\n") {
		s := strings.TrimSpace(ln)
		if s == "Success" || strings.HasPrefix(s, "Success ") || strings.HasPrefix(s, "Success:") {
			return true
		}
	}
	return false
}

func firstNonEmptyLine(out, fallback string) string {
	for _, ln := range strings.Split(out, "\n") {
		if s := strings.TrimSpace(ln); s != "" {
			return s
		}
	}
	return fallback
}

func firstMatchingLine(out, needleLower string) string {
	for _, ln := range strings.Split(out, "\n") {
		s := strings.TrimSpace(ln)
		if strings.Contains(strings.ToLower(s), needleLower) {
			return s
		}
	}
	return firstNonEmptyLine(out, "unknown failure")
}

func defaultInstallExec(ctx context.Context, name string, args []string, onLine func(string)) (string, error) {
	cmd := activity.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		return "", err
	}
	var collected strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			ln := strings.TrimSpace(sc.Text())
			if ln != "" {
				collected.WriteString(ln)
				collected.WriteByte('\n')
				onLine(ln)
			}
		}
	}()
	waitErr := cmd.Wait()
	_ = pw.Close()
	<-done
	_ = pr.Close()
	return collected.String(), waitErr
}
