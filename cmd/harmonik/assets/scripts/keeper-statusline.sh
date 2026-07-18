#!/usr/bin/env bash
# keeper-statusline.sh — reads Claude Code statusLine JSON from stdin,
# extracts .context_window.used_percentage, .context_window.total_input_tokens,
# .context_window_size, and .session_id, and atomically writes
# .harmonik/keeper/<agent>.ctx = {"pct":<N>,"tokens":<N>,"window_size":<N>,"session_id":<S>,"ts":<RFC3339>}.
#
# The field path is .context_window.used_percentage (verified empirically).
# It reads NA right after a /clear, in which case the write is skipped.
#
# tokens and window_size default to 0 when absent (older Claude Code versions
# that do not emit these fields). The keeper watcher falls back to pct-based
# gating when tokens == 0 || window_size == 0.
#
# Usage
#   Called automatically as the statusLine.command in ~/.claude/settings.json.
#   The recommended way to wire this is via `harmonik keeper enable`:
#     harmonik keeper enable <agent> --project /path/to/project --scripts /path/to/scripts
#   This writes a HARMONIK_AGENT-free command — agent name is derived from the
#   tmux session name at runtime so ALL concurrent sessions share one entry (hk-nm32w).
#
#   If you wire it manually, do NOT hardcode HARMONIK_AGENT=<name> in the command:
#   that overrides the inherited env var for EVERY concurrent Claude Code session,
#   causing all sessions to write the same .ctx file (ctx pollution, hk-67k).
#   Rely on the tmux-name fallback instead, or ensure each agent's process
#   environment already has HARMONIK_AGENT=<name> set — which `harmonik crew start`
#   does automatically via HARMONIK_AGENT=<name> in the crew env.
#
# Environment
#   HARMONIK_PROJECT   Absolute path to the project root (fallback: $PWD).
#   HARMONIK_AGENT     Agent name to namespace the .ctx file (fallback: "default").
#   HARMONIK_KEEPER_AGENT  Backward-compat alias for HARMONIK_AGENT (checked second),
#                      matching the stop/precompact/sessionstart hooks.
#   HARMONIK_KEEPER_1M_EFFECTIVE_FRACTION
#                      Effective fraction of the nominal 1M context window for [1m]
#                      models (default: 0.5, i.e. ~500k tokens). window_size is set to
#                      floor(1000000 * fraction) and pct is recomputed as tokens/window
#                      so the keeper's pct guard fires at the correct fill level instead
#                      of the nominal-1M under-reported value (hk-d8dj0).
#
# Output
#   Atomically writes (via a rename-to-final) to:
#     $HARMONIK_PROJECT/.harmonik/keeper/$HARMONIK_AGENT.ctx
#   The file contains a single JSON line:
#     {"pct":<float>,"tokens":<int>,"window_size":<int>,"session_id":<string>,"ts":"<RFC3339>"}
#
# Refs: hk-8vzek (session-keeper Phase-1), hk-cl74g (absolute-token gate fix),
#       hk-67c (infer window_size from model id for [1m] models),
#       hk-whd (robust .model extraction — handles nested {id,display_name} object form).
set -euo pipefail

# Derive agent name: HARMONIK_AGENT → HARMONIK_KEEPER_AGENT (backward compat) →
# tmux session name → "default". The tmux fallback means a single global
# statusLine entry in ~/.claude/settings.json works correctly for all concurrent
# agent sessions; each session writes to its own .ctx file keyed by the tmux
# session name (hk-nm32w).
if [ -n "${HARMONIK_AGENT:-}" ]; then
    AGENT="${HARMONIK_AGENT}"
elif [ -n "${HARMONIK_KEEPER_AGENT:-}" ]; then
    AGENT="${HARMONIK_KEEPER_AGENT}"
elif [ -n "${TMUX:-}" ]; then
    AGENT="$(tmux display-message -p '#S' 2>/dev/null || echo default)"
else
    AGENT="default"
