package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Run launches the interactive interface in the alternate screen buffer.
// When the user quits, the terminal is restored untouched — the shell is
// exactly where they left it, the way lazygit exits.
func Run(opts Options) error {
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
