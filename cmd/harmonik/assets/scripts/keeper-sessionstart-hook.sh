#!/usr/bin/env bash
# keeper-sessionstart-hook.sh — Claude Code SessionStart hook that writes the
# live session_id to the single-writer <agent>.sid channel (hk-8prq).
#
# SessionStart fires at session boot (source=startup), after a /clear
# (source=clear), and on resume (source=resume), each carrying the CURRENT
# session_id on stdin. By recording it to <agent>.sid — a channel the daemon
# NEVER writes — the keeper gets an unambiguous source of the interactive
# session's identity, free of the multi-writer races that plague the gauge
# (.ctx is written by the statusline on every UI repaint and can lag or be
# clobbered by a transiently-dispatched daemon session). ReadCtxFile reads .sid
# as PRIMARY identity, falling back to the gauge only when .sid is absent or
# malformed.
#
# SAFETY: this hook only ever WRITES the channel; it never blocks the session
# (always exits 0). A missing/invalid session_id or agent name is a silent
# no-op so an unmanaged or oddly-configured session is never disrupted.
#
# Usage — add to ~/.claude/settings.json hooks section:
#   "SessionStart": [
#     {
#       "hooks": [
#         {
#           "type": "command",
#           "command": "HARMONIK_PROJECT=/path/to/project /path/to/scripts/keeper-sessionstart-hook.sh"
#         }
#       ]
#     }
#   ]
#
# Environment:
#   HARMONIK_PROJECT        Absolute path to the project root (fallback: $PWD).
#   HARMONIK_AGENT          Agent name (primary).
#   HARMONIK_KEEPER_AGENT   Backward-compat alias (checked second).
#
# Files written:
#   $HARMONIK_PROJECT/.harmonik/keeper/$AGENT.sid — lowercased session_id + "\n".
#
# Exit codes:
#   0 — always (never blocks the session).
#
# Refs: hk-8prq (single-writer .sid session-id channel).
set -euo pipefail

# Read the SessionStart payload from stdin and extract session_id. jq absence is
# a silent no-op (the keeper falls back to the gauge id).
PAYLOAD="$(cat)"
if ! command -v jq >/dev/null 2>&1; then
    exit 0
fi
SID="$(printf '%s' "${PAYLOAD}" | jq -r '.session_id // empty' 2>/dev/null || true)"
[ -n "${SID}" ] || exit 0

# Derive agent name: HARMONIK_AGENT → HARMONIK_KEEPER_AGENT → tmux session.
if [ -n "${HARMONIK_AGENT:-}" ]; then
    AGENT="${HARMONIK_AGENT}"
elif [ -n "${HARMONIK_KEEPER_AGENT:-}" ]; then
    AGENT="${HARMONIK_KEEPER_AGENT}"
elif [ -n "${TMUX:-}" ]; then
    AGENT="$(tmux display-message -p '#S' 2>/dev/null || echo default)"
else
    exit 0
fi
# Reject path-traversal agent names (silent no-op).
case "${AGENT}" in
    */*|*..*) exit 0 ;;
esac

PROJECT="${HARMONIK_PROJECT:-${PWD}}"
KEEPER_DIR="${PROJECT}/.harmonik/keeper"
SID_FILE="${KEEPER_DIR}/${AGENT}.sid"

mkdir -p "${KEEPER_DIR}"
# Lowercase-normalise so the keeper's identity comparisons are stable and the
# uppercase conversation/transcript-dir UUID is never bound. Single writer: a
# plain truncate-write is race-free (the daemon never touches this channel).
printf '%s\n' "$(printf '%s' "${SID}" | tr '[:upper:]' '[:lower:]')" > "${SID_FILE}"

exit 0
