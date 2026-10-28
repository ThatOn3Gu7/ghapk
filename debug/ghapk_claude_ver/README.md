# ghapk

Install the newest GitHub Actions debug APK on your phone — from a full-screen
terminal interface, in the spirit of lazygit.

```
 ghapk  acme/nazo  ⎇ main                                  ▯ nazo v1.4.1
 1 Builds   2 Downloads   3 Device   4 Settings   5 Log
╭─ Builds ─────────────── 2/12 ─╮╭─ Artifacts ─────────────── #411 ─╮
│ ⠋ #412 feat: live leaderboard ││ ✓ nazo-debug-arm64-v8a    2.9 MB │
│ ✓ #411 fix: quiz timer drift  ││ • nazo-debug-armeabi-v7a  2.9 MB │
│ ✗ #410 refactor: split Quiz.. ││ – nazo-release-arm64-v8a  2.9 MB │
```

Type `ghapk`, look around, press `↵` on a build. When you quit, your terminal is
exactly as you left it (the interface lives on the alternate screen, like vim).

It is a rewrite of the original `ghapk` shell script as a single static Go
program with **no dependencies** beyond the standard library. It still talks to
GitHub only through the `gh` CLI, so your existing login keeps working, and it
reads the same config files and install history.

## Install (Termux)

```sh
pkg install golang gh git
gh auth login

cd ghapk                      # the folder that contains go.mod and main.go
go build -trimpath -ldflags "-s -w" -o $PREFIX/bin/ghapk .
ghapk
```

(`make install DEST=$PREFIX/bin` does the same.) The `.` at the end matters: it
means "the program in this folder". Building needs nothing from the internet,
because the project has no third-party code. If Go complains about a C compiler,
run `pkg install clang` or add `CGO_ENABLED=0` in front of `go build`.

### One file instead of a folder

`ghapk` compiles to **one self-contained executable** (about 5 MB). After
building you can delete the source folder; the file in `$PREFIX/bin` is the whole
tool.

To build it on a PC for your phone, target **Android**, not plain Linux. Android
blocks some system calls that ordinary Linux programs use, and Go only avoids
them when told it is building for Android:

```sh
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ghapk-android-arm64 .
# copy it to the phone, then in Termux:
chmod +x ghapk-android-arm64 && mv ghapk-android-arm64 $PREFIX/bin/ghapk
```

`make release` produces Android, Linux and macOS binaries plus checksums in
`dist/`, and tagging `v1.0.0` on GitHub does the same in CI and attaches them to
a release (`.github/workflows/release.yml`).

## The interface

| Tab | What it does |
| --- | --- |
| **1 Builds** | Recent runs, the artifacts of the selected run, and details. Filter by branch, workflow, status; search; mark several. `↵` shows an install plan: version vs. what you have, which artifact, what changed since your last install. |
| **2 Downloads** | Every download/install as a step list with live progress, speed and ETA; questions (downgrade? different signing key?) appear as dialogs. Kept APKs can be reinstalled. |
| **3 Device** | A doctor: `gh`, login, rate limit, Android version and ABIs, package-manager access, Shizuku, installer, free space, the installed app and its signing key. |
| **4 Settings** | Every setting with its source (default / global / repo / env / flag). Saved per repo or everywhere. |
| **5 Log** | What ghapk did and why. |

Keys: `1`–`5` or `Tab` switch screens · `j k ↑ ↓` move · `↵` install · `d` download
only · `space` mark · `b` branch · `w` workflow · `f` status · `/` search · `I`
install the newest match · `W` wait for a running build, then install · `e` why a
build failed · `o` open in browser · `R` change repository · `?` help · `q` quit.
Everything is also tappable: tabs, rows, the buttons in the details pane, and the
hints on the bottom line. Tap a selected row again to act on it.

Themes (`theme` setting, or `--theme`): `default nord tokyo gruvbox dracula mono cb ansi`.
`cb` is colour-blind safe; `mono` and `ansi` work on any terminal. Colours fall
back automatically from true-colour to 256 to 16, and `glyphs = ascii` swaps
every symbol for plain characters.

## Without the interface

Anything the old script did still works, and runs without opening the
interface:

```sh
ghapk -r owner/name -y            # install the newest matching build
ghapk -r owner/name --dry-run     # show the plan, change nothing
ghapk --run 1234 --no-open        # download one run, don't install
ghapk --watch -y                  # wait for a running build, then install
ghapk --doctor                    # check the setup (exit code 14 if broken)
ghapk --set theme=nord            # save a setting (--set-global, --unset KEY)
ghapk --help                      # all flags and exit codes
```

Without a terminal (cron, pipes) it falls back to this mode automatically.
Exit codes are the original script's. Flags that only styled the old text UI
(`--width`, `--spinner`, `--layout`, …) are accepted and ignored. `--last` is
not carried over.

## Layout

```
main.go          starts internal/cli
internal/
  cli/           flags and the plain (non-interactive) mode
  engine/        the brain: plan, download → verify → unpack → validate → install jobs
  ghub/          gh CLI gateway: retries, ETag cache, error classes, downloads
  device/        phone detection, installed-app query, installer chain, verify
  apk/           binary-manifest and signing-certificate reader (no aapt needed)
  pick/          which artifact suits this phone (scoring)
  config/        settings registry, layered config files, history
  glob/ version/ exit/   small pure helpers
  term/          raw terminal, input parser, cell buffer, diffing renderer
  ui/            themes, glyphs, boxes, lists, inputs, progress bars
  tui/           the screens and dialogs
scripts/         fake gh + fake phone for tests, screenshot driver, demo
```

`make test` runs everything (about a minute: it drives the real engine and
interface against the fake GitHub and fake phone). `make demo` opens the
interface on that pretend world so you can click around safely.

See `docs/DESIGN.md` for how the pieces fit and what was kept from the shell
script.
