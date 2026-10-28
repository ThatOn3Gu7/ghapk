package tui

import (
	"ghapk/internal/term"
	"ghapk/internal/ui"
)

type helpEntry struct{ k, d string }

func (a *App) openHelp() {
	th := a.th
	var rows []trow
	head := func(s string) {
		if len(rows) > 0 {
			rows = append(rows, blankRow())
		}
		rows = append(rows, trow{spans: []ui.Span{ui.Sp(s, th.Title())}})
	}
	add := func(es ...helpEntry) {
		for _, e := range es {
			rows = append(rows, trow{spans: []ui.Span{ui.Sp(term.PadRight(e.k, 12), th.Key()), ui.Sp(e.d, th.Base())}})
		}
	}
	head("Everywhere")
	add(helpEntry{"1-5  Tab", "switch screens (or tap the tab)"},
		helpEntry{"R", "choose another repository"},
		helpEntry{"I", "install the newest matching build"},
		helpEntry{"?", "this help"},
		helpEntry{"q  Ctrl-C", "quit — your terminal is left as it was"})
	head("Builds")
	add(helpEntry{"j k ↑ ↓", "move"}, helpEntry{"g G", "first / last"},
		helpEntry{"↵  i", "install the selected build (shows a plan first)"},
		helpEntry{"→  l", "look at its artifacts; tap a row twice to install"},
		helpEntry{"d", "download only (marked builds, or this one)"},
		helpEntry{"space", "mark a build"},
		helpEntry{"b  w", "pick a branch / workflow"},
		helpEntry{"f", "cycle: all · success · failed · running"},
		helpEntry{"/", "search the list"}, helpEntry{"x", "clear filters"},
		helpEntry{"W", "wait for a running build, then install it"},
		helpEntry{"e", "why did it fail?"},
		helpEntry{"o  y", "open / copy the build's web address"},
		helpEntry{"r", "refresh"})
	head("Downloads")
	add(helpEntry{"c", "cancel the running job"}, helpEntry{"x  X", "remove finished job / all finished"},
		helpEntry{"s", "saved APKs: ↵ installs one again"}, helpEntry{"A", "answer a waiting question"})
	head("Device")
	add(helpEntry{"r", "re-check everything"}, helpEntry{"p", "set the app's package name"})
	head("Settings")
	add(helpEntry{"↵", "change (switch, list or text)"}, helpEntry{"space", "cycle choices"},
		helpEntry{"d", "reset to default"}, helpEntry{"s", "save for this repo / for everything"})
	head("Touch")
	add(helpEntry{"tap", "select; tap again to act"}, helpEntry{"drag/scroll", "move through lists"},
		helpEntry{"bottom bar", "tap any hint to use it"})
	a.modal = &textModal{title: "Keys", rows: rows, footer: "↑↓ scroll · Esc close"}
}
