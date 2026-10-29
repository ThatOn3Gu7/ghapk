package tui

import (
	"github.com/ThatOn3Gu7/ghapk/internal/config"
	"github.com/ThatOn3Gu7/ghapk/internal/device"
	"github.com/ThatOn3Gu7/ghapk/internal/score"
)

// archPrefs is the TUI-local alias kept for readability in messages.go.
type archPrefs = struct{ Arch, Variant string }

func scoreDeviceFrom(d device.Info) score.Device {
	return score.Device{ABIs: d.ABIs, SDK: d.SDK}
}

func scorePrefsFrom(p archPrefs) score.Prefs {
	return score.Prefs{Arch: p.Arch, Variant: p.Variant}
}

func prefsFromSettings(s config.Settings) score.Prefs {
	arch := s.Arch
	if s.AllArch {
		arch = "any"
	}
	return score.Prefs{Arch: arch, Variant: s.Variant}
}
