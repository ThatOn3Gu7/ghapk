package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// HistoryEntry records identity metadata from the most recent APK installed
// or handed off for a repository, allowing GHAPK to detect package or signing
// key changes even on devices where dumpsys is inaccessible without Shizuku/ADB.
type HistoryEntry struct {
	Installs int    `json:"installs"`
	Run      int64  `json:"run,omitempty"`
	SHA      string `json:"sha,omitempty"`
	Artifact string `json:"artifact,omitempty"`
	Digest   string `json:"digest,omitempty"`
	VCode    int64  `json:"vcode_num,omitempty"`
	VName    string `json:"vname,omitempty"`
	Pkg      string `json:"pkg,omitempty"`
	When     int64  `json:"when,omitempty"`
	Sig      string `json:"sig,omitempty"`
}

var histMu sync.Mutex

type histDoc struct {
	Version int                     `json:"version"`
	Repos   map[string]HistoryEntry `json:"repos"`
}

// HistoryPath returns the path to history.json (honoring GHAPK_HISTORY for tests).
func HistoryPath() string {
	if p := strings.TrimSpace(os.Getenv("GHAPK_HISTORY")); p != "" {
		return p
	}
	return filepath.Join(Dir(), "history.json")
}

func loadHistoryUnlocked() histDoc {
	d := histDoc{Version: 1, Repos: map[string]HistoryEntry{}}
	b, err := os.ReadFile(HistoryPath())
	if err != nil {
		return d
	}
	var in histDoc
	if json.Unmarshal(b, &in) == nil && in.Repos != nil {
		d.Repos = in.Repos
	}
	return d
}

// History returns the stored HistoryEntry for repo, if any.
func History(repo string) (HistoryEntry, bool) {
	if strings.TrimSpace(repo) == "" {
		return HistoryEntry{}, false
	}
	histMu.Lock()
	defer histMu.Unlock()
	e, ok := loadHistoryUnlocked().Repos[repo]
	return e, ok
}

// RecordInstall updates history.json atomically for repo.
func RecordInstall(repo string, update HistoryEntry) error {
	if strings.TrimSpace(repo) == "" {
		return nil
	}
	histMu.Lock()
	defer histMu.Unlock()
	d := loadHistoryUnlocked()
	prev := d.Repos[repo]
	update.Installs = prev.Installs + 1
	if update.When == 0 {
		update.When = time.Now().Unix()
	}
	if update.Pkg == "" {
		update.Pkg = prev.Pkg
	}
	if update.Sig == "" {
		update.Sig = prev.Sig
	}
	d.Repos[repo] = update

	names := make([]string, 0, len(d.Repos))
	for k := range d.Repos {
		names = append(names, k)
	}
	sort.Strings(names)
	var sb strings.Builder
	sb.WriteString("{\n  \"version\": 1,\n  \"repos\": {\n")
	for i, n := range names {
		kb, _ := json.Marshal(n)
		vb, _ := json.Marshal(d.Repos[n])
		sb.WriteString("    " + string(kb) + ": " + string(vb))
		if i < len(names)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("  }\n}\n")

	p := HistoryPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
