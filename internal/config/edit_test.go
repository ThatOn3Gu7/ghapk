package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingScopePreservesOverridesAndComments(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	if err := os.WriteFile(globalPath(), []byte("# retained\ntheme=mocha\nkeep=7\n"), 0600); err != nil {
		t.Fatal(err)
	}
	repo := "owner/repo"
	v := "paper"
	if err := SaveSetting(repo, "theme", &v); err != nil {
		t.Fatal(err)
	}
	if got := LoadForRepo(repo).Theme; got != "paper" {
		t.Fatalf("repo: %s", got)
	}
	if got := SettingSource(repo, "theme"); got != "repo" {
		t.Fatal(got)
	}
	v = "emerald"
	if err := SaveSetting("", "theme", &v); err != nil {
		t.Fatal(err)
	}
	if got := LoadForRepo(repo).Theme; got != "paper" {
		t.Fatalf("global overwrote repo: %s", got)
	}
	b, err := os.ReadFile(globalPath())
	if err != nil || !strings.Contains(string(b), "# retained\n") || !strings.Contains(string(b), "keep=7\n") {
		t.Fatalf("other settings lost: %s %v", b, err)
	}
	t.Setenv("GHAPK_THEME", "action")
	if got := LoadForRepo(repo).Theme; got != "action" || SettingSource(repo, "theme") != "env" {
		t.Fatal("env did not win")
	}
	t.Setenv("GHAPK_THEME", "")
	if err := SaveSetting(repo, "theme", nil); err != nil {
		t.Fatal(err)
	}
	if got := LoadForRepo(repo).Theme; got != "emerald" || SettingSource(repo, "theme") != "global" {
		t.Fatalf("reset failed: %s", got)
	}
	if err := SaveSetting("../escape/repo", "theme", &v); err == nil {
		t.Fatal("accepted unsafe repo path")
	}
	if _, err := os.Stat(filepath.Join(Dir(), "escape_repo.conf")); !os.IsNotExist(err) {
		t.Fatal("unsafe path created")
	}
}
func TestNewSettingsValidationAndUntrustedBoundary(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	tests := map[string]string{"glyphs": "ascii", "mouse": "0", "all-arch": "1", "identical-check": "0", "install-wait": "120"}
	for k, v := range tests {
		if err := SaveSetting("", k, &v); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		if got := Value(Load(), k); got != v {
			t.Fatalf("%s = %s, want %s", k, got, v)
		}
	}
	for _, tc := range []struct{ k, v string }{{"theme", "no-such-theme"}, {"install-wait", "0"}, {"limit", "500"}, {"branch", "a\ninjected=1"}, {"branch", "hi #comment"}} {
		s := Defaults()
		if SetValue(&s, tc.k, tc.v) == nil {
			t.Fatalf("accepted %s=%q", tc.k, tc.v)
		}
	}
	s := Defaults()
	blocked := ApplyRemoteRepoConfig(&s, "all-arch=1\nidentical-check=0\nmouse=0\ninstall-wait=5\nglyphs=ascii\n")
	if len(blocked) != 5 || s.AllArch || !s.IdenticalCheck || !s.Mouse || s.InstallWait != 90 || s.Glyphs != "auto" {
		t.Fatalf("untrusted settings applied: %+v %v", s, blocked)
	}
}
