package version

import "testing"

func TestExtract(t *testing.T) {
	cases := map[string]string{
		"app-v1.2.3-debug":         "1.2.3",
		"MyApp_2024.06_debug.apk":  "",
		"app-1.4-arm64":            "1.4",
		"build 01.02.2024":         "",
		"nazo-debug-2026.10.13":    "",
		"app-v2.0.1-rc1-universal": "2.0.1",
		"debug":                    "",
		"app-42":                   "",
	}
	for in, want := range cases {
		if got := Extract(in); got != want {
			t.Errorf("Extract(%q)=%q want %q", in, got, want)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2", "1.2.0", 0},
		{"1.10", "1.9", 1},
		{"1.0.0", "1.0.1", -1},
		{"3", "3-rc1", 1},
		{"2.0-rc1", "2.0-rc2", -1},
		{"1.2.3.4", "1.2.3", 1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}
