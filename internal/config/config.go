// Package config loads and saves ghapk settings with explicit trust boundaries
// between user-owned configuration and untrusted repository-supplied hints.
//
// The file format uses the same KEY=VALUE line syntax as the original script:
//
//	~/.config/ghapk/config                    — trusted global user config
//	~/.config/ghapk/repos/<owner>_<repo>.conf — trusted per-repo user config (outranks global)
//	.ghapk.conf / .github/ghapk.conf          — untrusted workspace/repo hints (lowest precedence;
//	                                            benign selection hints only, never safety/destructive keys)
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Settings is the effective configuration for one ghapk run or TUI session.
type Settings struct {
	Repo            string `key:"repo"`
	Branch          string `key:"branch"`
	Workflow        string `key:"workflow"`
	ArtifactName    string `key:"artifact"`
	ArtifactPattern string `key:"artifact-pattern"`
	FilePattern     string `key:"file"`
	ExcludePattern  string `key:"exclude"`
	Arch            string `key:"arch"`      // auto any arm64 armv7 x86_64 x86 universal
	Variant         string `key:"variant"`   // auto debug release any
	Source          string `key:"source"`    // auto actions releases
	Downgrade       string `key:"downgrade"` // auto allow never
	PackageName     string `key:"package"`
	RunLimit        int    `key:"limit"`
	Theme           string `key:"theme"`
	NoOpen          bool   `key:"no-open"`
	KeepCache       bool   `key:"keep-cache"`
	KeepCount       int    `key:"keep"`
	Watch           bool   `key:"watch"`
	AutoRefreshSecs int    `key:"auto-refresh"`

	// Safety policies — always enabled by default and modifiable ONLY by
	// trusted local user config/flags, never by untrusted repo configs.
	PkgCheck      bool `key:"pkg-check"`
	SigCheck      bool `key:"sig-check"`
	VerifyDigest  bool `key:"verify-digest"`
	VerifyInstall bool `key:"verify-install"`
}

// Defaults returns the out-of-the-box settings with all safety checks enabled.
func Defaults() Settings {
	return Settings{
		Arch:            "auto",
		Variant:         "auto",
		Source:          "auto",
		Downgrade:       "auto",
		RunLimit:        20,
		Theme:           "action",
		KeepCount:       3,
		AutoRefreshSecs: 60,
		PkgCheck:        true,
		SigCheck:        true,
		VerifyDigest:    true,
		VerifyInstall:   true,
	}
}

// Dir is the per-user config directory (~/.config/ghapk), honoring
// GHAPK_CONFIG_DIR and XDG_CONFIG_HOME.
func Dir() string {
	if d := strings.TrimSpace(os.Getenv("GHAPK_CONFIG_DIR")); d != "" {
		return d
	}
	if x := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); x != "" {
		return filepath.Join(x, "ghapk")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".config", "ghapk")
}

func globalPath() string { return filepath.Join(Dir(), "config") }

// RepoPath mirrors the script: repos/<owner>_<repo>.conf
func RepoPath(repo string) string {
	r := strings.ReplaceAll(repo, "/", "_")
	return filepath.Join(Dir(), "repos", r+".conf")
}

// CacheDir mirrors the script's CACHE_DIR (~/.cache/ghapk), honoring
// GHAPK_CACHE_DIR, CACHE_DIR, and XDG_CACHE_HOME.
func CacheDir() string {
	if d := strings.TrimSpace(os.Getenv("GHAPK_CACHE_DIR")); d != "" {
		return d
	}
	if d := strings.TrimSpace(os.Getenv("CACHE_DIR")); d != "" {
		return d
	}
	if x := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); x != "" {
		return filepath.Join(x, "ghapk")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".cache", "ghapk")
}

// canonKey normalizes aliases and underscore variants to canonical key names.
func canonKey(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	k = strings.ReplaceAll(k, "_", "-")
	switch k {
	case "repository":
		return "repo"
	case "artifact-name":
		return "artifact"
	case "artifacts":
		return "artifact-pattern"
	case "file-pattern":
		return "file"
	case "exclude-pattern":
		return "exclude"
	case "source-mode":
		return "source"
	case "run-limit":
		return "limit"
	case "package-name", "pkg":
		return "package"
	case "downgrade-mode":
		return "downgrade"
	case "arch-pref", "abi":
		return "arch"
	case "variant-pref", "build-type":
		return "variant"
	case "refresh":
		return "auto-refresh"
	}
	return k
}

