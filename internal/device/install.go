package device

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Install hands a downloaded APK over to the platform's installer, exactly
// following the script's hand-off order:
//
//	termux-open → am start (VIEW intent) → adb install → xdg-open → print path
//
// downgrade=true maps to `adb install -d` / the pm flags the script used.
func Install(path string, allowDowngrade bool, progress func(string)) error {
	log := func(format string, a ...any) {
		if progress != nil {
			progress(fmt.Sprintf(format, a...))
		}
	}

	if execExists("termux-open") || execExists(androidBin("termux-open")) {
		log("opening with termux-open…")
		return runStream("termux-open", []string{path}, func(s string) { log("%s", s) })
	}
	if IsAndroid() && execExists(androidBin("am")) {
		log("sending VIEW intent with am start…")
		args := []string{
			"start", "-a", "android.intent.action.VIEW",
			"-d", "file://" + path,
			"-t", "application/vnd.android.package-archive",
		}
		if err := runStream(androidBin("am"), args, func(s string) { log("%s", s) }); err == nil {
			return nil
		}
	}
	if have("adb") {
		args := []string{"install", "-r"}
		if allowDowngrade {
			args = append(args, "-d")
		}
		args = append(args, path)
		log("installing with adb (%s)…", strings.Join(args, " "))
		return runStream("adb", args, func(s string) { log("%s", s) })
	}
	if have("xdg-open") {
		log("no android installer available; opening with xdg-open…")
		cmd := exec.Command("xdg-open", path)
		if err := cmd.Start(); err != nil {
			return err
		}
		return nil
	}
	log("no installer found on this machine — the APK is at:\n  %s", path)
	return nil
}

// CanInstallHere reports whether any handoff path exists.
func CanInstallHere() bool {
	return execExists("termux-open") || execExists(androidBin("am")) ||
		have("adb") || have("xdg-open")
}

// runStream runs a command streaming combined output lines into log.
func runStream(name string, args []string, log func(string)) error {
	cmd := exec.Command(name, args...)
	pr, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	buf := make([]byte, 4096)
	var line strings.Builder
	for {
		n, err := pr.Read(buf)
		if n > 0 {
			for _, c := range string(buf[:n]) {
				if c == '\n' {
					if s := strings.TrimSpace(line.String()); s != "" {
						log(s)
					}
					line.Reset()
				} else {
					line.WriteRune(c)
				}
			}
		}
		if err != nil {
			break
		}
	}
	if s := strings.TrimSpace(line.String()); s != "" {
		log(s)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Minute):
		_ = cmd.Process.Kill()
		return fmt.Errorf("%s timed out", name)
	}
}
