package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// A binding maps one configurable physical key to the existing internal
// action key. Aliases have separate names, so each can be changed independently.
type bindingDef struct{ name, action, initial string }

func bind(name, key string) bindingDef { return bindingDef{name, key, key} }

var dashboardBindings = []bindingDef{
	bind("quit", "q"), bind("suspend", "ctrl+z"), bind("help", "?"), bind("doctor", "D"),
	bind("repo_picker", "R"), bind("branch_picker", "B"), bind("workflow_picker", "W"),
	bind("theme", "t"), bind("watch", "w"), bind("refresh", "r"),
	bind("next_panel", "tab"), bind("previous_panel", "shift+tab"),
	bind("first", "g"), bind("last", "G"), bind("down", "down"), bind("down_alternate", "j"),
	bind("up", "up"), bind("up_alternate", "k"), bind("page_down", "pgdown"), bind("page_down_alternate", "ctrl+d"),
	bind("page_up", "pgup"), bind("page_up_alternate", "ctrl+u"), bind("filter", "/"), bind("select", "enter"),
	bind("back", "esc"), bind("left", "left"), bind("left_alternate", "h"), bind("right", "right"), bind("right_alternate", "l"), bind("preview", "v"),
	bind("download", "d"), bind("install", "i"), bind("open_browser", "o"), bind("open_folder", "O"),
	bind("favorite_repo", "f"), bind("edit_repo", "e"), bind("remove_repo", "x"),
	bind("tab_repos", "1"), bind("tab_flows", "2"), bind("tab_builds", "3"), bind("tab_downloads", "4"),
	bind("tab_settings", "5"), bind("tab_device", "6"), bind("tab_log", "7"),
}

