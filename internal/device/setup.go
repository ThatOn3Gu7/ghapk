package device

import (
	"context"
	"strings"
	"time"
)

// Setup reports actual tools and whether a package-manager query succeeded.
type Setup struct {
	Installers     []string
	RishPresent    bool
	Notifications  bool
	PackageManager string // successful route; empty when unavailable
}

func ProbeSetupContext(ctx context.Context, in Info) Setup {
	var s Setup
	for _, name := range []string{"termux-open", "am", "adb"} {
		if hasPath(name) {
			s.Installers = append(s.Installers, name)
		}
	}
	s.RishPresent = hasPath("rish")
	s.Notifications = hasPath("termux-notification")
	type candidate struct {
		bin   string
		args  []string
		route string
		dump  bool
	}
	var cmds []candidate
	if in.OS == "android" {
		if s.RishPresent {
			cmds = append(cmds, candidate{"rish", []string{"-c", "cmd package list packages android"}, "rish", false}, candidate{"rish", []string{"-c", "dumpsys package android"}, "rish", true})
		}
		cmds = append(cmds, candidate{"cmd", []string{"package", "list", "packages", "android"}, "cmd", false}, candidate{"pm", []string{"list", "packages", "android"}, "pm", false}, candidate{"dumpsys", []string{"package", "android"}, "dumpsys", true}, candidate{"pm", []string{"dump", "android"}, "pm", true})
	}
	if in.ADBConnected {
		cmds = append(cmds, candidate{"adb", []string{"shell", "pm", "list", "packages", "android"}, "adb", false}, candidate{"adb", []string{"shell", "dumpsys", "package", "android"}, "adb", true})
	}
	for _, c := range cmds {
		if ctx.Err() != nil {
			break
		}
		if !hasPath(c.bin) {
			continue
		}
		out, err := runShortCmdContext(ctx, 1500*time.Millisecond, c.bin, c.args...)
		if err == nil && (containsFrameworkPackage(out) || c.dump && containsFrameworkDump(out)) && !strings.Contains(out, "SecurityException") && !strings.Contains(out, "Permission Denial") {
			s.PackageManager = c.route
			break
		}
	}
	return s
}

func containsFrameworkPackage(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "package:android" {
			return true
		}
	}
	return false
}

func containsFrameworkDump(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Package [android] ") || strings.TrimSpace(line) == "Package [android]" {
			return true
		}
	}
	return false
}
