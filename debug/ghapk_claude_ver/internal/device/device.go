// Package device is everything about the phone ghapk runs on: which ABIs and
// Android version it has, which version of an app is installed, how to hand an
// APK to the installer, and how to check afterwards that it landed.
//
// Android restricts pm/cmd/dumpsys to the shell user from Android 14 on, so
// there are up to four "doors" to the package manager: a direct list or dump,
// and the same two through Shizuku's rish. The first live one is used.
package device

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"ghapk/internal/pick"
)

var androidDirs = []string{"/system/bin", "/system/xbin", "/system_ext/bin", "/product/bin", "/vendor/bin", "/odm/bin", "/sbin"}

// Info describes the machine.
type Info struct {
	Android bool
	Host    string // "Android", "Linux", "macOS"
	SDK     int    // API level, 0 if unknown
	Release string // "14"
	Model   string
	ABIs    []string // as reported, best first
	Cats    []string // arm64 armv7 x86_64 x86, best first
	Arch    string   // primary ABI
}

// Detect reads the machine's properties.
func Detect() Info {
	in := Info{}
	in.Android = os.Getenv("TERMUX_VERSION") != "" || exists("/system/bin/getprop") ||
		strings.Contains(os.Getenv("PREFIX"), "com.termux") || runtime.GOOS == "android"
	switch {
	case in.Android:
		in.Host = "Android"
	case runtime.GOOS == "darwin":
		in.Host = "macOS"
	default:
		in.Host = "Linux"
	}
	if gp := androidBin("getprop"); gp != "" {
		in.SDK, _ = strconv.Atoi(onlyDigits(prop(gp, "ro.build.version.sdk")))
		in.Release = prop(gp, "ro.build.version.release")
		in.Model = prop(gp, "ro.product.model")
		if l := prop(gp, "ro.product.cpu.abilist"); l != "" {
			for _, a := range strings.Split(l, ",") {
				if a = strings.TrimSpace(a); a != "" {
					in.ABIs = append(in.ABIs, a)
				}
			}
		}
		if len(in.ABIs) == 0 {
			if a := prop(gp, "ro.product.cpu.abi"); a != "" {
				in.ABIs = []string{a}
			}
		}
	}
	if len(in.ABIs) == 0 {
		switch runtime.GOARCH {
		case "arm64":
			in.ABIs = []string{"arm64-v8a", "armeabi-v7a", "armeabi"}
		case "arm":
			in.ABIs = []string{"armeabi-v7a", "armeabi"}
		case "amd64":
			in.ABIs = []string{"x86_64", "x86"}
		case "386":
			in.ABIs = []string{"x86"}
		default:
			in.ABIs = []string{"unknown"}
		}
	}
	in.Cats = pick.Cats(in.ABIs)
	in.Arch = in.ABIs[0]
	return in
}

func prop(gp, name string) string {
	out, err := exec.Command(gp, name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(string(out), "\r", ""))
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// androidBin finds a tool on PATH or in the places Android keeps its own. On
// Termux /system/bin is not on PATH, so pm, cmd, am and dumpsys live out of sight.
func androidBin(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, d := range androidDirs {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// scrubbed is the environment for Android's own tools. Termux sets
// LD_LIBRARY_PATH and LD_PRELOAD for its own binaries, and Android's tools
// crash or misbehave when they inherit them.
func scrubbed() []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "LD_LIBRARY_PATH=") || strings.HasPrefix(e, "LD_PRELOAD=") || strings.HasPrefix(e, "PATH=") {
			continue
		}
		env = append(env, e)
	}
	return append(env, "PATH="+strings.Join(androidDirs, ":")+":"+os.Getenv("PATH"))
}

func runTool(ctx context.Context, timeout time.Duration, bin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = scrubbed()
	var out, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &se
	err := cmd.Run()
	return out.String(), err
}

// ---- doors ------------------------------------------------------------------

// Door names how the package manager can be reached.
type Door string

const (
	DoorList     Door = "list"      // cmd/pm package list, direct
	DoorDump     Door = "dump"      // dumpsys/pm dump, direct
	DoorRishList Door = "rish-list" // the same through Shizuku
	DoorRishDump Door = "rish-dump"
	DoorNone     Door = "none"
)

// Device is the live connection to the phone's package manager.
type Device struct {
	Info
	mu     sync.Mutex
	door   Door
	probed bool
	rish   string
}

// New detects the machine. Probing the package manager is lazy.
func New() *Device { return &Device{Info: Detect()} }

