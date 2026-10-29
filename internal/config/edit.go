package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// Value returns the canonical serialized form of a typed setting.
func Value(s Settings, key string) string {
	key = canonKey(key)
	v, typ := reflect.ValueOf(s), reflect.TypeOf(s)
	for i := 0; i < v.NumField(); i++ {
		if typ.Field(i).Tag.Get("key") != key {
			continue
		}
		switch v.Field(i).Kind() {
		case reflect.String:
			return v.Field(i).String()
		case reflect.Bool:
			return untruth(v.Field(i).Bool())
		case reflect.Int:
			return strconv.FormatInt(v.Field(i).Int(), 10)
		}
	}
	return ""
}

// SetValue validates an interactive edit before applying it. Untrusted config
// continues to go through SanitizeUntrustedRepoConfig rather than this API.
func SetValue(s *Settings, key, value string) error {
	key, value = canonKey(key), strings.TrimSpace(value)
	if strings.ContainsAny(value, "\r\n\x00") || strings.Contains(value, " #") || strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") || strings.HasSuffix(value, "\"") || strings.HasSuffix(value, "'") {
		return fmt.Errorf("value cannot contain newlines or config comment/quote delimiters")
	}
	switch key {
	case "notify-on":
		if !oneOf(value, "always", "success", "failure", "never") {
			return fmt.Errorf("unknown notification policy")
		}
	case "glyphs":
		if !oneOf(value, "auto", "unicode", "ascii") {
			return fmt.Errorf("unknown glyph style")
		}
	case "install-wait":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 3600 {
			return fmt.Errorf("must be 1–3600 seconds")
		}
	case "theme":
		if !oneOf(value, "action", "mocha", "emerald", "paper") {
			return fmt.Errorf("unknown theme")
		}
	case "arch":
		if normArch(value) == "" {
			return fmt.Errorf("unknown architecture")
		}
	case "variant":
		if !oneOf(value, "auto", "debug", "release", "any") {
			return fmt.Errorf("unknown variant")
		}
	case "source":
		if !oneOf(value, "auto", "actions", "releases") {
			return fmt.Errorf("unknown source")
		}
	case "downgrade":
		if !oneOf(value, "auto", "allow", "never") {
			return fmt.Errorf("unknown downgrade policy")
		}
	case "limit":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 200 {
			return fmt.Errorf("limit must be 1–200")
		}
	case "keep", "auto-refresh":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 86400 {
			return fmt.Errorf("must be 0–86400")
		}
	case "pkg-check", "sig-check", "verify-digest", "verify-install", "keep-cache", "watch", "no-open", "mouse", "all-arch", "identical-check", "exit-summary", "speak", "toast":
		if !oneOf(value, "1", "0", "on", "off", "true", "false") {
			return fmt.Errorf("expected on or off")
		}
	case "repo":
		if value != "" && !validRepoHint(value) {
			return fmt.Errorf("expected owner/repo")
		}
	case "branch", "workflow", "artifact", "artifact-pattern", "file", "exclude", "package", "out":
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
	setTrustedField(s, key, value)
	return nil
}
func oneOf(v string, options ...string) bool {
	for _, x := range options {
		if v == x {
			return true
		}
	}
	return false
}

// LoadSavedScope returns the value to edit at a particular scope, before
// environment and CLI overrides. An empty repo means the global layer only.
func LoadSavedScope(repo string) Settings {
	s := Defaults()
	global := readKV(globalPath())
	safe, _ := SanitizeUntrustedRepoConfig(readKV(".ghapk.conf"))
	seen := map[string]bool{}
	for k := range global {
		seen[k] = true
	}
	applyUntrustedHints(&s, safe, seen)
	applyTop(&s, global)
	if validRepoHint(repo) {
		applyTop(&s, readKV(RepoPath(repo)))
		s.Repo = repo
	}
	return s
}

// SettingSource identifies trusted precedence; workspace hints are lowest.
func SettingSources(repo string) map[string]string {
	sources := map[string]string{}
	safe, _ := SanitizeUntrustedRepoConfig(readKV(".ghapk.conf"))
	for k := range safe {
		sources[k] = "workspace"
	}
	for k := range readKV(globalPath()) {
		sources[k] = "global"
	}
	if validRepoHint(repo) {
		for k := range readKV(RepoPath(repo)) {
			sources[k] = "repo"
		}
	}
	for k := range readTrustedEnv() {
		sources[k] = "env"
	}
	return sources
}
func SettingSource(repo, key string) string {
	if s := SettingSources(repo)[canonKey(key)]; s != "" {
		return s
	}
	return "default"
}
func HasRepoSetting(repo, key string) bool {
	if !validRepoHint(repo) {
		return false
	}
	_, ok := readKV(RepoPath(repo))[canonKey(key)]
	return ok
}

// SaveSetting changes only one key, preserving unrelated lines and comments.
// nil removes the selected scope's override, revealing the next layer.
func SaveSetting(repo, key string, value *string) error {
	key = canonKey(key)
	var path string
	if repo == "" {
		path = globalPath()
	} else {
		if !validRepoHint(repo) || strings.ContainsAny(repo, `\\`) || strings.Contains(repo, "..") {
			return fmt.Errorf("invalid repository")
		}
		path = RepoPath(repo)
	}
	if value != nil {
		s := Defaults()
		if err := SetValue(&s, key, *value); err != nil {
			return err
		}
	}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		k, _, ok := strings.Cut(line, "=")
		if ok && !strings.HasPrefix(strings.TrimSpace(line), "#") && canonKey(k) == key {
			continue
		}
		if line != "" || len(b) != 0 {
			lines = append(lines, line)
		}
	}
	if value != nil {
		lines = append(lines, key+"="+*value)
	}
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	return save(path, body)
}
