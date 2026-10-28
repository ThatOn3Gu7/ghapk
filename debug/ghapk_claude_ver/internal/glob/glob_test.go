package glob

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pat, s string
		want   bool
	}{
		{"*.apk", "app-debug.apk", true},
		{"*.apk", "APP-Debug.APK", true}, // case-insensitive
		{"*.apk", "app.apk.zip", false},
		{"*debug*", "my-Debug-build", true},
		{"*debug*", "release", false},
		{"*", "a/b/c", true}, // * crosses /
		{"app-?.apk", "app-1.apk", true},
		{"app-?.apk", "app-12.apk", false},
		{"[a-c]x", "bx", true},
		{"[!a-c]x", "bx", false},
		{"[!a-c]x", "dx", true},
		{"@(a|b).apk", "a.apk", true},
		{"@(a|b).apk", "c.apk", false},
		{"+(ab).x", "ababab.x", true},
		{"+(ab).x", ".x", false},
		{"*(ab).x", ".x", true},
		{"?(v)1.0", "1.0", true},
		{"?(v)1.0", "v1.0", true},
		{"!(*release*).apk", "debug.apk", true},
		{"!(*release*).apk", "release.apk", false},
		{"app-@(arm64-v8a|x86_64)-debug", "app-arm64-v8a-debug", true},
		{"a[", "a[", true}, // broken class is literal
		{"x(", "x(", true},
	}
	for _, c := range cases {
		if got := Compile(c.pat).Match(c.s); got != c.want {
			t.Errorf("Match(%q,%q)=%v want %v", c.pat, c.s, got, c.want)
		}
	}
}

func TestList(t *testing.T) {
	p := SplitList(" *debug* , ,*apk* ")
	if len(p) != 2 {
		t.Fatalf("want 2 patterns, got %d", len(p))
	}
	if !MatchAny("my-apk", p) || MatchAny("zzz", p) {
		t.Fatal("MatchAny wrong")
	}
}
