#!/usr/bin/env bash
# keeper-restart-verified.sh — fire a keeper restart and VERIFY the ACK landed.
#
# This is the EXTERNAL watcher for the SELF restart-now case (hk-uldg). On a self
# restart-now the firing agent gets `/clear`-wiped before its ACK lands, so the
# firing agent CANNOT wait for its own ACK — it is mid-suicide. This wrapper runs
# as a separate OS process: it fires `harmonik keeper restart-now`, parses the
# printed `nonce=rn-...`, then runs `harmonik keeper await-ack --kind restart` for
# the SAME agent. Because await-ack is a separate process, the agent's `/clear`
# does not kill it; it confirms the keeper actually delivered the ACK.
#
# Use this where the keeper/captain would fire a SELF restart instead of calling
# `harmonik keeper restart-now` bare — it turns an assumed restart into a VERIFIED
# one. For the captain-watches-CREW case, the captain runs `restart-now --agent
# <crew>` then `await-ack --agent <crew>` directly (it survives the crew's /clear);
# the captain skill documents that path.
#
# WINDOW-NESTING (hk-z036): this wrapper drives the restart purely by `--agent`.
# The inject/verify TARGET — the captain session's `agent` window
# ($CAP_TMUX:agent) — is bound into the keeper at launch via `--tmux
# <session>:agent` (captain-launch.sh step 4). A self restart-now respawns ONLY
# the `agent` window; the keeper window (and this verification path) survives.
#
# Usage:
#   ./scripts/captain-tools/keeper-restart-verified.sh <agent> [--project DIR] \
#       [--timeout 30s] [--poll 1s]
#
# Or via env:
#   HK_PROJECT=/path/to/project ./keeper-restart-verified.sh captain
#
# Exit codes:
#   0  — restart fired AND `[KEEPER ACK <nonce>] received restart` observed (alive)
#   1  — argument error, restart-now failed, or nonce could not be parsed
#   3  — restart fired but the ACK never landed within the timeout
#        (await-ack emitted session_keeper_ack_timeout to events.jsonl; the keeper
#        may be dead / watching the wrong pane / unable to verify the session id —
#        INVESTIGATE per docs/keeper-restart-now-ack-protocol.md §6, and the caller
#        should comms-alert the operator: `harmonik comms send --to operator \
#        --topic keeper-alert --from <lane> "keeper ACK timeout for <agent>"`).

set -uo pipefail

if [[ $# -lt 1 || "${1:-}" == -* ]]; then
  echo "keeper-restart-verified: usage: $0 <agent> [--project DIR] [--timeout 30s] [--poll 1s]" >&2
  exit 1
fi

AGENT="$1"
shift

PROJECT="${HK_PROJECT:-}"
# restart-now does the keeper's freshness checks + three injects around the ACK,
# so a slightly longer default than ping's 15s is reasonable (design 18 §2).
TIMEOUT="30s"
POLL="1s"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project) PROJECT="$2"; shift 2 ;;
    --timeout) TIMEOUT="$2"; shift 2 ;;
    --poll)    POLL="$2"; shift 2 ;;
    *) echo "keeper-restart-verified: unknown argument: $1" >&2; exit 1 ;;
  esac
done

PROJ_FLAGS=()
if [[ -n "$PROJECT" ]]; then
  PROJ_FLAGS=(--project "$PROJECT")
fi

# 1. Fire the restart. restart-now prints:
#    keeper restart-now: agent="<agent>" nonce=rn-<millis> restart driven (...)
if ! RESTART_OUT="$(harmonik keeper restart-now --agent "$AGENT" "${PROJ_FLAGS[@]}")"; then
  echo "$RESTART_OUT"
  echo "keeper-restart-verified: restart-now failed — not verifying" >&2
  exit 1
fi
echo "$RESTART_OUT"

# 2. Parse the nonce token (rn-<millis>) from the printed line.
NONCE="$(printf '%s\n' "$RESTART_OUT" | sed -n 's/.*nonce=\(rn-[0-9]*\).*/\1/p' | head -n1)"
if [[ -z "$NONCE" ]]; then
  echo "keeper-restart-verified: could not parse nonce=rn-... from restart-now output" >&2
  exit 1
fi
echo "keeper-restart-verified: parsed nonce=$NONCE; awaiting ACK (timeout=$TIMEOUT)"

# 3. Wait for the ACK in the SAME agent's pane. This process is external to the
#    firing agent, so the agent's /clear does not kill it. await-ack exits 3 +
#    emits session_keeper_ack_timeout if the ACK never lands.
harmonik keeper await-ack \
  --agent "$AGENT" \
  --nonce "$NONCE" \
  --kind restart \
  --timeout "$TIMEOUT" \
  --poll "$POLL" \
  "${PROJ_FLAGS[@]}"
AWAIT_RC=$?

if [[ $AWAIT_RC -ne 0 ]]; then
  echo "keeper-restart-verified: ACK NOT observed for $AGENT (nonce=$NONCE) — keeper unverified; INVESTIGATE." >&2
fi
exit $AWAIT_RC
