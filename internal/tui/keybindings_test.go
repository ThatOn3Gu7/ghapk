package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ThatOn3Gu7/ghapk/internal/activity"
	"github.com/ThatOn3Gu7/ghapk/internal/tui/components"
)

func keyConfig(t *testing.T, edit func(map[string]map[string]string)) *KeyBindings {
	t.Helper()
	d := defaultKeyDocument()
	edit(d)
	k, err := compileKeyBindings(d)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
func updateKey(t *testing.T, m Model, k string) Model {
	t.Helper()
	out, _ := m.Update(keyMessage(k))
	switch v := out.(type) {
	case Model:
		return v
	case *Model:
		return *v
	}
	t.Fatal("unexpected model")
	return m
}

func TestDefaultKeyCatalogRoundTrips(t *testing.T) {
	k := defaultKeyBindings()
	for ctx, byKey := range k.reverse {
		for physical, action := range byKey {
			msg := keyMessage(physical)
			if got := msg.String(); got != physical {
				t.Errorf("%s %s -> %q", ctx, physical, got)
			}
			if got := k.actions[ctx][action]; got != physical {
				t.Errorf("%s %s -> %s != %s", ctx, action, got, physical)
			}
		}
	}
}
func TestKeybindingFileCreationRestartAndIsolation(t *testing.T) {
	t.Setenv("GHAPK_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := KeybindingPath()
	if !strings.HasSuffix(path, filepath.Join("twinpane", "keybinding.json")) {
		t.Fatalf("path %s", path)
	}
	first, err := LoadKeyBindings(path)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0077 != 0 {
		t.Fatalf("not private: %o", fi.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]map[string]string
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["dashboard"]["repo_picker"] = "z"
	raw, _ = json.MarshalIndent(doc, "", "  ")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if first.actions["dashboard"]["R"] != "R" {
		t.Fatal("running session changed before restart")
	}
	second, err := LoadKeyBindings(path)
	if err != nil {
		t.Fatal(err)
	}
	if second.actions["dashboard"]["R"] != "z" {
		t.Fatal("restart did not load edited key")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("existing config overwritten")
	}
	t.Setenv("GHAPK_CONFIG_DIR", t.TempDir())
	if !strings.HasSuffix(KeybindingPath(), "keybinding.json") || KeybindingPath() == path {
		t.Fatal("config override ignored")
	}
}
func TestInvalidKeybindingFilesFailWithoutOverwrite(t *testing.T) {
	for _, tc := range []struct{ label, document, want string }{
		{"invalid json", `{"dashboard":`, "must be an object"},
		{"duplicate section", `{"dashboard":{},"dashboard":{}}`, "duplicate section"},
		{"duplicate entry", `{"dashboard":{"quit":"a","quit":"b"}}`, "duplicate binding"},
		{"unknown section", `{"mine":{}}`, "unknown section"},
		{"new action", `{"dashboard":{"launch_rocket":"z"}}`, "unknown binding"},
		{"array", `{"dashboard":{"quit":["x","y"]}}`, "one key string"},
		{"multi key", `{"dashboard":{"quit":"xy"}}`, "one key"},
		{"invalid named", `{"dashboard":{"quit":"shift+1"}}`, "one key"},
		{"bad alt", `{"dashboard":{"quit":"alt+alt+x"}}`, "invalid key"},
		{"empty", `{"dashboard":{"quit":""}}`, "invalid key"},
		{"duplicate physical", `{"dashboard":{"quit":"r"}}`, "conflicts"},
		{"alias duplicate", `{"dashboard":{"quit":"ctrl+i"}}`, "conflicts"},
		{"reserved", `{"dashboard":{"quit":"ctrl+c"}}`, "reserved"},
		{"typing", `{"repo_picker":{"choose":"z"}}`, "reserved for typing"},
		{"trailing", `{} {}`, "unexpected data"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "keys.json")
			if err := os.WriteFile(path, []byte(tc.document), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadKeyBindings(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
			after, _ := os.ReadFile(path)
			if string(after) != tc.document {
				t.Fatal("invalid file overwritten")
			}
		})
	}
}
func TestContextConflictsAndSwaps(t *testing.T) {
	d := defaultKeyDocument()
	d["dashboard"]["quit"] = "z"
	d["help"]["close"] = "z" // disjoint contexts may reuse a key
	if _, err := compileKeyBindings(d); err != nil {
		t.Fatal(err)
	}
	d = defaultKeyDocument()
	d["dashboard"]["quit"] = "t"
	d["dashboard"]["theme"] = "q" // valid full swap
	if _, err := compileKeyBindings(d); err != nil {
		t.Fatal(err)
	}
	d = defaultKeyDocument()
	d["dashboard"]["quit"] = "s" // conflicts with downloads.saved
	if _, err := compileKeyBindings(d); err == nil || !strings.Contains(err.Error(), "downloads") {
		t.Fatalf("missing cross-section conflict: %v", err)
	}
}
func TestRemappedControlsTextInputAndTouch(t *testing.T) {
	keys := keyConfig(t, func(d map[string]map[string]string) {
		d["dashboard"]["repo_picker"] = "z"
		d["dashboard"]["tab_settings"] = "p"
		d["settings"]["reset"] = "u"
		d["repo_picker"]["choose"] = "ctrl+o"
		d["repo_picker"]["cancel"] = "ctrl+x"
		d["filter"]["cancel"] = "ctrl+x"
		d["help"]["wrap"] = "z"
		d["confirm"]["accept"] = "f2"
	})
	m := buildModel(t, 80, 26)
	m.bindings = keys
	m = updateKey(t, m, "R")
	if m.modal != ModalNone {
		t.Fatal("old repo shortcut still active")
	}
	m = updateKey(t, m, "z")
	if m.modal != ModalPickRepo {
		t.Fatal("remapped picker did not open")
	}
	for _, r := range "zrepo" {
		m = updateKey(t, m, string(r))
	}
	if !strings.Contains(m.repoPicker.input.Value(), "zrepo") {
		t.Fatal("normal typing intercepted")
	}
	m = updateKey(t, m, "esc")
	if m.modal != ModalPickRepo {
		t.Fatal("old cancel still active")
	}
	m = updateKey(t, m, "ctrl+x")
	if m.modal != ModalNone {
		t.Fatal("new cancel did not work")
	}
	// The visible status hint continues to dispatch the internal action.
	m.focus = PanelRepos
	sb := m.statusBar()
	_, spans := sb.Layout(m.st)
	for _, span := range spans {
		if sb.Hints[span.Index].Action == "R" {
			out, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: span.Start, Y: m.height - 1})
			m = out.(Model)
			break
		}
	}
	if m.modal != ModalPickRepo {
		t.Fatal("remapped touch hint failed")
	}
	m = updateKey(t, m, "ctrl+x")
	m = updateKey(t, m, "5")
	if m.focus == PanelSettings {
		t.Fatal("old panel key active")
	}
	m = updateKey(t, m, "p")
	if m.focus != PanelSettings {
		t.Fatal("new panel key failed")
	}
	sb = m.statusBar()
	found := false
	for _, h := range sb.Hints {
		if h.Action == "d" {
			found = true
			if h.Key != "u" {
				t.Fatalf("wrong hint: %+v", h)
			}
		}
	}
	if !found {
		t.Fatal("reset hint missing")
	}
	m = updateKey(t, m, "?")
	if m.modal != ModalHelp {
		t.Fatal("help failed")
	}
	if !strings.Contains(m.helpTitle(), "z:wrap") {
		t.Fatal("help title didn't update")
	}
	m = updateKey(t, m, "W")
	if m.helpWrapOff {
		t.Fatal("old help key still active")
	}
	m = updateKey(t, m, "z")
	if !m.helpWrapOff {
		t.Fatal("new help wrap key failed")
	}
	m = updateKey(t, m, "esc")
	m.modal = ModalConfirm
	m.confirm = components.Confirm{Title: "question", Options: []string{"yes", "no"}}
	m.confirmKind = confirmNone
	m = updateKey(t, m, "enter")
	if m.modal != ModalConfirm {
		t.Fatal("old confirmation key still active")
	}
	m = updateKey(t, m, "f2")
	if m.modal != ModalNone {
		t.Fatal("new confirmation key failed")
	}
	m = updateKey(t, m, "ctrl+c")
	if !m.quitAfterJobs && !m.quitting { // tea.Quit command itself is enough when no jobs
		// Verify the emergency key goes directly to the original handler.
		translated, ok := m.translateKey(keyMessage("ctrl+c"))
		if !ok || translated.String() != "ctrl+c" {
			t.Fatal("emergency quit lost")
		}
	}
}
func TestLogRemappedScrollingDoesNotWrite(t *testing.T) {
	store, err := activity.Open(t.TempDir(), activity.DefaultMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	keys := keyConfig(t, func(d map[string]map[string]string) { d["log"]["down_alternate"] = "z"; d["log"]["follow"] = "F" })
	m := buildModel(t, 40, 24)
	m.bindings = keys
	m.activity = store
	m.focus = PanelLog
	for i := 0; i < 50; i++ {
		_ = store.Write("job", fmt.Sprint(i))
	}
	count := len(store.Entries())
	for i := 0; i < 20; i++ {
		m = updateKey(t, m, "z")
	}
	m = updateKey(t, m, "F")
	m = updateKey(t, m, "j")
	if len(store.Entries()) != count {
		t.Fatal("log navigation created feedback entries")
	}
}

func TestTextEditingRemapsWithoutStealingTypedText(t *testing.T) {
	keys := keyConfig(t, func(d map[string]map[string]string) { d["text_editing"]["backspace"] = "ctrl+g" })
	m := buildModel(t, 80, 26)
	m.bindings = keys
	m = updateKey(t, m, "R")
	for _, r := range "hello" {
		m = updateKey(t, m, string(r))
	}
	before := m.repoPicker.input.Value()
	m = updateKey(t, m, "backspace")
	if m.repoPicker.input.Value() != before {
		t.Fatal("old editing key remained active")
	}
	m = updateKey(t, m, "ctrl+g")
	if got := m.repoPicker.input.Value(); got != strings.TrimSuffix(before, "o") {
		t.Fatalf("custom backspace: %q", got)
	}
}

func TestSafetyChoiceRemappingStillRequiresConfirmation(t *testing.T) {
	keys := keyConfig(t, func(d map[string]map[string]string) { d["confirm"]["option_2"] = "f2"; d["confirm"]["accept"] = "f3" })
	m := buildModel(t, 40, 24)
	m.bindings = keys
	m.modal = ModalConfirm
	m.confirmKind = confirmJobSafety
	m.confirm = components.Confirm{Title: "Safety", Options: []string{"Cancel", "Proceed"}, Cursor: 0}
	m = updateKey(t, m, "2")
	if m.confirm.Cursor != 0 {
		t.Fatal("old option shortcut still active")
	}
	m = updateKey(t, m, "f2")
	if m.confirm.Cursor != 1 || m.modal != ModalConfirm {
		t.Fatal("remapped option did not select safely")
	}
	if label := m.questionOptionLabel(1, "Proceed"); !strings.HasPrefix(label, "f2  ") {
		t.Fatalf("misleading choice hint: %q", label)
	}
	m = updateKey(t, m, "enter")
	if m.modal != ModalConfirm {
		t.Fatal("old accept still active")
	}
}

func TestLongCustomizedKeysFitAndKeepTouchTargets(t *testing.T) {
	keys := keyConfig(t, func(d map[string]map[string]string) {
		d["dashboard"]["tab_repos"] = "ctrl+shift+up"
		d["dashboard"]["tab_flows"] = "ctrl+shift+down"
		d["repo_picker"]["choose"] = "ctrl+shift+left"
		d["repo_picker"]["cancel"] = "ctrl+shift+right"
		d["confirm"]["option_1"] = "ctrl+shift+up"
		d["confirm"]["accept"] = "ctrl+shift+left"
		d["confirm"]["cancel"] = "ctrl+shift+right"
	})
	for _, size := range [][2]int{{32, 18}, {40, 24}, {80, 26}} {
		m := buildModel(t, size[0], size[1])
		m.bindings = keys
		for p, r := range m.compactTabRects() {
			if r.x+r.w > m.width {
				t.Fatalf("%v tab %v outside screen %+v", size, p, r)
			}
		}
		m.openRepoPicker()
		lines := strings.Split(m.View(), "\n")
		lineWidths(t, m.width, m.height, lines)
		choose, cancel := m.repoPickerHints()
		if choose == "" || cancel == "" {
			t.Fatal("missing touch hints")
		}
		_, status := m.repoPickerBounds()
		x := status.x + components.VisibleWidth(choose+repoHintSeparator)
		out, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: status.y + 1})
		m = out.(Model)
		if m.modal != ModalNone {
			t.Fatal("custom picker cancel touch target failed")
		}
		m.modal = ModalConfirm
		m.confirmKind = confirmJobSafety
		m.confirm = components.Confirm{Title: "Safety", Body: []string{"Read this"}, Options: []string{"Cancel", "Proceed"}}
		lines = strings.Split(m.View(), "\n")
		lineWidths(t, m.width, m.height, lines)
	}
}

