package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ghapk-tui-tests-")
	if err != nil {
		panic(err)
	}
	for _, key := range []string{"GHAPK_CONFIG_DIR", "GHAPK_REPO", "GHAPK_BRANCH", "GHAPK_WORKFLOW"} {
		_ = os.Unsetenv(key)
	}
	_ = os.Setenv("XDG_CONFIG_HOME", dir)
	_ = os.Setenv("GHAPK_CACHE_DIR", filepath.Join(dir, "cache"))
	_ = os.Setenv("GHAPK_HISTORY", filepath.Join(dir, "history.json"))
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
