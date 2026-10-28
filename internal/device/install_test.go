package device

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createDummyAPK(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app-debug.apk")
	if err := os.WriteFile(p, []byte("PK\x03\x04dummy-apk-payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func withDeviceStubs(
	t *testing.T,
	onAndroid bool,
	bins map[string]bool,
	execFn func(ctx context.Context, name string, args []string, onLine func(string)) (string, error),
	probeFn func(ctx context.Context, name string, args ...string) (string, error),
) {
	t.Helper()
	origLook := lookPathFn
	origOS := detectOSFn
	origExec := installExecFn
	origProbe := probeCmdHook
	t.Cleanup(func() {
		lookPathFn = origLook
		detectOSFn = origOS
		installExecFn = origExec
		probeCmdHook = origProbe
	})
	detectOSFn = func() bool { return onAndroid }
	lookPathFn = func(file string) (string, error) {
		if bins[file] {
			return "/mock/bin/" + file, nil
		}
		return "", errors.New("not found")
	}
	if execFn != nil {
		installExecFn = execFn
	}
	if probeFn != nil {
		probeCmdHook = probeFn
	}
}

func TestXdgOpenNeverTreatedAsInstaller(t *testing.T) {
	apkPath := createDummyAPK(t)
	calledXdg := false

	withDeviceStubs(t, false, map[string]bool{
		"xdg-open":    true,
		"termux-open": true, // even if present on non-Android desktop, ignore Android-only openers
		"am":          true,
	}, func(ctx context.Context, name string, args []string, onLine func(string)) (string, error) {
		if name == "xdg-open" {
			calledXdg = true
		}
		t.Fatalf("unexpected command execution on desktop without adb: %s %v", name, args)
		return "", nil
	}, nil)

	info := Probe()
	if info.CanInstall || info.InstallVia != "" {
		t.Fatalf("desktop with xdg-open and no adb must not claim CanInstall: got CanInstall=%v InstallVia=%q", info.CanInstall, info.InstallVia)
	}

	res, err := InstallAPK(apkPath, false, nil)
	if err == nil {
		t.Fatal("expected error when no real Android installer is available on desktop")
	}
	if res.Outcome != OutcomeFailed || res.Confirmed() || res.Launched() {
		t.Fatalf("expected OutcomeFailed, got %+v", res)
	}
	if calledXdg {
		t.Fatal("xdg-open must never be invoked as an APK installer")
	}
}

func TestADBInstallConfirmedSuccess(t *testing.T) {
	apkPath := createDummyAPK(t)
	var gotArgs []string

	withDeviceStubs(t, false, map[string]bool{"adb": true},
		func(ctx context.Context, name string, args []string, onLine func(string)) (string, error) {
			if name != "adb" {
				t.Fatalf("expected adb, got %s", name)
			}
			gotArgs = append([]string(nil), args...)
			out := "Performing Streamed Install\nSuccess\n"
			onLine("Performing Streamed Install")
			onLine("Success")
			return out, nil
		},
		func(ctx context.Context, name string, args ...string) (string, error) {
			return "device\n", nil
		},
	)

	info := Probe()
	if !info.CanInstall || info.InstallVia != "adb" || !info.ADBConnected {
		t.Fatalf("unexpected probe: %+v", info)
	}

	res, err := InstallAPK(apkPath, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Outcome != OutcomeInstalled || !res.Confirmed() || res.Via != "adb" {
		t.Fatalf("expected confirmed OutcomeInstalled via adb, got %+v", res)
	}
	for _, a := range gotArgs {
		if a == "-d" {
			t.Fatalf("did not expect -d when allowDowngrade=false: %v", gotArgs)
		}
	}
}

func TestADBInstallExitZeroWithFailureOutputIsRejected(t *testing.T) {
	apkPath := createDummyAPK(t)

	// Older adb builds can exit 0 while printing Failure [INSTALL_FAILED_...].
	withDeviceStubs(t, false, map[string]bool{"adb": true},
		func(ctx context.Context, name string, args []string, onLine func(string)) (string, error) {
			return "Performing Streamed Install\nFailure [INSTALL_FAILED_UPDATE_INCOMPATIBLE: Package com.example signatures do not match]\n", nil
		}, nil)

	res, err := InstallAPK(apkPath, false, nil)
	if err == nil {
		t.Fatal("expected error when adb prints Failure [...] despite exit code 0")
	}
	if res.Outcome != OutcomeFailed || res.Confirmed() {
		t.Fatalf("expected OutcomeFailed, got %+v", res)
	}
	if res.FailureCode != "INSTALL_FAILED_UPDATE_INCOMPATIBLE" {
		t.Fatalf("FailureCode = %q, want INSTALL_FAILED_UPDATE_INCOMPATIBLE", res.FailureCode)
	}
	if !strings.Contains(res.Diagnosis, "different key") {
		t.Fatalf("expected signature-mismatch diagnosis, got %q", res.Diagnosis)
	}
}

func TestADBInstallExitZeroWithoutSuccessIsUnconfirmedFailure(t *testing.T) {
	apkPath := createDummyAPK(t)

	withDeviceStubs(t, false, map[string]bool{"adb": true},
		func(ctx context.Context, name string, args []string, onLine func(string)) (string, error) {
			return "Performing Streamed Install\n", nil
		}, nil)

	res, err := InstallAPK(apkPath, false, nil)
	if err == nil || res.Outcome != OutcomeFailed || res.Confirmed() {
		t.Fatalf("adb exiting 0 without Success must not be treated as installed: res=%+v err=%v", res, err)
	}
}

func TestADBInstallNoDevicesDiagnosis(t *testing.T) {
	apkPath := createDummyAPK(t)

	withDeviceStubs(t, false, map[string]bool{"adb": true},
		func(ctx context.Context, name string, args []string, onLine func(string)) (string, error) {
			return "adb: error: failed to get feature set: no devices/emulators found\n", errors.New("exit status 1")
		}, nil)

	res, err := InstallAPK(apkPath, false, nil)
	if err == nil || res.Outcome != OutcomeFailed {
		t.Fatalf("expected failure when no adb device connected, got res=%+v err=%v", res, err)
	}
	if !strings.Contains(strings.ToLower(res.Diagnosis), "debugging") {
		t.Fatalf("expected actionable debugging diagnosis, got %q", res.Diagnosis)
	}
}

func TestTermuxOpenReturnsLaunchedNotInstalled(t *testing.T) {
	apkPath := createDummyAPK(t)

	withDeviceStubs(t, true, map[string]bool{"termux-open": true, "xdg-open": true},
		func(ctx context.Context, name string, args []string, onLine func(string)) (string, error) {
			if name != "termux-open" {
				t.Fatalf("expected termux-open on Termux, got %s", name)
			}
			return "", nil
		}, nil)

	res, err := InstallAPK(apkPath, false, nil)
	if err != nil {
		t.Fatalf("unexpected error launching termux-open: %v", err)
	}
	if res.Outcome != OutcomeLaunched || res.Confirmed() || !res.Launched() {
		t.Fatalf("termux-open must return OutcomeLaunched (unconfirmed), got %+v", res)
	}
}

func TestAMStartDetectsInlineErrorDespiteExitZero(t *testing.T) {
	apkPath := createDummyAPK(t)

	withDeviceStubs(t, true, map[string]bool{"am": true},
		func(ctx context.Context, name string, args []string, onLine func(string)) (string, error) {
			return "Starting: Intent { act=android.intent.action.VIEW }\nError: Activity not started, unable to resolve Intent\n", nil
		}, nil)

	res, err := InstallAPK(apkPath, false, nil)
	if err == nil || res.Outcome != OutcomeFailed {
		t.Fatalf("expected am start inline Error: to fail, got res=%+v err=%v", res, err)
	}

	// Clean am start output -> OutcomeLaunched (never OutcomeInstalled)
	withDeviceStubs(t, true, map[string]bool{"am": true},
		func(ctx context.Context, name string, args []string, onLine func(string)) (string, error) {
			return "Starting: Intent { act=android.intent.action.VIEW dat=file:///... }\n", nil
		}, nil)

	res, err = InstallAPK(apkPath, false, nil)
	if err != nil || res.Outcome != OutcomeLaunched || res.Confirmed() {
		t.Fatalf("clean am start must return OutcomeLaunched, got res=%+v err=%v", res, err)
	}
}

func TestValidPackageStrict(t *testing.T) {
	good := []string{"com.example.app", "org.termux.x11", "a.b2_c.d"}
	for _, g := range good {
		if !ValidPackage(g) {
			t.Errorf("expected valid package %q", g)
		}
	}
	bad := []string{"", "singleword", "com..app", "com.example;id", "com.example/app", "1com.example"}
	for _, b := range bad {
		if ValidPackage(b) {
			t.Errorf("expected invalid package %q", b)
		}
	}
}