// untrustedAllowedKeys is the strict allowlist of benign selection hints that
// an untrusted workspace (.ghapk.conf) or remote repository (.github/ghapk.conf)
// may suggest when the user has not configured a value locally.
//
// Security-sensitive, destructive, filesystem, and policy keys (such as
// uninstall-first, downgrade, pkg-check, sig-check, verify-digest,
// verify-install, package, no-open, out, keep, keep-cache) are NEVER accepted
// from untrusted configs.
var untrustedAllowedKeys = map[string]bool{
	"repo":             true,
	"branch":           true,
	"workflow":         true,
	"artifact":         true,
	"artifact-pattern": true,
	"file":             true,
	"exclude":          true,
	"source":           true,
}

// SanitizeUntrustedRepoConfig filters parsed KEY=VALUE pairs from an untrusted
// repository or working-directory config, returning only benign selection hints
// and a sorted list of blocked keys that were rejected.
func SanitizeUntrustedRepoConfig(raw map[string]string) (safe map[string]string, blocked []string) {
	safe = map[string]string{}
	blockedSet := map[string]bool{}
	for rk, v := range raw {
		k := canonKey(rk)
		if !untrustedAllowedKeys[k] {
			blockedSet[k] = true
			continue
		}
		if k == "source" {
			sv := strings.ToLower(strings.TrimSpace(v))
			if sv != "auto" && sv != "actions" && sv != "releases" {
				blockedSet[k] = true
				continue
			}
			v = sv
		}
		if k == "repo" && !validRepoHint(v) {
			blockedSet[k] = true
			continue
		}
		safe[k] = v
	}
	for k := range blockedSet {
		blocked = append(blocked, k)
	}
	sort.Strings(blocked)
	return safe, blocked
}

