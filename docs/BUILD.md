# Building ghapk as a single executable

Go compiles everything — the app *and* all its TUI dependencies — into
one static binary. There is no interpreter, no runtime folder, no
dependency directory. One file is the whole tool.

## Zero. Prerequisites

- Go ≥ 1.22 installed (https://go.dev/dl). Nothing else — no CGO, no NDK,
  no extra system libraries.
- Optional: [UPX](https://upx.github.io) if you want the binary smaller.

## 1. The plain build

```bash
cd ghapk
go build -o ghapk .
```

That's it: `./ghapk` is a self-contained executable (~8–9 MB) that runs
with no other files. Move it anywhere, rename it, put it on your PATH:

```bash
mv ghapk ~/.local/bin/        # or /usr/local/bin, or ~/bin on Termux
```

## 2. A stripped, smaller single file

Two linker flags remove the symbol table and debug info; trimming the
build path makes it reproducible:

```bash
go build -trimpath -ldflags="-s -w" -o ghapk .
```

Optional shrink on top of that with UPX (typically −60 %):

```bash
upx --best --lzma ghapk
```

Result: a ~2–3 MB single file.

## 3. Cross-compiling — the binary must match the machine

A binary only runs on the OS/architecture it was built for, so build one
per target. Go makes this trivial and needs **no Android NDK** (pure Go,
CGO off by default for cross builds):

```bash
# your phone running Termux (arm64)
GOOS=android GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o ghapk-android-arm64 .

# 32-bit ARM Linux (Raspberry Pi etc.)
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

## 4. Installing from source without a folder

You don't even need to keep the source checkout: `go install` compiles
and drops the finished binary into `$(go env GOPATH)/bin`:

```bash
go install github.com/ghapk/ghapk@latest     # once published
# or from a local checkout:
cd ghapk && go install .
```

Add `~/go/bin` (or `$PREFIX/bin` on Termux, which `go` already uses) to
your PATH and `ghapk` is just a command.

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

## 6. Distribution as a GitHub release (optional)

Very meta, but fitting: tag a commit and attach the `bin/` files:

```bash
git tag v4.0.0 && git push --tags
gh release create v4.0.0 bin/* --title "ghapk v4.0.0"
```

Users then download one file for their platform and run it. No
packaging system required anywhere in the pipeline.

## 7. What "single executable" cannot remove

One honest caveat: ghapk talks to GitHub **through the `gh` CLI**
(authentication, the API, artifact downloads) — exactly like the
original script did. So the runtime requirement on any machine is:

1. `gh` installed (`pkg install gh` / `apt install gh` / `brew install gh`),
2. `gh auth login` done once.

Everything else — TUI, scoring, unzipping, device probing — lives inside
the one `ghapk` file.
