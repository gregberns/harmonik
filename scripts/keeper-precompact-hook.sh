#!/usr/bin/env bash
# keeper-precompact-hook.sh — Claude Code PreCompact hook that blocks lossy
# native auto-compaction on managed agents and signals the keeper watcher to
# run an intent-preserving handoff→clear→resume cycle instead.
#
# The PreCompact hook fires synchronously inside the claude process before
# auto-compaction and CAN block it by exiting 2 (decision:block). This is the
# BACKSTOP for when the gauge-threshold cycle (keeper watcher) did not fire in
# time — e.g., one huge turn jumps from below act_pct straight into compaction.
#
# SAFETY: only managed agents are blocked. Unmanaged sessions ALWAYS fall
# through to native compaction (fail-open). This is identical to the .managed
# guard on hk-djdng (keeper-stop-hook.sh) and hk-22i70 (cycle core).
#
# BOUNDED FALLBACK (can't-cycle policy):
#   The hook runs synchronously inside claude; the keeper watcher is an async
#   poller. If the hook blocks compaction (exit 2) but the keeper then cannot
#   cycle (HoldingDispatch, anti-loop, or watcher not running), the session
#   would be stuck with compaction permanently blocked.
#
#   Resolution: block at most once per compaction wave using the .precompact
#   marker file. On the first PreCompact fire the hook writes the marker and
#   exits 2 (block). On any subsequent PreCompact fire while the marker still
#   exists, the hook exits 0 (fail-open) — allowing native compaction. The
#   keeper watcher clears the marker after dispatching the cycle; if the
#   watcher can't cycle it never clears the marker, so the NEXT PreCompact
#   fire falls through to native compaction automatically.
#
# MECHANISM:
#   1. Not managed → exit 0 (fail-open; never block unmanaged sessions).
#   2. Marker <agent>.precompact exists → exit 0 (fail-open; already blocked
#      once; keeper either cycled or cannot cycle — allow native compaction).
#   3. Marker absent → write <agent>.precompact, exit 2 (decision:block).
#      The keeper watcher detects the marker on its next poll tick, runs the
#      cycle, emits session_keeper_precompact_blocked, and clears the marker.
#
# Usage — add to ~/.claude/settings.json hooks section:
#   "PreCompact": [
#     {
#       "hooks": [
#         {
#           "type": "command",
#           "command": "HARMONIK_PROJECT=/path/to/project HARMONIK_KEEPER_AGENT=orchestrator /path/to/scripts/keeper-precompact-hook.sh"
#         }
#       ]
#     }
#   ]
#
# Environment:
#   HARMONIK_PROJECT        Absolute path to the project root (fallback: $PWD).
#   HARMONIK_KEEPER_AGENT   Agent name (fallback: "default"; also accepted as $1).
#
# Files written:
#   $HARMONIK_PROJECT/.harmonik/keeper/$AGENT.precompact — trigger marker
#     (RFC3339 timestamp as content; cleared by the keeper watcher after cycle)
#
# Exit codes:
#   0 — fail-open: allow native compaction
#   2 — decision:block: suppress native compaction; keeper will run cycle
#
# Refs: hk-aalsm (session-keeper Phase-2 PreCompact backstop).
set -euo pipefail

AGENT="${HARMONIK_KEEPER_AGENT:-${1:-default}}"
# Reject path-traversal agent names (fail-open = allow native compaction).
case "$AGENT" in
    */*|*..*) exit 0 ;;
esac
PROJECT="${HARMONIK_PROJECT:-${PWD}}"
KEEPER_DIR="${PROJECT}/.harmonik/keeper"
MANAGED_FILE="${KEEPER_DIR}/${AGENT}.managed"
PRECOMPACT_FILE="${KEEPER_DIR}/${AGENT}.precompact"

# Gate 1: fail-open on unmanaged sessions. NEVER block compaction unless the
# agent has explicitly opted in via the .managed marker.
if [ ! -f "${MANAGED_FILE}" ]; then
    exit 0
fi

# Gate 2: bounded fallback. If the marker already exists we already blocked
# once. The keeper either ran the cycle (clearing the marker) or cannot cycle
# (HoldingDispatch / watcher not running). Either way, allow native compaction
# this time so the session is never permanently wedged.
if [ -f "${PRECOMPACT_FILE}" ]; then
    exit 0
fi

# Gate 3: write the precompact trigger marker and block this compaction. The
# keeper watcher will detect the marker on its next poll tick and run the
# intent-preserving cycle, then clear the marker.
mkdir -p "${KEEPER_DIR}"
date -u +"%Y-%m-%dT%H:%M:%SZ" > "${PRECOMPACT_FILE}"

# Exit 2 = decision:block: suppress native (lossy) compaction.
exit 2
