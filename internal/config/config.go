// Package config loads and saves ghapk settings.
//
// The file format is intentionally the same KEY=VALUE line format the
// original bash script used, so existing users can keep their files:
//
//	~/.config/ghapk/config                 — global, every repo
//	~/.config/ghapk/repos/<owner>_<repo>.conf — one repo, outranks global
//	.ghapk.conf                            — working directory, outranked by flags
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	Watch           bool   `key:"watch"`
	AutoRefreshSecs int    `key:"auto-refresh"`
}

// Defaults returns the out-of-the-box settings.
func Defaults() Settings {
	return Settings{
		Arch:            "auto",
		Variant:         "auto",
		Source:          "auto",
		Downgrade:       "auto",
		RunLimit:        20,
		Theme:           "action",
		AutoRefreshSecs: 60,
	}
}

// Dir is the per-user config directory (~/.config/ghapk).
func Dir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
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

// CacheDir mirrors the script's CACHE_DIR (~/.cache/ghapk).
func CacheDir() string {
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "ghapk")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".cache", "ghapk")
}

// Load reads global + working-dir config, then the per-repo file if the repo
// is known. Later sources only fill fields the earlier ones left empty,
// exactly like the script's apply_config_line.
func Load() Settings {
	s := Defaults()
	kv := map[string]string{}
	mergeInto := func(path string) {
		for k, v := range readKV(path) {
			if _, seen := kv[k]; !seen {
				kv[k] = v
			}
		}
	}
	mergeInto(globalPath())
	mergeInto(".ghapk.conf")
	apply(&s, kv)
	if s.Repo != "" {
		// per-repo file wins over global: re-apply its values on top
		applyTop(&s, readKV(RepoPath(s.Repo)))
	}
	return s
}

// LoadForRepo returns settings with a specific repo's per-repo file applied.
func LoadForRepo(repo string) Settings {
	s := Load()
	s.Repo = repo
	applyTop(&s, readKV(RepoPath(repo)))
	return s
}

func lowerKey(k string) string { return strings.ToLower(strings.TrimSpace(k)) }

// readKV parses a KEY=VALUE file; blank lines and '#' comments are ignored.
func readKV(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[lowerKey(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return out
}

func apply(s *Settings, kv map[string]string) {
	for k, v := range kv {
		setField(s, k, v, false)
	}
}

func applyTop(s *Settings, kv map[string]string) {
	for k, v := range kv {
		setField(s, k, v, true)
	}
}

func setField(s *Settings, k, v string, force bool) {
	setS := func(dst *string) {
		if force || *dst == "" {
			*dst = v
		}
	}
	switch k {
	case "repo":
		setS(&s.Repo)
	case "branch":
		setS(&s.Branch)
	case "workflow":
		setS(&s.Workflow)
	case "artifact":
		setS(&s.ArtifactName)
	case "artifact_pattern", "artifact-pattern":
		setS(&s.ArtifactPattern)
	case "file", "file_pattern", "file-pattern":
		setS(&s.FilePattern)
	case "exclude", "exclude_pattern", "exclude-pattern":
		setS(&s.ExcludePattern)
	case "arch", "arch_pref":
		if v != "" {
			s.Arch = v
		}
	case "variant", "variant_pref":
		if v != "" {
			s.Variant = v
		}
	case "source", "source_mode":
		if v != "" {
			s.Source = v
		}
	case "downgrade", "downgrade_mode":
		if v != "" {
			s.Downgrade = v
		}
	case "package", "package_name":
		setS(&s.PackageName)
	case "limit", "run_limit":
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			s.RunLimit = n
		}
	case "theme":
		if v != "" {
			s.Theme = v
		}
	case "no_open", "no-open":
		s.NoOpen = truthy(v)
	case "keep_cache", "keep-cache":
		s.KeepCache = truthy(v)
	case "watch":
		s.Watch = truthy(v)
	case "auto_refresh", "auto-refresh":
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n >= 0 {
			s.AutoRefreshSecs = n
		}
	}
}

func truthy(v string) bool {
	switch strings.ToLower(v) {
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

// SaveGlobal writes the global config file.
func SaveGlobal(s Settings) error { return save(globalPath(), encode(s, false)) }

// SaveRepo writes the per-repo config file.
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
	if s.ArtifactPattern != "" {
		kvline(&b, "artifact-pattern", s.ArtifactPattern)
	}
	if s.FilePattern != "" {
		kvline(&b, "file", s.FilePattern)
	}
	if s.PackageName != "" {
		kvline(&b, "package", s.PackageName)
	}
	kvline(&b, "no-open", untruth(s.NoOpen))
	kvline(&b, "keep-cache", untruth(s.KeepCache))
	kvline(&b, "watch", untruth(s.Watch))
	return b.String()
}

func kvline(b *strings.Builder, k, v string) { fmt.Fprintf(b, "%s=%s\n", k, v) }

func save(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o600)
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
	// collapse any duplicate created by the rename
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
	// favourites stay on top
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
