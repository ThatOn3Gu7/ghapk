package device

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// UninstallMismatchedRishContext is used only after an explicit, per-action
// destructive confirmation. Never trust history or a possibly remote adb
// device: re-read this exact package through local Shizuku immediately first.
func UninstallMismatchedRishContext(ctx context.Context, pkg, originalCert, apkCert string) error {
	if !detectOSFn() || !ValidPackage(pkg) || originalCert == "" || apkCert == "" || strings.EqualFold(originalCert, apkCert) || !probeRishContext(ctx) {
		return errors.New("local Shizuku or a verified signing-key clash is unavailable")
	}
	user := os.Getuid() / 100000
	current, err := runShortCmdContext(ctx, 3*time.Second, "rish", "-c", "am get-current-user")
	currentUser, parseErr := strconv.Atoi(strings.TrimSpace(current))
	if err != nil || parseErr != nil || currentUser != user {
		return errors.New("cannot verify the current Android user; refusing to uninstall")
	}
	listed, err := runShortCmdContext(ctx, 3*time.Second, "rish", "-c", fmt.Sprintf("pm list packages --user %d %s", user, pkg))
	listedHere := false
	for _, line := range strings.Split(listed, "\n") {
		if strings.TrimSpace(line) == "package:"+pkg {
			listedHere = true
		}
	}
	if err != nil || !listedHere {
		return errors.New("package not confirmed for the current user")
	}
	out, err := runShortCmdContext(ctx, 3*time.Second, "rish", "-c", "dumpsys package "+pkg)
	if err != nil || strings.Contains(out, "SecurityException") || strings.Contains(out, "Permission Denial") {
		return errors.New("could not recheck the app through Shizuku")
	}
	app, found := parseDumpsysPackage(pkg, out)
	if found && app.CertSHA256 == "" {
		app.CertSHA256 = readInstalledAPKCert(out)
	}
	if !found || !strings.EqualFold(app.CertSHA256, originalCert) {
		return errors.New("installed package or signing key changed; refusing to uninstall")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	out, err = runShortCmdContext(ctx, 30*time.Second, "rish", "-c", fmt.Sprintf("pm uninstall --user %d %s", user, pkg))
	if err != nil || strings.TrimSpace(out) != "Success" {
		return fmt.Errorf("Android did not confirm uninstall: %s", strings.TrimSpace(out))
	}
	return nil
}
