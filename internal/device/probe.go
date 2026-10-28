// Package device probes the local system and talks to an Android installer.
//
// ghapk runs in two habitats:
//   - on the Android phone itself inside Termux (the primary target)
//   - on a desktop Linux/macOS/Windows machine, downloading only or
//     pushing over adb
//
// Every probe degrades quietly when a tool is missing: on a desktop we
// still know our arch and leave SDK=0 so scoring falls back cleanly.
// Desktop file openers such as xdg-open are NEVER treated as APK installers.
package device

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Info describes the target device as well as we can tell.
type Info struct {
	OS           string   // "android" or runtime.GOOS
	Model        string   // e.g. "Pixel 7", or hostname/kernel on desktop
	Manufacturer string   // e.g. "Google", or vendor on desktop
	Release      string   // Android release ("14") or distro/OS version
	Kernel       string   // kernel release, e.g. "6.6.87"
	CPUModel     string   // CPU model name
	Cores        int      // logical CPU count
	MemTotalMB   int64    // total RAM in MiB
	MemFreeMB    int64    // available RAM in MiB
	DiskTotalGB  float64  // storage total in GiB (cache filesystem)
	DiskFreeGB   float64  // storage free in GiB
	Uptime       string   // human uptime ("3d 4h")
	Shell        string   // user's shell basename
	Terminal     string   // TERM / TERM_PROGRAM
	ABIs         []string // ordered preference, Android names ("arm64-v8a", ...)
	SDK          int      // Android API level, 0 if unknown
	CanInstall   bool     // at least one real Android installer path is available
	InstallVia   string   // "termux-open", "am", "adb", or ""
	HasRish      bool     // Shizuku rish shell is available and responsive
	ADBConnected bool     // adb is present and at least one device is in "device" state
}

// Injectable hooks for deterministic unit testing in package device.
var (
	lookPathFn   = exec.LookPath
	detectOSFn   = detectAndroidEnv
	probeCmdHook func(ctx context.Context, name string, args ...string) (string, error)
)

func detectAndroidEnv() bool {
	if runtime.GOOS == "android" {
		return true
	}
	if p := os.Getenv("PREFIX"); p != "" && strings.Contains(p, "com.termux") {
		return true
	}
	if os.Getenv("TERMUX_VERSION") != "" {
		return true
	}
	return false
}

// Probe inspects the environment once; fast (<200ms) and safe to call from
// a goroutine at startup.
func Probe() Info { return ProbeContext(context.Background()) }

// ProbeContext allows interactive preflights and jobs to stop device probes.
func ProbeContext(ctx context.Context) Info {
	in := Info{
		OS:    runtime.GOOS,
		Cores: runtime.NumCPU(),
	}

	onAndroid := detectOSFn()
	if onAndroid {
		in.OS = "android"
		in.Model = getpropContext(ctx, "ro.product.model")
		in.Manufacturer = getpropContext(ctx, "ro.product.manufacturer")
		in.Release = "Android " + getpropContext(ctx, "ro.build.version.release")
		if v, err := strconv.Atoi(getpropContext(ctx, "ro.build.version.sdk")); err == nil {
			in.SDK = v
		}
		if list := getpropContext(ctx, "ro.product.cpu.abilist"); list != "" {
			for _, a := range strings.Split(list, ",") {
				if a = strings.TrimSpace(a); a != "" {
					in.ABIs = append(in.ABIs, a)
				}
			}
		}
		in.HasRish = probeRishContext(ctx)
	}

	if len(in.ABIs) == 0 {
		if a := goarchToABI(runtime.GOARCH); a != "" {
			in.ABIs = []string{a}
		}
	}

	probeHost(&in)

	in.InstallVia = pickInstaller(onAndroid)
	if in.InstallVia == "adb" || hasPath("adb") {
		in.ADBConnected = probeADBConnectedContext(ctx)
	}
	if !onAndroid && in.ADBConnected {
		// Host architecture is not the attached Android device's architecture.
		in.ABIs = nil
		if value, err := runShortCmdContext(ctx, 2*time.Second, "adb", "shell", "getprop", "ro.product.cpu.abilist"); err == nil {
			for _, a := range strings.Split(strings.TrimSpace(value), ",") {
				a = strings.TrimSpace(a)
				if a != "" && strings.IndexFunc(a, func(r rune) bool {
					return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
				}) < 0 {
					in.ABIs = append(in.ABIs, a)
				}
			}
		}
		if value, err := runShortCmdContext(ctx, 2*time.Second, "adb", "shell", "getprop", "ro.build.version.sdk"); err == nil {
			if sdk, e := strconv.Atoi(strings.TrimSpace(value)); e == nil {
				in.SDK = sdk
			}
		}
	}
	in.CanInstall = in.InstallVia != ""
	return in
}

func hasPath(bin string) bool {
	_, err := lookPathFn(bin)
	return err == nil
}

// pickInstaller selects a genuine Android installation or package-installer
// intent mechanism. Generic desktop openers (such as xdg-open or open) are
// never treated as APK installers.
func pickInstaller(onAndroid bool) string {
	if onAndroid {
		if hasPath("termux-open") {
			return "termux-open"
		}
		if hasPath("am") {
			return "am"
		}
	}
	if hasPath("adb") {
		return "adb"
	}
	return ""
}

