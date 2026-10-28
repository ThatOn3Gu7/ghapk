package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/ghapi"
	"github.com/ghapk/ghapk/internal/tui/components"
)

func runStartupCommand(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("startup didn't return fetch command")
	}
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				run(sub)
			}
		case UserReposMsg:
			next, _ := m.Update(msg)
			m = next.(Model)
		}
	}
	run(cmd)
	return m
}

func TestFreshAuthExecutesRepositoryFetch(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
printf '%s' '[{"full_name":"alice/private","private":true},{"full_name":"friend/shared"}][{"full_name":"team/app","private":true}]'
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	s := config.Defaults()
	s.Repo = ""
	m := New(Options{Settings: s})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m = mm.(Model)
	mm, cmd := m.Update(AuthCheckedMsg{Version: "2.23.0", User: "alice"})
	m = mm.(Model)
	if m.modal != ModalPickRepo || !m.repoPicker.loading {
		t.Fatal("fresh start didn't open loading picker")
	}
	m = runStartupCommand(t, m, cmd)
	if m.repoPicker.loading || !m.userReposLoaded || len(m.cachedUserRepos) != 3 {
		t.Fatalf("discovery did not finish: %+v", m.repoPicker)
	}
	view := components.StripANSI(m.View())
	for _, want := range []string{"alice/private", "friend/shared", "team/app"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %s: %s", want, view)
		}
	}
	if strings.Contains(view, "↑/↓ select") {
		t.Fatal("removed instruction still rendered")
	}
	m = pressText(m, "team app")
	if len(m.repoPicker.list.Items) != 1 || m.repoPicker.list.Items[0].ID != "team/app" {
		t.Fatal("multiword filter failed")
	}
	m = pressEnter(m)
	if m.settings.Repo != "team/app" {
		t.Fatal("selection failed")
	}
}

func TestRepoDiscoveryCacheLoadingErrorAndStaleResults(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := newTestModel()
	cmd := m.openRepoPicker()
	if cmd == nil || !m.userReposLoading {
		t.Fatal("missing fetch")
	}
	seq := m.userReposSeq
	mm, _ := m.Update(keyOf("esc"))
	m = mm.(Model)
	cmd = m.openRepoPicker()
	if cmd == nil || m.userReposSeq != seq || !m.repoPicker.loading {
		t.Fatal("reopening lost pending fetch or started duplicate")
	}
	mm, _ = m.Update(UserReposMsg{Seq: seq, Err: errors.New("network unavailable")})
	m = mm.(Model)
	if m.userReposLoaded || m.userReposLoading || !strings.Contains(components.StripANSI(m.View()), "Load failed") {
		t.Fatal("failure not shown")
	}
	_, status := m.repoPickerBounds()
	mm, cmd = m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: status.x, Y: status.y})
	m = mm.(Model)
	if cmd == nil || !m.userReposLoading || m.userReposSeq == seq {
		t.Fatal("tap retry didn't start fetch")
	}
	mm, _ = m.Update(UserReposMsg{Seq: seq, List: []ghapi.Repo{{FullName: "stale/old"}}})
	m = mm.(Model)
	if !m.userReposLoading || len(m.cachedUserRepos) != 0 {
		t.Fatal("stale result accepted")
	}
	mm, _ = m.Update(UserReposMsg{Seq: m.userReposSeq, List: []ghapi.Repo{}})
	m = mm.(Model)
	if !m.userReposLoaded || m.repoPicker.loading {
		t.Fatal("empty response not cached")
	}
	mm, _ = m.Update(keyOf("esc"))
	m = mm.(Model)
	if cmd = m.openRepoPicker(); cmd != nil || m.repoPicker.loading {
		t.Fatal("empty cache refetched")
	}
}

