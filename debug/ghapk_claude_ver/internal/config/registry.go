// Package config holds ghapk's settings: the registry of every key, the layered
// config files (CLI > per-repo > ./.ghapk.conf > global > in-repo), the
// install history and the small bit of TUI state that survives between runs.
//
// The file formats are the ones the original shell script used, so existing
// ~/.config/ghapk files keep working untouched.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Kind is the type of a setting.
type Kind int

const (
	String Kind = iota
	Bool
	Int
	Enum
)

// Setting describes one saved setting.
type Setting struct {
	Key     string
	Kind    Kind
	Default string
	Choices []string // Enum only
	Group   string
	Help    string
	Legacy  bool // accepted in files, ignored (belonged to the old shell UI)
}

// Groups, in display order.
const (
	GSource  = "Source"
	GSelect  = "APK selection"
	GInstall = "Install & safety"
	GNotify  = "Notifications"
	GUI      = "Interface"
	GWatch   = "Watching"
)

// Groups lists the setting groups in the order the editor shows them.
var Groups = []string{GSource, GSelect, GInstall, GWatch, GNotify, GUI}

// ThemeNames are the built-in colour schemes.
var ThemeNames = []string{"default", "nord", "tokyo", "gruvbox", "dracula", "mono", "cb", "ansi"}

// Registry is every setting, in display order within its group.
var Registry = []Setting{
	{Key: "repo", Group: GSource, Help: "OWNER/NAME of the repository to read builds from. Empty means: ask at start-up, or use the git remote of the current directory."},
	{Key: "workflow", Group: GSource, Help: "Only look at one workflow (file name or id). Empty means all workflows."},
	{Key: "branch", Group: GSource, Help: "Only look at runs on this branch. Empty means all branches."},
	{Key: "source", Kind: Enum, Default: "auto", Choices: []string{"auto", "actions", "releases"}, Group: GSource,
		Help: "Where APKs come from: workflow artifacts, published releases, or artifacts first with releases as the fallback."},
	{Key: "limit", Kind: Int, Default: "20", Group: GSource, Help: "How many recent runs to load (1-200)."},

	{Key: "artifact", Group: GSelect, Help: "Exact artifact name to fetch. Bypasses every pattern and score."},
	{Key: "artifact-pattern", Default: "*debug*,*apk*", Group: GSelect, Help: "Comma-separated globs matched against artifact names."},
	{Key: "file", Default: "*.apk", Group: GSelect, Help: "Comma-separated globs for the file inside the artifact zip."},
	{Key: "exclude", Group: GSelect, Help: "Comma-separated globs of artifacts/files to leave out."},
	{Key: "arch", Kind: Enum, Default: "auto", Choices: []string{"auto", "any", "arm64", "armv7", "x86_64", "x86", "universal"}, Group: GSelect,
		Help: "Which ABI to prefer. auto reads this device. any disables the filter."},
	{Key: "variant", Kind: Enum, Default: "auto", Choices: []string{"auto", "debug", "release", "any"}, Group: GSelect,
		Help: "Prefer debug or release builds. auto prefers debug."},
	{Key: "all-arch", Kind: Bool, Default: "0", Group: GSelect, Help: "Offer every architecture a run built, not only the one for this device."},
	{Key: "out", Group: GSelect, Help: "Also copy the downloaded APK into this folder, e.g. ~/storage/downloads."},

	{Key: "package", Group: GInstall, Help: "Android package name to compare against (e.g. com.example.app). Read from the device when possible."},
	{Key: "downgrade", Kind: Enum, Default: "auto", Choices: []string{"auto", "allow", "skip"}, Group: GInstall,
		Help: "What to do with a build older than the installed one: auto asks, allow installs anyway, skip refuses."},
	{Key: "confirm-install", Kind: Bool, Default: "1", Group: GInstall, Help: "Ask before handing the APK to the Android installer."},
	{Key: "pkg-check", Kind: Bool, Default: "1", Group: GInstall, Help: "Refuse an APK whose package name differs from the installed app."},
	{Key: "sig-check", Kind: Bool, Default: "1", Group: GInstall, Help: "Warn when the APK is signed with a different key than the installed app."},
	{Key: "identical-check", Kind: Bool, Default: "1", Group: GInstall, Help: "Point out a build that is byte-identical to one you already have."},
	{Key: "uninstall-first", Kind: Bool, Default: "0", Group: GInstall, Help: "On a signing-key clash, uninstall the old app first (erases its data; needs Shizuku)."},
	{Key: "verify-install", Kind: Bool, Default: "1", Group: GInstall, Help: "After handing off, check the device really is on the new versionCode."},
	{Key: "verify-digest", Kind: Bool, Default: "1", Group: GInstall, Help: "Compare each download with the SHA-256 GitHub published for it."},
	{Key: "keep", Kind: Int, Default: "3", Group: GInstall, Help: "How many downloaded APKs to keep in the cache (0 keeps only the newest)."},
	{Key: "install-wait", Kind: Int, Default: "90", Group: GInstall, Help: "Seconds to wait for the installer prompt to be accepted before giving up the check."},

	{Key: "refresh", Kind: Int, Default: "30", Group: GWatch, Help: "Seconds between automatic run-list refreshes while the app is open. 0 turns it off."},
	{Key: "watch-timeout", Kind: Int, Default: "1800", Group: GWatch, Help: "How long to wait for a running build before giving up (seconds)."},

	{Key: "notify-on", Kind: Enum, Default: "always", Choices: []string{"always", "success", "failure", "never"}, Group: GNotify,
		Help: "Which outcomes raise a notification."},
	{Key: "speak", Kind: Bool, Default: "0", Group: GNotify, Help: "Say the outcome aloud (termux-tts-speak)."},
	{Key: "toast", Kind: Bool, Default: "0", Group: GNotify, Help: "Show a brief overlay (termux-toast)."},

	{Key: "theme", Kind: Enum, Default: "default", Choices: ThemeNames, Group: GUI, Help: "Colour scheme."},
	{Key: "glyphs", Kind: Enum, Default: "auto", Choices: []string{"auto", "unicode", "ascii"}, Group: GUI, Help: "Draw with Unicode symbols or plain ASCII."},
	{Key: "mouse", Kind: Bool, Default: "1", Group: GUI, Help: "Taps and clicks select rows; the wheel scrolls."},
	{Key: "remember", Kind: Enum, Default: "auto", Choices: []string{"auto", "yes", "no"}, Group: GUI,
		Help: "After you pick a branch or workflow by hand: auto asks to save it for this repo, yes saves, no never."},
	{Key: "exit-summary", Kind: Bool, Default: "1", Group: GUI, Help: "Print one line with the saved APK path after leaving, if you downloaded something."},

	// Settings the old shell UI had that mean nothing to a real TUI. Still
	// accepted so an old config file never errors.
	{Key: "width", Legacy: true}, {Key: "spinner", Legacy: true}, {Key: "bar", Legacy: true},
	{Key: "layout", Legacy: true}, {Key: "background", Legacy: true}, {Key: "title", Legacy: true},
	{Key: "ask", Legacy: true},
}