func probeRish() bool { return probeRishContext(context.Background()) }
func probeRishContext(ctx context.Context) bool {
	if !hasPath("rish") {
		return false
	}
	out, err := runShortCmdContext(ctx, 2*time.Second, "rish", "-c", "id")
	return err == nil && strings.Contains(out, "uid=")
}

func probeADBConnected() bool { return probeADBConnectedContext(context.Background()) }
func probeADBConnectedContext(ctx context.Context) bool {
	if !hasPath("adb") {
		return false
	}
	out, err := runShortCmdContext(ctx, 2*time.Second, "adb", "get-state")
	return err == nil && strings.TrimSpace(out) == "device"
}

func runShortCmd(timeout time.Duration, name string, args ...string) (string, error) {
	return runShortCmdContext(context.Background(), timeout, name, args...)
}
func runShortCmdContext(parent context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if probeCmdHook != nil {
		return probeCmdHook(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	b, err := cmd.CombinedOutput()
	return string(b), err
}

// probeHost fills the cross-platform hardware/OS fields from /proc and env.
func probeHost(in *Info) {
	// kernel release
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		in.Kernel = strings.TrimSpace(string(b))
	}
	// distro name on Linux (when not Android)
	if in.OS != "android" {
		if name := readOSRelease(); name != "" {
			in.Release = name
		}
		if in.Model == "" {
			if b, err := os.ReadFile("/sys/devices/virtual/dmi/id/product_name"); err == nil {
				m := strings.TrimSpace(string(b))
				if m != "" && m != "System Product Name" {
					in.Model = m
				}
			}
		}
		if in.Model == "" {
			if h, err := os.Hostname(); err == nil {
				in.Model = h
			}
		}
	}
	// CPU model
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			for _, key := range []string{"Hardware", "model name", "Processor"} {
				if strings.HasPrefix(line, key) {
					if _, v, ok := strings.Cut(line, ":"); ok {
						if v = strings.TrimSpace(v); v != "" {
							in.CPUModel = v
							break
						}
					}
				}
			}
			if in.CPUModel != "" {
				break
			}
		}
	}
	// RAM from /proc/meminfo
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			switch {
			case strings.HasPrefix(line, "MemTotal:"):
				in.MemTotalMB = parseMemKB(line) / 1024
			case strings.HasPrefix(line, "MemAvailable:"):
				in.MemFreeMB = parseMemKB(line) / 1024
			}
		}
	}
	// uptime
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(b))
		if len(fields) > 0 {
			if secs, err := strconv.ParseFloat(fields[0], 64); err == nil {
				in.Uptime = humanUptime(int64(secs))
			}
		}
	}
	// disk space on home/cache via df -k (portable, avoids syscall differences)
	target := os.Getenv("HOME")
	if target == "" {
		target = "."
	}
	if out, err := exec.Command("df", "-Pk", target).Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) >= 2 {
			f := strings.Fields(lines[len(lines)-1])
			if len(f) >= 4 {
				tot, _ := strconv.ParseFloat(f[1], 64)
				avail, _ := strconv.ParseFloat(f[3], 64)
				in.DiskTotalGB = tot / (1024 * 1024)
				in.DiskFreeGB = avail / (1024 * 1024)
			}
		}
	}
	// shell & terminal
	if sh := os.Getenv("SHELL"); sh != "" {
		if i := strings.LastIndex(sh, "/"); i >= 0 {
			sh = sh[i+1:]
		}
		in.Shell = sh
	}
	term := os.Getenv("TERM_PROGRAM")
	if term == "" && os.Getenv("TERMUX_VERSION") != "" {
		term = "termux " + os.Getenv("TERMUX_VERSION")
	}
	if term == "" {
		term = os.Getenv("TERM")
	}
	in.Terminal = term
}

func readOSRelease() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

func parseMemKB(line string) int64 {
	for _, f := range strings.Fields(line) {
		if n, err := strconv.ParseInt(f, 10, 64); err == nil {
			return n
		}
	}
	return 0
}

func humanUptime(secs int64) string {
	d := secs / 86400
	h := (secs % 86400) / 3600
	m := (secs % 3600) / 60
	switch {
	case d > 0:
		return strconv.FormatInt(d, 10) + "d " + strconv.FormatInt(h, 10) + "h"
	case h > 0:
		return strconv.FormatInt(h, 10) + "h " + strconv.FormatInt(m, 10) + "m"
	default:
		return strconv.FormatInt(m, 10) + "m"
	}
}

func getprop(key string) string { return getpropContext(context.Background(), key) }
func getpropContext(parent context.Context, key string) string {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "getprop", key)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func goarchToABI(arch string) string {
	switch arch {
	case "arm64":
		return "arm64-v8a"
	case "arm":
		return "armeabi-v7a"
	case "amd64":
		return "x86_64"
	case "386":
		return "x86"
	}
	return ""
}

// Word returns a short human label for the device panel.
func (in Info) Word() string {
	if in.OS == "android" {
		m := in.Model
		if m == "" {
			m = "Android"
		}
		if len(in.ABIs) > 0 {
			return m + " · " + in.ABIs[0]
		}
		return m
	}
	if len(in.ABIs) > 0 {
		return in.OS + " · " + in.ABIs[0]
	}
	return in.OS
}
