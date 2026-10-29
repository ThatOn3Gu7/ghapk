<p align="center">
  <img src="assets/logo.png" alt="ghapk logo" width="180"/>
</p>

<h1 align="center">ghapk</h1>

<p align="center">
  <strong>GitHub Actions &rarr; APK installer, built for developers on Android (Termux), Linux & macOS.</strong>
</p>

<p align="center">
  <a href="https://github.com/ThatOn3Gu7/ghapk/releases"><img src="https://img.shields.io/github/v/release/ThatOn3Gu7/ghapk?color=a6e3a1&style=flat-square" alt="Latest Release"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/go-1.26-89dceb?style=flat-square&logo=go" alt="Go Version"></a>
  <img src="https://img.shields.io/badge/license-MIT-f9e2af?style=flat-square" alt="License">
  <a href="https://termux.dev/"><img src="https://img.shields.io/badge/platform-Android%20%7C%20Linux%20%7C%20macOS-cba6f7?style=flat-square" alt="Platform"></a>
</p>

---

## ⚡ What is ghapk?

Testing Android apps built with **GitHub Actions or Releases** usually means endless friction: opening your phone's browser, authenticating to GitHub, finding the workflow run, downloading a multi-hundred megabyte zip file, extracting the `.apk`, finding it in your file manager, and manually installing it.

**`ghapk` eliminates all of that.**

With a single keyboard shortcut or touch tap from your terminal, `ghapk`:
1. Finds GitHub Actions artifacts or APKs attached to GitHub Releases.
2. Inspects your actual physical device hardware (CPU architecture / ABI, Android SDK level, debug/release preference).
3. Evaluates and scores every available artifact to automatically pick the optimal build for your device.
4. Downloads and extracts the APK, including from nested ZIP archives.
5. Optionally hands the APK to Android's package installer or installs to a connected Android device with `adb`.

All through a sleek, reactive, **LazyGit-style Terminal User Interface (TUI)** that cleans up after itself and leaves your terminal scrollback untouched.

```text
  ██████╗ ██╗  ██╗   █████╗ ██████╗ ██╗  ██╗
 ██╔════╝ ██║  ██║  ██╔══██╗██╔══██╗██║ ██╔╝
 ██║  ███╗███████║  ███████║██████╔╝█████╔╝ 
 ██║   ██║██╔══██║  ██╔══██║██╔═══╝ ██╔═██╗ 
 ╚██████╔╝██║  ██║  ██║  ██║██║     ██║  ██╗
  ╚═════╝ ╚═╝  ╚═╝  ╚═╝  ╚═╝╚═╝     ╚═╝  ╚═╝
             GitHub Actions APK Manager
```

---

## 🚀 Quick Install

### One-line curl (Recommended)

Run this in **Termux**, **Linux**, or **macOS**. It automatically detects your operating system and CPU architecture, downloads the right binary, and puts it in your path:

```bash
curl -fsSL https://raw.githubusercontent.com/ThatOn3Gu7/ghapk/master/install.sh | bash
```

*(Or with `wget`: `wget -qO- https://raw.githubusercontent.com/ThatOn3Gu7/ghapk/master/install.sh | bash`)*

---

### Prerequisites

`ghapk` uses GitHub's official command line interface (`gh`) to communicate with the GitHub Actions API securely:

```bash
# On Android (Termux)
pkg install gh
gh auth login

# On macOS (Homebrew)
brew install gh
gh auth login

# On Ubuntu / Debian
sudo apt install gh
gh auth login
```

Once logged in, simply launch:

```bash
ghapk
```

---

## 📦 Pre-Built Binaries

