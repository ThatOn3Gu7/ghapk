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
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running the TUI: %w", err)
	}
	return nil
}
