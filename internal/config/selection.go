package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Selection remembers UI choices separately from user configuration. Pointer
// fields distinguish an explicit "all" selection from an unset preference.
type Selection struct {
	Branch   *string `json:"branch,omitempty"`
	Workflow *string `json:"workflow,omitempty"`
}
type Selections struct {
	LastRepo string               `json:"last_repo,omitempty"`
	Repos    map[string]Selection `json:"repos,omitempty"`
}

var selectionMu sync.Mutex

func selectionPath() string { return filepath.Join(Dir(), "selections.json") }

func loadSelections() (Selections, error) {
	s := Selections{Repos: map[string]Selection{}}
	data, err := os.ReadFile(selectionPath())
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(data, &s); err != nil {
		return Selections{}, fmt.Errorf("read selections: %w", err)
	}
	if s.Repos == nil {
		s.Repos = map[string]Selection{}
	}
	return s, nil
}
func LoadSelections() (Selections, error) {
	selectionMu.Lock()
	defer selectionMu.Unlock()
	return loadSelections()
}

// RememberSelection updates only the fields the user explicitly selected.
// A nil branch/workflow leaves the existing choice untouched.
func RememberSelection(repo string, last bool, branch, workflow *string) error {
	if !validRepoHint(repo) {
		return fmt.Errorf("invalid repository")
	}
	selectionMu.Lock()
	defer selectionMu.Unlock()
	s, err := loadSelections()
	if err != nil {
		return err
	}
	if last {
		s.LastRepo = repo
	}
	if branch != nil || workflow != nil {
		k := strings.ToLower(repo)
		v := s.Repos[k]
		if branch != nil {
			v.Branch = branch
		}
		if workflow != nil {
			v.Workflow = workflow
		}
		s.Repos[k] = v
	}
	return writeSelections(s)
}

// ForgetSelectedRepo prevents removing a recent entry from reopening it next time.
func ForgetSelectedRepo(repo string) error {
	selectionMu.Lock()
	defer selectionMu.Unlock()
	s, err := loadSelections()
	if err != nil {
		return err
	}
	if !strings.EqualFold(s.LastRepo, repo) {
		return nil
	}
	s.LastRepo = ""
	return writeSelections(s)
}

func writeSelections(s Selections) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(Dir(), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(Dir(), ".selections-*.tmp")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(path, selectionPath())
}

// ClearRememberedFilter lets an explicit Settings edit take effect on the
// next launch instead of being shadowed by an older picker selection.
func ClearRememberedFilter(repo, key string) error {
	if !validRepoHint(repo) {
		return fmt.Errorf("invalid repository")
	}
	if key != "branch" && key != "workflow" {
		return fmt.Errorf("invalid filter")
	}
	selectionMu.Lock()
	defer selectionMu.Unlock()
	s, err := loadSelections()
	if err != nil {
		return err
	}
	r := strings.ToLower(repo)
	v, ok := s.Repos[r]
	if !ok {
		return nil
	}
	if key == "branch" {
		v.Branch = nil
	} else {
		v.Workflow = nil
	}
	if v.Branch == nil && v.Workflow == nil {
		delete(s.Repos, r)
	} else {
		s.Repos[r] = v
	}
	return writeSelections(s)
}