var keySections = map[string][]bindingDef{
	"dashboard": dashboardBindings,
	"text_editing": {
		bind("character_forward", "right"), bind("character_forward_alternate", "ctrl+f"), bind("character_backward", "left"), bind("character_backward_alternate", "ctrl+b"),
		bind("word_forward", "alt+right"), bind("word_forward_ctrl", "ctrl+right"), bind("word_forward_alternate", "alt+f"),
		bind("word_backward", "alt+left"), bind("word_backward_ctrl", "ctrl+left"), bind("word_backward_alternate", "alt+b"),
		bind("delete_word_backward", "alt+backspace"), bind("delete_word_backward_alternate", "ctrl+w"), bind("delete_word_forward", "alt+delete"), bind("delete_word_forward_alternate", "alt+d"),
		bind("delete_after_cursor", "ctrl+k"), bind("delete_before_cursor", "ctrl+u"), bind("backspace", "backspace"), bind("backspace_alternate", "ctrl+h"),
		bind("delete", "delete"), bind("delete_alternate", "ctrl+d"), bind("line_start", "home"), bind("line_start_alternate", "ctrl+a"), bind("line_end", "end"), bind("line_end_alternate", "ctrl+e"),
		bind("paste", "ctrl+v"), bind("accept_suggestion", "tab"), bind("next_suggestion", "down"), bind("next_suggestion_alternate", "ctrl+n"), bind("previous_suggestion", "up"), bind("previous_suggestion_alternate", "ctrl+p"),
	},
	// In dashboard subcontexts, these replace the dashboard actions with the
	// same internal key; unrelated dashboard bindings remain available.
	"downloads":        {bind("saved", "s"), bind("cancel_job", "c"), bind("remove_job_or_apk", "x"), bind("remove_finished", "X"), bind("install_saved", "i"), bind("open_detail", "enter"), bind("back", "esc"), bind("down", "down"), bind("down_alternate", "j"), bind("up", "up"), bind("up_alternate", "k"), bind("page_down", "pgdown"), bind("page_down_alternate", "ctrl+d"), bind("page_up", "pgup"), bind("page_up_alternate", "ctrl+u")},
	"settings":         {bind("scope", "s"), bind("reset", "d"), bind("change", "enter"), bind("change_space", " "), bind("next_value", "right"), bind("next_value_alternate", "l"), bind("previous_value", "left"), bind("previous_value_alternate", "h"), bind("down", "down"), bind("down_alternate", "j"), bind("up", "up"), bind("up_alternate", "k"), bind("page_down", "pgdown"), bind("page_down_alternate", "ctrl+d"), bind("page_up", "pgup"), bind("page_up_alternate", "ctrl+u"), bind("first", "g"), bind("last", "G")},
	"device":           {bind("recheck", "r"), bind("down", "down"), bind("down_alternate", "j"), bind("up", "up"), bind("up_alternate", "k"), bind("page_down", "pgdown"), bind("page_down_alternate", "ctrl+d"), bind("page_up", "pgup"), bind("page_up_alternate", "ctrl+u"), bind("first", "g"), bind("last", "G")},
	"log":              {bind("follow", "f"), bind("copy", "y"), bind("down", "down"), bind("down_alternate", "j"), bind("up", "up"), bind("up_alternate", "k"), bind("page_down", "pgdown"), bind("page_down_alternate", "ctrl+d"), bind("page_up", "pgup"), bind("page_up_alternate", "ctrl+u"), bind("first", "g"), bind("last", "G")},
	"workflow_preview": {bind("back", "esc"), bind("left", "left"), bind("left_alternate", "h"), bind("next_panel", "tab"), bind("previous_panel", "shift+tab"), bind("down", "down"), bind("down_alternate", "j"), bind("up", "up"), bind("up_alternate", "k"), bind("page_down", "pgdown"), bind("page_down_alternate", "ctrl+d"), bind("page_up", "pgup"), bind("page_up_alternate", "ctrl+u"), bind("first", "g"), bind("last", "G")},
	"setup":            {bind("quit", "q"), bind("quit_alternate", "esc"), bind("retry", "r"), bind("retry_alternate", "enter")},
	"help":             {bind("wrap", "W"), bind("wrap_alternate", "w"), bind("close", "esc"), bind("close_alternate", "q"), bind("close_help_key", "?"), bind("down", "down"), bind("down_alternate", "j"), bind("up", "up"), bind("up_alternate", "k"), bind("page_down", "pgdown"), bind("page_down_alternate", "ctrl+d"), bind("page_up", "pgup"), bind("page_up_alternate", "ctrl+u")},
	"doctor":           {bind("close", "esc"), bind("close_alternate", "q"), bind("close_doctor_key", "D"), bind("refresh", "r"), bind("down", "down"), bind("down_alternate", "j"), bind("up", "up"), bind("up_alternate", "k")},
	"repo_picker":      pickerBindings(true), "filter_picker": pickerBindings(false),
	"editor":  {bind("cancel", "esc"), bind("save", "enter")},
	"filter":  {bind("cancel", "esc"), bind("keep", "enter"), bind("backspace", "backspace"), bind("delete", "delete"), bind("up", "up"), bind("down", "down")},
	"confirm": {bind("cancel", "esc"), bind("cancel_alternate", "q"), bind("cancel_no", "n"), bind("accept", "enter"), bind("accept_space", " "), bind("accept_yes", "y"), bind("previous", "left"), bind("previous_up", "up"), bind("previous_k", "k"), bind("previous_h", "h"), bind("previous_tab", "shift+tab"), bind("next", "right"), bind("next_down", "down"), bind("next_j", "j"), bind("next_l", "l"), bind("next_tab", "tab"), bind("page_down", "pgdown"), bind("page_up", "pgup"), bind("option_1", "1"), bind("option_2", "2"), bind("option_3", "3"), bind("option_4", "4"), bind("option_5", "5"), bind("option_6", "6"), bind("option_7", "7"), bind("option_8", "8"), bind("option_9", "9")},
	"plan":    {bind("cancel", "esc"), bind("cancel_alternate", "q"), bind("up", "up"), bind("up_alternate", "k"), bind("down", "down"), bind("down_alternate", "j"), bind("next", "tab"), bind("next_right", "right"), bind("next_alternate", "l"), bind("previous", "shift+tab"), bind("previous_left", "left"), bind("previous_alternate", "h"), bind("select", "enter"), bind("select_space", " ")},
	"task":    {bind("close", "esc"), bind("close_alternate", "q"), bind("previous", "left"), bind("previous_alternate", "h"), bind("previous_tab", "shift+tab"), bind("next", "right"), bind("next_alternate", "l"), bind("next_tab", "tab"), bind("select", "enter"), bind("select_space", " "), bind("install", "i")},
}

