package cli

import "ghapk/internal/config"

func loadLast() string { return config.LoadState().LastRepo }

func saveLast(repo string) {
	s := config.LoadState()
	s.Touch(repo)
}
