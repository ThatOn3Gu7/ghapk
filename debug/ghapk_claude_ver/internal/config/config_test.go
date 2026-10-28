package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAndCanon(t *testing.T) {
	vals, unk := Parse("# c\nREPO = a/b # tail\nartifact_pattern=*x*\nARCH=arm64-v8a\nbogus=1\nuninstall-first = YES\n")
	if vals["repo"] != "a/b" || vals["artifact-pattern"] != "*x*" || vals["arch"] != "arm64" || vals["uninstall-first"] != "1" {
		t.Fatalf("bad parse: %v", vals)
	}
	if len(unk) != 1 || unk[0] != "bogus" {
		t.Fatalf("unknown: %v", unk)
	}
}

func TestPrecedenceAndSetKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHAPK_CONFIG_DIR", dir)
	for _, k := range []string{"REPO", "BRANCH", "GHAPK_BRANCH"} {
		t.Setenv(k, "")
	}
	if err := SetKey(GlobalFile(), "branch", "main"); err != nil {
		t.Fatal(err)
	}
	if err := SetKey(RepoFile("o/r"), "branch", "dev"); err != nil {
		t.Fatal(err)
	}
	c := Load(LoadOpts{Repo: "o/r", LocalFile: filepath.Join(dir, "none")})
	if c.Str("branch") != "dev" || c.Source("branch") != SrcRepo {
		t.Fatalf("repo file should win: %q %s", c.Str("branch"), c.Source("branch"))
	}
	c.Set("branch", "feat")
	if c.Str("branch") != "feat" || c.Source("branch") != SrcCLI {
		t.Fatal("cli should win")
	}
	c2 := Load(LoadOpts{Repo: "x/y", LocalFile: filepath.Join(dir, "none")})
	if c2.Str("branch") != "main" || c2.Source("branch") != SrcGlobal {
		t.Fatal("global fallback")
	}
	if c2.Str("artifact-pattern") != "*debug*,*apk*" || c2.Source("artifact-pattern") != SrcDef {
		t.Fatal("default")
	}
	// replace in place, keep comments, delete on empty
	p := GlobalFile()
	b, _ := os.ReadFile(p)
	os.WriteFile(p, append([]byte("# hello\n"), b...), 0o600)
	if err := SetKey(p, "branch", "x"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if string(b) != "# hello\nbranch = x\n" {
		t.Fatalf("got %q", b)
	}
	if err := SetKey(p, "branch", ""); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if string(b) != "# hello\n" {
		t.Fatalf("got %q", b)
	}
	if _, err := Validate("theme", "nope"); err == nil {
		t.Fatal("theme should be validated")
	}
}

func TestHistoryRoundTrip(t *testing.T) {
	t.Setenv("GHAPK_HISTORY", filepath.Join(t.TempDir(), "h.json"))
	if err := SaveHistory("a/b", Entry{Installs: 2, Run: 9, VCode: "12"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveHistory("c/d", Entry{Installs: 1}); err != nil {
		t.Fatal(err)
	}
	e, ok := History("a/b")
	if !ok || e.Installs != 2 || e.Run != 9 {
		t.Fatalf("%+v", e)
	}
	b, _ := os.ReadFile(HistoryFile())
	if !contains(string(b), "\"a/b\": {\"installs\":2") {
		t.Fatalf("one-entry-per-line format lost:\n%s", b)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
