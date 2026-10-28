// ghapk — GitHub Actions APK installer.
//
// Typed with no arguments it opens the interactive TUI (the primary
// interface). Subcommands give script-friendly, non-interactive access:
//
//	ghapk                 open the TUI
//	ghapk get [flags]     classic linear pipeline (like the original script)
//	ghapk doctor          environment health check
//	ghapk version         print version
//	ghapk help            full usage
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/flow"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui"
)

const (
	appName = "ghapk"
	tagline = "newest debug APK — your installer"
)

// appVersion is a var so the Makefile can inject it:
// go build -ldflags="-X main.appVersion=4.0.0"
var appVersion = "4.0.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "help", "--help", "-h":
			usage()
			return 0
		case "version", "--version", "-v":
			fmt.Printf("%s %s — %s\n", appName, appVersion, tagline)
			return 0
		case "doctor":
			return doctorCmd()
		case "get", "download":
			code, _ := getCmd(args[1:])
			return code
		}
	}
	// default: launch the TUI, honouring CLI flags as session overrides
	_, opts := getCmd(args)
	if err := tui.Run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "ghapk:", err)
		return 1
	}
	return 0
}

// getCmd parses shared flags and either runs the classic pipeline (when the
// "get" subcommand was used) or returns session options for the TUI.
func getCmd(args []string) (int, tui.Options) {
	settings := config.Load()
	var (
		yes     bool
		install bool
		dry     bool
		printD  string
	)
	for i := 0; i < len(args); i++ {
		a := args[i]
		val := func() string {
			if strings.Contains(a, "=") {
				return strings.SplitN(a, "=", 2)[1]
			}
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch {
		case a == "--repo" || strings.HasPrefix(a, "--repo="):
			settings.Repo = val()
		case a == "--workflow" || strings.HasPrefix(a, "--workflow="):
			settings.Workflow = val()
		case a == "--branch" || strings.HasPrefix(a, "--branch="):
			settings.Branch = val()
		case a == "--artifact" || strings.HasPrefix(a, "--artifact="):
			settings.ArtifactPattern = val()
		case a == "--file" || strings.HasPrefix(a, "--file="):
			settings.FilePattern = val()
		case a == "--exclude" || strings.HasPrefix(a, "--exclude="):
			settings.ExcludePattern = val()
		case a == "--source" || strings.HasPrefix(a, "--source="):
			settings.Source = val()
		case a == "--arch":
			i++
			settings.Arch = args[i]
		case strings.HasPrefix(a, "--arch="):
			settings.Arch = strings.TrimPrefix(a, "--arch=")
		case a == "--variant":
			i++
			settings.Variant = args[i]
		case strings.HasPrefix(a, "--variant="):
			settings.Variant = strings.TrimPrefix(a, "--variant=")
		case a == "--limit":
			i++
			fmt.Sscanf(args[i], "%d", &settings.RunLimit)
		case strings.HasPrefix(a, "--limit="):
			fmt.Sscanf(strings.TrimPrefix(a, "--limit="), "%d", &settings.RunLimit)
		case a == "--package" || strings.HasPrefix(a, "--package="):
			settings.PackageName = val()
		case a == "--downgrade" || strings.HasPrefix(a, "--downgrade="):
			settings.Downgrade = val()
		case a == "--theme" || strings.HasPrefix(a, "--theme="):
			settings.Theme = val()
		case a == "--no-open":
			settings.NoOpen = true
		case a == "--watch":
			settings.Watch = true
		case a == "-y" || a == "--yes":
			yes = true
		case a == "-i" || a == "--install":
			install = true
		case a == "--dry-run":
			dry = true
		case a == "--print-path":
			printD = config.CacheDir() + "/apk"
		case a == "-h" || a == "--help":
			usage()
			return 0, tui.Options{}
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "ghapk: unknown flag %s\n", a)
				return 2, tui.Options{}
			}
		}
	}
	opts := tui.Options{Settings: settings}
	// if this parse came from "get", run the classic pipeline
	if len(os.Args) > 1 && (os.Args[1] == "get" || os.Args[1] == "download") {
		return flow.Run(flow.GetOptions{
			Settings: settings, Yes: yes,
			Install: install, DryRun: dry, PrintDir: printD,
		}), opts
	}
	return 0, opts
}

func doctorCmd() int {
	fmt.Printf("%s %s — doctor\n\n", appName, appVersion)
	check := func(ok bool, name, good, bad string) {
		if ok {
			fmt.Printf("  ✓ %-12s %s\n", name, good)
		} else {
			fmt.Printf("  ✗ %-12s %s\n", name, bad)
		}
	}
	have := ghapi.Available()
	check(have, "gh", "installed "+ghapi.Version(), "not installed — https://cli.github.com")
	if have {
		u, err := ghapi.AuthStatus()
		check(err == nil, "auth", "@"+u, "run: gh auth login")
	}
	d := device.Probe()
	if d.OS == "android" {
		check(true, "device", fmt.Sprintf("%s (%s, sdk %d)", d.Model, strings.Join(d.ABIs, ","), d.SDK), "")
	} else {
		fmt.Printf("  · %-12s %s — downloads OK, install needs adb\n", "device", d.Word())
	}
	switch d.InstallVia {
	case "adb":
		if d.ADBConnected {
			check(true, "installer", "adb (device connected)", "")
		} else {
			fmt.Printf("  ! %-12s %s\n", "installer", "adb installed (no device currently connected)")
		}
	case "termux-open", "am":
		check(true, "installer", d.InstallVia+" (Android installer prompt)", "")
	default:
		check(false, "installer", "", "none found (install adb on desktop or termux-tools on Termux)")
	}
	dir := config.CacheDir()
	if err := os.MkdirAll(dir, 0o755); err == nil {
		check(true, "cache", dir, "")
	}
	return 0
}

func usage() {
	fmt.Println(strings.TrimSpace(`ghapk — GitHub Actions APK installer

USAGE
  ghapk                    open the interactive TUI
  ghapk get [flags]        classic one-shot pipeline (like the old script)
  ghapk doctor             check your environment
  ghapk version            print version

COMMON FLAGS (TUI session or get)
  --repo owner/name        repository to pull artifacts from
  --workflow NAME          restrict to one workflow
  --branch NAME            restrict to one branch
  --artifact PATTERN       artifact name glob, e.g. "*debug*"
  --file PATTERN           file glob inside the zip
  --arch PREF              auto|any|arm64|armv7|x86_64|x86|universal
  --variant PREF           auto|debug|release|any
  --limit N                scan the N newest runs (default 20)
  --package NAME           track the installed version of com.example.app
  --theme NAME             action|mocha|emerald|paper

get ONLY
  -i, --install            hand the APK to the system installer
  -y, --yes                never ask
      --dry-run            resolve and print the plan, fetch nothing
      --print-path         print where the APK would land

IN THE TUI press ? for the full key list, s for settings.
Config: ~/.config/ghapk/config and repos/<owner>_<repo>.conf
(KEY=VALUE lines — same format as the original script).`))
}
