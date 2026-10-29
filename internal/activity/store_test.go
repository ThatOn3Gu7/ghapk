package activity

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPersistenceAcrossSessionsAndPrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	one, err := Open(dir, DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err = one.Write("job", "first session job"); err != nil {
		t.Fatal(err)
	}
	if err = one.Close(); err != nil {
		t.Fatal(err)
	}
	two, err := Open(dir, DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()
	entries := two.Entries()
	if len(entries) != 4 {
		t.Fatalf("entries: %+v", entries)
	}
	if entries[0].Session != entries[2].Session || entries[0].Session == entries[3].Session || entries[2].Text != "Session ended" || entries[3].Text != "Session started" {
		t.Fatalf("session separation: %+v", entries)
	}
	fi, err := os.Stat(filepath.Join(dir, "activity.log"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0077 != 0 {
		t.Fatalf("log not private: %o", fi.Mode().Perm())
	}
}
func TestBoundedRotationRetainsNewestCompleteRecords(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, 4096)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		if err = s.Write("job", fmt.Sprintf("entry-%03d %s", i, strings.Repeat("x", 80))); err != nil {
			t.Fatal(err)
		}
	}
	fi, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() > 4096 {
		t.Fatalf("log too large: %d", fi.Size())
	}
	entries := s.Entries()
	if len(entries) == 0 || !strings.Contains(entries[len(entries)-1].Text, "entry-119") {
		t.Fatalf("newest entry lost: %+v", entries)
	}
	f, err := os.Open(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Entry
		if err = json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("partial log record: %v", err)
		}
	}
	if err = sc.Err(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(dir, 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if len(restarted.Entries()) < 2 {
		t.Fatal("rotation lost entire history")
	}
}
func TestConcurrentStoresDoNotLoseRecords(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(dir, DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(dir, DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s := a
			if n%2 == 0 {
				s = b
			}
			for j := 0; j < 30; j++ {
				if err := s.Write("test", fmt.Sprintf("worker-%d-entry-%d", n, j)); err != nil {
					t.Errorf("write: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()
	entries := a.Entries()
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Kind == "test" {
			seen[e.Text] = true
		}
	}
	if len(seen) != 240 {
		t.Fatalf("lost or duplicate records: %d", len(seen))
	}
	if b.Version() == 0 {
		t.Fatal("version not updated")
	}
}
func TestCommandLoggingRedactsCredentialsAndQueries(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	SetActive(s)
	defer SetActive(nil)
	if err := CommandContext(context.Background(), "/bin/echo", "https://ghp_123456789abcdefgh@github.com/repos/a/b?token=super-secret", "--token", "mystery-secret").Run(); err != nil {
		t.Fatal(err)
	}
	if err := Command("/bin/false").Run(); err == nil {
		t.Fatal("false unexpectedly succeeded")
	}
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"ghp_123456789abcdefgh", "super-secret", "mystery-secret"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("secret leaked: %s", raw)
		}
	}
	text := string(raw)
	if !strings.Contains(text, "query redacted") || !strings.Contains(text, "[redacted]") || !strings.Contains(text, "failed") {
		t.Fatalf("missing command audit: %s", text)
	}
	if got := Redact("Authorization: Bearer abc.def token=abc xyz github_pat_abcdefghijk https://example.com/?signature=topsecret"); strings.Contains(got, "abc.def") || strings.Contains(got, "github_pat_") || strings.Contains(got, "topsecret") {
		t.Fatalf("redaction failed: %s", got)
	}
}
func TestRejectsSymlinkLog(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "activity.log")); err != nil {
		t.Skip(err)
	}
	if _, err := Open(dir, DefaultMaxBytes); err == nil {
		t.Fatal("symlink accepted")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "safe" {
		t.Fatal("symlink target modified")
	}
}
func TestHugeEscapedRecordAndExternalDeletion(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Write("test", strings.Repeat("\"", 10000)); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(s.Path())
	if fi.Size() > 4096 {
		t.Fatalf("oversize record: %d", fi.Size())
	}
	if err = os.Remove(s.Path()); err != nil {
		t.Fatal(err)
	}
	if err = s.Write("test", "new after delete"); err != nil {
		t.Fatal(err)
	}
	entries := s.Entries()
	if len(entries) != 1 || entries[0].Text != "new after delete" {
		t.Fatalf("stale cached entries after delete: %+v", entries)
	}
}
func TestRepairsPartialTailBeforeAppending(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, 4096)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Write("test", "before crash")
	_ = s.Close()
	f, err := os.OpenFile(s.Path(), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"at":"partial`)
	_ = f.Close()
	restarted, err := Open(dir, 4096)
	if err != nil {
		t.Fatal(err)
	}
	_ = restarted.Write("test", "after crash")
	_ = restarted.Close()
	again, err := Open(dir, 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	seen := map[string]bool{}
	for _, e := range again.Entries() {
		seen[e.Text] = true
	}
	if !seen["before crash"] || !seen["after crash"] {
		t.Fatalf("valid entries lost after tail repair: %+v", seen)
	}
	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var e Entry
		if err = json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("invalid record %q: %v", line, err)
		}
	}
}

func TestRepairTailAfterExternalChangeAndRefresh(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, 4096)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(s.Path(), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"at":"partial`)
	_ = f.Close()
	_ = s.Version()
	if err = s.Write("test", "survives refresh"); err != nil {
		t.Fatal(err)
	}
	next, err := Open(dir, 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	found := false
	for _, e := range next.Entries() {
		if e.Text == "survives refresh" {
			found = true
		}
	}
	if !found {
		t.Fatal("refresh left partial tail, losing new entry")
	}
}