Pre-compiled standalone binaries are available on the **[Releases](https://github.com/ThatOn3Gu7/ghapk/releases)** page:

| Platform | Architecture | Target File |
| :--- | :--- | :--- |
| **Android (Termux)** | `arm64` (aarch64) | [`ghapk-android-arm64`](https://github.com/ThatOn3Gu7/ghapk/releases/latest) |
| **Linux** | `amd64` (x86_64) | [`ghapk-linux-amd64`](https://github.com/ThatOn3Gu7/ghapk/releases/latest) |
| **Linux** | `arm64` (aarch64) | [`ghapk-linux-arm64`](https://github.com/ThatOn3Gu7/ghapk/releases/latest) |
| **macOS** | `arm64` (Apple Silicon) | [`ghapk-darwin-arm64`](https://github.com/ThatOn3Gu7/ghapk/releases/latest) |
| **macOS** | `amd64` (Intel) | [`ghapk-darwin-amd64`](https://github.com/ThatOn3Gu7/ghapk/releases/latest) |
| **Windows** | `amd64` (x86_64) | [`ghapk-windows-amd64.exe`](https://github.com/ThatOn3Gu7/ghapk/releases/latest) |

### Install via Go

With Go 1.26 or newer:

```bash
go install github.com/ThatOn3Gu7/ghapk@latest
```

Or install from a checkout:

```bash
git clone https://github.com/ThatOn3Gu7/ghapk.git
cd ghapk
go install .
```

Ensure `$(go env GOPATH)/bin` is on your PATH. The binary still needs the authenticated `gh` CLI to access GitHub.

---

## 🎮 Navigation & Keybindings

`ghapk` is built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and supports both **keyboard navigation** and **mouse / touch gestures**:

```
 1 Repos   2 Flows   3 Builds   4 Downloads   5 Settings   6 Device   7 Log
╭─ Builds ────────────────────╮╭─ Artifacts ────────────────────────────╮
│ ✓ #42 feat: block+pipe logo ││ ✓ app-arm64-v8a-debug       14.2 MB  │
│ ✓ #41 fix: centering        ││ · app-universal-debug       20.1 MB  │
│ ✗ #40 refactor: split       │╰───────────────────────────────────────╯
│                             │╭─ Details ──────────────────────────────╮
│                             ││ feat: block+pipe logo                 │
│                             ││ Result    ✓ success                   │
│                             ││ Build     #42                         │
│                             ││ Branch    main                        │
│                             ││ Workflow  Build APK                   │
│                             ││                                       │
│                             ││ Install Download Open                 │
╰─────────────────────────────╯╰───────────────────────────────────────╯
```

Your last selected repository, branch, and workflow are remembered between TUI
sessions. Branch and workflow choices are kept separately for each repository,
including “all branches” and “any workflow”. Explicit command-line flags and
`GHAPK_REPO`, `GHAPK_BRANCH`, or `GHAPK_WORKFLOW` environment variables take
precedence for that launch.

| Key | Action |
| :--- | :--- |
| `Tab` / `Shift+Tab` | Cycle through screens |
| `1` – `7` | Jump to Repos, Flows, Builds, Downloads, Settings, Device, or Log |
| `Enter` | Select a repo, branch, or workflow; press again to move on |
| `↑` `↓` / `j` `k` | Move cursor in the current list |
| `g` / `G` | Jump to top / bottom |
| `Ctrl+D` / `Ctrl+U` | Half-page down / up |
| `Enter` / `→` | Open a build’s artifacts; Enter on an artifact opens the install plan |
| **Mouse / Touch** | Tap to select; tap the repo name for the picker or a bottom hint to run it; tap outside an overlay to dismiss it; swipe Device details, or swipe/drag the scroll bar in Help |
| `d` | Download selected artifact to local cache |
| `i` | Check the device, choose an artifact, then install or download only |
| `R` | Repository picker (accessible GitHub repos, recents, git remote, search) |
| `B` | Switch the Actions branch filter (release assets are not branch-filtered) |
| `r` | Refresh current view from GitHub API |
| `w` | Toggle watch mode (live polling for running CI workflows) |
| `/` | Real-time case-insensitive substring filter within current panel |
| `f` | Star / favorite a repository |
| `s` | In Downloads, focus saved APKs; in Settings, switch save scope |
| `t` | Cycle color theme (`action`, `mocha`, `emerald`, `paper`) |
| `D` | Run diagnostic Doctor tool |
| `?` | Toggle help overlay |
| `q` / `Ctrl+C` | Clean exit (restores terminal state) |

On a fresh start, the repository picker loads all repositories accessible to the authenticated `gh` account, including private and organization repositories where authorized. No local clone is required. Tap a repository to choose it, swipe to scroll, or type to filter or enter `owner/repo`. If loading fails, tap retry or press `Ctrl+R`.

There is no preloaded demo repository. The picker can show your accessible GitHub repositories, recent choices, and the current checkout’s GitHub origin (marked `git remote`); the Repos screen can also suggest that origin with a `(git)` label. These are suggestions, not an active selection until you choose one. On later launches, ghapk can restore a previous selection or use a repo from your config, `GHAPK_REPO`, or `--repo`.

During installation, ghapk compares the APK's actual package and version code with the device. A matching version code prompts **Keep APK, skip install** or **Reinstall**; it does not mean the files are identical. Different package IDs (such as debug and release variants) are separate apps. If signing keys differ, Android may reject an update; when local Shizuku can verify the installed app, ghapk can offer an explicitly confirmed uninstall first. **Uninstalling erases that app's data.** It is never automatic. If checks cannot be performed, the Downloads log says so. An Android installer handoff is not reported as a confirmed reinstall based on an unchanged version code.

When several APKs in an archive rank equally, choose which one to extract instead of silently guessing. Safety questions use numbered options; use ↑/↓ and Enter, or tap an option. Swipe or use Page Down to read a long warning before proceeding.

Artifact rows use name-based platform or file-type icons (🤖 Android, 📱 iOS, 🍎 macOS, ⊞ Windows, 🐧 Linux, 🌐 web, 📄 document, 📦 archive, ◇ unknown). An `!` marks an artifact rejected by your architecture or variant preferences. Icons are hints, not verified compatibility: Actions artifact names don't reveal their contents or app icons, and installation remains APK-focused. Select **ASCII** in Settings (or set `GHAPK_ASCII=1` in auto mode) for ASCII borders, indicators, and icons; non-English repository names and other user text are preserved.

On Repos, select a repository and then a branch; choose “all branches” to leave Actions runs unfiltered. Each box title shows the number of matching options, including off-screen rows and “all branches” when present; counts update when filtering. Branches come from GitHub and, when the current working tree has a matching GitHub origin, its local branches. A local-only branch needs a pushed Actions run to have downloadable artifacts.

In Help, press `W` (or tap the wrap label) to toggle word wrapping; when wrapping is off, long lines are clipped on the right.

The UI uses your terminal's background, while keeping selection and action highlights. Press `t` to cycle themes; `paper` is designed for light terminal backgrounds.

*Note for mobile users:* Below 64 columns, Builds shows the list first. Tap a build or press `→` to see its artifacts and details; tap the top border or press `←` to go back.

---

## 🤖 Non-Interactive & Scripting Mode

Need to fetch APKs from a shell script or CI pipeline without opening the interactive TUI? `ghapk get` provides a pure CLI command:

```bash
# Download and install the best APK for the current device
ghapk get --repo ThatOn3Gu7/Nazo -i

# Download specific architecture and build variant
ghapk get --repo ThatOn3Gu7/Nazo --arch arm64 --variant debug

# Dry-run: calculate and print the plan without downloading
ghapk get --repo ThatOn3Gu7/Nazo --dry-run

# Run environment health check
ghapk doctor
```

`get` downloads by default; `-i` requests installation. `--no-open` skips
installation in CLI mode, while an explicit TUI Install still works. `--yes`
accepts non-downgrade safety warnings, but cannot override a blocked install;
automatic downgrades require `--downgrade=allow`. A handoff that cannot be
verified is reported as unconfirmed, not as a confirmed install. Progress, messages and the resulting APK path are printed to stderr.

Useful options (also available to the TUI unless noted):

| Option | Purpose |
| :--- | :--- |
| `--source auto\|actions\|releases` | Choose Actions, release assets, or fallback |
| `--branch`, `--workflow` | Filter Actions runs |
| `--artifact PATTERN`, `--exclude PATTERN` | Filter names: Actions uses case-insensitive substring matching; Releases uses case-insensitive globs |
| `--file GLOB` | Match files in archives, e.g. `--file '*release*.apk'` |
| `--out DIR` | Also copy the APK to a directory |
| `--keep N`, `--refresh SECONDS` | Control cache retention and refresh interval |
| `--watch`, `--limit N` | Watch for runs or limit the number scanned |
| `--package NAME`, `--downgrade auto\|allow\|never` | Package and downgrade safety policy |
| `--no-pkg-check`, `--no-sig-check`, `--no-verify-digest`, `--no-verify-install`, `--no-identical-check` | Disable individual checks; use with care |
| `--all-arch`, `--ascii`, `--unicode`, `--no-mouse` | Override device or interface preferences |
| `--install-wait SECONDS`, `--notify-on POLICY`, `--no-notify`, `--speak`, `--toast` | Installation verification and Termux notifications |
| `get --dry-run` | Resolve a plan without downloading |
| `get --print-path` | Use the APK cache as the output directory (the resulting path is logged to stderr) |

Run `ghapk help` for a quick usage summary.

---

## 🛠 Project Structure

```text
ghapk/
├── assets/                  # Branding, icons, logos, and previews
│   ├── logo.png             # High-resolution badge icon
│   └── logo.svg             # Crisp vector graphic badge
├── main.go                  # Application entrypoint
├── internal/
│   ├── activity/            # Persistent activity log
│   ├── apk/                 # APK metadata and installation safety
│   ├── config/              # Persistent configuration (~/.config/ghapk/)
│   ├── device/              # Android & host probing (ABI, SDK, dumpsys, getprop)
│   ├── flow/                # Headless one-shot CLI execution (ghapk get)
│   ├── ghapi/               # GitHub Actions REST API client wrapper (via gh CLI)
│   ├── score/               # Artifact scoring algorithm (ABI, SDK, variant match)
│   └── tui/                 # Full-screen Bubble Tea terminal interface
│       ├── components/      # UI widgets (boxes, badges, scrollbars, dialogs)
│       ├── theme/           # Color palettes (Action, Mocha, Emerald, Paper)
│       ├── app.go           # Central state model and message router
│       ├── content.go       # Panel rendering (ANSI Shadow logo, shimmering, lists)
│       ├── mouse.go         # Terminal mouse & touch event translation
│       └── view.go          # Screen composition & responsive breakpoints
├── docs/
│   ├── BUILD.md             # Compilation and release guide
│   └── releases/            # Handwritten release notes
├── .github/workflows/
│   └── release.yml          # Automated CI cross-platform build & release
├── Makefile                 # Easy developer build automation
└── install.sh               # One-line universal curl/bash installer
```

---

## 🏗 Building from Source

`ghapk` requires Go 1.26 or newer to build and does not require CGO.

```bash
# Clone the repository
git clone https://github.com/ThatOn3Gu7/ghapk.git
cd ghapk

# Build locally
make

# Build optimized binary for Termux (Android arm64)
make termux

# Cross-compile stripped binaries for all platforms
make release
```

Detailed instructions on compiler optimization, UPX compression, and distribution can be found in **[docs/BUILD.md](docs/BUILD.md)**.

---

## ⚙ Configuration

Open **Settings** from its tab (or press `5`). Enter changes a setting, `d`
removes the override at the chosen scope, and `s` (or a tap on the `save:`
badge or `s scope` footer) switches the selected setting between global and
this repo. Selection rules such as branch, workflow and file patterns default to
this repo; theme, glyphs, notifications and other general preferences default to
global. The save scope applies to the selected setting and is remembered for
the current session. Changes are saved immediately. The `[repo]`, `[global]`,
`[env]` and `[cli]` labels show the active value's source; repo overrides take
precedence over global settings, and environment variables and CLI flags win
over both. Existing repo overrides are preserved: to use the global value for
one, select that setting, switch to `this repo`, and press `d` (or tap `d reset`).
The `t` theme shortcut saves using the Theme setting's chosen scope. Touch works
for selecting and scrolling; tap a selected row again to change it.

Settings include selection and safety,
output copies, refresh, notifications, glyphs, mouse and theme. The per-pick
remember policy and watch timeout are not yet available; the install plan and
its confirmations remain in place. Unattended `uninstall-first` remains
intentionally unsupported: uninstalling an app can erase its data and still
requires explicit safety confirmations.

Settings can also be customized directly via configuration files:

- **Global Config:** `~/.config/ghapk/config`
- **Per-Repository Config:** `~/.config/ghapk/repos/<owner>_<repo>.conf`
- **Local Workspace Config:** `.ghapk.conf`
- **Downloaded APK Storage:** `~/.cache/ghapk/apk/`

Configuration files contain `KEY=VALUE` lines. Precedence is: command-line
flags, nonempty `GHAPK_*` environment variables, your per-repository config,
your global config, then safe workspace/repository hints and defaults. For
example, `GHAPK_SOURCE=releases` corresponds to `source=releases`. A local
`.ghapk.conf` or a repository’s `.github/ghapk.conf` can suggest only repo,
branch, workflow, artifact, file, exclude, and source selection; it cannot
turn off safety checks, set output paths, or trigger uninstalls. Remembered TUI
selections are stored separately and explicit CLI/environment selections win.
`GHAPK_CONFIG_DIR` overrides the config directory; otherwise `XDG_CONFIG_HOME`
changes its parent. `GHAPK_CACHE_DIR`, then `CACHE_DIR`, then `XDG_CACHE_HOME`
change the cache location.

**Source** in Settings can be `actions`, `releases`, or `auto`: auto uses
Actions where available and falls back to release assets. A release asset can
be an APK or a ZIP containing APKs; Actions artifacts are archives. Branch
filters apply to Actions, not releases. Archive extraction has size and path
safety limits. With digest verification enabled, ghapk checks a SHA-256 digest
when GitHub supplies one and reports unverified when it doesn't. Cached
archives are reused only when validated; disabling verification weakens this
check. The one-line installer separately verifies the binary against the
release's `sha256sums.txt` when the file, a matching entry, and a SHA-256
tool are available. Otherwise the installer proceeds without verification.
To request a particular release, pipe the installer into
`GHAPK_VERSION=v4.3.0 bash`.

Termux notifications require the relevant Termux:API app and command-line tools.
Settings can choose when to notify and optionally speak or show a toast; absent
helpers are skipped. Use `ghapk doctor` to check GitHub authentication and
installer availability.

### Custom key bindings

On the first TUI launch, ghapk creates `~/.config/twinpane/keybinding.json`.
Edit existing entries and restart ghapk to use them. `XDG_CONFIG_HOME` changes
the parent config directory; `GHAPK_CONFIG_DIR` places the file directly in
that directory. For example, changing `"repo_picker": "R"` in `dashboard` to
`"repo_picker": "z"` makes `z` open the repository picker. Each entry is one
key or chord, not a list or a sequence. Keys are case-sensitive: use `A` for
Shift+A, `space`, `enter`, `up`, `f2`, `ctrl+k`, or `alt+x`, for example.
Terminal support for particular chords varies. Some combinations are the same
as another key in a terminal (for example, Ctrl+I is Tab and Ctrl+M is Enter);
they are treated as the same key for conflict checks.

Sections separate screens and dialogs; a key may be reused in different
contexts, but conflicting bindings in the same context are rejected. Dashboard
bindings apply to dashboard screens unless that screen's section overrides the
same action. `text_editing` applies to text widgets unless a picker intercepts
that key; filter editing has its own section. The file contains the existing
actions and their alternative shortcuts, not a way to create new actions.
Plain printable keys are reserved for typing in text fields. Ctrl+C always
remains an emergency quit, even when other shortcuts are customized.

Invalid JSON, unknown actions, duplicate/conflicting keys, and unsupported or
multi-key values stop the TUI with an error naming the problem; your file is
not overwritten. To swap two shortcuts, change both entries before restarting.
Fix the file or remove it to regenerate defaults. Help and touch hints show
customized keys where space permits; long tab shortcuts may be omitted from the
tab labels on narrow screens but still work.

### Activity log

The **Log** tab (7) shows timestamped activity and job steps across sessions,
grouped by date. Use the arrow keys or swipe to scroll, `f` to follow new
entries, and `y` to copy the last 60 entries. Browsing the log does not add
scroll entries. The private log is stored at
`~/.config/ghapk/activity.log` (or under `GHAPK_CONFIG_DIR` / `XDG_CONFIG_HOME`)
and keeps the newest entries when it reaches 1 MiB. Log-file errors are shown in the Log tab.

The log can include repository names, file paths, command names and their
arguments. Typed search/edit text and streamed installer output are not recorded;
error messages may include command output. Recognizable credentials and URL queries are redacted. Review the log before
sharing it, because automatic redaction cannot identify every possible secret.

### Downloads and installation

Install first checks the device and presents a plan; APK manifest and signing
information are inspected **after** downloading. Choose an artifact, then
**Install**, **Download only**, or **Cancel**. Downloads run in the background;
the Downloads tab shows progress, checksum status, processing steps and logs.
On a narrow screen, new jobs open their details automatically; press Esc to
return to the lists, then select a job and press Enter (or tap it again) to
reopen it. Swipe lists or details to scroll. In Downloads, `c` cancels a job, `x` removes
a finished job or deletes a selected saved APK (with confirmation), `X`
removes finished jobs, and `s` focuses saved APKs. Select a saved APK and press
Enter to install it with the same safety checks.

Jobs are kept only for the current session; saved APKs survive restarts.
Cancelled downloads restart from the beginning. Cancellation cannot undo an
installer handoff already made to Android. An installer launch is reported as
unconfirmed unless installation can actually be verified on the device.


---

## 📄 License

The project is described as MIT &copy; [ThatOn3Gu7](https://github.com/ThatOn3Gu7); the repository currently does not include a LICENSE file.