// CanInstall reports whether this host can hand an APK to an installer.
func (d *Device) CanInstall() bool { return d.Android }

func (d *Device) rishPath() string {
	if d.rish != "" {
		return d.rish
	}
	if v := os.Getenv("GHAPK_RISH"); v != "" {
		switch strings.ToLower(v) {
		case "0", "off", "no", "false":
			d.rish = "-"
			return ""
		default:
			d.rish = v
			return v
		}
	}
	cands := []string{"rish"}
	h, _ := os.UserHomeDir()
	pfx := os.Getenv("PREFIX")
	for _, c := range []string{filepath.Join(pfx, "bin", "rish"), filepath.Join(h, "rish"), filepath.Join(h, ".rish", "rish"),
		"/data/data/com.termux/files/usr/bin/rish", filepath.Join(pfx, "bin", "rish_auto"), filepath.Join(h, "rish_auto")} {
		cands = append(cands, c)
	}
	for _, c := range cands {
		if p, err := exec.LookPath(c); err == nil {
			d.rish = p
			return p
		}
	}
	d.rish = "-"
	return ""
}

// HasRish reports whether Shizuku's rish is installed.
func (d *Device) HasRish() bool { return d.rishPath() != "" && d.rishPath() != "-" }

func (d *Device) rishRun(ctx context.Context, cmdline string) (string, error) {
	p := d.rishPath()
	if p == "" || p == "-" {
		return "", fmt.Errorf("no rish")
	}
	t := 10 * time.Second
	if v, _ := strconv.Atoi(os.Getenv("GHAPK_RISH_TIMEOUT")); v > 0 {
		t = time.Duration(v) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, t)
	defer cancel()
	cmd := exec.CommandContext(ctx, p, "-c", cmdline)
	app := os.Getenv("RISH_APPLICATION_ID")
	if app == "" {
		app = "com.termux"
	}
	cmd.Env = append(scrubbed(), "RISH_PRESERVE_ENV=0", "RISH_APPLICATION_ID="+app)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}

// direct runs the first of the named tools that exists, with args.
func (d *Device) direct(ctx context.Context, tools [][]string) (string, bool) {
	for _, t := range tools {
		bin := androidBin(t[0])
		if bin == "" {
			continue
		}
		out, err := runTool(ctx, 8*time.Second, bin, t[1:]...)
		if err == nil && strings.TrimSpace(out) != "" {
			return out, true
		}
	}
	return "", false
}

// Probe finds the first live door once per run. It asks about the framework
// package "android" and an unfiltered list, so "this app is not installed yet"
// is never mistaken for "the door is shut".
func (d *Device) Probe(ctx context.Context) Door {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.probed {
		return d.door
	}
	d.probed = true
	d.door = DoorNone
	if !d.Android {
		return d.door
	}
	if _, ok := d.direct(ctx, [][]string{{"cmd", "package", "list", "packages"}, {"pm", "list", "packages"}}); ok {
		d.door = DoorList
	} else if _, ok := d.direct(ctx, [][]string{{"dumpsys", "package", "android"}, {"pm", "dump", "android"}}); ok {
		d.door = DoorDump
	} else if d.HasRish() {
		if o, err := d.rishRun(ctx, "pm list packages"); err == nil && strings.TrimSpace(o) != "" {
			d.door = DoorRishList
		} else if o, err := d.rishRun(ctx, "dumpsys package android"); err == nil && strings.TrimSpace(o) != "" {
			d.door = DoorRishDump
		}
	}
	return d.door
}

// Live reports whether any door to the package manager works.
func (d *Device) Live(ctx context.Context) bool { return d.Probe(ctx) != DoorNone }

// Reset forgets the probe result, e.g. after the user starts Shizuku.
func (d *Device) Reset() { d.mu.Lock(); d.probed = false; d.rish = ""; d.mu.Unlock() }