func validRepoHint(s string) bool {
	if strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	parts := strings.Split(s, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

// ApplyRemoteRepoConfig parses text from an untrusted in-repository config
// (such as .github/ghapk.conf), strips any safety/destructive keys, and fills
// only unset benign selection fields on s. It returns any blocked key names.
func ApplyRemoteRepoConfig(s *Settings, text string) []string {
	raw := parseKVText(text)
	safe, blocked := SanitizeUntrustedRepoConfig(raw)
	applyUntrustedHints(s, safe, nil)
	return blocked
}

// Load reads trusted global user config, trusted per-repo user config, and
// trusted GHAPK_* environment overrides, while treating working-directory
// .ghapk.conf strictly as lowest-precedence untrusted selection hints.
func Load() Settings {
	return loadWithLocal(".ghapk.conf", "")
}

// LoadForRepo returns settings with a specific repo's trusted per-repo config
// applied on top of global settings.
func LoadForRepo(repo string) Settings {
	return loadWithLocal(".ghapk.conf", repo)
}

func loadWithLocal(localUntrustedPath, explicitRepo string) Settings {
	s := Defaults()

	globalKV := readKV(globalPath())
	envKV := readTrustedEnv()
	untrustedRaw := readKV(localUntrustedPath)
	untrustedSafe, _ := SanitizeUntrustedRepoConfig(untrustedRaw)

	// Track which keys were explicitly configured by trusted sources so
	// untrusted workspace hints cannot override even default-valued enums
	// like source="auto" when the user explicitly wrote source=auto globally.
	trustedSeen := map[string]bool{}
	for k := range globalKV {
		trustedSeen[k] = true
	}
	for k := range envKV {
		trustedSeen[k] = true
	}

	// 1. Lowest precedence: sanitized untrusted workspace hints.
	applyUntrustedHints(&s, untrustedSafe, trustedSeen)

	// 2. Trusted global user file.
	applyTop(&s, globalKV)

	// 3. Determine target repo so we can load its trusted per-repo config.
	repo := explicitRepo
	if repo == "" {
		repo = envKV["repo"]
	}
	if repo == "" {
		repo = s.Repo
	}
	if repo != "" {
		s.Repo = repo
		repoKV := readKV(RepoPath(repo))
		applyTop(&s, repoKV)
	}

	// 4. Highest config-layer precedence before CLI flags: GHAPK_* env vars.
	applyTop(&s, envKV)
	if explicitRepo != "" {
		s.Repo = explicitRepo
	}
	return s
}

func readTrustedEnv() map[string]string {
	out := map[string]string{}
	keys := []string{
		"repo", "branch", "workflow", "artifact", "artifact-pattern",
		"file", "exclude", "arch", "variant", "source", "downgrade",
		"package", "limit", "theme", "no-open", "keep-cache", "keep",
		"watch", "auto-refresh", "pkg-check", "sig-check",
		"verify-digest", "verify-install",
	}
	for _, k := range keys {
		envName := "GHAPK_" + strings.ToUpper(strings.ReplaceAll(k, "-", "_"))
		if v, ok := os.LookupEnv(envName); ok && strings.TrimSpace(v) != "" {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}

func readKV(path string) map[string]string {
	if path == "" {
		return map[string]string{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	return parseKVText(string(b))
}

func parseKVText(text string) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		out[canonKey(k)] = strings.Trim(v, `"'`)
	}
	return out
}

func applyUntrustedHints(s *Settings, safe map[string]string, trustedSeen map[string]bool) {
	for k, v := range safe {
		if v == "" || (trustedSeen != nil && trustedSeen[k]) {
			continue
		}
		switch k {
		case "repo":
			if s.Repo == "" {
				s.Repo = v
			}
		case "branch":
			if s.Branch == "" {
				s.Branch = v
			}
		case "workflow":
			if s.Workflow == "" {
				s.Workflow = v
			}
		case "artifact":
			if s.ArtifactName == "" {
				s.ArtifactName = v
			}
		case "artifact-pattern":
			if s.ArtifactPattern == "" {
				s.ArtifactPattern = v
			}
		case "file":
			if s.FilePattern == "" {
				s.FilePattern = v
			}
		case "exclude":
			if s.ExcludePattern == "" {
				s.ExcludePattern = v
			}
		case "source":
			if s.Source == "" || s.Source == "auto" {
				s.Source = v
			}
		}
	}
}

func applyTop(s *Settings, kv map[string]string) {
	for k, v := range kv {
		setTrustedField(s, canonKey(k), v)
	}
}

func setTrustedField(s *Settings, k, v string) {
	switch k {
	case "repo":
		if v != "" {
			s.Repo = v
		}
	case "branch":
		s.Branch = v
	case "workflow":
		s.Workflow = v
	case "artifact":
		s.ArtifactName = v
	case "artifact-pattern":
		s.ArtifactPattern = v
	case "file":
		s.FilePattern = v
	case "exclude":
		s.ExcludePattern = v
	case "arch":
		if nv := normArch(v); nv != "" {
			s.Arch = nv
		}
	case "variant":
		switch strings.ToLower(v) {
		case "auto", "debug", "release", "any":
			s.Variant = strings.ToLower(v)
		}
	case "source":
		switch strings.ToLower(v) {
		case "auto", "actions", "releases":
			s.Source = strings.ToLower(v)
		}
	case "downgrade":
		switch strings.ToLower(v) {
		case "auto", "allow":
			s.Downgrade = strings.ToLower(v)
		case "never", "skip":
			s.Downgrade = "never"
		}
	case "package":
		s.PackageName = v
	case "limit":
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 200 {
				n = 200
			}
			s.RunLimit = n
		}
	case "theme":
		if v != "" {
			s.Theme = v
		}
	case "no-open":
		s.NoOpen = truthy(v)
	case "keep-cache":
		s.KeepCache = truthy(v)
	case "keep":
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			s.KeepCount = n
		}
	case "watch":
		s.Watch = truthy(v)
	case "auto-refresh":
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			s.AutoRefreshSecs = n
		}
	case "pkg-check":
		s.PkgCheck = truthy(v)
	case "sig-check":
		s.SigCheck = truthy(v)
	case "verify-digest":
		s.VerifyDigest = truthy(v)
	case "verify-install":
		s.VerifyInstall = truthy(v)
	case "uninstall-first":
		// Intentionally ignored even in config files: GHAPK never executes
		// unattended package uninstallation or app-data erasure from config.
	}
}

func normArch(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "arm64-v8a", "aarch64", "arm64":
		return "arm64"
	case "armeabi", "armeabi-v7a", "armv7":
		return "armv7"
	case "x64", "amd64", "x86_64":
		return "x86_64"
	case "x86", "auto", "any", "universal":
		return strings.ToLower(strings.TrimSpace(v))
	}
	return ""
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "y":
		return true
	}
	return false
}

