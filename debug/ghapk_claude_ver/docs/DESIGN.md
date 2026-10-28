# Design notes

## Threading

One goroutine owns all UI state. Background work (GitHub calls, jobs, device
probes) never touches it: it finishes and posts a closure (`a.async`) or a
message to a channel the UI loop reads. The loop wakes on input, on a posted
message, or on a 100 ms tick that only redraws when something is animating.
Engine jobs run on their own goroutines; the UI reads immutable `JobView`
snapshots, and a job's questions go through a `Prompter` that parks the job
until a dialog is answered (or the job is cancelled).

## Rendering

Screens draw into a `term.Buffer` (a grid of cells: rune, width, style). The
renderer compares it with the previous frame and emits only changed cells,
wrapped in synchronized-update escapes (`?2026`) so supporting terminals never
tear. Wide characters occupy two cells; colours degrade true-colour → 256 → 16
using per-role fallbacks. Clickable regions are registered while drawing
(`ui.Hits`), so the mouse handler never needs to know the layout.

Layout is responsive: ≥100 columns shows three panes; narrower screens show one
pane at a time and drill in (`→` / tap). Below 32×9 it just asks for room.

## Terminal hygiene

`term.Terminal.Start` enters raw mode, the alternate screen, hides the cursor,
turns off autowrap, enables bracketed paste and SGR mouse. `Close` undoes each
one, is idempotent, and runs on normal exit, on SIGTERM/SIGHUP, and from a
panic handler. Nothing is printed to the normal screen except an optional
one-line "saved …" summary after the screen is restored.

## What came from the shell script

Everything with behaviour: the gateway with ETag caching and full-jitter retry,
the error classes and exit codes, artifact scoring (debug/release, ABI, API
level, checksum files), the glob dialect (extglob, case-insensitive), version
extraction and comparison, `versionCode` as the downgrade authority, APK
manifest and signing-block parsing, the installer chain and post-install
verification, the layered config files and `history.json`. All of it is covered
by tests that run against a fake `gh` (`scripts/fakegh`) and a fake Android
(`scripts/fakegh/bin`).

## What was dropped

The text-UI layer (spinners, banner, width/layout/bar options, `--last`).
