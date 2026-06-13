#!/usr/bin/env bash
# captain-launch.sh — launch the harmonik Captain LLM session with a STABLE,
# caller-minted --session-id, then arm the session-keeper against it.
#
# WHY the --session-id matters (restart-continuity, load-bearing):
#   The captain is a `claude --remote-control` session, exactly like a crew. The
#   session-keeper's in-process wind-down cycle (handoff → /clear → /session-resume,
#   internal/keeper/cycle.go) can only REBIND to the same conversation if the
#   session has a STABLE id to `--resume`. Crews mint+reuse a uuid via
#   `harmonik crew start` (internal/daemon/crewstart.go resolveSessionID) — that is
#   exactly what lets a keeper-cycled crew come back as the same session. The
#   captain, historically launched as a bare `claude --remote-control captain` with
#   NO --session-id, had no stable id, so the keeper could only ever WARN it (the
#   non-destructive warn injection, internal/keeper/injector.go, whose text ends
#   "…then run /quit"); when the captain obeyed /quit it exited and nothing
#   respawned it → dead and stayed dead. Minting --session-id here mirrors the crew
#   model and gives the keeper a session to rebind.
#
# Usage:
#   captain-launch.sh [tmux-session-name]
#
# Required environment (no default — must be set by the caller):
#   HK_PROJECT — absolute path to the harmonik project directory.
#                Example: export HK_PROJECT=/path/to/harmonik
#
# Optional overrides:
#   CAP_TMUX   — first positional arg, or $CAP_TMUX, or "captain"
#   CAP_NAME   — $CAP_NAME, or "captain"  (the --remote-control / comms identity)
#   CAP_WARN   — $CAP_WARN, or 30   (keeper --warn-pct; bare 80/90 defeats intent on a 1M window)
#   CAP_ACT    — $CAP_ACT,  or 35   (keeper --act-pct)

set -euo pipefail

# HK_PROJECT is required — no hardcoded default so this script is portable across
# machines and users. Set it in your shell profile or before invoking this script.
: "${HK_PROJECT:?HK_PROJECT must be set to the harmonik project directory (e.g. export HK_PROJECT=/path/to/harmonik)}"

CAP_TMUX="${1:-${CAP_TMUX:-captain}}"
CAP_NAME="${CAP_NAME:-captain}"
CAP_WARN="${CAP_WARN:-30}"
CAP_ACT="${CAP_ACT:-35}"

# Mint a stable session id ONCE; the keeper's clear→resume cycle re-binds to it.
SID="$(uuidgen)"

echo "captain-launch: name=$CAP_NAME tmux=$CAP_TMUX session_id=$SID warn=$CAP_WARN act=$CAP_ACT project=$HK_PROJECT"

# 1) Launch the captain in its own tmux session with the MINTED --session-id.
#    Interactive, remote-controllable (operator can watch at claude.ai/code).
#    HARMONIK_AGENT is set explicitly so keeper-statusline.sh uses the correct
#    name without relying on the tmux-name fallback or a hardcoded default in
#    ~/.claude/settings.json (hk-67k: ctx-pollution fix follow-up).
tmux new-session -d -s "$CAP_TMUX" -e "HARMONIK_AGENT=$CAP_NAME" \
  "claude --dangerously-skip-permissions --remote-control \"$CAP_NAME\" --session-id \"$SID\""

# 2) Arm the session-keeper AFTER the captain is up. The keeper drives the
#    in-process handoff→/clear→/session-resume cycle against this tmux session.
#    --warn-pct/--act-pct must be passed explicitly (bare defaults are 80/90).
tmux new-session -d -s "hk-keeper-$CAP_NAME" \
  "harmonik keeper --agent \"$CAP_NAME\" --tmux \"$CAP_TMUX\" --warn-pct $CAP_WARN --act-pct $CAP_ACT"

echo "captain-launch: captain up in tmux '$CAP_TMUX' (session_id $SID); keeper armed in tmux 'hk-keeper-$CAP_NAME' (warn $CAP_WARN / act $CAP_ACT)."
echo "captain-launch: NOTE — a stable --session-id is what lets the keeper's clear→resume cycle survive (mirrors the crew model)."