func pickerBindings(repo bool) []bindingDef {
	b := []bindingDef{bind("cancel", "esc"), bind("choose", "enter"), bind("down", "down"), bind("down_alternate", "ctrl+n"), bind("down_tab", "tab"), bind("up", "up"), bind("up_alternate", "ctrl+p"), bind("up_tab", "shift+tab"), bind("page_down", "pgdown"), bind("page_down_alternate", "ctrl+d"), bind("page_up", "pgup"), bind("page_up_alternate", "ctrl+u")}
	if repo {
		b = append(b, bind("retry", "ctrl+r"))
	}
	return b
}

var dashboardContexts = map[string]bool{"dashboard": true, "downloads": true, "settings": true, "device": true, "log": true, "workflow_preview": true}
var textContexts = map[string]bool{"repo_picker": true, "filter_picker": true, "editor": true, "filter": true, "text_editing": true}

type KeyBindings struct {
	sections map[string]map[string]string
	reverse  map[string]map[string]string
	actions  map[string]map[string]string
}

// KeybindingPath uses the requested twinpane config location. GHAPK_CONFIG_DIR
// remains an explicit override, useful for isolated profiles and tests.
func KeybindingPath() string {
	if d := strings.TrimSpace(os.Getenv("GHAPK_CONFIG_DIR")); d != "" {
		return filepath.Join(d, "keybinding.json")
	}
	if d := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); d != "" {
		return filepath.Join(d, "twinpane", "keybinding.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "keybinding.json")
	}
	return filepath.Join(home, ".config", "twinpane", "keybinding.json")
}

func defaultKeyDocument() map[string]map[string]string {
	doc := map[string]map[string]string{}
	for section, defs := range keySections {
		doc[section] = map[string]string{}
		for _, d := range defs {
			v := d.initial
			if v == " " {
				v = "space"
			}
			doc[section][d.name] = v
		}
	}
	return doc
}
func defaultKeyBindings() *KeyBindings {
	k, err := compileKeyBindings(defaultKeyDocument())
	if err != nil {
		panic(err)
	}
	return k
}

// LoadKeyBindings creates an editable template on first launch and never
// overwrites an existing file. Invalid input is rejected before the TUI starts.
func LoadKeyBindings(path string) (*KeyBindings, error) {
	data, err := readKeyFile(path)
	if os.IsNotExist(err) {
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		raw, e := json.MarshalIndent(defaultKeyDocument(), "", "  ")
		if e != nil {
			return nil, e
		}
		raw = append(raw, '\n')
		tmp, e := os.CreateTemp(filepath.Dir(path), ".keys-*.tmp")
		if e != nil {
			return nil, e
		}
		name := tmp.Name()
		defer os.Remove(name)
		if _, e = tmp.Write(raw); e == nil {
			e = tmp.Sync()
		}
		if ce := tmp.Close(); e == nil {
			e = ce
		}
		if e != nil {
			return nil, e
		}
		if e = os.Link(name, path); e != nil && !os.IsExist(e) {
			// Some user-selected config filesystems do not support hard links.
			// O_EXCL still prevents replacing a file created by another process.
			f, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if createErr != nil && !os.IsExist(createErr) {
				return nil, createErr
			}
			if createErr == nil {
				_, createErr = f.Write(raw)
				if closeErr := f.Close(); createErr == nil {
					createErr = closeErr
				}
				if createErr != nil {
					return nil, createErr
				}
			}
		}
		data, err = readKeyFile(path)
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 1024*1024 {
		return nil, fmt.Errorf("file is larger than 1 MiB")
	}
	doc, err := parseKeyDocument(data)
	if err != nil {
		return nil, err
	}
	return compileKeyBindings(doc)
}

func readKeyFile(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("key binding file is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 1024*1024+1))
}

// Decode explicitly so duplicate JSON keys cannot silently override an action.
func parseKeyDocument(data []byte) (map[string]map[string]string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("expected a JSON object")
	}
	doc := defaultKeyDocument()
	seenSections := map[string]bool{}
	for dec.More() {
		t, e := dec.Token()
		if e != nil {
			return nil, e
		}
		section, ok := t.(string)
		if !ok {
			return nil, fmt.Errorf("invalid section")
		}
		if seenSections[section] {
			return nil, fmt.Errorf("duplicate section %q", section)
		}
		seenSections[section] = true
		dst, ok := doc[section]
		if !ok {
			return nil, fmt.Errorf("unknown section %q", section)
		}
		t, e = dec.Token()
		if e != nil || t != json.Delim('{') {
			return nil, fmt.Errorf("section %q must be an object", section)
		}
		seen := map[string]bool{}
		for dec.More() {
			t, e = dec.Token()
			if e != nil {
				return nil, e
			}
			name, ok := t.(string)
			if !ok {
				return nil, fmt.Errorf("invalid binding name in %s", section)
			}
			if seen[name] {
				return nil, fmt.Errorf("duplicate binding %s.%s", section, name)
			}
			seen[name] = true
			if _, ok = dst[name]; !ok {
				return nil, fmt.Errorf("unknown binding %s.%s", section, name)
			}
			var value string
			if e = dec.Decode(&value); e != nil {
				return nil, fmt.Errorf("%s.%s must be one key string", section, name)
			}
			dst[name] = value
		}
		if _, e = dec.Token(); e != nil {
			return nil, e
		}
	}
	if _, err = dec.Token(); err != nil {
		return nil, err
	}
	if _, err = dec.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected data after JSON object")
		}
		return nil, err
	}
	return doc, nil
}

