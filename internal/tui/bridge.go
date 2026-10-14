package tui

import (
	"github.com/ghapk/ghapk/internal/config"
	"github.com/ghapk/ghapk/internal/device"
	"github.com/ghapk/ghapk/internal/score"
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
	return score.Prefs{Arch: s.Arch, Variant: s.Variant}
}
