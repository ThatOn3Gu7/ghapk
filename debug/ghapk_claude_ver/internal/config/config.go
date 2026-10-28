package config

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ghapk/internal/glob"
)

// Layer names, also shown in the settings editor as the "source" of a value.
const (
	SrcCLI    = "flag"
	SrcEnv    = "env"
	SrcRepo   = "repo"
	SrcLocal  = "local"
	SrcGlobal = "global"
	SrcInRepo = "in-repo"
	SrcDef    = "default"
)

type layer struct {
	name string
	vals map[string]string
}

// Config is a resolved view over the layers. The first layer holding a
// non-empty value for a key wins; the registry default is the last resort.
type Config struct {
	layers []layer
}

// Dirs returns the config and cache directories, honouring XDG.
func ConfigDir() string {
	if d := os.Getenv("GHAPK_CONFIG_DIR"); d != "" {
		return d
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		h, _ := os.UserHomeDir()
		base = filepath.Join(h, ".config")
	}
	return filepath.Join(base, "ghapk")
}

// CacheDir is where downloads, logs and the HTTP cache live.
func CacheDir() string {
	if d := os.Getenv("GHAPK_CACHE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("CACHE_DIR"); d != "" { // the shell script's name for it
		return d
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		h, _ := os.UserHomeDir()
		base = filepath.Join(h, ".cache")
	}
	return filepath.Join(base, "ghapk")
}

// GlobalFile is the file --set-global writes.
func GlobalFile() string { return filepath.Join(ConfigDir(), "config") }

// RepoFile is the per-repo file for "owner/name".
func RepoFile(repo string) string {
	if repo == "" {
		return ""
	}
	return filepath.Join(ConfigDir(), "repos", strings.ReplaceAll(repo, "/", "_")+".conf")
}

// LocalFile is ./.ghapk.conf, or the file named by --config.
const LocalFile = ".ghapk.conf"

// RepoConfigPath is where a config committed to the repository itself lives.
const RepoConfigPath = ".github/ghapk.conf"

// Parse reads the "key = value" format: # starts a comment, values may contain
// '=', keys accept any spelling Canon knows. Unknown keys are returned so the
// caller can say so, but never stop the parse.
func Parse(text string) (vals map[string]string, unknown []string) {
	vals = map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		ck, ok := Canon(k)
		if !ok {
			unknown = append(unknown, k)
			continue
		}
		if nv, err := Validate(ck, v); err == nil {
			v = nv
		}
		if _, dup := vals[ck]; !dup {
			vals[ck] = v
		}
	}
	return
}

// LoadOpts says where the layers come from.
type LoadOpts struct {
	Repo      string            // canonical OWNER/NAME, "" if not chosen yet
	CLI       map[string]string // canonical key -> value from flags
	LocalFile string            // override for ./.ghapk.conf
	InRepo    string            // text of .github/ghapk.conf, if fetched
}

var legacyEnv = map[string]string{
	"REPO": "repo", "WORKFLOW": "workflow", "BRANCH": "branch", "ARTIFACT_NAME": "artifact",
	"ARTIFACT_PATTERN": "artifact-pattern", "FILE_PATTERN": "file", "EXCLUDE_PATTERN": "exclude",
	"SOURCE_MODE": "source", "RUN_LIMIT": "limit", "ARCH_PREF": "arch", "VARIANT_PREF": "variant",
	"PACKAGE_NAME": "package", "DOWNGRADE_MODE": "downgrade",
}

func envLayer() map[string]string {
	m := map[string]string{}
	for name, key := range legacyEnv {
		if v := os.Getenv(name); v != "" {
			m[key] = v
		}
	}
	for _, s := range Registry {
		env := "GHAPK_" + strings.ToUpper(strings.ReplaceAll(s.Key, "-", "_"))
		if v := os.Getenv(env); v != "" {
			m[s.Key] = v
		}
	}
	return m
}

func readFile(p string) map[string]string {
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	m, _ := Parse(string(b))
	return m
}