func TestKeySyntaxAndAliases(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"shift+a", "A"}, {"ctrl+i", "tab"}, {"ctrl+m", "enter"}, {"ctrl+[", "esc"}, {"ctrl+?", "backspace"}, {"space", " "}, {"alt+space", "alt+ "}, {"f2", "f2"}, {"alt+right", "alt+right"}, {"é", "é"}} {
		got, err := canonicalPhysical(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("%q = %q (%v), want %q", tc.in, got, err, tc.want)
			continue
		}
		if keyMessage(got).String() != got {
			t.Errorf("%q does not round-trip as a key", got)
		}
	}
}
func TestKeyFileTooLargeOrNotRegular(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadKeyBindings(dir); err == nil || !strings.Contains(err.Error(), "regular") {
		t.Fatalf("directory: %v", err)
	}
	path := filepath.Join(dir, "huge")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", 1024*1024+2)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKeyBindings(path); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("large file: %v", err)
	}
}

func TestRemappedHintsRemainTappable(t *testing.T) {
	keys := keyConfig(t, func(d map[string]map[string]string) {
		d["log"]["follow"] = "F"
		d["help"]["wrap"] = "alt+ctrl+shift+right"
		d["repo_picker"]["choose"] = "ctrl+o"
		d["repo_picker"]["cancel"] = "ctrl+x"
	})
	m := buildModel(t, 32, 18)
	m.bindings = keys
	m.focus = PanelLog
	sb := m.statusBar()
	_, spans := sb.Layout(m.st)
	found := false
	for _, span := range spans {
		if sb.Hints[span.Index].Action == "f" {
			found = true
			if sb.Hints[span.Index].Key != "F" {
				t.Fatal("wrong follow hint")
			}
			out, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: span.Start, Y: m.height - 1})
			m = out.(Model)
			break
		}
	}
	if !found || m.logState.follow {
		t.Fatal("custom follow hint did not respond to touch")
	}
	m = updateKey(t, m, "?")
	if m.modal != ModalHelp {
		t.Fatal("help not open")
	}
	r := m.helpWrapBounds()
	if r.w == 0 {
		t.Fatalf("long help key lost touch hint: %s", m.helpTitle())
	}
	out, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: r.x, Y: r.y})
	m = out.(Model)
	if !m.helpWrapOff {
		t.Fatal("long-key help hint untappable")
	}
	m = updateKey(t, m, "esc")
	m.settings.Repo = "owner/repo"
	m.openRepoPicker()
	choose, cancel := m.repoPickerHints()
	if choose == "" || cancel == "" {
		t.Fatalf("missing picker hints %q %q", choose, cancel)
	}
	_, status := m.repoPickerBounds()
	x := status.x + components.VisibleWidth(choose+repoHintSeparator)
	out, _ = m.Update(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: x, Y: status.y + 1})
	m = out.(Model)
	if m.modal != ModalNone {
		t.Fatal("custom picker cancel hint untappable")
	}
}

