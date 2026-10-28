package engine

import (
	"ghapk/internal/glob"
	"ghapk/internal/version"
)

func matchAny(s string, p []*glob.Pattern) bool { return glob.MatchAny(s, p) }
func extractVersion(s string) string            { return version.Extract(s) }
func cmpVersion(a, b string) int                { return version.Compare(a, b) }
