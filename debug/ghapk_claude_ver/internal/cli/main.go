// Command ghapk finds the newest GitHub Actions build of your app and installs
// it on your phone. Run it with no arguments for the interactive interface.
package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"ghapk/internal/config"
	"ghapk/internal/engine"
	"ghapk/internal/exit"
	"ghapk/internal/term"
	"ghapk/internal/tui"
)

// Version is set at build time: -ldflags "-X ghapk/internal/cli.Version=1.0.0".
var Version = "dev"

// Run executes ghapk with the given arguments and returns the exit code.
func Run(args []string) (code int) {
	c, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghapk:", err)
		fmt.Fprintln(os.Stderr, "try: ghapk --help")
		return exit.Usage
	}
	switch {
	case c.help:
		fmt.Print(helpText())
		return 0
	case c.version:
		fmt.Println("ghapk", Version)
		return 0
	case c.completion != "":
		return printCompletion(c.completion)
	}
	if len(c.setOps) > 0 || c.showSet {
		return doSet(c)
	}
	if c.clean {
		return doClean(c)
	}
	if c.doctor {
		return doDoctor(c)
	}
	if c.wantHeadless() {
		return headless(c)
	}
	return runTUI(c)
}

func runTUI(c *cli) int {
	// A TUI needs a real terminal; without one fall back to the plain flow so
	// `ghapk | tee log` and cron keep doing something useful.
	t, err := term.Open()
	if err != nil {
		return headless(c)
	}
	eng := newEngine(c)
	defer eng.Close()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP)
	var last string
	app := tui.New(eng, 80, 24, tui.Options{Repo: c.vals["repo"], PickRepo: c.pickRepo, LastAPK: &last, NoMouse: c.noMouse, StartPlan: c.startPlan})
	go func() {
		<-sig
		t.Close() // restores the terminal even if the loop is busy
		os.Exit(exit.Interrupted)
	}()
	if err := app.Run(t); err != nil {
		t.Close()
		fmt.Fprintln(os.Stderr, "ghapk:", err)
		return exit.Dep
	}
	if last != "" && eng.Config().Bool("exit-summary") {
		fmt.Printf("ghapk: saved %s\n", last)
	}
	return 0
}

func doSet(c *cli) int {
	eng := newEngine(c)
	defer eng.Close()
	status := 0
	for _, op := range c.setOps {
		key, ok := config.Canon(op.key)
		if !ok {
			fmt.Fprintf(os.Stderr, "ghapk: %q is not a setting (see --set with no arguments)\n", op.key)
			status = exit.Usage
			continue
		}
		path := config.GlobalFile()
		if !op.global && c.vals["repo"] != "" {
			path = config.RepoFile(c.vals["repo"])
		} else if !op.global && eng.Config().Str("repo") != "" {
			path = config.RepoFile(eng.Config().Str("repo"))
		}
		if err := config.SetKey(path, key, op.val); err != nil {
			fmt.Fprintln(os.Stderr, "ghapk:", err)
			status = exit.Usage
			continue
		}
		if op.val == "" {
			fmt.Printf("%s removed from %s\n", key, path)
		} else {
			fmt.Printf("%s = %s  (%s)\n", key, op.val, path)
		}
	}
	if c.showSet {
		cfg := config.Load(config.LoadOpts{CLI: c.vals})
		for _, s := range config.Visible() {
			fmt.Printf("%-18s %-24s %s\n", s.Key, cfg.Str(s.Key), cfg.Source(s.Key))
		}
	}
	return status
}

func doClean(c *cli) int {
	eng := newEngine(c)
	defer eng.Close()
	n := eng.CacheSize()
	if err := eng.Clean(); err != nil {
		fmt.Fprintln(os.Stderr, "ghapk:", err)
		return exit.API
	}
	fmt.Printf("cleaned %s from %s\n", engine.FmtBytes(n), eng.CacheDir)
	return 0
}

func helpText() string {
	return strings.TrimLeft(`
ghapk — install the newest GitHub Actions debug APK on your phone

USAGE
  ghapk                       open the interactive interface
  ghapk -r owner/name         open it on a repository
  ghapk -y                    no interface: install the newest build, asking only what matters
  ghapk --dry-run             say what would be installed, change nothing

WHAT TO LOOK AT
  -r, --repo OWNER/NAME       repository (or a GitHub URL)
  -b, --branch NAME|list      only builds from this branch
  -w, --workflow FILE|list    only this workflow
      --run N                 use this run instead of the newest
  -n, --name NAME             exact artifact name
      --artifact-pattern G    artifact name globs, comma separated
      --file G                file globs inside the artifact
      --exclude G             globs to leave out
      --source auto|actions|releases     (--no-releases, --releases-only)
  -l, --limit N               how many runs to read
      --arch ARCH             auto|any|arm64|armv7|x86_64|x86|universal
      --variant V             auto|debug|release|any
      --all-arch              offer every architecture

WHAT TO DO
  -y, --yes                   take sensible defaults; still asks about risky things
      --no-open               download and verify, but do not install
      --watch                 if a build is running, wait for it first
      --print-path            print only the saved APK path on stdout
  -o, --out DIR               also copy the APK into DIR
  -k, --keep N                APKs to keep in the cache
      --package NAME          Android package to compare against
      --downgrade auto|allow|skip
      --no-pkg-check  --no-sig-check  --no-identical-check
      --no-verify-digest  --no-verify-install  --uninstall-first

SETTINGS
      --set KEY=VALUE         save for this repo     (--set-global: for everything)
      --unset KEY             remove a saved value
      --set                   show every setting and where it comes from
      --theme NAME            default nord tokyo gruvbox dracula mono cb ansi
      --ascii | --unicode     force plain or fancy symbols
      --no-mouse              turn mouse/touch off

OTHER
  -d, --doctor                check gh, the phone and the app
      --clean                 delete cached downloads
      --plain                 never open the interface
      --tui                   always open the interface
      --completion SHELL      bash | zsh | fish
  -V, --version   -h, --help

EXIT CODES
  0 ok · 2 usage · 3 missing tool · 4 not signed in · 5 GitHub error · 6 no artifact
  7 download · 8 archive · 9 no APK inside · 10 install · 12 downgrade refused
  14 doctor found a blocker · 15 rate limited · 16 network · 17 expired · 19 checksum
  20 signing key differs · 21 no space · 130 interrupted
`, "\n")
}

func newEngine(c *cli) *engine.Engine {
	e := engine.New(c.vals)
	e.GH.NoCache = c.noCache
	return e
}
