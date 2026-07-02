#!/usr/bin/env sh
set -eu

DEFAULT_REGISTRY_URL="https://raw.githubusercontent.com/thanhtung2693/specgate/main"
REGISTRY_URL="${REGISTRY_URL:-$DEFAULT_REGISTRY_URL}"
AGENT=""
DRY_RUN=0
INSTALL_DIR=""
PROJECT_LOCAL=0
SKIP_CLI=0

usage() {
  echo "Usage: install.sh [--registry URL] [--agent cursor|codex|claude|all|cursor,codex|cursor,claude|codex,claude] [--install-dir PATH] [--project-local] [--skip-cli] [--dry-run]" >&2
  echo "Run with no --agent to choose one or more IDEs interactively." >&2
  echo "By default, IDE files are installed globally under your home directory." >&2
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --registry) REGISTRY_URL="$2"; shift 2 ;;
    --agent) AGENT="$2"; shift 2 ;;
    --install-dir) INSTALL_DIR="$2"; shift 2 ;;
    --project-local) PROJECT_LOCAL=1; shift ;;
    --skip-cli) SKIP_CLI=1; shift ;;
    --dry-run|--print) DRY_RUN=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

choose_agents() {
  if [ -n "$AGENT" ]; then
    return 0
  fi
  if [ ! -r /dev/tty ]; then
    echo "No --agent given and no interactive terminal available." >&2
    usage
    exit 2
  fi
  {
    echo "Step 1/3 Choose IDE setup"
    echo "Which IDEs should SpecGate set up?"
    echo "  1) Cursor"
    echo "  2) Codex"
    echo "  3) Claude Code"
    echo "  4) All of them"
    echo "  Tip: enter comma-separated values such as 1,2"
    printf "Select [1-4, comma-separated]: "
  } > /dev/tty
  read -r choice < /dev/tty
  case "$choice" in
    1) AGENT="cursor" ;;
    2) AGENT="codex" ;;
    3) AGENT="claude" ;;
    4) AGENT="all" ;;
    *,*)
      normalized=""
      old_ifs=${IFS}
      IFS=','
      set -- $choice
      IFS=${old_ifs}
      for item in "$@"; do
        case "$(echo "$item" | tr -d ' ')" in
          1) agent="cursor" ;;
          2) agent="codex" ;;
          3) agent="claude" ;;
          4) AGENT="all"; normalized="all"; break ;;
          *) echo "Invalid selection: $choice" >&2; exit 2 ;;
        esac
        case ",$normalized," in
          *,"$agent",*) ;;
          *) [ -n "$normalized" ] && normalized="${normalized},${agent}" || normalized="$agent" ;;
        esac
      done
      [ -n "$AGENT" ] || AGENT="$normalized"
      ;;
    *) echo "Invalid selection: $choice" >&2; exit 2 ;;
  esac
}

normalize_agents() {
  input="$1"
  normalized=""
  old_ifs=${IFS}
  IFS=','
  set -- $input
  IFS=${old_ifs}
  for item in "$@"; do
    target=$(printf "%s" "$item" | tr -d '[:space:]')
    case "$target" in
      all) normalized="all"; break ;;
      cursor|codex|claude) ;;
      "") echo "Invalid empty --agent entry in: $input" >&2; exit 2 ;;
      *) echo "Unsupported agent: $target" >&2; usage; exit 2 ;;
    esac
    case ",$normalized," in
      *,"$target",*) ;;
      *) [ -n "$normalized" ] && normalized="${normalized},${target}" || normalized="$target" ;;
    esac
  done
  [ -n "$normalized" ] || { echo "No supported agents selected." >&2; usage; exit 2; }
  printf "%s\n" "$normalized"
}

install_cli() {
  if [ "$REGISTRY_URL" = "$DEFAULT_REGISTRY_URL" ]; then
    cli_url="$REGISTRY_URL/scripts/install-cli.sh"
  else
    cli_url="$REGISTRY_URL/cli/install.sh"
  fi
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] install CLI from $cli_url"
    return 0
  fi
  echo "Step 2/3 Install SpecGate CLI"
  tmpfile=$(mktemp "${TMPDIR:-/tmp}/specgate-cli-wrapper.XXXXXX")
  trap 'rm -f "$tmpfile"' EXIT INT TERM HUP
  curl -fsSL "$cli_url" -o "$tmpfile"
  set --
  if [ -n "$INSTALL_DIR" ]; then
    set -- "$@" --install-dir "$INSTALL_DIR"
  fi
  if [ "$REGISTRY_URL" != "$DEFAULT_REGISTRY_URL" ]; then
    set -- "$@" --server "$REGISTRY_URL"
  fi
  sh "$tmpfile" "$@"
  rm -f "$tmpfile"
  trap - EXIT INT TERM HUP
}

find_specgate_bin() {
  if [ -n "$INSTALL_DIR" ] && [ -x "$INSTALL_DIR/specgate" ]; then
    printf "%s\n" "$INSTALL_DIR/specgate"
    return 0
  fi
  if [ -n "${HOME:-}" ] && [ -x "$HOME/.local/bin/specgate" ]; then
    printf "%s\n" "$HOME/.local/bin/specgate"
    return 0
  fi
  if [ -x "/usr/local/bin/specgate" ]; then
    printf "%s\n" "/usr/local/bin/specgate"
    return 0
  fi
  if command -v specgate >/dev/null 2>&1; then
    command -v specgate
    return 0
  fi
  printf "%s\n" "specgate"
}

install_plugins() {
  specgate_bin=$(find_specgate_bin)
  set -- plugins install --agent "$AGENT" --registry "$REGISTRY_URL"
  if [ "$PROJECT_LOCAL" -eq 1 ]; then
    set -- "$@" --project-local
  fi
  if [ "$DRY_RUN" -eq 1 ]; then
    set -- "$@" --dry-run
    echo "[dry-run] $specgate_bin $*"
    return 0
  fi
  echo "Step 3/3 Write IDE files"
  "$specgate_bin" "$@"
  echo ""
  echo "SpecGate IDE setup complete for: $AGENT"
  echo "Restart the selected IDE(s) so new skills, hooks, and rules are loaded."
  echo "Verify with: specgate plugins doctor --agent $AGENT"
}

choose_agents
AGENT=$(normalize_agents "$AGENT")
if [ "$DRY_RUN" -ne 1 ]; then
  echo "Step 1/3 Choose IDE setup"
  echo "  Selected: $AGENT"
fi
if [ "$SKIP_CLI" -eq 1 ]; then
  if [ "$DRY_RUN" -ne 1 ]; then
    echo "Step 2/3 Install SpecGate CLI"
    echo "  Skipped (--skip-cli)"
  fi
else
  install_cli
fi
install_plugins