var byKey = func() map[string]*Setting {
	m := map[string]*Setting{}
	for i := range Registry {
		m[Registry[i].Key] = &Registry[i]
	}
	return m
}()

// aliases maps every spelling the shell script accepted to the canonical key.
var aliases = map[string]string{
	"repository": "repo", "artifact-name": "artifact", "artifacts": "artifact-pattern",
	"file-pattern": "file", "exclude-pattern": "exclude", "source-mode": "source",
	"run-limit": "limit", "out-dir": "out", "package-name": "package",
	"downgrade-mode": "downgrade", "arch-pref": "arch", "variant-pref": "variant",
	"notify": "notify-on", "allarch": "all-arch", "identical-ask": "identical-check",
	"bar-style": "bar", "pkg": "package", "abi": "arch", "build-type": "variant",
}

// Canon turns any accepted spelling of a key into its canonical form.
// ok is false for keys that are not settings at all.
func Canon(k string) (string, bool) {
	k = strings.ToLower(strings.TrimSpace(k))
	k = strings.ReplaceAll(k, "_", "-")
	if a, ok := aliases[k]; ok {
		k = a
	}
	_, ok := byKey[k]
	return k, ok
}

// Lookup returns the registry entry for a canonical key.
func Lookup(k string) *Setting { return byKey[k] }

// Visible returns the settings an editor should show (everything non-legacy).
func Visible() []Setting {
	var out []Setting
	for _, s := range Registry {
		if !s.Legacy {
			out = append(out, s)
		}
	}
	return out
}

// NormBool maps 1/on/true/yes (any case) to "1" and everything else to "0".
func NormBool(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "on", "true", "yes":
		return "1"
	}
	return "0"
}

// Validate checks a value for a canonical key and returns the normalised form.
// An empty value is always valid: it means "unset".
func Validate(key, val string) (string, error) {
	val = strings.TrimSpace(val)
	s := byKey[key]
	if s == nil {
		return "", fmt.Errorf("%q is not a setting", key)
	}
	if val == "" || s.Legacy {
		return val, nil
	}
	switch s.Kind {
	case Bool:
		switch strings.ToLower(val) {
		case "1", "on", "true", "yes", "0", "off", "false", "no":
			return NormBool(val), nil
		}
		return "", fmt.Errorf("%s must be on or off", key)
	case Int:
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return "", fmt.Errorf("%s must be a whole number", key)
		}
		if key == "limit" && n < 1 {
			return "", fmt.Errorf("limit must be 1 or more")
		}
		return strconv.Itoa(n), nil
	case Enum:
		v := strings.ToLower(val)
		if key == "arch" { // the shell accepted several spellings
			switch v {
			case "arm64-v8a", "aarch64":
				v = "arm64"
			case "armeabi", "armeabi-v7a":
				v = "armv7"
			case "x64", "amd64":
				v = "x86_64"
			}
		}
		for _, c := range s.Choices {
			if c == v {
				return v, nil
			}
		}
		return "", fmt.Errorf("%s must be one of: %s", key, strings.Join(s.Choices, ", "))
	}
	return val, nil
}