fi
# Reject path-traversal / absolute-escape in the agent name: it is interpolated
# into a filesystem path, so a value containing a path separator or ".." could
# steer writes outside .harmonik/keeper. Fail closed on any such value.
case "${AGENT}" in
    */*|*..*) echo "keeper-statusline: refusing unsafe agent name: ${AGENT}" >&2; exit 1 ;;
esac
PROJECT="${HARMONIK_PROJECT:-${PWD}}"
# HARMONIK_KEEPER_WINDOW_SIZE: optional explicit override for window_size when
# Claude Code omits context_window_size (e.g. [1m] models). Must be a positive integer.
KEEPER_WINDOW_SIZE_OVERRIDE="${HARMONIK_KEEPER_WINDOW_SIZE:-0}"
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
# Normalise to lowercase: Claude Code may emit the conversation/transcript-dir
# UUID (uppercase UUIDv4) as session_id instead of the actual session UUID.
# Lowercasing prevents the uppercase guard in the keeper watcher from rejecting
# a legitimately-latched SID and also prevents poisoning .managed with a
# conversation UUID (hk-mzdm).
SESSION_ID="$(printf '%s' "${SESSION_ID}" | tr '[:upper:]' '[:lower:]')"
TS="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"

# Extract absolute token counts — default to 0 when absent (older Claude Code).
TOKENS="$(printf '%s' "${INPUT}" | jq -r '.context_window.total_input_tokens // 0' 2>/dev/null || echo '0')"
# context_window_size appears at the top level in some Claude Code versions and nested under
# .context_window in others (per the documented schema). Try both paths; the first non-zero wins.
WINDOW_SIZE="$(printf '%s' "${INPUT}" | jq -r '(.context_window_size // .context_window.context_window_size // 0)' 2>/dev/null || echo '0')"

# Sanitise: replace non-integer values with 0.
if ! printf '%s' "${TOKENS}" | grep -qE '^[0-9]+$'; then TOKENS=0; fi
if ! printf '%s' "${WINDOW_SIZE}" | grep -qE '^[0-9]+$'; then WINDOW_SIZE=0; fi

# Infer window_size when Claude Code omits context_window_size (e.g. [1m] models).
# Priority: explicit env override → model-id detection → leave at 0 (pct-only fallback).
if [ "${WINDOW_SIZE}" -eq 0 ]; then
    if [ "${KEEPER_WINDOW_SIZE_OVERRIDE}" -gt 0 ] 2>/dev/null; then
        WINDOW_SIZE="${KEEPER_WINDOW_SIZE_OVERRIDE}"
    else
        # Claude Code may emit .model as a flat string ("claude-opus-4-8 [1m]") or as a
        # nested object ({id, display_name}). Build a single candidate string from all
        # available paths so the [1m] check works regardless of format.
        MODEL_STR="$(printf '%s' "${INPUT}" | jq -r '
            if .model == null then ""
            elif (.model | type) == "string" then .model
            else ((.model.id // "") + " " + (.model.display_name // ""))
            end' 2>/dev/null || true)"
        if printf '%s' "${MODEL_STR}" | grep -qF '[1m]'; then
            # Compute effective window as a fraction of the nominal 1M (hk-d8dj0).
            # The keeper's pct guard (cf.Pct < WarnPct) is a NECESSARY condition for
            # warn/act to fire. Claude Code reports pct = tokens/1M, so a session at
            # 372k tokens reads as 37% — permanently below the 80% WarnPct — even
            # though tokens (372k) already exceed the abs warn threshold (200k).
            # Expressing the window as floor(1M * fraction) lets operators tune the
            # effective size via env without a code change.
            _1M_FRACTION="${HARMONIK_KEEPER_1M_EFFECTIVE_FRACTION:-0.5}"
            WINDOW_SIZE="$(awk "BEGIN {printf \"%d\n\", 1000000 * ${_1M_FRACTION}}")"
            # Recompute pct relative to the effective window so the pct guard fires at
            # the correct fill level. Skip when TOKENS==0 (older Claude Code that omits
            # total_input_tokens) or when WINDOW_SIZE==0 (awk produced 0 for a zero
            # fraction — avoids division by zero and leaves pct at the payload value).
            if [ "${TOKENS}" -gt 0 ] && [ "${WINDOW_SIZE}" -gt 0 ]; then
                PCT="$(jq -rn "${TOKENS} / ${WINDOW_SIZE} * 100 | [., 100] | min")"
            fi
        fi
    fi
fi

# Encode session_id as a JSON string (handles empty and special chars).
SESSION_ID_JSON="$(printf '%s' "${SESSION_ID}" | jq -Rc . 2>/dev/null || printf '""')"

mkdir -p "${CTX_DIR}"
printf '{"pct":%s,"tokens":%s,"window_size":%s,"session_id":%s,"ts":"%s"}\n' \
    "${PCT}" \
    "${TOKENS}" \
    "${WINDOW_SIZE}" \
    "${SESSION_ID_JSON}" \
    "${TS}" > "${TMP_FILE}"
mv "${TMP_FILE}" "${CTX_FILE}"
