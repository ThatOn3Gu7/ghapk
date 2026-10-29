#!/usr/bin/env bash
# ghapk — One-line universal installer for Android (Termux), Linux, and macOS
# Repository: https://github.com/ThatOn3Gu7/ghapk

set -euo pipefail

REPO="ThatOn3Gu7/ghapk"
DEFAULT_TAG="v4.3.0"
INSTALL_NAME="ghapk"

# Determine terminal color support
if [ -t 1 ] || [ "${FORCE_COLOR:-0}" = "1" ]; then
    ESC=$(printf '\033')
    RESET="${ESC}[0m"
    BOLD="${ESC}[1m"
    DIM="${ESC}[2m"
    GREEN="${ESC}[1;32m"
    CYAN="${ESC}[1;36m"
    YELLOW="${ESC}[1;33m"
    RED="${ESC}[1;31m"
else
    RESET=""
    BOLD=""
    DIM=""
    GREEN=""
    CYAN=""
    YELLOW=""
    RED=""
fi

info()  { printf "%s %s\n" "${CYAN}==>${RESET}" "$*"; }
ok()    { printf "%s %s\n" "${GREEN}==>${RESET}" "$*"; }
warn()  { printf "%s %s\n" "${YELLOW}==> WARNING:${RESET}" "$*"; }
err()   { printf "%s %s\n" "${RED}==> ERROR:${RESET}" "$*" >&2; exit 1; }

# Print Banner
printf "\n"
printf "%s  ██████╗ ██╗  ██╗   █████╗ ██████╗ ██╗  ██╗%s\n" "${CYAN}" "${RESET}"
printf "%s ██╔════╝ ██║  ██║  %s██╔══██╗██╔══██╗██║ ██╔╝%s\n" "${CYAN}" "${GREEN}" "${RESET}"
printf "%s ██║  ███╗███████║  %s███████║██████╔╝█████╔╝ %s\n" "${CYAN}" "${GREEN}" "${RESET}"
printf "%s ██║   ██║██╔══██║  %s██╔══██║██╔═══╝ ██╔═██╗ %s\n" "${CYAN}" "${GREEN}" "${RESET}"
printf "%s ╚██████╔╝██║  ██║  %s██║  ██║██║     ██║  ██╗%s\n" "${CYAN}" "${GREEN}" "${RESET}"
printf "%s  ╚═════╝ ╚═╝  ╚═╝  %s╚═╝  ╚═╝╚═╝     ╚═╝  ╚═╝%s\n" "${CYAN}" "${GREEN}" "${RESET}"
printf "%s         GitHub Actions APK Manager Installer%s\n\n" "${DIM}" "${RESET}"

# 1. Detect Operating System
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

# 4. Resolve Target Version
TAG="${GHAPK_VERSION:-}"
if [ -z "$TAG" ]; then
    # Try fetching latest release from GitHub API
    LATEST_JSON="$(curl -fsSL -H "Accept: application/vnd.github+json" "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null || true)"
    if [ -n "$LATEST_JSON" ]; then
        TAG="$(printf '%s' "$LATEST_JSON" | grep -m1 '"tag_name":' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/' || true)"
    fi
    # Fallback to default tag if API was unreachable or rate-limited
    if [ -z "$TAG" ]; then
        TAG="$DEFAULT_TAG"
    fi
fi

info "Target version: ${BOLD}${TAG}${RESET}"

# 5. Download Binary & Checksums
TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'ghapk')"
trap 'rm -rf "$TMP_DIR"' EXIT
TMP_FILE="$TMP_DIR/$TARGET_BINARY"

DOWNLOAD_SUCCESS=0

# Method A: Use authenticated GitHub CLI if available (supports private repos seamlessly)
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
    info "Attempting download via authenticated GitHub CLI..."
    if gh release download "$TAG" -R "$REPO" --pattern "$TARGET_BINARY" --dir "$TMP_DIR" >/dev/null 2>&1; then
        DOWNLOAD_SUCCESS=1
        # Try fetching checksums as well
        gh release download "$TAG" -R "$REPO" --pattern "sha256sums.txt" --dir "$TMP_DIR" >/dev/null 2>&1 || true
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
        # Try fetching checksums as well
        curl -fsSL "${CURL_AUTH[@]}" -o "$TMP_DIR/sha256sums.txt" "https://github.com/${REPO}/releases/download/${TAG}/sha256sums.txt" 2>/dev/null || true
    fi
fi

if [ "$DOWNLOAD_SUCCESS" -eq 0 ]; then
    err "Failed to download ${TARGET_BINARY} for version ${TAG}.\n    Ensure internet connectivity or run 'gh auth login' if accessing a private repo."
fi

# Verify SHA256 Checksum if available
if [ -f "$TMP_DIR/sha256sums.txt" ] && grep -q "$TARGET_BINARY" "$TMP_DIR/sha256sums.txt" 2>/dev/null; then
    EXPECTED_SHA="$(grep "$TARGET_BINARY" "$TMP_DIR/sha256sums.txt" | awk '{print $1}')"
    ACTUAL_SHA=""
    if command -v sha256sum >/dev/null 2>&1; then
        ACTUAL_SHA="$(sha256sum "$TMP_FILE" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        ACTUAL_SHA="$(shasum -a 256 "$TMP_FILE" | awk '{print $1}')"
    fi

    if [ -n "$ACTUAL_SHA" ] && [ -n "$EXPECTED_SHA" ]; then
        if [ "$ACTUAL_SHA" = "$EXPECTED_SHA" ]; then
            ok "SHA256 checksum verified successfully."
        else
            err "Checksum verification failed!\n    Expected: $EXPECTED_SHA\n    Actual:   $ACTUAL_SHA"
        fi
    fi
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
        printf "    %sexport PATH=\"%s:\$PATH\"%s\n" "$CYAN" "$BIN_DIR" "$RESET"
        printf "    (Add this line to your ~/.bashrc or ~/.zshrc)\n\n"
        ;;
esac

# 8. GitHub CLI Prerequisite Check
if ! command -v gh >/dev/null 2>&1; then
    warn "The GitHub CLI ('gh') is required for ghapk to fetch runs and artifacts."
    if [ "$IS_TERMUX" -eq 1 ]; then
        printf "    Install it via: %spkg install gh && gh auth login%s\n\n" "$CYAN" "$RESET"
    elif [ "$OS" = "darwin" ]; then
        printf "    Install it via: %sbrew install gh && gh auth login%s\n\n" "$CYAN" "$RESET"
    else
        printf "    Install it via: https://cli.github.com/ and run %sgh auth login%s\n\n" "$CYAN" "$RESET"
    fi
else
    if ! gh auth status >/dev/null 2>&1; then
        warn "GitHub CLI ('gh') is installed but not authenticated."
        printf "    Run: %sgh auth login%s\n\n" "$CYAN" "$RESET"
    else
        ok "GitHub CLI ('gh') is installed and authenticated."
    fi
fi

ok "Installation complete! Run ${CYAN}ghapk${RESET} to start."
