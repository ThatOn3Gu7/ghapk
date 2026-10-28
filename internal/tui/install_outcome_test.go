package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/ghapk/ghapk/internal/device"
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
