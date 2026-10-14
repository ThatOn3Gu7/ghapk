// Package device probes the machine ghapk runs on. On Android (Termux) it
// reads ABIs, SDK level and installed packages; anywhere else it degrades
// gracefully and reports "cannot install APKs here".
package device

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Info is everything the UI shows about the host device.
type Info struct {
	OS          string // "android", "linux", "darwin", ...
	Model       string
	ABIs        []string
	SDK         int
	CanInstall  bool
	InstallVia  string // best installer available: termux-open / am / adb / xdg-open / ""
	HasDumpsys  bool
	HasGetprop  bool
	HasPackageM bool

	// general spec sheet — probed on every OS, upgraded from fastfetch
	// when that is installed
	Release      string // Android 15, "Ubuntu 24.04", macOS 15.1…
	Manufacturer string
	Kernel       string
	Uptime       string
	Cores        int
	MemTotalMB   int64
	MemFreeMB    int64
	DiskTotalGB  float64
	DiskFreeGB   float64
	CPUModel     string
	Shell        string
	Terminal     string
	Fastfetch    bool // true when the numbers came from fastfetch
}

// have reports whether a command resolves on PATH.
func have(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// androidBin mirrors _android_bin: /system/bin is not on Termux's PATH.
func androidBin(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	p := "/system/bin/" + name
	if have("sh") {
		return p // existence checked by exec error at run time; fine for /system/bin
	}
	return name
}

func run(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := newCmdTimeout(timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

// IsAndroid mirrors the script's host detection.
func IsAndroid() bool {
	if have("getprop") || fileExists("/system/bin/getprop") {
		out, err := run(3*time.Second, androidBin("getprop"), "ro.build.version.sdk")
		if err == nil && out != "" {
			return true
		}
	}
	return false
}

func fileExists(p string) bool {
	_, err := exec.LookPath(p)
	if err == nil {
		return true
	}
	return strings.HasPrefix(p, "/system/") // keep candidate; run() will fail fast if absent
}

// Probe collects device information. Cheap, safe to call on any OS.
func Probe() Info {
	var in Info
	in.OS = hostOS()
	if !IsAndroid() {
		in.OS = hostOS()
		if have("adb") {
			in.InstallVia = "adb"
		} else if have("xdg-open") {
			in.InstallVia = "xdg-open"
		}
		probeGeneral(&in)
		return in
	}
	in.OS = "android"
	in.CanInstall = true
	in.HasGetprop = true
	get := func(prop string) string {
		out, err := run(3*time.Second, androidBin("getprop"), prop)
		if err != nil {
			return ""
		}
		return out
	}
	in.Manufacturer = strings.TrimSpace(get("ro.product.manufacturer"))
	in.Model = strings.TrimSpace(get("ro.product.model"))
	rel := strings.TrimSpace(get("ro.build.version.release"))
	if rel != "" {
		in.Release = "Android " + rel
	}
	if codename := strings.TrimSpace(get("ro.build.version.codename")); codename != "" && codename != "REL" {
		in.Release += " (" + codename + ")"
	}
	if s := get("ro.build.version.sdk"); s != "" {
		in.SDK, _ = strconv.Atoi(s)
	}
	abilist := get("ro.product.cpu.abilist")
	if abilist == "" {
		abilist = get("ro.product.cpu.abi")
	}
	for _, a := range strings.Split(abilist, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			in.ABIs = append(in.ABIs, a)
		}
	}
	in.HasDumpsys = execExists(androidBin("dumpsys"))
	in.HasPackageM = execExists(androidBin("pm"))
	// chip: SooC model > hardware chipname > board platform
	for _, prop := range []string{"ro.soc.model", "ro.hardware.chipname", "ro.board.platform"} {
		if v := strings.TrimSpace(get(prop)); v != "" {
			in.CPUModel = v
			break
		}
	}
	switch {
	case execExists("termux-open") || execExists(androidBin("termux-open")):
		in.InstallVia = "termux-open"
	case in.HasPackageM:
		in.InstallVia = "am"
	case have("adb"):
		in.InstallVia = "adb"
	}
	probeGeneral(&in)
	return in
}

// probeGeneral fills the OS-neutral spec fields. When fastfetch is
// installed its JSON output provides the polished values (CPU model,
// disk, terminal…); otherwise we read /proc and friends ourselves.
func probeGeneral(in *Info) {
	in.Cores = runtime.NumCPU()
	in.Kernel = unameLine()
	in.Uptime = uptimeLine()
	in.MemTotalMB, in.MemFreeMB = memInfoLine()
	in.DiskTotalGB, in.DiskFreeGB = diskLine()
	in.CPUModel = cpuModelLine()
	in.Shell = shellLine()
	in.Terminal = termLine()
	if in.Release == "" {
		in.Release = osReleaseLine()
	}
	if ff, ok := fastfetchJSON(); ok {
		applyFastfetch(in, ff)
	}
}

func execExists(p string) bool {
	if strings.Contains(p, "/") {
		cmd := exec.Command("test", "-x", p)
		return cmd.Run() == nil
	}
	return have(p)
}

// ScoreDevice adapts Info to the score package's view of the device.
func (in Info) ScoreDevice() (abis []string, sdk int) { return in.ABIs, in.SDK }

// Word is the one-word host summary the doctor prints (_host_word).
func (in Info) Word() string {
	if in.OS == "android" {
		if in.SDK > 0 {
			return fmt.Sprintf("Android (API %d)", in.SDK)
		}
		return "Android"
	}
	return in.OS
}

// InstalledApp is what pm/dumpsys know about a package.
type InstalledApp struct {
	Package     string
	VersionName string
	VersionCode int64
	ABI         string
}

var (
	verNameRe = regexp.MustCompile(`versionName=([^\s]+)`)
	verCodeRe = regexp.MustCompile(`versionCode=([0-9]+)`)
	abiRe     = regexp.MustCompile(`primaryCpuAbi=([^\s]+)`)
	listVerRe = regexp.MustCompile(`^(\S+) versionCode:([0-9]+)`)
)

// QueryPackage mirrors installed_info in the script: try pm list with
// --show-versioncode first, then dumpsys/pm dump for versionName and ABI.
func QueryPackage(pkg string) (InstalledApp, bool) {
	res := InstalledApp{Package: pkg}
	if pkg == "" {
		return res, false
	}
	found := false
	if execExists(androidBin("pm")) {
		if out, err := run(10*time.Second, androidBin("pm"), "list", "packages", "--show-versioncode", pkg); err == nil {
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "package:"))
				if strings.HasPrefix(line, pkg+" ") || strings.HasPrefix(line, pkg+" versionCode") || line == pkg {
					found = true
					if m := listVerRe.FindStringSubmatch(line); m != nil {
						res.VersionCode, _ = strconv.ParseInt(m[2], 10, 64)
					}
				}
			}
		}
	}
	// detail pass: dumpsys (or pm dump)
	for _, tool := range []string{"dumpsys", "pm"} {
		if !execExists(androidBin(tool)) {
			continue
		}
		var out string
		var err error
		if tool == "dumpsys" {
			out, err = run(15*time.Second, androidBin("dumpsys"), "package", pkg)
		} else {
			out, err = run(15*time.Second, androidBin("pm"), "dump", pkg)
		}
		if err != nil || !strings.Contains(out, "versionCode") {
			continue
		}
		found = true
		if res.VersionCode == 0 {
			if m := verCodeRe.FindStringSubmatch(out); m != nil {
				res.VersionCode, _ = strconv.ParseInt(m[1], 10, 64)
			}
		}
		if m := verNameRe.FindStringSubmatch(out); m != nil {
			res.VersionName = m[1]
		}
		if m := abiRe.FindStringSubmatch(out); m != nil {
			res.ABI = m[1]
		}
		break
	}
	return res, found
}