func TestRepoPickerPhoneTouchLayoutAndSwipe(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	for _, sz := range [][2]int{{32, 18}, {40, 24}, {64, 18}, {80, 26}, {120, 40}} {
		t.Run(fmt.Sprint(sz), func(t *testing.T) {
			t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
			m := buildModel(t, sz[0], sz[1])
			m.cachedUserRepos = nil
			for i := 0; i < 24; i++ {
				m.cachedUserRepos = append(m.cachedUserRepos, ghapi.Repo{FullName: fmt.Sprintf("team/repo%02d", i)})
			}
			m.userReposLoaded = true
			m.openRepoPicker()
			bounds, _ := m.repoPickerBounds()
			view := m.View()
			lines := strings.Split(view, "\n")
			if len(lines) != sz[1] || lipgloss.Width(view) > sz[0] {
				t.Fatalf("wrong dimensions: %d x %d", lipgloss.Width(view), len(lines))
			}
			if !strings.Contains(components.StripANSI(lines[bounds.y+1]), "team/repo01") {
				t.Fatalf("bounds not matching row bounds=%+v box=%q screen=%q", bounds, components.StripANSI(m.modalBox()), components.StripANSI(view))
			}
			if !strings.Contains(view, "┃") {
				t.Fatal("missing scroll indicator")
			}
			x, y := bounds.x+2, bounds.y+1
			mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y})
			m = mm.(Model)
			if cmd != nil || m.modal != ModalPickRepo {
				t.Fatal("selected before release")
			}
			mm, cmd = m.Update(tea.MouseMsg{Type: tea.MouseRelease, Action: tea.MouseActionRelease, X: x, Y: y})
			m = mm.(Model)
			if cmd == nil || m.settings.Repo != "team/repo01" || m.modal != ModalNone {
				t.Fatalf("tap chose %s", m.settings.Repo)
			}
			m.openRepoPicker()
			bounds, _ = m.repoPickerBounds()
			x, y = bounds.x+2, bounds.y+3
			mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: y})
			m = mm.(Model)
			mm, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion, X: x, Y: y - 2})
			m = mm.(Model)
			mm, cmd = m.Update(tea.MouseMsg{Type: tea.MouseRelease, Action: tea.MouseActionRelease, X: x, Y: y - 2})
			m = mm.(Model)
			if cmd != nil || m.modal != ModalPickRepo || m.repoPicker.list.Cursor < 2 {
				t.Fatal("swipe selected rather than scrolling")
			}
		})
	}
}

func TestRepoPickerRealTeaTouch(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nprintf '%s' '{}'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	m := buildModel(t, 40, 24)
	m.cachedUserRepos = []ghapi.Repo{{FullName: "team/touch"}}
	m.userReposLoaded = true
	m.openRepoPicker()
	b, _ := m.repoPickerBounds()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	var out syncBuf
	updates := make(chan Model, 100)
	p := tea.NewProgram(touchHarness{model: m, updates: updates}, tea.WithInput(input), tea.WithOutput(&out), tea.WithMouseCellMotion(), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	defer p.Kill()
	p.Send(tea.WindowSizeMsg{Width: 40, Height: 24})
	if _, err := fmt.Fprintf(writer, "\x1b[<0;%d;%dM\x1b[<0;%d;%dm", b.x+3, b.y+1, b.x+3, b.y+1); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case state := <-updates:
			if state.settings.Repo == "team/touch" && state.modal == ModalNone {
				p.Quit()
				return
			}
		case err := <-done:
			t.Fatalf("program exited: %v", err)
		case <-timer.C:
			t.Fatalf("timed out: %s", components.StripANSI(out.String()))
		}
	}
}

