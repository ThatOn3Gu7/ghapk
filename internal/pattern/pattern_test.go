package pattern

import "testing"

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"app", "my-app-debug.apk", true},
		{"APP", "my-app-debug.apk", true},
		{"*.apk", "app.apk", true},
		{"*.apk", "app.zip", false},
		{"app-?.apk", "app-1.apk", true},
		{"app-[12].apk", "app-2.apk", true},
		{"app-*-release", "app-arm64-release", true},
		{"app-*-release", "app-arm64-debug", false},
		{"  app  ", "my-app-debug.apk", true},
		{"", "app.apk", false},
		{"[", "app.apk", false},
	}
	for _, tt := range tests {
		if got := Match(tt.pattern, tt.name); got != tt.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}

func TestMatchList(t *testing.T) {
	tests := []struct {
		patterns string
		name     string
		want     bool
	}{
		{"foo,bar", "bar.apk", true},
		{"foo, bar", "bar.apk", true},
		{"foo,bar", "baz.apk", false},
		{"*,!debug", "app-debug.apk", true},
		{"", "app.apk", false},
	}
	for _, tt := range tests {
		if got := MatchList(tt.patterns, tt.name); got != tt.want {
			t.Errorf("MatchList(%q, %q) = %v, want %v", tt.patterns, tt.name, got, tt.want)
		}
	}
}
