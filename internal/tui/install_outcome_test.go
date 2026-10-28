package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghapk/ghapk/internal/apk"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/ghapi"
)

func TestTaskModalHonestInstallOutcomes(t *testing.T) {
	// 1. Launched (unconfirmed) must NOT render as confirmed/installed.
	m := newTestModel()
	m.task = newTask("Installing", "app-debug.apk", progressColor{m.st.P.Accent, m.st.P.AccentAlt})
	m.task.Kind = "install"
	m.task.phase = "installing"
	m.modal = ModalTask

	mm, _ := m.Update(InstallDoneMsg{
		Result: device.InstallResult{
			Outcome: device.OutcomeLaunched,
			Via:     "termux-open",
			Detail:  "handed off to Android package installer",
		},
	})
	mo := mm.(Model)
	v := mo.View()
	if !strings.Contains(v, "installer launched (unconfirmed)") {
		t.Fatalf("expected unconfirmed launched status in modal view, got:\n%s", v)
	}
	if strings.Contains(v, "✓ installed") {
		t.Fatalf("launched intent must never render as confirmed installed:\n%s", v)
	}
	if !strings.Contains(mo.toast, "confirm on device") {
		t.Fatalf("expected honest toast for launched installer, got %q", mo.toast)
	}

	// 2. Positively confirmed install via adb renders as confirmed installed.
	m2 := newTestModel()
	m2.task = newTask("Installing", "app-debug.apk", progressColor{m2.st.P.Accent, m2.st.P.AccentAlt})
	m2.task.Kind = "install"
	m2.task.phase = "installing"
	m2.modal = ModalTask

	mm2, _ := m2.Update(InstallDoneMsg{
		Result: device.InstallResult{
			Outcome: device.OutcomeInstalled,
			Via:     "adb",
			Detail:  "installed and confirmed via adb",
		},
	})
	mo2 := mm2.(Model)
	v2 := mo2.View()
	if !strings.Contains(v2, "installed") {
		t.Fatalf("expected confirmed installed status in modal view, got:\n%s", v2)
	}
	if !strings.Contains(mo2.toast, "installed via adb") {
		t.Fatalf("expected confirmed toast, got %q", mo2.toast)
	}

	// 3. Failure surfaces diagnosis and error clearly.
	m3 := newTestModel()
	m3.task = newTask("Installing", "app-debug.apk", progressColor{m3.st.P.Accent, m3.st.P.AccentAlt})
	m3.task.Kind = "install"
	m3.task.phase = "installing"
	m3.modal = ModalTask

	mm3, _ := m3.Update(InstallDoneMsg{
		Result: device.InstallResult{
			Outcome:     device.OutcomeFailed,
			Via:         "adb",
			FailureCode: "INSTALL_FAILED_UPDATE_INCOMPATIBLE",
			Diagnosis:   "signed with a different key",
		},
		Err: errors.New("adb install failed (INSTALL_FAILED_UPDATE_INCOMPATIBLE)"),
	})
	mo3 := mm3.(Model)
	v3 := mo3.View()
	if !strings.Contains(v3, "failed") || !strings.Contains(v3, "different key") {
		t.Fatalf("expected failure and diagnosis in modal view, got:\n%s", v3)
	}
}

func TestStartInstallPromptsOnDowngradeAutoAndBlocksReflexY(t *testing.T) {
	t.Setenv("GHAPK_HISTORY", filepath.Join(t.TempDir(), "history.json"))
	apkPath := filepath.Join(t.TempDir(), "app-older.apk")
	if err := os.WriteFile(apkPath, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newTestModel()
	m.settings.Downgrade = "auto"
	m.settings.PackageName = "com.example.rocket"
	m.instPkg = "com.example.rocket"
	m.instFound = true
	m.instApp = device.InstalledApp{
		Package:     "com.example.rocket",
		VersionCode: 500,
		VersionName: "5.0.0",
		CertSHA256:  strings.Repeat("a", 64),
		Queryable:   true,
	}
	m.downloadedAPK = ghapi.ExtractedAPK{
		Path: apkPath,
		Name: "app-older.apk",
		Size: 5,
		Meta: apk.Inspection{
			HaveInfo: true,
			Info: apk.Info{
				Package:     "com.example.rocket",
				VersionCode: 400,
				VersionName: "4.0.0",
			},
			Cert: apk.Cert{SHA256: strings.Repeat("b", 64), Scheme: "v3"},
		},
	}

	cmd := m.startInstall(apkPath)
	if cmd != nil {
		t.Fatal("downgrade=auto + signature mismatch must open ModalConfirm before running installer")
	}
	if m.modal != ModalConfirm || m.confirm.Cursor != 1 {
		t.Fatalf("expected ModalConfirm with safe default Cursor=1 (Cancel), got modal=%v cursor=%d", m.modal, m.confirm.Cursor)
	}
	v := m.View()
	if !strings.Contains(v, "Downgrade warning") || !strings.Contains(v, "Signing key mismatch") {
		t.Fatalf("expected downgrade and signing key warnings in modal:\n%s", v)
	}

	// Pressing 'y' must NOT bypass safety override confirmation!
	mm, cmd2 := m.Update(key("y"))
	mo := mm.(Model)
	if mo.modal != ModalConfirm || cmd2 != nil {
		t.Fatalf("reflex 'y' must not bypass safety confirmation: modal=%v cmd=%v", mo.modal, cmd2)
	}

	// Pressing Enter on default cursor (1 = Cancel) cancels install cleanly.
	mm3, cmd3 := mo.Update(keyOf("enter"))
	mo3 := mm3.(Model)
	if mo3.modal != ModalNone || cmd3 != nil {
		t.Fatalf("Enter on default Cancel must abort install cleanly: modal=%v cmd=%v", mo3.modal, cmd3)
	}
}
