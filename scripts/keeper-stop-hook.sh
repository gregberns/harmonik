#!/usr/bin/env bash
# keeper-stop-hook.sh — Claude Code Stop hook that touches
# .harmonik/keeper/<agent>.idle to signal an await-input boundary.
#
# The Stop hook fires only at await-input boundaries (verified by Anthropic).
# The watcher uses this marker as the crisp idle signal for Phase-2 gating:
# .idle newer than .ctx means the agent stopped AFTER its last context update.
#
# Usage
#   Add to ~/.claude/settings.json hooks:
#     "Stop": [
#       {
#         "hooks": [
#           {
#             "type": "command",
#             "command": "HARMONIK_PROJECT=/path/to/project /path/to/scripts/keeper-stop-hook.sh"
#           }
#         ]
#       }
#     ]
#   Agent name is derived at runtime from $HARMONIK_AGENT (set by the daemon
#   or crew launch), then $HARMONIK_KEEPER_AGENT (backward-compat alias), then
#   the tmux session name, then "default".
#
# Environment
#   HARMONIK_PROJECT        Absolute path to the project root (fallback: $PWD).
#   HARMONIK_AGENT          Agent name to namespace the .idle file (primary; fallback: "default").
#   HARMONIK_KEEPER_AGENT   Backward-compat alias for HARMONIK_AGENT (checked second).
#                           Can also be passed as $1.
#
# Output
#   Touches (creates or updates mtime of):
#     $HARMONIK_PROJECT/.harmonik/keeper/$AGENT.idle
#
# Refs: hk-djdng (session-keeper Phase-2 foundation),
#       hk-p9kw (unify HARMONIK_AGENT / HARMONIK_KEEPER_AGENT).
set -euo pipefail

# Derive agent name: HARMONIK_AGENT → HARMONIK_KEEPER_AGENT (backward compat) →
# positional arg → tmux session name → "default".
# The tmux fallback means a single global hook entry in ~/.claude/settings.json
# works correctly for all concurrent agent sessions (hk-nm32w).
if [ -n "${HARMONIK_AGENT:-}" ]; then
    AGENT="${HARMONIK_AGENT}"
elif [ -n "${HARMONIK_KEEPER_AGENT:-}" ]; then
    AGENT="${HARMONIK_KEEPER_AGENT}"
elif [ -n "${1:-}" ]; then
    AGENT="${1}"
elif [ -n "${TMUX:-}" ]; then
    AGENT="$(tmux display-message -p '#S' 2>/dev/null || echo default)"
else
    AGENT="default"
fi
# Reject path-traversal / absolute-escape in the agent name: it is interpolated
# into a filesystem path, so a value containing a path separator or ".." could
# steer writes outside .harmonik/keeper. Fail closed on any such value.
case "${AGENT}" in
    */*|*..*) echo "keeper-stop-hook: refusing unsafe agent name: ${AGENT}" >&2; exit 1 ;;
esac
PROJECT="${HARMONIK_PROJECT:-${PWD}}"
KEEPER_DIR="${PROJECT}/.harmonik/keeper"
IDLE_FILE="${KEEPER_DIR}/${AGENT}.idle"

mkdir -p "${KEEPER_DIR}"
touch "${IDLE_FILE}"
