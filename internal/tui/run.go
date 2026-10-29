package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ghapk/ghapk/internal/activity"
	"github.com/ghapk/ghapk/internal/config"
)

// Run launches the interactive interface in the alternate screen buffer.
// When the user quits, the terminal is restored untouched — the shell is
// exactly where they left it, the way lazygit exits.
func Run(opts Options) error {
	path := KeybindingPath()
	keys, err := LoadKeyBindings(path)
	if err != nil {
		return fmt.Errorf("key bindings (%s): %w; fix the file or remove it to restore defaults", path, err)
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