// Load builds the layered config. Precedence, highest first:
// CLI flags, environment, per-repo file, ./.ghapk.conf, global file, in-repo file.
func Load(o LoadOpts) *Config {
	c := &Config{}
	c.layers = append(c.layers, layer{SrcCLI, o.CLI})
	c.layers = append(c.layers, layer{SrcEnv, envLayer()})
	repo := o.Repo
	if repo == "" {
		repo = o.CLI["repo"]
	}
	if repo == "" {
		repo = envLayer()["repo"]
	}
	local := o.LocalFile
	if local == "" {
		local = LocalFile
	}
	if repo == "" { // a repo named by the local/global file still picks its own file
		for _, p := range []string{local, GlobalFile()} {
			if r := readFile(p)["repo"]; r != "" {
				repo = r
				break
			}
		}
	}
	c.layers = append(c.layers, layer{SrcRepo, readFile(RepoFile(repo))})
	c.layers = append(c.layers, layer{SrcLocal, readFile(local)})
	c.layers = append(c.layers, layer{SrcGlobal, readFile(GlobalFile())})
	if o.InRepo != "" {
		m, _ := Parse(o.InRepo)
		c.layers = append(c.layers, layer{SrcInRepo, m})
	}
	return c
}

// Source returns where the effective value of key comes from.
func (c *Config) Source(key string) string {
	for _, l := range c.layers {
		if l.vals[key] != "" {
			return l.name
		}
	}
	return SrcDef
}

// Str is the effective string value of a canonical key.
func (c *Config) Str(key string) string {
	for _, l := range c.layers {
		if v := l.vals[key]; v != "" {
			return v
		}
	}
	if s := byKey[key]; s != nil {
		return s.Default
	}
	return ""
}

// Bool is the effective boolean value.
func (c *Config) Bool(key string) bool { return NormBool(c.Str(key)) == "1" }

// Int is the effective integer value, falling back to the default when the
// stored value does not parse.
func (c *Config) Int(key string) int {
	n, err := strconv.Atoi(c.Str(key))
	if err != nil {
		n, _ = strconv.Atoi(byKey[key].Default)
	}
	return n
}

// Limit is the run limit clamped to 1..200.
func (c *Config) Limit() int {
	n := c.Int("limit")
	if n <= 0 {
		return 20
	}
	if n > 200 {
		return 200
	}
	return n
}

// Patterns returns the compiled glob list for a pattern-valued key.
func (c *Config) Patterns(key string) []*glob.Pattern {
	return glob.SplitList(c.Str(key))
}

// Set overrides a key at the CLI layer for this process (used by the TUI when
// you change a value without saving it).
func (c *Config) Set(key, val string) {
	if c.layers[0].vals == nil {
		c.layers[0].vals = map[string]string{}
	}
	if val == "" {
		delete(c.layers[0].vals, key)
		return
	}
	c.layers[0].vals[key] = val
}

// ---- writing ---------------------------------------------------------------

// SetKey writes key=value into a config file, replacing an existing line in
// place and keeping every other line and comment. An empty value removes the
// key. The write is atomic.
func SetKey(path, key, val string) error {
	ck, ok := Canon(key)
	if !ok {
		return errors.New("not a setting: " + key)
	}
	nv, err := Validate(ck, val)
	if err != nil {
		return err
	}
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		if len(lines) == 1 && lines[0] == "" {
			lines = nil
		}
	}
	out := make([]string, 0, len(lines)+1)
	done := false
	for _, ln := range lines {
		code := ln
		if i := strings.IndexByte(code, '#'); i >= 0 {
			code = code[:i]
		}
		if eq := strings.IndexByte(code, '='); eq >= 0 {
			if k, _ := Canon(code[:eq]); k == ck && strings.TrimSpace(code[:eq]) != "" {
				if nv != "" && !done {
					out = append(out, ck+" = "+nv)
					done = true
				}
				continue
			}
		}
		out = append(out, ln)
	}
	if nv != "" && !done {
		out = append(out, ck+" = "+nv)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	body := strings.Join(out, "\n")
	if body != "" {
		body += "\n"
	}
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ExpandHome turns a leading ~ into $HOME.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, strings.TrimPrefix(p, "~"))
	}
	return p
}
