package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectionsRoundTripAndExplicitAll(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHAPK_CONFIG_DIR", dir)
	branch, wf := "feature/a", "Android CI"
	if err := RememberSelection("Owner/One", true, &branch, nil); err != nil {
		t.Fatal(err)
	}
	if err := RememberSelection("owner/one", true, nil, &wf); err != nil {
		t.Fatal(err)
	}
	other := "release"
	if err := RememberSelection("Owner/Two", true, &other, nil); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSelections()
	if err != nil {
		t.Fatal(err)
	}
	if s.LastRepo != "Owner/Two" || *s.Repos["owner/one"].Branch != branch || *s.Repos["owner/one"].Workflow != wf {
		t.Fatalf("wrong choices: %+v", s)
	}
	all := ""
	if err := RememberSelection("owner/one", true, &all, &all); err != nil {
		t.Fatal(err)
	}
	s, err = LoadSelections()
	if err != nil || s.Repos["owner/one"].Branch == nil || *s.Repos["owner/one"].Branch != "" || s.Repos["owner/one"].Workflow == nil || *s.Repos["owner/one"].Workflow != "" {
		t.Fatalf("all not preserved: %+v %v", s, err)
	}
	info, err := os.Stat(filepath.Join(dir, "selections.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file permissions: %v %v", info, err)
	}
	if err = ForgetSelectedRepo("OWNER/ONE"); err != nil {
		t.Fatal(err)
	}
	s, _ = LoadSelections()
	if s.LastRepo != "" || s.Repos["owner/one"].Workflow == nil {
		t.Fatal("forget must clear last repo, not its preferences")
	}
}

func TestSelectionsCorruptFileIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHAPK_CONFIG_DIR", dir)
	path := filepath.Join(dir, "selections.json")
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RememberSelection("one/repo", true, nil, nil); err == nil {
		t.Fatal("corrupt state silently overwritten")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "not json" {
		t.Fatal("corrupt file changed")
	}
}