// canonicalPhysical accepts a single key as Bubble Tea actually reports it.
// Ctrl+I/M/[ are normalized to Tab/Enter/Esc: terminals cannot distinguish them.
func canonicalPhysical(s string) (string, error) {
	if s == "space" {
		return " ", nil
	}
	if s == "ctrl+i" {
		return "tab", nil
	}
	if s == "ctrl+m" {
		return "enter", nil
	}
	if s == "ctrl+[" {
		return "esc", nil
	}
	if s == "ctrl+?" {
		return "backspace", nil
	}
	if strings.HasPrefix(s, "shift+") && len(s) == 7 && s[6] >= 'a' && s[6] <= 'z' {
		return strings.ToUpper(s[6:]), nil
	}
	if strings.HasPrefix(s, "alt+") {
		if strings.HasPrefix(strings.TrimPrefix(s, "alt+"), "alt+") {
			return "", fmt.Errorf("invalid key %q", s)
		}
		v, e := canonicalPhysical(strings.TrimPrefix(s, "alt+"))
		if e != nil {
			return "", fmt.Errorf("invalid key %q", s)
		}
		return "alt+" + v, nil
	}
	if utf8.RuneCountInString(s) == 1 {
		r, _ := utf8.DecodeRuneInString(s)
		if !unicode.IsControl(r) && unicode.IsGraphic(r) {
			return s, nil
		}
	}
	for i := -200; i <= 127; i++ {
		n := tea.KeyType(i).String()
		if n != "" && n != "runes" && n == s {
			return s, nil
		}
	}
	return "", fmt.Errorf("invalid key %q (use one key, such as x, enter, ctrl+k, or f2)", s)
}
func keyMessage(name string) tea.KeyMsg {
	alt := strings.HasPrefix(name, "alt+")
	if alt {
		name = strings.TrimPrefix(name, "alt+")
	}
	if name == " " {
		return tea.KeyMsg{Type: tea.KeySpace, Alt: alt}
	}
	if utf8.RuneCountInString(name) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name), Alt: alt}
	}
	for i := -200; i <= 127; i++ {
		if tea.KeyType(i).String() == name {
			return tea.KeyMsg{Type: tea.KeyType(i), Alt: alt}
		}
	}
	return tea.KeyMsg{Type: tea.KeyNull}
}

