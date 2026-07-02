#!/usr/bin/env sh
set -eu

SRC="${SPECGATE_PLUGIN_SOURCE:-watch/plugins}"
DEST="${SPECGATE_EMBEDDED_PLUGIN_DEST:-internal/agentpackages/plugins}"
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

if [ ! -d "$SRC" ]; then
  exit 0
fi

sync_dir() {
  src="$1"
  dest="$2"
  if [ ! -d "$src" ]; then
    return 0
  fi
  if [ -d "$dest" ] && diff -qr "$src" "$dest" >/dev/null 2>&1; then
    return 0
  fi
  rm -rf "$dest"
  mkdir -p "$(dirname "$dest")"
  cp -R "$src" "$dest"
}

sync_file() {
  src="$1"
  dest="$2"
  if [ ! -f "$src" ]; then
    return 0
  fi
  if [ -f "$dest" ] && cmp -s "$src" "$dest"; then
    return 0
  fi
  mkdir -p "$(dirname "$dest")"
  cp "$src" "$dest"
}

sync_dir "$SRC/skills" "$DEST/skills"
sync_dir "$SRC/hooks" "$DEST/hooks"
sync_dir "$SRC/assets" "$DEST/assets"
sync_dir "$SRC/rules" "$DEST/rules"
sync_dir "$SRC/.agents" "$DEST/.agents"
sync_dir "$SRC/.codex-plugin" "$DEST/.codex-plugin"
sync_dir "$SRC/.claude-plugin" "$DEST/.claude-plugin"
sync_dir "$SRC/.cursor-plugin" "$DEST/.cursor-plugin"
sync_file "$SRC/package.json" "$DEST/package.json"

python3 "$SCRIPT_DIR/generate-plugin-metadata.py" --plugin-dir "$DEST"

rm -rf "$DEST/cursor" "$DEST/claude" "$DEST/codex"

chmod +x "$DEST/hooks/run-hook.cmd" "$DEST/hooks/session-start" 2>/dev/null || true
