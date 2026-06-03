#!/usr/bin/env bash
# keeper-statusline.sh — reads Claude Code statusLine JSON from stdin,
# extracts .context_window.used_percentage and .session_id, and atomically
# writes .harmonik/keeper/<agent>.ctx = {"pct":<N>,"session_id":<S>,"ts":<RFC3339>}.
#
# The field path is .context_window.used_percentage (verified empirically).
# It reads NA right after a /clear, in which case the write is skipped.
#
# Usage
#   Called automatically as the statusLine.command in ~/.claude/settings.json.
#   Add to your settings.json:
#     "statusLine": {
#       "command": "HARMONIK_PROJECT=/path/to/project HARMONIK_AGENT=orchestrator /path/to/scripts/keeper-statusline.sh"
#     }
#
# Environment
#   HARMONIK_PROJECT   Absolute path to the project root (fallback: $PWD).
#   HARMONIK_AGENT     Agent name to namespace the .ctx file (fallback: "default").
#
# Output
#   Atomically writes (via a rename-to-final) to:
#     $HARMONIK_PROJECT/.harmonik/keeper/$HARMONIK_AGENT.ctx
#   The file contains a single JSON line:
#     {"pct":<float>,"session_id":<string>,"ts":"<RFC3339>"}
#
# Refs: hk-8vzek (session-keeper Phase-1).
set -euo pipefail

AGENT="${HARMONIK_AGENT:-default}"
PROJECT="${HARMONIK_PROJECT:-${PWD}}"
CTX_DIR="${PROJECT}/.harmonik/keeper"
CTX_FILE="${CTX_DIR}/${AGENT}.ctx"
TMP_FILE="${CTX_FILE}.tmp.$$"

# Read entire stdin once.
INPUT="$(cat)"

# Extract the percentage — may be absent or "NA" right after /clear.
PCT="$(printf '%s' "${INPUT}" | jq -r '.context_window.used_percentage // empty' 2>/dev/null || true)"

# Skip write when the field is absent or non-numeric (e.g. "NA").
if [ -z "${PCT}" ] || ! printf '%s' "${PCT}" | grep -qE '^[0-9]+(\.[0-9]+)?$'; then
    exit 0
fi

SESSION_ID="$(printf '%s' "${INPUT}" | jq -r '.session_id // ""' 2>/dev/null || true)"
TS="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

# Encode session_id as a JSON string (handles empty and special chars).
SESSION_ID_JSON="$(printf '%s' "${SESSION_ID}" | jq -Rc . 2>/dev/null || printf '""')"

mkdir -p "${CTX_DIR}"
printf '{"pct":%s,"session_id":%s,"ts":"%s"}\n' \
    "${PCT}" \
    "${SESSION_ID_JSON}" \
    "${TS}" > "${TMP_FILE}"
mv "${TMP_FILE}" "${CTX_FILE}"
