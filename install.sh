#!/usr/bin/env bash
# ghapk — One-line installer for Android (Termux), Linux, and macOS
# Repository: https://github.com/ThatOn3Gu7/ghapk

set -euo pipefail

REPO="ThatOn3Gu7/ghapk"
DEFAULT_TAG="v4.2.6"
INSTALL_NAME="ghapk"

# Colors
BOLD='\033[1m'
GREEN='\033[38;2;166;227;161m'
CYAN='\033[38;2;137;180;250m'
YELLOW='\033[38;2;249;226;175m'
RED='\033[38;2;243;139;168m'
DIM='\033[2m'
RESET='\033[0m'

info()  { printf "${CYAN}==>${RESET} ${BOLD}%s${RESET}\n" "$*"; }
ok()    { printf "${GREEN}==>${RESET} ${BOLD}%s${RESET}\n" "$*"; }
warn()  { printf "${YELLOW}==> WARNING:${RESET} %s\n" "$*"; }
err()   { printf "${RED}==> ERROR:${RESET} %s\n" "$*" >&2; exit 1; }

printf "\n"
printf "${CYAN}  ██████╗ ██╗  ██╗   █████╗ ██████╗ ██╗  ██╗${RESET}\n"
printf "${CYAN} ██╔════╝ ██║  ██║  ${GREEN}██╔══██╗██╔══██╗██║ ██╔╝${RESET}\n"
printf "${CYAN} ██║  ███╗███████║  ${GREEN}███████║██████╔╝█████╔╝ ${RESET}\n"
printf "${CYAN} ██║   ██║██╔══██║  ${GREEN}██╔══██║██╔═══╝ ██╔═██╗ ${RESET}\n"
printf "${CYAN} ╚██████╔╝██║  ██║  ${GREEN}██║  ██║██║     ██║  ██╗${RESET}\n"
printf "${CYAN}  ╚═════╝ ╚═╝  ╚═╝  ${GREEN}╚═╝  ╚═╝╚═╝     ╚═╝  ╚═╝${RESET}\n"
printf "${DIM}         GitHub Actions APK Manager Installer${RESET}\n\n"

# 1. Detect OS
OS=""
IS_TERMUX=0

if [ -n "${TERMUX_VERSION:-}" ] || [ -d "/data/data/com.termux" ] || [ "${PREFIX:-}" = "/data/data/com.termux/files/usr" ]; then
    OS="android"
    IS_TERMUX=1
else
    UNAME_S="$(uname -s 2>/dev/null || echo "Unknown")"
    case "$UNAME_S" in
        Linux*)   OS="linux" ;;
        Darwin*)  OS="darwin" ;;
        CYGWIN*|MINGW*|MSYS*) OS="windows" ;;
        *)        err "Unsupported operating system: $UNAME_S" ;;
    esac
fi

# 2. Detect Architecture
UNAME_M="$(uname -m 2>/dev/null || echo "Unknown")"
ARCH=""
case "$UNAME_M" in
    x86_64|amd64)   ARCH="amd64" ;;
    aarch64|arm64)  ARCH="arm64" ;;
    armv7*|armhf)   err "32-bit ARM is not officially supported. ghapk requires 64-bit (arm64 or amd64)." ;;
    *)              err "Unsupported architecture: $UNAME_M" ;;
esac

TARGET_BINARY="ghapk-${OS}-${ARCH}"
if [ "$OS" = "windows" ]; then
    TARGET_BINARY="ghapk-windows-amd64.exe"
    INSTALL_NAME="ghapk.exe"
fi

info "Detected platform: ${BOLD}${OS}/${ARCH}${RESET} (${TARGET_BINARY})"

# 3. Determine Installation Directory
BIN_DIR=""
if [ "$IS_TERMUX" -eq 1 ]; then
    BIN_DIR="${PREFIX:-/data/data/com.termux/files/usr}/bin"
elif [ -w "/usr/local/bin" ]; then
    BIN_DIR="/usr/local/bin"
elif [ -d "$HOME/.local/bin" ] || mkdir -p "$HOME/.local/bin" 2>/dev/null; then
    BIN_DIR="$HOME/.local/bin"
elif [ -d "$HOME/bin" ] || mkdir -p "$HOME/bin" 2>/dev/null; then
    BIN_DIR="$HOME/bin"
else
    BIN_DIR="/usr/local/bin"
fi

