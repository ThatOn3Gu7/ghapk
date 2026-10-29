# Building ghapk as a single executable

Go compiles everything — the app *and* all its TUI dependencies — into
one static binary. There is no interpreter, no runtime folder, no
dependency directory. One file is the whole tool.

## Zero. Prerequisites

- Go ≥ 1.24 installed (https://go.dev/dl). Nothing else — no CGO, no NDK,
  no extra system libraries.
- Optional: [UPX](https://upx.github.io) if you want the binary smaller.

## 1. The plain build

```bash
cd ghapk
go build -o ghapk .
```

That's it: `./ghapk` is a self-contained executable. Accessing GitHub still
requires the authenticated `gh` CLI. Move it anywhere, rename it, put it on your PATH:

```bash
mv ghapk ~/.local/bin/        # or /usr/local/bin, or ~/bin on Termux
```

## 2. A stripped, smaller single file

Two linker flags remove the symbol table and debug info; trimming the
build path removes local source paths:

```bash
go build -trimpath -ldflags="-s -w" -o ghapk .
```

Optional shrink on top of that with UPX (typically −60 %):

```bash
upx --best --lzma ghapk
```

The resulting size varies by version and target.

## 3. Cross-compiling — the binary must match the machine

A binary only runs on the OS/architecture it was built for, so build one
per target. Go makes this trivial and needs **no Android NDK** (pure Go,
CGO off by default for cross builds):

```bash
# your phone running Termux (arm64)
GOOS=android GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o ghapk-android-arm64 .

# 64-bit ARM Linux (Raspberry Pi etc.)
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o ghapk-linux-arm64 .

# everyday x86_64 Linux
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ghapk-linux-amd64 .

# macOS (Apple Silicon / Intel)
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o ghapk-darwin-arm64 .
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ghapk-darwin-amd64 .

# Windows
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ghapk-windows-amd64.exe .
```

Copy the right one to the device (e.g. `adb push ghapk-android-arm64
/data/local/tmp/`, or `scp` to the Pi), `chmod +x`, run.

`make release` in the repo does all of the above and writes
`bin/ghapk-<os>-<arch>` plus `bin/sha256sums.txt`.

## 4. Installing from a checkout

The project uses local module replacements, so install from the checkout
rather than using a remote `go install ...@latest` command:

```bash
cd ghapk
go install .
```

Add `$(go env GOPATH)/bin` to your PATH. You can remove the checkout after
installation. The installed binary still needs an authenticated `gh` CLI.

## 5. On Termux specifically

```bash
pkg install golang gh
cd ghapk
go build -trimpath -ldflags="-s -w" -o $PREFIX/bin/ghapk .
gh auth login
ghapk
```

You can also take the `ghapk-android-arm64` binary you built on a PC
(`make termux`), push it to the phone and drop it into
`$PREFIX/bin/`:

```bash
adb push bin/ghapk-android-arm64 /sdcard/ghapk
# on the phone, in Termux:
cp /sdcard/ghapk $PREFIX/bin/ghapk && chmod +x $PREFIX/bin/ghapk
```

> Termux note: binaries built `GOOS=android` run fine in Termux. If you
> build `GOOS=linux GOARCH=arm64` instead (e.g. for proot-distro), that
> binary needs `termux-elf-cleaner` only when the Go linker outputs an
> ELF with the old section flags — current Go versions are clean; if in
> doubt prefer the `android` target.

## 6. Distribution as a GitHub release

`make release` builds six binaries and their `sha256sums.txt`. Set `VERSION`
when building a different version, for example `make release VERSION=4.3.0`.

The release workflow runs for `v*` tags or by manual dispatch on the desired target ref. Add handwritten
notes at `docs/releases/<tag>.md` before tagging. It builds the same six targets,
packages the checked-out source, and includes its SHA-256 in `sha256sums.txt`.
If a release for that tag already exists, the workflow leaves it and its assets
untouched by default. To upload to an existing draft, use manual dispatch with
`upload_draft` enabled; the draft must target the exact commit being built.
Published releases are never overwritten. For a manual
release, build from the exact committed target, add a source archive and its
checksum, then use `gh release create` with `--target` and `--notes-file`.
Review the tag, notes, assets, and checksums before publishing.

## 7. What "single executable" cannot remove

One honest caveat: ghapk talks to GitHub **through the `gh` CLI**
(authentication, the API, artifact downloads) — exactly like the
original script did. So the runtime requirement on any machine is:

1. `gh` installed (`pkg install gh` / `apt install gh` / `brew install gh`),
2. `gh auth login` done once.

Everything else — TUI, scoring, unzipping, device probing — lives inside
the one `ghapk` file.
