package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ---- install history (history.json, same layout as the shell script) -------

// Entry is what ghapk remembers about the last install for one repository.
type Entry struct {
	Installs int    `json:"installs"`
	Run      int64  `json:"run"`
	SHA      string `json:"sha"`
	Artifact string `json:"artifact"`
	Digest   string `json:"digest"`
	VCode    string `json:"vcode"`
	Pkg      string `json:"pkg"`
	When     int64  `json:"when"`
	Sig      string `json:"sig"`
}

// HistoryFile is where the history lives.
func HistoryFile() string {
	if p := os.Getenv("GHAPK_HISTORY"); p != "" {
		return p
	}
	return filepath.Join(ConfigDir(), "history.json")
}

var histMu sync.Mutex

type histDoc struct {
	Version int              `json:"version"`
	Repos   map[string]Entry `json:"repos"`
}

func loadHistory() histDoc {
	d := histDoc{Version: 1, Repos: map[string]Entry{}}
	b, err := os.ReadFile(HistoryFile())
	if err != nil {
		return d
	}
	var in histDoc
	if json.Unmarshal(b, &in) == nil && in.Repos != nil {
		d.Repos = in.Repos
	}
	return d
}

// History returns the entry for a repo, ok=false when there is none.
func History(repo string) (Entry, bool) {
	histMu.Lock()
	defer histMu.Unlock()
	e, ok := loadHistory().Repos[repo]
	return e, ok
}

// AllHistory returns every repo's entry.
func AllHistory() map[string]Entry {
	histMu.Lock()
	defer histMu.Unlock()
	return loadHistory().Repos
}

// SaveHistory merges e into the repo's entry. The file is written one repo per
// line so the original shell script can still read it.
func SaveHistory(repo string, e Entry) error {
	histMu.Lock()
	defer histMu.Unlock()
	d := loadHistory()
	d.Repos[repo] = e
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
	p := HistoryFile()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ---- TUI state (recent repos) ----------------------------------------------

// State is the small amount of UI state kept between sessions.
type State struct {
	LastRepo string   `json:"last_repo"`
	Recent   []string `json:"recent"`
}

func stateFile() string { return filepath.Join(ConfigDir(), "state.json") }

// LoadState reads state.json; a missing or broken file is an empty state.
func LoadState() State {
	var s State
	if b, err := os.ReadFile(stateFile()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// Touch records repo as the most recently used and saves the state.
func (s *State) Touch(repo string) {
	if repo == "" {
		return
	}
	s.LastRepo = repo
	out := []string{repo}
	for _, r := range s.Recent {
		if r != repo && len(out) < 12 {
			out = append(out, r)
		}
	}
	s.Recent = out
	_ = s.Save()
}

// Save writes state.json atomically.
func (s *State) Save() error {
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	tmp := stateFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return os.Rename(tmp, stateFile())
}
