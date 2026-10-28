package tui

import "time"

// Start runs the start-up sequence without a terminal (tests, screenshots).
func (a *App) Start() { a.startup() }

// Pump handles background messages until none has arrived for idle.
func (a *App) Pump(idle time.Duration) {
	a.afterEvent()
	for {
		select {
		case m := <-a.msgs:
			a.handleMsg(m)
			a.afterEvent()
		case <-time.After(idle):
			return
		}
	}
}

// Resize sets the virtual screen size.
func (a *App) Resize(w, h int) { a.W, a.H = w, h; a.buf.Resize(w, h) }

// Quit reports whether the user asked to leave.
func (a *App) Quit() bool { return a.quit }

// Modal reports whether a dialog is open.
func (a *App) Modal() bool { return a.modal != nil }
