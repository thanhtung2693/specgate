#!/usr/bin/env sh
# SpecGate CLI public installer.
#
# Usage:
#   sh scripts/install-cli.sh
#   sh scripts/install-cli.sh --version v1.2.3 --server https://my.specgate.example
#
# This source copy is embedded into the instance-aware `/cli/install.sh` route.
#
# Flags:
#   --version <tag>       CLI version to install (default: latest public release, including prereleases)
#   --install-dir <path>  Installation directory (default: current specgate dir, else ~/.local/bin, else /usr/local/bin)
#   --server <url>        SpecGate server URL to configure after install
#   --no-config           Skip post-install server configuration
set -e

SPECGATE_VERSION=""
INSTALL_DIR=""
SERVER_URL=""
NO_CONFIG=0
GITHUB_REPO="thanhtung2693/specgate"
BINARY_NAME="specgate"

# ---------------------------------------------------------------------------
# Parse flags
# ---------------------------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    --version)
      SPECGATE_VERSION="$2"
      shift 2
      ;;
    --install-dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    --server)
      SERVER_URL="$2"
      shift 2
      ;;
    --no-config)
      NO_CONFIG=1
      shift
      ;;
    *)
      echo "Unknown flag: $1" >&2
      exit 1
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
if [ -z "$INSTALL_DIR" ]; then
  if command -v "$BINARY_NAME" >/dev/null 2>&1; then
    CURRENT_BIN="$(command -v "$BINARY_NAME" || true)"
    case "$CURRENT_BIN" in
      /*)
        CURRENT_DIR="$(dirname "$CURRENT_BIN")"
        if [ -w "$CURRENT_DIR" ]; then
          INSTALL_DIR="$CURRENT_DIR"
        fi
        ;;
    esac
  fi
fi

if [ -z "$INSTALL_DIR" ] && [ -n "${HOME:-}" ]; then
  USER_BIN="${HOME}/.local/bin"
  mkdir -p "$USER_BIN" 2>/dev/null || true
  if [ -w "$USER_BIN" ]; then
    INSTALL_DIR="$USER_BIN"
  fi
fi

if [ -z "$INSTALL_DIR" ]; then
  INSTALL_DIR="/usr/local/bin"
fi

# ---------------------------------------------------------------------------
# Detect OS and architecture
# ---------------------------------------------------------------------------
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin)  OS_NAME="darwin" ;;
  Linux)   OS_NAME="linux" ;;
  CYGWIN*|MINGW*|MSYS*) OS_NAME="windows" ;;
  *)
    echo "Unsupported OS: $OS" >&2
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH_NAME="amd64" ;;
  arm64|aarch64) ARCH_NAME="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

# ---------------------------------------------------------------------------
# Resolve version
# ---------------------------------------------------------------------------
if [ -z "$SPECGATE_VERSION" ]; then
  echo "Step 1/5 Resolve version"
  echo "  Resolving latest SpecGate CLI version..."
  SPECGATE_VERSION="$(curl -fsSL "https://api.github.com/repos/${GITHUB_REPO}/releases" \
    | grep '"tag_name"' | sed 's/.*"tag_name": "\(.*\)".*/\1/' | head -1)"
  if [ -z "$SPECGATE_VERSION" ]; then
    echo "Failed to resolve latest version" >&2
    exit 1
  fi
else
  echo "Step 1/5 Resolve version"
  echo "  Using ${SPECGATE_VERSION}"
fi

echo "Step 2/5 Prepare install target"
echo "  Installing SpecGate CLI ${SPECGATE_VERSION} (${OS_NAME}/${ARCH_NAME}) to ${INSTALL_DIR}"

# ---------------------------------------------------------------------------
# Download archive and checksums
# ---------------------------------------------------------------------------
BASE_URL="https://github.com/${GITHUB_REPO}/releases/download/${SPECGATE_VERSION}"
ARCHIVE_NAME="specgate_${SPECGATE_VERSION#v}_${OS_NAME}_${ARCH_NAME}"