# 4. Resolve Version
TAG="${GHAPK_VERSION:-}"
if [ -z "$TAG" ]; then
    # Try fetching latest release from public GitHub API
    LATEST_JSON="$(curl -fsSL -H "Accept: application/vnd.github+json" "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null || true)"
    if [ -n "$LATEST_JSON" ]; then
        TAG="$(printf '%s' "$LATEST_JSON" | grep -m1 '"tag_name":' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/' || true)"
    fi
    # If API call failed (e.g. private repo or offline), use fallback default
    if [ -z "$TAG" ]; then
        TAG="$DEFAULT_TAG"
    fi
fi

info "Target version: ${BOLD}${TAG}${RESET}"

# 5. Download Binary
TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'ghapk')"
trap 'rm -rf "$TMP_DIR"' EXIT
TMP_FILE="$TMP_DIR/$TARGET_BINARY"

DOWNLOAD_SUCCESS=0

# Method A: Use authenticated GitHub CLI if available (supports private repos seamlessly)
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
    info "Attempting download via authenticated GitHub CLI..."
    if gh release download "$TAG" -R "$REPO" --pattern "$TARGET_BINARY" --dir "$TMP_DIR" >/dev/null 2>&1; then
        DOWNLOAD_SUCCESS=1
    fi
fi

# Method B: Direct curl download from GitHub releases
if [ "$DOWNLOAD_SUCCESS" -eq 0 ]; then
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${TAG}/${TARGET_BINARY}"
    info "Downloading from ${DOWNLOAD_URL}..."
    
    CURL_AUTH=()
    if [ -n "${GITHUB_TOKEN:-}" ]; then
        CURL_AUTH=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
    elif [ -n "${GH_TOKEN:-}" ]; then
        CURL_AUTH=(-H "Authorization: Bearer ${GH_TOKEN}")
    fi

    if curl -fL "${CURL_AUTH[@]}" -o "$TMP_FILE" "$DOWNLOAD_URL" 2>/dev/null; then
        DOWNLOAD_SUCCESS=1
    fi
fi

if [ "$DOWNLOAD_SUCCESS" -eq 0 ]; then
    err "Failed to download ${TARGET_BINARY} for version ${TAG}.\n    If the repository is private, ensure 'gh auth login' is active or pass GITHUB_TOKEN=... to curl."
fi

# 6. Install to Destination
mkdir -p "$BIN_DIR" 2>/dev/null || true
DEST_PATH="$BIN_DIR/$INSTALL_NAME"

if [ -w "$BIN_DIR" ]; then
    mv -f "$TMP_FILE" "$DEST_PATH"
else
    info "Elevated permissions required to install to ${BIN_DIR}..."
    if command -v sudo >/dev/null 2>&1; then
        sudo mv -f "$TMP_FILE" "$DEST_PATH"
    else
        err "Cannot write to ${BIN_DIR} and sudo is not available."
    fi
fi

chmod +x "$DEST_PATH"
ok "Installed ${BOLD}${INSTALL_NAME}${RESET} to ${BOLD}${DEST_PATH}${RESET}"

# 7. PATH Check
case ":$PATH:" in
    *":$BIN_DIR:"*) ;;
    *)
        warn "${BIN_DIR} is not in your \$PATH."
        printf "    Add it by running:\n"
        printf "    ${CYAN}export PATH=\"%s:\$PATH\"${RESET}\n" "$BIN_DIR"
        printf "    (Add this line to your ~/.bashrc or ~/.zshrc)\n\n"
        ;;
esac

# 8. GitHub CLI Prerequisite Check
if ! command -v gh >/dev/null 2>&1; then
    warn "The GitHub CLI ('gh') is required for ghapk to fetch runs and artifacts."
    if [ "$IS_TERMUX" -eq 1 ]; then
        printf "    Install it via: ${CYAN}pkg install gh && gh auth login${RESET}\n\n"
    elif [ "$OS" = "darwin" ]; then
        printf "    Install it via: ${CYAN}brew install gh && gh auth login${RESET}\n\n"
    else
        printf "    Install it via: https://cli.github.com/ and run ${CYAN}gh auth login${RESET}\n\n"
    fi
else
    if ! gh auth status >/dev/null 2>&1; then
        warn "GitHub CLI ('gh') is installed but not authenticated."
        printf "    Run: ${CYAN}gh auth login${RESET}\n\n"
    else
        ok "GitHub CLI ('gh') is installed and authenticated."
    fi
fi

ok "Installation complete! Run ${CYAN}ghapk${RESET} to start."