func untruth(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// SaveGlobal writes the global config file atomically.
func SaveGlobal(s Settings) error { return save(globalPath(), encode(s, false)) }

// SaveRepo writes the per-repo config file atomically.
func SaveRepo(s Settings) error {
	if s.Repo == "" {
		return fmt.Errorf("no repo set")
	}
	return save(RepoPath(s.Repo), encode(s, true))
}

func encode(s Settings, repoLevel bool) string {
	var b strings.Builder
	b.WriteString("# ghapk config — KEY=VALUE, one per line\n")
	if !repoLevel {
		if s.Theme != "" && s.Theme != "action" {
			kvline(&b, "theme", s.Theme)
		}
		kvline(&b, "auto-refresh", fmt.Sprint(s.AutoRefreshSecs))
	}
	kvline(&b, "arch", s.Arch)
	kvline(&b, "variant", s.Variant)
	kvline(&b, "source", s.Source)
	kvline(&b, "downgrade", s.Downgrade)
	kvline(&b, "limit", fmt.Sprint(s.RunLimit))
	if s.Branch != "" {
		kvline(&b, "branch", s.Branch)
	}
	if s.Workflow != "" {
		kvline(&b, "workflow", s.Workflow)
	}
	if s.ArtifactName != "" {
		kvline(&b, "artifact", s.ArtifactName)
	}
	if s.ArtifactPattern != "" {
		kvline(&b, "artifact-pattern", s.ArtifactPattern)
	}
	if s.FilePattern != "" {
		kvline(&b, "file", s.FilePattern)
	}
	if s.ExcludePattern != "" {
		kvline(&b, "exclude", s.ExcludePattern)
	}
	if s.PackageName != "" {
		kvline(&b, "package", s.PackageName)
	}
	kvline(&b, "pkg-check", untruth(s.PkgCheck))
	kvline(&b, "sig-check", untruth(s.SigCheck))
	kvline(&b, "verify-digest", untruth(s.VerifyDigest))
	kvline(&b, "verify-install", untruth(s.VerifyInstall))
	kvline(&b, "no-open", untruth(s.NoOpen))
	kvline(&b, "keep-cache", untruth(s.KeepCache))
	kvline(&b, "keep", fmt.Sprint(s.KeepCount))
	kvline(&b, "watch", untruth(s.Watch))
	return b.String()
}

func kvline(b *strings.Builder, k, v string) { fmt.Fprintf(b, "%s=%s\n", k, v) }

func save(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ───────────────────────── recents ─────────────────────────

type RecentRepo struct {
	Repo      string
	UsedAt    time.Time
	Favourite bool
}

func recentsPath() string { return filepath.Join(Dir(), "recents") }

// LoadRecents reads the recently-used repos, most recent first.
func LoadRecents() []RecentRepo {
	f, err := os.Open(recentsPath())
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []RecentRepo
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		r := RecentRepo{Repo: parts[0]}
		if len(parts) > 1 {
			r.UsedAt, _ = time.Parse(time.RFC3339, parts[1])
		}
		if len(parts) > 2 {
			r.Favourite = parts[2] == "fav"
		}
		out = append(out, r)
	}
	return out
}

// TouchRecent records a repo as used now, keeping at most 25 entries and
// preserving any favourite flag the entry had.
func TouchRecent(repo string) {
	rs := LoadRecents()
	fav := false
	next := []RecentRepo{{Repo: repo, UsedAt: time.Now()}}
	for _, r := range rs {
		if r.Repo == repo {
			fav = r.Favourite
			continue
		}
		next = append(next, r)
	}
	next[0].Favourite = fav
	if len(next) > 25 {
		next = next[:25]
	}
	saveRecents(next)
}

// RemoveRecent deletes an entry from the recents file.
func RemoveRecent(repo string) {
	rs := LoadRecents()
	next := rs[:0]
	for _, r := range rs {
		if r.Repo != repo {
			next = append(next, r)
		}
	}
	saveRecents(next)
}

// RenameRecent replaces a repo slug, keeping usage time and favourite flag.
func RenameRecent(old, new string) {
	rs := LoadRecents()
	for i := range rs {
		if rs[i].Repo == old {
			rs[i].Repo = new
		}
	}
	seen := map[string]bool{}
	next := rs[:0]
	for _, r := range rs {
		if !seen[r.Repo] {
			seen[r.Repo] = true
			next = append(next, r)
		}
	}
	saveRecents(next)
}

// ToggleFavourite flips the star on a recent entry; returns the new state.
func ToggleFavourite(repo string) bool {
	rs := LoadRecents()
	state := false
	for i := range rs {
		if rs[i].Repo == repo {
			rs[i].Favourite = !rs[i].Favourite
			state = rs[i].Favourite
		}
	}
	saveRecents(rs)
	return state
}

func saveRecents(rs []RecentRepo) {
	var b strings.Builder
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Favourite != rs[j].Favourite {
			return rs[i].Favourite
		}
		return rs[i].UsedAt.After(rs[j].UsedAt)
	})
	for _, r := range rs {
		fav := ""
		if r.Favourite {
			fav = "\tfav"
		}
		fmt.Fprintf(&b, "%s\t%s%s\n", r.Repo, r.UsedAt.Format(time.RFC3339), fav)
	}
	_ = save(recentsPath(), b.String())
}