if [ "$OS_NAME" = "windows" ]; then
  ARCHIVE_FILE="${ARCHIVE_NAME}.zip"
else
  ARCHIVE_FILE="${ARCHIVE_NAME}.tar.gz"
fi

CHECKSUMS_FILE="specgate_${SPECGATE_VERSION#v}_checksums.txt"
TMPDIR="$(mktemp -d)"
# shellcheck disable=SC2064
trap "rm -rf '$TMPDIR'" EXIT

echo "Step 3/5 Download release"
curl -fsSL "${BASE_URL}/${CHECKSUMS_FILE}" -o "${TMPDIR}/${CHECKSUMS_FILE}"
curl -fsSL "${BASE_URL}/${ARCHIVE_FILE}" -o "${TMPDIR}/${ARCHIVE_FILE}"

# ---------------------------------------------------------------------------
# Verify SHA-256 checksum
# ---------------------------------------------------------------------------
echo "Step 4/5 Verify package"
echo "  Verifying checksum..."
cd "$TMPDIR"
if command -v sha256sum >/dev/null 2>&1; then
  SHA_CMD="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  SHA_CMD="shasum -a 256"
else
  echo "Neither sha256sum nor shasum found; skipping verification" >&2
  SHA_CMD=""
fi

if [ -n "$SHA_CMD" ]; then
  EXPECTED="$(grep " ${ARCHIVE_FILE}$" "${CHECKSUMS_FILE}" | awk '{print $1}')"
  if [ -z "$EXPECTED" ]; then
    echo "Checksum entry for ${ARCHIVE_FILE} not found" >&2
    exit 1
  fi
  ACTUAL="$(${SHA_CMD} "${ARCHIVE_FILE}" | awk '{print $1}')"
  if [ "$EXPECTED" != "$ACTUAL" ]; then
    echo "Checksum mismatch: expected ${EXPECTED}, got ${ACTUAL}" >&2
    exit 1
  fi
  echo "Checksum verified."
fi
cd - >/dev/null

# ---------------------------------------------------------------------------
# Extract and install atomically
# ---------------------------------------------------------------------------
echo "Step 5/5 Install binary"
if [ "$OS_NAME" = "windows" ]; then
  unzip -q "${TMPDIR}/${ARCHIVE_FILE}" -d "$TMPDIR"
  BINARY="${TMPDIR}/${BINARY_NAME}.exe"
else
  tar -xzf "${TMPDIR}/${ARCHIVE_FILE}" -C "$TMPDIR"
  BINARY="${TMPDIR}/${BINARY_NAME}"
fi

chmod +x "$BINARY"

# Atomic install: write to a temp file beside the target, then rename.
mkdir -p "$INSTALL_DIR"
DEST="${INSTALL_DIR}/${BINARY_NAME}"
if [ "$OS_NAME" = "windows" ]; then
  DEST="${INSTALL_DIR}/${BINARY_NAME}.exe"
fi
TMP_DEST="${DEST}.tmp.$$"
cp "$BINARY" "$TMP_DEST"
mv "$TMP_DEST" "$DEST"

echo "Installed: ${DEST}"

# ---------------------------------------------------------------------------
# Post-install configuration
# ---------------------------------------------------------------------------
if [ "$NO_CONFIG" -eq 0 ] && [ -n "$SERVER_URL" ]; then
  echo "Configuring server: ${SERVER_URL}"
  "${DEST}" config set server "${SERVER_URL}"
  "${DEST}" doctor
fi

echo ""
echo "SpecGate CLI ${SPECGATE_VERSION} installed successfully."
if [ "$NO_CONFIG" -eq 1 ] || [ -z "$SERVER_URL" ]; then
  echo "Run 'specgate config set server <url>' to configure your SpecGate instance."
fi
