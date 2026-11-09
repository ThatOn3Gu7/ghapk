// Package pattern provides the shared, case-insensitive name matching used by
// CLI and TUI artifact and file filters.
package pattern

import (
	"path"
	"strings"
)

// Match reports whether name matches pattern, ignoring case. A pattern without
// glob metacharacters is treated as a substring so plain words behave
// intuitively; otherwise * and ? follow path.Match semantics. A malformed
// pattern falls back to a substring match with outer * trimmed.
func Match(pattern, name string) bool {
	p := strings.ToLower(strings.TrimSpace(pattern))
	if p == "" {
		return false
	}
	n := strings.ToLower(name)
	if !strings.ContainsAny(p, "*?[") {
		return strings.Contains(n, p)
	}
	ok, err := path.Match(p, n)
	if err != nil {
		sub := strings.Trim(p, "*")
		return sub != "" && strings.Contains(n, sub)
	}
	return ok
}

// MatchList reports whether name matches any comma-separated pattern.
func MatchList(patterns, name string) bool {
	for _, part := range strings.Split(patterns, ",") {
		part = strings.TrimSpace(part)
		if part != "" && Match(part, name) {
			return true
		}
	}
	return false
}
