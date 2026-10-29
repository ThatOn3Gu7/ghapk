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
// go build -ldflags="-X main.appVersion=4.3.0"
var appVersion = "4.3.0"

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
	code, opts := getCmd(args)
	if code != 0 {
		return code
	}
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
		yes                                            bool
		install                                        bool
		dry                                            bool
		printD                                         string
		repoOverride, branchOverride, workflowOverride bool
	)
	seen := map[string]bool{}
	flagKeys := map[string]string{"--repo": "repo", "--branch": "branch", "--workflow": "workflow", "--artifact": "artifact-pattern", "--file": "file", "--exclude": "exclude", "--source": "source", "--arch": "arch", "--variant": "variant", "--limit": "limit", "--package": "package", "--downgrade": "downgrade", "--theme": "theme", "--no-open": "no-open", "--watch": "watch", "--out": "out", "--keep": "keep", "--refresh": "auto-refresh", "--install-wait": "install-wait", "--notify-on": "notify-on", "--glyphs": "glyphs", "--all-arch": "all-arch", "--no-identical-check": "identical-check", "--no-pkg-check": "pkg-check", "--no-sig-check": "sig-check", "--no-verify-install": "verify-install", "--no-verify-digest": "verify-digest", "--no-mouse": "mouse", "--ascii": "glyphs", "--unicode": "glyphs", "--speak": "speak", "--toast": "toast", "--no-notify": "notify-on"}
	for i := 0; i < len(args); i++ {
		a := args[i]
		name := strings.SplitN(a, "=", 2)[0]
		if k, ok := flagKeys[name]; ok {
			seen[k] = true
		}
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
			repoOverride = true
		case a == "--workflow" || strings.HasPrefix(a, "--workflow="):
			settings.Workflow = val()
			workflowOverride = true
		case a == "--branch" || strings.HasPrefix(a, "--branch="):
			settings.Branch = val()
			branchOverride = true
		case a == "--artifact" || strings.HasPrefix(a, "--artifact="):
			settings.ArtifactPattern = val()
		case a == "--file" || strings.HasPrefix(a, "--file="):
			settings.FilePattern = val()
		case a == "--exclude" || strings.HasPrefix(a, "--exclude="):
			settings.ExcludePattern = val()
		case a == "--source" || strings.HasPrefix(a, "--source="):
			settings.Source = val()
		case name == "--arch" || name == "--variant" || name == "--limit" || name == "--out" || name == "--keep" || name == "--refresh" || name == "--install-wait" || name == "--notify-on" || name == "--glyphs":
			value := val()
			if value == "" && name != "--out" {
				fmt.Fprintf(os.Stderr, "ghapk: %s requires a value\n", name)
				return 2, tui.Options{}
			}
			if err := config.SetValue(&settings, flagKeys[name], value); err != nil {
				fmt.Fprintf(os.Stderr, "ghapk: %s: %v\n", name, err)
				return 2, tui.Options{}
			}
		case name == "--all-arch" || name == "--speak" || name == "--toast":
			_ = config.SetValue(&settings, flagKeys[name], "1")
		case name == "--no-identical-check" || name == "--no-pkg-check" || name == "--no-sig-check" || name == "--no-verify-install" || name == "--no-verify-digest" || name == "--no-mouse":
			_ = config.SetValue(&settings, flagKeys[name], "0")
		case name == "--ascii":
			settings.Glyphs = "ascii"
		case name == "--unicode":
			settings.Glyphs = "unicode"
		case name == "--no-notify":
			settings.NotifyOn = "never"
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
	overrides := map[string]string{}
	for k := range seen {
		overrides[k] = config.Value(settings, k)
	}
	opts := tui.Options{Settings: settings, RepoOverride: repoOverride, BranchOverride: branchOverride, WorkflowOverride: workflowOverride, CLIOverrides: overrides}
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
  --artifact PATTERN       filter Actions names or release asset globs
  --file PATTERN           file glob inside the zip
  --arch PREF              auto|any|arm64|armv7|x86_64|x86|universal
  --variant PREF           auto|debug|release|any
  --limit N                scan the N newest runs (default 20)
  --package NAME           track the installed version of com.example.app
  --theme NAME             action|mocha|emerald|paper
  --out DIR                copy APKs to DIR as well as the cache
  --all-arch               offer APKs for all architectures
  --refresh SECONDS        auto-refresh interval (0 disables)
  --install-wait SECONDS   wait for installation verification
  --notify-on POLICY       always|success|failure|never
  --ascii / --unicode      choose interface symbols
  --no-mouse               disable mouse and touch

get ONLY
  -i, --install            hand the APK to the system installer
  -y, --yes                accept non-downgrade warnings
      --dry-run            resolve and print the plan, fetch nothing
      --print-path         use the APK cache as the output directory

IN THE TUI press ? for the full key list; Settings is the fifth panel; Log is the seventh.
Config: ~/.config/ghapk/config and repos/<owner>_<repo>.conf
(KEY=VALUE lines — same format as the original script).`))
}