func compileKeyBindings(doc map[string]map[string]string) (*KeyBindings, error) {
	k := &KeyBindings{sections: doc, reverse: map[string]map[string]string{}, actions: map[string]map[string]string{}}
	names := make([]string, 0, len(keySections))
	for n := range keySections {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, ctx := range names {
		defs := append([]bindingDef(nil), keySections[ctx]...)
		parent := ""
		if dashboardContexts[ctx] && ctx != "dashboard" {
			parent = "dashboard"
		}
		if ctx == "editor" || ctx == "repo_picker" || ctx == "filter_picker" {
			parent = "text_editing"
		}
		if parent != "" {
			over := map[string]bool{}
			for _, d := range defs {
				over[d.action] = true
			}
			for _, d := range keySections[parent] {
				if !over[d.action] {
					defs = append(defs, bindingDef{name: parent + "." + d.name, action: d.action, initial: d.initial})
				}
			}
		}
		rev, acts, owner := map[string]string{}, map[string]string{}, map[string]string{}
		for _, d := range defs {
			section, name := ctx, d.name
			if i := strings.IndexByte(name, '.'); i >= 0 {
				section, name = name[:i], name[i+1:]
			}
			raw := doc[section][name]
			physical, e := canonicalPhysical(raw)
			if e != nil {
				return nil, fmt.Errorf("%s.%s: %w", section, name, e)
			}
			if physical == "ctrl+c" {
				return nil, fmt.Errorf("%s.%s: ctrl+c is reserved for emergency quit", section, name)
			}
			if textContexts[ctx] && utf8.RuneCountInString(physical) == 1 && !strings.HasPrefix(physical, "alt+") {
				return nil, fmt.Errorf("%s.%s: printable keys are reserved for typing", section, name)
			}
			if prev, exists := owner[physical]; exists {
				return nil, fmt.Errorf("%s: key %q conflicts between %s and %s.%s", ctx, raw, prev, section, name)
			}
			owner[physical] = section + "." + name
			rev[physical] = d.action
			acts[d.action] = physical
		}
		k.reverse[ctx] = rev
		k.actions[ctx] = acts
	}
	return k, nil
}

func (m Model) keyContext() string {
	if m.screen == ScreenSetup {
		return "setup"
	}
	switch m.modal {
	case ModalHelp:
		return "help"
	case ModalDoctor:
		return "doctor"
	case ModalPickRepo:
		return "repo_picker"
	case ModalPickBranch, ModalPickWorkflow:
		return "filter_picker"
	case ModalEditRepo:
		return "editor"
	case ModalConfirm:
		return "confirm"
	case ModalTask:
		return "task"
	case ModalPlan:
		return "plan"
	}
	if m.filterMode {
		return "filter"
	}
	if m.focus == PanelSettings && m.settingsUI != nil && m.settingsUI.editing {
		return "editor"
	}
	if m.focus == PanelWorkflows && m.wfYAMLFocused {
		return "workflow_preview"
	}
	switch m.activeTab() {
	case PanelDownloads:
		return "downloads"
	case PanelSettings:
		return "settings"
	case PanelDevice:
		return "device"
	case PanelLog:
		return "log"
	}
	return "dashboard"
}
func (m Model) translateKey(msg tea.KeyMsg) (tea.KeyMsg, bool) {
	if msg.String() == "ctrl+c" {
		return msg, true
	}
	if m.bindings == nil {
		return msg, true
	}
	ctx := m.keyContext()
	if !msg.Paste {
		if action, ok := m.bindings.reverse[ctx][msg.String()]; ok {
			return keyMessage(action), true
		}
	}
	if textContexts[ctx] {
		// An old control key that was reassigned must not retain its old action.
		if _, wasAction := m.bindings.actions[ctx][msg.String()]; wasAction && !msg.Paste {
			return tea.KeyMsg{}, false
		}
		return msg, true
	}
	return tea.KeyMsg{}, false
}
func (m Model) keyName(ctx, action string) string {
	if m.bindings != nil {
		if key, ok := m.bindings.actions[ctx][action]; ok {
			if key == " " {
				return "space"
			}
			if key == "alt+ " {
				return "alt+space"
			}
			return key
		}
	}
	if action == " " {
		return "space"
	}
	return action
}