var (
	reVName = regexp.MustCompile(`versionName=([A-Za-z0-9._+-]*)`)
	reVCode = regexp.MustCompile(`versionCode=([0-9]+)`)
	reABI   = regexp.MustCompile(`primaryCpuAbi=([A-Za-z0-9_-]+)`)
	reSigs  = regexp.MustCompile(`signatures=PackageSignatures\{[^\[\]]*\[([0-9a-fA-F]{64,})\]`)
	rePkg   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)+$`)
)

// ValidPackage reports whether s looks like an Android package name. It guards
// every place a name becomes part of a shell command.
func ValidPackage(s string) bool { return rePkg.MatchString(s) }

// Installed is what the device says about one app.
type Installed struct {
	Pkg         string
	Known       bool // a door was live, so "not found" is meaningful
	Found       bool
	VersionName string
	VersionCode int64
	ABI         string
	CertSHA     string // SHA-256 of the signing certificate, when the dump shows it
}

// Query reads the installed state of pkg.
func (d *Device) Query(ctx context.Context, pkg string) Installed {
	in := Installed{Pkg: pkg}
	if pkg == "" || !ValidPackage(pkg) {
		return in
	}
	door := d.Probe(ctx)
	in.Known = door != DoorNone
	if !in.Known {
		return in
	}
	if dump := d.dump(ctx, pkg, door); dump != "" {
		if m := reVName.FindStringSubmatch(dump); m != nil {
			in.VersionName = m[1]
		}
		if m := reVCode.FindStringSubmatch(dump); m != nil {
			in.VersionCode, _ = strconv.ParseInt(m[1], 10, 64)
		}
		if m := reABI.FindStringSubmatch(dump); m != nil && m[1] != "null" {
			in.ABI = m[1]
		}
		if m := reSigs.FindStringSubmatch(dump); m != nil {
			in.CertSHA = fingerprint(m[1])
		}
	}
	if in.VersionCode == 0 {
		in.VersionCode = d.versionCode(ctx, pkg, door)
	}
	in.Found = in.VersionCode > 0
	return in
}

func (d *Device) dump(ctx context.Context, pkg string, door Door) string {
	try := func(direct bool) string {
		if direct {
			o, _ := d.direct(ctx, [][]string{{"dumpsys", "package", pkg}, {"pm", "dump", pkg}})
			return o
		}
		if o, err := d.rishRun(ctx, "dumpsys package '"+pkg+"'"); err == nil && strings.TrimSpace(o) != "" {
			return o
		}
		o, _ := d.rishRun(ctx, "pm dump '"+pkg+"'")
		return o
	}
	switch door {
	case DoorDump, DoorList:
		if o := try(true); o != "" {
			return o
		}
		if d.HasRish() {
			return try(false)
		}
	case DoorRishDump, DoorRishList:
		return try(false)
	}
	return ""
}

// VersionCode is the cheap query used while polling after an install.
func (d *Device) VersionCode(ctx context.Context, pkg string) int64 {
	if !ValidPackage(pkg) {
		return 0
	}
	return d.versionCode(ctx, pkg, d.Probe(ctx))
}

func (d *Device) versionCode(ctx context.Context, pkg string, door Door) int64 {
	want := "package:" + pkg + " versionCode:"
	parse := func(out string) int64 {
		for _, ln := range strings.Split(out, "\n") {
			ln = strings.TrimSpace(ln)
			if strings.HasPrefix(ln, want) { // the filter is a substring match, so check exactly
				n, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(ln, want)), 10, 64)
				return n
			}
		}
		return 0
	}
	switch door {
	case DoorList:
		o, _ := d.direct(ctx, [][]string{{"cmd", "package", "list", "packages", "--show-versioncode", pkg},
			{"pm", "list", "packages", "--show-versioncode", pkg}})
		if n := parse(o); n > 0 {
			return n
		}
	case DoorRishList:
		o, _ := d.rishRun(ctx, "pm list packages --show-versioncode '"+pkg+"'")
		if n := parse(o); n > 0 {
			return n
		}
	}
	if m := reVCode.FindStringSubmatch(d.dump(ctx, pkg, door)); m != nil {
		n, _ := strconv.ParseInt(m[1], 10, 64)
		return n
	}
	return 0
}

// Uninstall removes an app through Shizuku. It is only offered when the clash
// was read off the device itself.
func (d *Device) Uninstall(ctx context.Context, pkg string) error {
	if !ValidPackage(pkg) {
		return fmt.Errorf("refusing odd package name %q", pkg)
	}
	if !d.HasRish() {
		return fmt.Errorf("uninstalling needs Shizuku (rish)")
	}
	if _, err := d.rishRun(ctx, "pm uninstall '"+pkg+"'"); err != nil {
		return err
	}
	if d.VersionCode(ctx, pkg) > 0 {
		return fmt.Errorf("%s is still installed", pkg)
	}
	return nil
}

// FreeBytes is the free space on the filesystem holding path.
func FreeBytes(path string) int64 { return freeBytes(path) }
