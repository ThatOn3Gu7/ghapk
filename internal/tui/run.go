package tui

import (
	"fmt"
	"strings"

	"github.com/ThatOn3Gu7/ghapk/internal/activity"
	"github.com/ThatOn3Gu7/ghapk/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// Run launches the interactive interface in the alternate screen buffer.
// When the user quits, the terminal is restored untouched — the shell is
// exactly where they left it, the way lazygit exits.
func Run(opts Options) error {
	path := KeybindingPath()
	keys, err := LoadKeyBindings(path)
	if err != nil {
		// A typo in the key file must not lock anyone out of the tool. Start
		// with the built-in keys, leave the file exactly as it is, and say so.
		keys = defaultKeyBindings()
		opts.StartupNotice = fmt.Sprintf("key bindings ignored (%s) — using defaults; fix or delete %s", firstLine(err.Error()), path)
	}
	opts.KeyBindings = keys
	store, logErr := activity.Open(config.Dir(), activity.DefaultMaxBytes)
	if logErr != nil {
		opts.LogError = logErr.Error()
	} else {
		opts.Activity = store
		activity.SetActive(store)
	}
	defer func() {
		activity.SetActive(nil)
		if store != nil {
			_ = store.Close()
		}
	}()
	opts = prepareStartupGlyphs(opts)
	m := New(opts)
	options := []tea.ProgramOption{tea.WithAltScreen()}
	if opts.Settings.Mouse {
		options = append(options, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(m, options...)
	result, err := p.Run()
	if err != nil {
		return fmt.Errorf("running the TUI: %w", err)
	}
	var final Model
	switch v := result.(type) {
	case Model:
		final = v
	case *Model:
		final = *v
	}
	if final.settings.ExitSummary && final.haveDownload && final.downloadedAPK.Path != "" {
		fmt.Printf("Saved APK: %s\n", final.downloadedAPK.Path)
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
