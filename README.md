# ghapk

**GitHub Actions → APK installer, now as a full-screen TUI.**

`ghapk` finds the newest successful build of your app in GitHub Actions,
scores every artifact for *your* device (ABI, API level, debug/release),
downloads the best one and hands it to Android's installer — all from an
interactive LazyGit-style terminal UI that leaves your shell untouched
when you quit.

It is a ground-up Go rewrite of the original 13,000-line `ghapk` bash
script, with the same scoring algorithm, the same config-file format and
the same install-handoff order — plus the interactive interface the
script always wanted.

```
  ghapk    acme/rocket   main  RUNNING                                          @tester  action
╭─ Repositories ──╮╭─ Runs  3 ─────────────╮╭─ Artifacts  3 ────────────────────╮
│ ▶ acme/rocket   ││  ✓ #412  fix login …  ││ ★ rocket-universal-debug.apk 20M  │
│                 ││  ⣻ #411  bump gradle   ││   rocket-arm64-v8a-debug.apk 14M  │
│                 ││  ✗ #410  fix the thing ││   mappings.txt                1M  │
╰─────────────────╯╰──────────────────────╯╰──────────────────────────────────╯
╭─ Workflows ─────╮╭─ Device ─────────────╮╭─ Details ─────────────────────────╮
│ ▶ ◆ any workflow││ model  Pixel 7       ││ ✓ #412 fix login crash            │
│   CI            ││ abis   arm64-v8a,…   ││ size     20.0 MB (zip)            │
│   Nightly       ││ sdk    34            ││ score    150                      │
╰─────────────────╯╰──────────────────────╯╰──────────────────────────────────╯
d:download  i:install  tab:panels  /:filter  s:settings  ?:help  q:quit
```

## Quick start

```bash
# prerequisites: the GitHub CLI, authenticated
pkg install gh        # Termux    (macOS: brew install gh)
gh auth login

ghapk                 # open the TUI
```

First launch asks for a repository (`owner/name`, with recents +
GitHub search + auto-detection from the git remote of your working
directory). Everything after that is point-and-shoot: pick a run, the
artifacts are ranked automatically, press `d` or `i`.

When you press `q`, your terminal scrollback is exactly where you left
it — the app lives entirely in the alternate screen buffer.

## Keys

| key | what it does |
| --- | --- |
| `tab` / `shift+tab`, `1`–`6` | switch panels |
| `j` `k` `↑` `↓` / `ctrl+d` `ctrl+u` / `g` `G` | move |
| `enter` | activate (switch repo, filter by workflow, …) |
| **mouse** | click focuses & selects, click again opens, wheel scrolls |
| `d` / `i` | download / download & install the selected artifact |
| `o` / `O` | open run in browser / open download folder |
| `R` | repository picker (recents, git remote, GitHub search) |
| `B` | branch picker |
| `r` / `w` | refresh now / watch mode (fast auto-refresh while builds run) |
| `/` | filter the focused list |
| `f` | star a recent repo |
| `s` | settings editor (per-repo or global save) |
| `t` | cycle theme: **action**, **mocha**, **emerald**, **paper** |
| `D` | doctor — environment health check |
| `?` | help |
| `ctrl+z` | suspend to shell (`fg` to return) |
| `q` / `ctrl+c` | quit |

Narrow terminals (a phone in portrait, < 96 columns) automatically get a
single-column tabbed layout instead of the grid.

## Classic mode

The original one-shot pipeline is still there for scripts and CI:

```bash
ghapk get --repo owner/repo --arch arm64 --variant debug -i
ghapk get --repo owner/repo --dry-run      # print the plan, fetch nothing
ghapk doctor                               # health check without the TUI
```

## Configuration

Same places, same `KEY=VALUE` format as the bash script:

- `~/.config/ghapk/config` — global
- `~/.config/ghapk/repos/<owner>_<repo>.conf` — per-repo (outranks global)
- `.ghapk.conf` — working directory
- flags on the command line — outrank everything

Everything editable from the settings screen (`s`): theme, arch
preference, variant, source, branch, workflow, artifact pattern, package
name, run limit, auto-refresh, watch, no-open, keep-cache.

Downloads land in `~/.cache/ghapk/apk/`.

## Architecture

```
ghapk/
├── main.go                  CLI entry (TUI default, get/doctor/version)
├── internal/
│   ├── config/              KEY=VALUE config, recents, favourites
│   ├── ghapi/               GitHub API via the gh CLI, downloads, unzip
│   ├── device/              Android probe (ABIs/SDK/pm), installer handoff
│   ├── score/               the artifact-ranking algorithm (ported from bash)
│   ├── flow/                classic linear pipeline (ghapk get)
│   └── tui/
│       ├── app.go           root model, messages, async commands
│       ├── dashboard.go     grid + compact layouts
│       ├── content.go       panel contents
│       ├── handlers.go      key handling
│       ├── pickers.go       repo/branch pickers
│       ├── settings.go      settings editor
│       ├── task.go          download/install progress modal
│       ├── help.go doctor.go
│       ├── theme/           palettes, glyphs, styles
│       └── components/      panels, lists, statusbar, modals
├── docs/BUILD.md            single-executable build guide
└── Makefile
```

Runs entirely in the **alternate screen buffer** (`ESC[?1049h` on start,
`ESC[?1049l` on exit) — quitting restores the shell exactly as it was,
no scrollback is cleared or redrawn. Set `GHAPK_ASCII=1` for terminals
without Unicode glyphs, `NO_COLOR=1` to disable color.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea),
[Bubbles](https://github.com/charmbracelet/bubbles) and
[Lip Gloss](https://github.com/charmbracelet/lipgloss) — the Go TUI
stack. All GitHub traffic goes through the `gh` CLI, so authentication
is `gh auth login` and nothing else.

## Building

```bash
make build        # ./ghapk static binary for this machine
make termux       # android/arm64 binary for your phone
make release      # every target, stripped + checksummed
```

See **[docs/BUILD.md](docs/BUILD.md)** for the full single-executable
story (stripping, UPX compression, cross-compiling for Termux, `go install`).