func TestCustomHelpWrapsLongLabels(t *testing.T) {
	keys := keyConfig(t, func(d map[string]map[string]string) {
		d["dashboard"]["tab_repos"] = "ctrl+shift+up"
		d["dashboard"]["tab_flows"] = "ctrl+shift+down"
		d["dashboard"]["tab_builds"] = "ctrl+shift+left"
	})
	for _, w := range []int{32, 40, 80, 120} {
		m := buildModel(t, w, 24)
		m.bindings = keys
		m.modal = ModalHelp
		m.helpVP, m.helpLineEntries = m.helpLayout()
		for _, line := range allHelpLines(m) {
			if components.VisibleWidth(line) > m.helpVP.Width {
				t.Fatalf("%d: overflowing help %q", w, line)
			}
		}
	}
}

func TestCustomPickerShowsKeysWhenSpaceAllows(t *testing.T) {
	keys := keyConfig(t, func(d map[string]map[string]string) {
		d["repo_picker"]["choose"] = "ctrl+o"
		d["repo_picker"]["cancel"] = "ctrl+x"
	})
	m := buildModel(t, 80, 26)
	m.bindings = keys
	m.openRepoPicker()
	choose, cancel := m.repoPickerHints()
	if !strings.Contains(choose, "ctrl+o") || !strings.Contains(cancel, "ctrl+x") {
		t.Fatalf("wrong picker hints %q %q", choose, cancel)
	}
	m.settings.Repo = "owner/repo"
	m.repoPicker.input.SetValue("owner/repo")
	m = updateKey(t, m, "ctrl+o")
	if m.modal != ModalNone {
		t.Fatal("custom choose key failed")
	}
}