func TestRepoPickerFiltersAndRejectsStaleSearch(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := newTestModel()
	m.cachedUserRepos = []ghapi.Repo{{FullName: "team/mobile-app", Private: true}, {FullName: "team/other"}}
	m.userReposLoaded = true
	m.openRepoPicker()
	m = pressText(m, "mobile team")
	if len(m.repoPicker.list.Items) != 1 || m.repoPicker.list.Items[0].ID != "team/mobile-app" {
		t.Fatal("multi-token filter failed")
	}
	m.repoPicker.input.SetValue("")
	m.repoPicker.lastQ = ""
	m.repoPicker.hits = nil
	m.repoPicker.rebuild(&m)
	m = pressText(m, "hello")
	oldSeq := m.searchSeq
	mm, _ := m.Update(SearchResultsMsg{Seq: oldSeq, Query: "hello", Hits: []ghapi.SearchHit{{FullName: "hello/old"}}})
	m = mm.(Model)
	if len(m.repoPicker.hits) != 1 {
		t.Fatal("search hit missing")
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = mm.(Model)
	if len(m.repoPicker.hits) != 0 {
		t.Fatal("old search hits persisted during input")
	}
	mm, _ = m.Update(SearchResultsMsg{Seq: oldSeq, Query: "hello", Hits: []ghapi.SearchHit{{FullName: "hello/old"}}})
	m = mm.(Model)
	if len(m.repoPicker.hits) != 0 {
		t.Fatal("stale results accepted")
	}
	m.repoPicker.input.SetValue("")
	m.repoPicker.lastQ = ""
	m.repoPicker.rebuild(&m)
	m = pressText(m, "someone/custom")
	m = pressEnter(m)
	if m.settings.Repo != "someone/custom" {
		t.Fatal("manual owner/repo no longer works")
	}
}

func TestRepoPickerAccountSwitchClearsOldData(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := newTestModel()
	m.cachedUserRepos = []ghapi.Repo{{FullName: "old/private", Private: true}}
	m.userReposLoaded = true
	m.openRepoPicker()
	mm, cmd := m.Update(AuthCheckedMsg{Version: "2.23", User: "another"})
	m = mm.(Model)
	if cmd == nil || len(m.cachedUserRepos) != 0 || !m.userReposLoading || m.repoPicker == nil || strings.Contains(m.View(), "old/private") {
		t.Fatal("old account's repositories remained visible")
	}
}

func TestRepoPickerFooterHintsAndTouch(t *testing.T) {
	for _, sz := range [][2]int{{32, 18}, {40, 24}, {80, 26}, {120, 40}} {
		t.Run(fmt.Sprint(sz), func(t *testing.T) {
			t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
			m := buildModel(t, sz[0], sz[1])
			m.cachedUserRepos = []ghapi.Repo{{FullName: "team/selected"}}
			m.userReposLoaded = true
			m.openRepoPicker()
			_, status := m.repoPickerBounds()
			lines := strings.Split(components.StripANSI(m.View()), "\n")
			if len(lines) != sz[1] || !strings.Contains(lines[status.y+1], "↵ choose · Esc cancel") {
				t.Fatalf("footer missing or outside screen: %q", lines[status.y+1])
			}
			// The separator does nothing, and choosing activates the selected repository.
			sepX := status.x + components.VisibleWidth(repoChooseHint)
			mm, cmd := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: sepX, Y: status.y + 1})
			m = mm.(Model)
			if cmd != nil || m.modal != ModalPickRepo {
				t.Fatal("separator activated a hint")
			}
			mm, cmd = m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: status.x, Y: status.y + 1})
			m = mm.(Model)
			if cmd == nil || m.modal != ModalNone || m.settings.Repo != "team/selected" {
				t.Fatal("choose hint did not select repo")
			}
			m.openRepoPicker()
			_, status = m.repoPickerBounds()
			cancelX := status.x + components.VisibleWidth(repoChooseHint+repoHintSeparator)
			mm, cmd = m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: cancelX, Y: status.y + 1})
			m = mm.(Model)
			if cmd != nil || m.modal != ModalNone || m.settings.Repo != "team/selected" {
				t.Fatal("cancel hint did not close picker")
			}
		})
	}
}

func TestRepoPickerFooterVisibleDuringLoadingAndError(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	m := buildModel(t, 32, 18)
	m.openRepoPicker()
	for _, state := range []string{"loading", "error"} {
		if state == "error" {
			next, _ := m.Update(UserReposMsg{Seq: m.userReposSeq, Err: errors.New("offline")})
			m = next.(Model)
		}
		view := components.StripANSI(m.View())
		if !strings.Contains(view, "↵ choose · Esc cancel") {
			t.Fatalf("footer missing during %s", state)
		}
		if state == "error" && !strings.Contains(view, "Load failed") {
			t.Fatal("error disappeared")
		}
	}
}
