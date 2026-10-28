package cli

import (
	"fmt"
	"strconv"
	"strings"

	"ghapk/internal/config"
)

type setOp struct {
	key, val string
	global   bool
}

type cli struct {
	vals map[string]string

	help, version, doctor, clean, fix bool
	yes, noOpen, dryRun, printPath    bool
	watch, strict                     bool
	tui, plain, noCache               bool
	noMouse, pickRepo, startPlan      bool
	showSet                           bool
	setOps                            []setOp
	runID                             int64
	completion                        string
	listBranches, listWorkflows       bool
}

// wantHeadless says whether the arguments ask for the old script-style run
// instead of the interface.
func (c *cli) wantHeadless() bool {
	if c.tui {
		return false
	}
	return c.plain || c.yes || c.noOpen || c.dryRun || c.printPath || c.watch || c.runID != 0 || c.listBranches || c.listWorkflows
}

// value flags -> canonical setting keys
var valueFlags = map[string]string{
	"-r": "repo", "--repo": "repo", "-b": "branch", "--branch": "branch", "-w": "workflow", "--workflow": "workflow",
	"-n": "artifact", "--name": "artifact", "--artifact": "artifact",
	"--artifact-pattern": "artifact-pattern", "--file": "file", "--exclude": "exclude", "--source": "source",
	"-l": "limit", "--limit": "limit", "-o": "out", "--out": "out", "-k": "keep", "--keep": "keep",
	"--arch": "arch", "--abi": "arch", "--variant": "variant", "--build-type": "variant",
	"--package": "package", "--pkg": "package", "--downgrade": "downgrade", "--notify-on": "notify-on",
	"--theme": "theme", "--watch-timeout": "watch-timeout", "--install-wait": "install-wait", "--refresh": "refresh",
	"--glyphs": "glyphs", "--remember-mode": "remember",
}

// boolean flags -> (key, value)
var boolFlags = map[string][2]string{
	"--all-arch": {"all-arch", "1"}, "--no-all-arch": {"all-arch", "0"},
	"--no-releases": {"source", "actions"}, "--releases-only": {"source", "releases"},
	"--no-verify-digest": {"verify-digest", "0"}, "--no-pkg-check": {"pkg-check", "0"},
	"--no-sig-check": {"sig-check", "0"}, "--no-identical-check": {"identical-check", "0"},
	"--uninstall-first": {"uninstall-first", "1"}, "--no-verify-install": {"verify-install", "0"},
	"--speak": {"speak", "1"}, "--no-speak": {"speak", "0"}, "--toast": {"toast", "1"}, "--no-toast": {"toast", "0"},
	"--no-notify": {"notify-on", "never"}, "--ascii": {"glyphs", "ascii"}, "--unicode": {"glyphs", "unicode"},
	"--no-confirm": {"confirm-install", "0"},
	"--remember":   {"remember", "yes"}, "--no-remember": {"remember", "no"},
}

// flags from the old shell UI that mean nothing now; accepted and ignored so
// old aliases and muscle memory keep working.
var ignoredValue = map[string]bool{"--width": true, "--spinner": true, "--bar": true, "--layout": true, "--background": true, "--title": true, "--prompt-timeout": true, "--config": true, "--repo-config": true}
var ignoredBool = map[string]bool{"--full-width": true, "--no-full-width": true, "--color": true, "--no-color": true, "--links": true, "--no-links": true,
	"-v": true, "--verbose": true, "-i": true, "--interactive": true, "--no-interactive": true, "--no-watch": true, "--ask": true, "--no-ask": true,
	"--detect-background": true, "--no-changes": true, "--edit": true, "--install-completion": true, "--fix": true, "--wait": true, "-p": true, "--pick": true}

func parseArgs(args []string) (*cli, error) {
	c := &cli{vals: map[string]string{}}
	setVal := func(key, val string) error {
		v, err := config.Validate(key, val)
		if err != nil {
			return err
		}
		c.vals[key] = v
		return nil
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, inline, hasInline := a, "", false
		if strings.HasPrefix(a, "--") {
			if eq := strings.IndexByte(a, '='); eq > 0 {
				name, inline, hasInline = a[:eq], a[eq+1:], true
			}
		}
		next := func() (string, error) {
			if hasInline {
				return inline, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", name)
			}
			i++
			return args[i], nil
		}
		switch name {
		case "-h", "--help":
			c.help = true
		case "-V", "--version":
			c.version = true
		case "-d", "--doctor":
			c.doctor = true
		case "--clean":
			c.clean = true
		case "-y", "--yes":
			c.yes = true
		case "--no-open":
			c.noOpen = true
		case "--dry-run":
			c.dryRun = true
		case "--print-path":
			c.printPath = true
		case "--watch":
			c.watch = true
		case "--strict":
			c.strict = true
		case "--tui":
			c.tui = true
		case "--plain":
			c.plain = true
		case "--no-http-cache":
			c.noCache = true
		case "--no-mouse":
			c.noMouse = true
		case "--pick-repo":
			c.pickRepo = true
		case "--newest":
			c.startPlan = true
		case "--run", "--run-id":
			v, err := next()
			if err != nil {
				return nil, err
			}
			n, err := strconv.ParseInt(strings.TrimPrefix(v, "#"), 10, 64)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("--run needs a run number, not %q", v)
			}
			c.runID = n
		case "--completion", "--complete":
			v, err := next()
			if err != nil {
				return nil, err
			}
			c.completion = v
		case "--set", "--set-global", "--unset":
			global := name == "--set-global"
			if name == "--unset" {
				v, err := next()
				if err != nil {
					return nil, err
				}
				c.setOps = append(c.setOps, setOp{key: v, global: global})
				continue
			}
			if !hasInline && (i+1 >= len(args) || strings.HasPrefix(args[i+1], "-")) {
				c.showSet = true
				continue
			}
			v, err := next()
			if err != nil {
				return nil, err
			}
			k, val, ok := strings.Cut(v, "=")
			if !ok {
				if i+1 < len(args) {
					i++
					val = args[i]
				}
			}
			c.setOps = append(c.setOps, setOp{key: k, val: val, global: global})
		default:
			if key, ok := valueFlags[name]; ok {
				v, err := next()
				if err != nil {
					return nil, err
				}
				switch {
				case key == "branch" && v == "list":
					c.listBranches = true
				case key == "workflow" && v == "list":
					c.listWorkflows = true
				case key == "branch" && v == "pick", key == "workflow" && v == "pick":
				default:
					if err := setVal(key, v); err != nil {
						return nil, fmt.Errorf("%s: %v", name, err)
					}
				}
			} else if kv, ok := boolFlags[name]; ok {
				if err := setVal(kv[0], kv[1]); err != nil {
					return nil, err
				}
			} else if ignoredValue[name] {
				if _, err := next(); err != nil {
					return nil, err
				}
			} else if ignoredBool[name] {
			} else if !strings.HasPrefix(a, "-") && strings.Count(a, "/") == 1 && c.vals["repo"] == "" {
				// a bare owner/name is a repository
				c.vals["repo"] = a
			} else {
				return nil, fmt.Errorf("unknown option %s", a)
			}
		}
	}
	return c, nil
}