func TestLongSafetyShortcutsKeepChoicesTouchable(t *testing.T) {
	keys := keyConfig(t, func(d map[string]map[string]string) {
		d["confirm"]["option_1"] = "ctrl+shift+up"
		d["confirm"]["option_2"] = "ctrl+shift+down"
	})
	for _, size := range [][2]int{{32, 18}, {40, 24}, {80, 26}} {
		m := buildModel(t, size[0], size[1])
		m.bindings = keys
		m.modal = ModalConfirm
		m.confirmKind = confirmJobSafety
		m.confirm = components.Confirm{Title: "Safety", Body: []string{"Read this first."}, Options: []string{"Cancel", "Install anyway"}}
		lines := strings.Split(m.modalLayer(), "\n")
		for i, opt := range m.confirm.Options {
			label := m.questionOptionLabel(i, opt)
			found := false
			for y, line := range lines {
				plain := components.StripANSI(line)
				idx := strings.Index(plain, label)
				if idx < 0 {
					continue
				}
				x := components.VisibleWidth(plain[:idx])
				if got := m.jobQuestionHit(x, y); got != i {
					t.Fatalf("%v option %q hit=%d", size, label, got)
				}
				found = true
				break
			}
			if !found {
				t.Fatalf("%v option not visible: %q", size, label)
			}
		}
	}
}
